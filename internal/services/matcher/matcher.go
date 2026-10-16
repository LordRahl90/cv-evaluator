package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// ErrNoCV is returned when there is no CV to match against.
var ErrNoCV = errors.New("no CV to match against")

const (
	// maxCVChars caps the CV text sent to the LLM. Real CVs are far
	// shorter; this only guards against pathological uploads.
	maxCVChars = 20000
	// maxRequirements caps the requirements kept from a response.
	maxRequirements = 20
	// mustHaveCap is the highest overall score a CV can get when it misses
	// a must-have requirement. It matches the score scale in the prompt.
	mustHaveCap = 69
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
	cvText, err := s.cvText(ctx, scope)
	if err != nil {
		return nil, "", err
	}

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

	// The full CV goes to the LLM: a CV has only a handful of sections, so
	// retrieving the closest ones barely filters anything and can drop
	// education or the title. Retrieval still feeds the similarity hints.
	matchInput := &entities.MatchInput{
		JobDescription: stripPromptTags(job.Detail),
		JobLocation:    stripPromptTags(job.Location),
		CV:             stripPromptTags(cvText),
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

// cvText returns the CV's full extracted text: the given CV's for a CV
// scope, otherwise the user's newest. It returns ErrNoCV when there is none.
func (s *Service) cvText(ctx context.Context, scope sectionScope) (string, error) {
	query := s.db.WithContext(ctx).Model(&models.CV{})
	if scope.cvID != (ulid.ULID{}) {
		query = query.Where("id = ?", scope.cvID)
	} else {
		query = query.Where("user_id = ?", scope.userID).Order("created_at DESC")
	}

	var contents []string
	if err := query.Limit(1).Pluck("extracted_content", &contents).Error; err != nil {
		return "", fmt.Errorf("load CV: %w", err)
	}
	if len(contents) == 0 || strings.TrimSpace(contents[0]) == "" {
		return "", ErrNoCV
	}
	text := strings.TrimSpace(contents[0])
	if r := []rune(text); len(r) > maxCVChars {
		text = string(r[:maxCVChars])
	}
	return text, nil
}

// promptTags are the tags the prompt wraps untrusted text in.
var promptTags = strings.NewReplacer(
	"</job_description>", "", "<job_description>", "",
	"</cv>", "", "<cv>", "",
	"</JOB_DESCRIPTION>", "", "<JOB_DESCRIPTION>", "",
	"</CV>", "", "<CV>", "",
)

// stripPromptTags removes the prompt's own tags from scraped or uploaded
// text, so a job posting or CV can't close its tag early and smuggle text
// into the instructions.
func stripPromptTags(text string) string {
	return promptTags.Replace(text)
}

// decodeResponse parses the LLM's JSON answer. The decode error is wrapped
// so callers can see why the response was rejected. Only known criteria are
// kept, each once (the first wins), with scores clamped to 0-100. Only
// well-formed requirements are kept, and a missing must-have caps the
// overall score at mustHaveCap.
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

	response.Requirements = cleanRequirements(response.Requirements)
	for _, r := range response.Requirements {
		if r.Importance == entities.RequirementMust && r.Met == entities.RequirementNotMet {
			response.OverallScore = min(response.OverallScore, mustHaveCap)
			break
		}
	}
	return response, nil
}

// cleanRequirements normalises the LLM's requirements and drops any whose
// importance or met value is not one of the allowed values.
func cleanRequirements(in []Requirement) []Requirement {
	out := make([]Requirement, 0, len(in))
	for _, r := range in {
		r.Requirement = strings.TrimSpace(r.Requirement)
		r.Importance = strings.ToLower(strings.TrimSpace(r.Importance))
		r.Met = strings.ToLower(strings.TrimSpace(r.Met))
		r.Evidence = strings.TrimSpace(r.Evidence)
		if r.Requirement == "" {
			continue
		}
		switch r.Importance {
		case entities.RequirementMust, entities.RequirementNice:
		default:
			continue
		}
		switch r.Met {
		case entities.RequirementMet, entities.RequirementPartial, entities.RequirementNotMet:
		default:
			continue
		}
		out = append(out, r)
		if len(out) == maxRequirements {
			break
		}
	}
	return out
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
