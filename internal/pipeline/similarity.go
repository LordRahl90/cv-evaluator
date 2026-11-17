package pipeline

import (
	"context"
	"fmt"
	"log/slog"

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
