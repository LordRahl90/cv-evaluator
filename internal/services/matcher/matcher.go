package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/llm/prompts"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/telemetry"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// ErrNoCV is returned when there is no CV to match against.
var ErrNoCV = errors.New("no CV to match against")

const (
	// maxCVChars caps the CV text sent to the LLM. Real CVs are far
	// shorter; this only guards against pathological uploads.
	maxCVChars = 20000
	// maxRequirements caps the requirements kept from a response.
	maxRequirements = 20
	// maxListItems caps strengths and missing skills, as the prompt asks.
	maxListItems = 5
	// mustHaveCap is the highest overall score a CV can get when it misses
	// a must-have requirement. It matches the score scale in the prompt.
	mustHaveCap = 69
)

type JobMatchLLMService interface {
	JobMatch(ctx context.Context, matchInput *entities.MatchInput) (string, error)
}

type Service struct {
	db          *gorm.DB
	jobMatchLLM JobMatchLLMService
}

// New returns a matcher that evaluates CVs with jobMatchLLM. It needs no
// embeddings: the LLM reads the full CV text.
func New(db *gorm.DB, jobMatchLLM JobMatchLLMService) *Service {
	return &Service{
		db:          db,
		jobMatchLLM: jobMatchLLM,
	}
}

// MatchByJobID matches the user's CV sections against a stored job, looked
// up by the provider's job id. Jobs are global, so the lookup is not scoped
// to the user; if several providers share the id, the oldest job wins.
func (s *Service) MatchByJobID(ctx context.Context, userID ulid.ULID, jobID string) (_ *Response, err error) {
	ctx, end := telemetry.Start(ctx, "matcher.Service.MatchByJobID", telemetry.ID(telemetry.AttrUserID, userID))
	defer end(&err)
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
func (s *Service) MatchJob(ctx context.Context, cvID ulid.ULID, job *models.Job) (_ *Response, _ string, err error) {
	ctx, end := telemetry.Start(ctx, "matcher.Service.MatchJob", telemetry.ID(telemetry.AttrCVID, cvID), telemetry.ID(telemetry.AttrJobID, job.ID))
	defer end(&err)
	slog.DebugContext(ctx, "matching CV with stored job", "cv_id", cvID, "job_id", job.ID)
	return s.match(ctx, sectionScope{cvID: cvID}, job)
}

func (s *Service) MatchByJobDescription(ctx context.Context, userID ulid.ULID, jobDescription string) (_ *Response, err error) {
	ctx, end := telemetry.Start(ctx, "matcher.Service.MatchByJobDescription", telemetry.ID(telemetry.AttrUserID, userID))
	defer end(&err)
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

	return s.evaluate(ctx, cvText, job)
}

// evaluate asks the LLM to evaluate cvText against job and decodes the
// answer. It returns the raw LLM output too, also on a decode failure.
//
// The full CV goes to the LLM: a CV has only a handful of sections, so
// retrieving the closest ones barely filtered anything and could drop
// education or the title.
func (s *Service) evaluate(ctx context.Context, cvText string, job *models.Job) (_ *Response, _ string, err error) {
	// The LLM call and decoding its answer are where match errors originate.
	ctx, end := telemetry.StartOrigin(ctx, "matcher.Service.evaluate")
	defer end(&err)
	matchInput := &entities.MatchInput{
		JobDescription: prompts.StripTags(job.Detail),
		JobLocation:    prompts.StripTags(job.Location),
		CV:             prompts.StripTags(cvText),
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
	telemetry.SetAttributes(ctx, telemetry.Count("criteria.count", len(response.Breakdown)))
	return response, raw, nil
}

// cvText returns the CV's full extracted text: the given CV's for a CV
// scope, otherwise the user's newest. It returns ErrNoCV when there is none.
func (s *Service) cvText(ctx context.Context, scope sectionScope) (_ string, err error) {
	ctx, end := telemetry.Start(ctx, "matcher.Service.cvText")
	defer end(&err)
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

	response.Strengths = cleanList(response.Strengths)
	response.MissingSkills = cleanList(response.MissingSkills)
	response.Summary = strings.TrimSpace(response.Summary)
	response.Requirements = cleanRequirements(response.Requirements)
	for _, r := range response.Requirements {
		if r.Importance == entities.RequirementMust && r.Met == entities.RequirementNotMet {
			response.OverallScore = min(response.OverallScore, mustHaveCap)
			break
		}
	}
	return response, nil
}

// DecodeRequirements returns the requirements in a stored raw LLM response,
// cleaned as decodeResponse cleans them. It returns nil when raw is not a
// JSON object or has no requirements, as responses from before requirements
// were asked for do not.
func DecodeRequirements(raw string) []Requirement {
	var response struct {
		Requirements []Requirement `json:"requirements"`
	}
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&response); err != nil {
		return nil
	}
	if reqs := cleanRequirements(response.Requirements); len(reqs) > 0 {
		return reqs
	}
	return nil
}

// cleanList trims the items, drops empty ones and keeps at most
// maxListItems.
func cleanList(in []string) []string {
	out := make([]string, 0, min(len(in), maxListItems))
	for _, item := range in {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		out = append(out, item)
		if len(out) == maxListItems {
			break
		}
	}
	return out
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
