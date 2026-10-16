package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

var (
	breakDown = map[string]float64{
		"summary":    0.1,
		"education":  0.15,
		"experience": 0.25,
		"skills":     0.4,
		"title":      0.1,
	}
)

type EmbeddingLLMService interface {
	CreateEmbedding(ctx context.Context, content string) ([]float32, error)
}

type JobMatchLLMService interface {
	JobMatch(ctx context.Context, matchInput *entities.MatchInput) (string, error)
}

type Service struct {
	db           *gorm.DB
	embeddingLLM EmbeddingLLMService
	jobMatchLLM  JobMatchLLMService
}

func New(db *gorm.DB, embeddingLLM EmbeddingLLMService, jobMatchLLM JobMatchLLMService) *Service {
	return &Service{
		db:           db,
		embeddingLLM: embeddingLLM,
		jobMatchLLM:  jobMatchLLM,
	}
}

// MatchByJobID matches the user's CV sections against a stored job, looked
// up by the provider's job id. Jobs are global, so the lookup is not scoped
// to the user; if several providers share the id, the oldest job wins.
func (s *Service) MatchByJobID(ctx context.Context, userID ulid.ULID, jobID string) (*Response, error) {
	slog.DebugContext(ctx, "matching user CV with job description", "user_id", userID, "job_id", jobID)

	var job models.Job
	if err := s.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("created_at ASC").
		First(&job).Error; err != nil {
		return nil, fmt.Errorf("failed to retrieve job: %w", err)
	}

	resp, _, err := s.match(ctx, sectionScope{userID: userID}, &job)
	return resp, err
}

// MatchJob evaluates one CV against a stored job. It reuses the job's stored
// embedding when there is one, so an embedded job is never embedded again.
// It returns the decoded response and the raw LLM output (also on a decode
// failure, so the caller can keep it for debugging).
func (s *Service) MatchJob(ctx context.Context, cvID ulid.ULID, job *models.Job) (*Response, string, error) {
	slog.DebugContext(ctx, "matching CV with stored job", "cv_id", cvID, "job_id", job.ID)
	return s.match(ctx, sectionScope{cvID: cvID}, job)
}

func (s *Service) MatchByJobDescription(ctx context.Context, userID ulid.ULID, jobDescription string) (*Response, error) {
	slog.DebugContext(ctx, "matching user CV with job description", "user_id", userID)

	resp, _, err := s.match(ctx, sectionScope{userID: userID}, &models.Job{Detail: jobDescription})
	return resp, err
}

// sectionScope selects the CV sections to match against: one CV's sections
// when cvID is set, otherwise all of the user's sections.
type sectionScope struct {
	userID ulid.ULID
	cvID   ulid.ULID
}

func (s *Service) match(ctx context.Context, scope sectionScope, job *models.Job) (*Response, string, error) {
	var queryEmbedding pgvector.Vector
	if job.IsEmbedded() {
		queryEmbedding = *job.Embedding
	} else {
		embedding, err := s.embeddingLLM.CreateEmbedding(ctx, job.Detail)
		if err != nil {
			return nil, "", err
		}
		queryEmbedding = pgvector.NewVector(embedding)
	}

	query := s.db.WithContext(ctx).
		Model(&models.SectionEmbedding{}).
		Select(
			"section_heading, section, embedding <=> ? AS distance, 1 - (embedding <=> ?) AS score",
			queryEmbedding, queryEmbedding,
		)
	if scope.cvID != (ulid.ULID{}) {
		query = query.Where("cv_id = ?", scope.cvID)
	} else {
		query = query.Where("user_id = ?", scope.userID)
	}

	var results []MatchResult
	if err := query.Order("distance ASC").Limit(10).Find(&results).Error; err != nil {
		return nil, "", err
	}

	header, err := s.cvHeader(ctx, scope)
	if err != nil {
		return nil, "", err
	}

	matchInput := &entities.MatchInput{
		JobDescription: job.Detail,
		JobLocation:    job.Location,
		CVHeader:       header,
		RelevantCV:     s.buildCVContext(ctx, results),
		Hints:          similarityHints(results),
		Criteria:       entities.EvaluationCriteria,
	}

	raw, err := s.jobMatchLLM.JobMatch(ctx, matchInput)
	if err != nil {
		return nil, "", err
	}

	response, err := decodeResponse(raw)
	if err != nil {
		return nil, raw, err
	}
	return response, raw, nil
}

// cvHeaderLength is how much of the start of the CV text goes into the
// prompt as the CV header.
const cvHeaderLength = 600

// cvHeader returns the start of the CV's text, where contact details such
// as the candidate's location usually are. For a user scope it uses the
// user's newest CV. It is empty when there is no CV.
func (s *Service) cvHeader(ctx context.Context, scope sectionScope) (string, error) {
	query := s.db.WithContext(ctx).Model(&models.CV{}).Select("extracted_content")
	if scope.cvID != (ulid.ULID{}) {
		query = query.Where("id = ?", scope.cvID)
	} else {
		query = query.Where("user_id = ?", scope.userID).Order("created_at DESC")
	}

	var contents []string
	if err := query.Limit(1).Pluck("extracted_content", &contents).Error; err != nil {
		return "", fmt.Errorf("load CV header: %w", err)
	}
	if len(contents) == 0 {
		return "", nil
	}
	header := strings.TrimSpace(contents[0])
	if r := []rune(header); len(r) > cvHeaderLength {
		header = string(r[:cvHeaderLength])
	}
	return header, nil
}

