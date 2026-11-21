package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// similarityExpr is the cosine similarity between a CV (alias c) and a job
// (alias j): 1 - cosine distance, so 1 is identical and 0 unrelated. It is
// NULL when either embedding is missing. A zero vector has no direction and
// pgvector returns NaN for it, which NULLIF turns into NULL too ('NaN' =
// 'NaN' is true in Postgres).
const similarityExpr = `NULLIF(1 - (c.full_embedding <=> j.embedding), 'NaN'::float8)`

const similaritySQL = `
SELECT ` + similarityExpr + ` AS similarity
FROM cvs c, jobs j
WHERE c.id = @cv AND j.id = @job`

// similarity returns the cosine similarity between the CV's and the job's
// embeddings, or nil when either is missing or empty (or either row is
// gone).
func similarity(db *gorm.DB, cvID, jobID ulid.ULID) (*float64, error) {
	var rows []struct{ Similarity *float64 }
	if err := db.Raw(similaritySQL, map[string]any{"cv": cvID, "job": jobID}).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("similarity of cv %s and job %s: %w", cvID, jobID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].Similarity, nil
}

// recordSimilarity computes the similarity for an evaluation. It never
// fails the evaluation: an error is logged and reported as not computed.
func (p *Pipeline) recordSimilarity(ctx context.Context, cvID, jobID ulid.ULID) (sim *float64, ok bool) {
	sim, err := similarity(p.db.WithContext(ctx), cvID, jobID)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "similarity not computed", "cv_id", cvID.String(), "job_id", jobID.String(), "error", err)
		}
		return nil, false
	}
	return sim, true
}

// rankSQL brings the similarity of up to @batch results waiting for
// evaluation up to date with their user's newest CV: results never ranked,
// and results ranked against another CV (the user uploaded a new one). A
// held result ranked again goes back to pending, since a new CV may score
// it above the floor; EvaluateOne holds it again if not. Results of users
// with no CV, and results whose job is not embedded yet, are left alone.
//
// It picks the CV as LatestCVSelector does. Rows are locked with SKIP
// LOCKED, so it never waits for, or races with, a claim.
const rankSQL = `
UPDATE search_results sr
SET similarity = r.similarity, similarity_cv_id = r.cv_id, evaluation_status = @pending, updated_at = @now
FROM (
	SELECT sr.id, c.id AS cv_id, ` + similarityExpr + ` AS similarity
	FROM search_results sr
	JOIN jobs j ON j.id = sr.job_id AND j.deleted_at IS NULL AND j.status = @embedded
	JOIN LATERAL (
		SELECT id, full_embedding FROM cvs
		WHERE user_id = sr.user_id AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	) c ON true
	WHERE sr.evaluation_status IN (@pending, @held) AND sr.similarity_cv_id IS DISTINCT FROM c.id
	LIMIT @batch
	FOR UPDATE OF sr SKIP LOCKED
) r
WHERE sr.id = r.id`

// releaseSQL sends up to @batch held results back to pending when the
// floor no longer holds them: it was turned off or lowered below their
// similarity.
const releaseSQL = `
UPDATE search_results SET evaluation_status = @pending, updated_at = @now
WHERE id IN (
	SELECT id FROM search_results
	WHERE evaluation_status = @held AND (@floorOff OR similarity IS NULL OR similarity >= @floor)
	LIMIT @batch
	FOR UPDATE SKIP LOCKED
)`

// Rank updates the similarity of one batch of results waiting for
// evaluation (see rankSQL) and releases one batch of held results the
// floor no longer holds. It returns how many rows changed; a full batch
// means more may be waiting.
//
// Ranking is its own loop rather than part of EvaluateOne: one UPDATE
// covers a whole batch, it runs whether or not evaluation workers are busy
// (so a new CV re-orders the queue before the next claims), and the claim
// stays a short indexed lookup.
func (p *Pipeline) Rank(ctx context.Context) (int, error) {
	var changed int
	err := observe(ctx, p.cfg.Observer, KindRankResults, func(ctx context.Context) error {
		args := map[string]any{
			"pending":  models.EvaluationPending,
			"held":     models.EvaluationHeld,
			"embedded": models.JobStatusEmbedded,
			"now":      p.now(),
			"batch":    p.cfg.RankBatch,
			"floor":    p.cfg.SimilarityFloor,
			"floorOff": p.cfg.SimilarityFloor <= 0,
		}
		db := p.db.WithContext(ctx)
		ranked := db.Exec(rankSQL, args)
		if ranked.Error != nil {
			return fmt.Errorf("rank search results: %w", ranked.Error)
		}
		released := db.Exec(releaseSQL, args)
		if released.Error != nil {
			return fmt.Errorf("release held search results: %w", released.Error)
		}
		changed = int(ranked.RowsAffected + released.RowsAffected)
		if changed > 0 {
			slog.DebugContext(ctx, "search results ranked",
				"ranked", ranked.RowsAffected, "released", released.RowsAffected)
		}
		return nil
	})
	return changed, err
}