// decodeResponse parses the LLM's JSON answer. The decode error is wrapped
// so callers can see why the response was rejected. Only known criteria are
// kept, each once (the first wins), with scores clamped to 0-100.
func decodeResponse(raw string) (*Response, error) {
	var response *Response
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}
	if response == nil {
		return nil, errors.New("failed to decode LLM response: empty response")
	}

	response.OverallScore = clampScore(response.OverallScore)
	kept := make([]CriterionScore, 0, len(response.Breakdown))
	seen := map[string]bool{}
	for _, s := range response.Breakdown {
		s.Criterion = strings.ToLower(strings.TrimSpace(s.Criterion))
		if !entities.IsEvaluationCriterion(s.Criterion) || seen[s.Criterion] {
			continue
		}
		seen[s.Criterion] = true
		s.Score = clampScore(s.Score)
		s.Reason = strings.TrimSpace(s.Reason)
		kept = append(kept, s)
	}
	response.Breakdown = kept
	return response, nil
}

func clampScore(n int) int {
	return max(0, min(100, n))
}

// hintSections are the CV sections reported as similarity hints, with the
// label they are shown under. Labels say what was compared, so the LLM does
// not mistake e.g. the title similarity for a seniority assessment.
var hintSections = []struct{ heading, label string }{
	{"skills", "Skills section similarity"},
	{"experience", "Experience section similarity"},
	{"education", "Education section similarity"},
	{"summary", "Summary section similarity"},
	{"title", "Title similarity"},
}

// similarityHints turns the closest CV sections into labelled hints: the
// best similarity per section (0-100), then the weighted similarity across
// sections using the section weights.
func similarityHints(results []MatchResult) []entities.SimilarityHint {
	best := map[string]float64{}
	for _, r := range results {
		heading := strings.ToLower(r.SectionHeading)
		if s := float64(r.Score); s > best[heading] {
			best[heading] = s
		}
	}

	var (
		hints              []entities.SimilarityHint
		weighted, totalWts float64
	)
	for _, sec := range hintSections {
		score, ok := best[sec.heading]
		if !ok {
			continue
		}
		hints = append(hints, entities.SimilarityHint{Label: sec.label, Score: score * 100})
		weighted += score * breakDown[sec.heading]
		totalWts += breakDown[sec.heading]
	}
	if totalWts > 0 {
		hints = append(hints, entities.SimilarityHint{
			Label: "Weighted similarity across sections",
			Score: weighted / totalWts * 100,
		})
	}
	return hints
}

func (s *Service) buildCVContext(ctx context.Context, input []MatchResult) string {
	slog.DebugContext(ctx, "building CV context", "input", input)
	grouped := groupSections(input)

	sortByScore(grouped.Skills)
	sortByScore(grouped.Experience)
	sortByScore(grouped.Summary)
	sortByScore(grouped.Other)
	var b strings.Builder
	// SKILLS (top 5)
	b.WriteString("[SKILLS - MOST RELEVANT]\n")
	for i, m := range grouped.Skills {
		if i >= 5 {
			break
		}
		_, _ = fmt.Fprintf(&b, "- %s\n", m.Content)
	}
	// EXPERIENCE (top 5)
	b.WriteString("\n[EXPERIENCE - MOST RELEVANT]\n")
	for i, m := range grouped.Experience {
		if i >= 5 {
			break
		}
		_, _ = fmt.Fprintf(&b, "- %s\n", m.Content)
	}
	// SUMMARY (top 2)
	b.WriteString("\n[SUMMARY]\n")
	for i, m := range grouped.Summary {
		if i >= 2 {
			break
		}
		//_, _ = b.WriteString(fmt.Sprintf("- %s\n", m.Content))
		_, _ = fmt.Fprintf(&b, "- %s\n", m.Content)
	}
	// OPTIONAL: OTHER CONTEXT (small cap)
	if len(grouped.Other) > 0 {
		b.WriteString("\n[ADDITIONAL CONTEXT]\n")
		for i, m := range grouped.Other {
			if i >= 3 {
				break
			}
			_, _ = fmt.Fprintf(&b, "- %s\n", m.Content)
		}
	}
	return b.String()
}

func groupSections(matches []MatchResult) CVContext {
	ctx := CVContext{}
	for _, m := range matches {
		switch strings.ToLower(m.SectionHeading) {
		case "skills":
			ctx.Skills = append(ctx.Skills, m)
		case "experience":
			ctx.Experience = append(ctx.Experience, m)
		case "summary":
			ctx.Summary = append(ctx.Summary, m)
		default:
			ctx.Other = append(ctx.Other, m)
		}
	}
	return ctx
}

func sortByScore(matches []MatchResult) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}
