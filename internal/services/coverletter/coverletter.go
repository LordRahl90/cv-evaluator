// Package coverletter writes a cover letter for a job from the user's CV
// and, when there is one, an evaluation of how well the CV fits the job.
//
// The LLM is asked for a formal letter, and every letter is checked on the
// server before it is returned (see Check): word count, no profanity, no
// clichés, no links, no contact details other than the user's own, no
// placeholders, no exclamation marks, a salutation and a sign-off. A letter
// that fails is sent back to the LLM once with the failed checks; if the
// second letter fails too, the request fails with ErrGenerationFailed.
// Letters are never stored.
package coverletter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/llm/prompts"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/services/users"
	"cv-evaluator/internal/telemetry"

	"github.com/oklog/ulid/v2"
)

var (
	// ErrNoCV is returned when the user has no CV to write the letter from.
	ErrNoCV = matcher.ErrNoCV
	// ErrGenerationFailed is returned when the LLM could not write a
	// letter that passes the checks, or could not be reached.
	ErrGenerationFailed = errors.New("cover letter generation failed")
)

const (
	// MaxJobDescriptionChars caps the job description sent to the LLM.
	MaxJobDescriptionChars = 20000
	// maxEvaluationItems and maxEvaluationText cap what is kept of a
	// client-supplied evaluation.
	maxEvaluationItems = 20
	maxEvaluationText  = 500
	maxSummaryText     = 1500
	// attempts is the number of letters asked for: the first one and one
	// revision.
	attempts = 2
)

// LLM writes the letter. It returns the raw JSON answer.
type LLM interface {
	CoverLetter(ctx context.Context, input *entities.CoverLetterInput) (string, error)
}

// CVs loads the user's newest CV text; *matcher.Service implements it.
type CVs interface {
	LatestCVText(ctx context.Context, userID ulid.ULID) (string, error)
}

// Profiles loads the user's profile; *users.Service implements it.
type Profiles interface {
	GetProfile(ctx context.Context, userID ulid.ULID) (*users.Profile, error)
}

// Letter is a written cover letter.
type Letter struct {
	Subject   string `json:"subject"`
	Letter    string `json:"letter"`
	WordCount int    `json:"word_count"`
}

// Candidate is who the letter is written for: the name signs it, and the
// email and phone are the only contact details it may contain.
type Candidate struct {
	Name  string
	Email string
	Phone string
}

// Request asks for a letter for the user's newest CV.
type Request struct {
	JobDescription string
	// Evaluation is an evaluation of the CV against the job, if the caller
	// has one. It is treated purely as data.
	Evaluation *matcher.Response
}

// Input is everything a letter is written from, for callers that load the
// CV themselves, such as a stored result with the CV it was evaluated
// against.
type Input struct {
	Candidate      Candidate
	CV             string
	JobDescription string
	Evaluation     *matcher.Response
}

// Service writes cover letters.
type Service struct {
	cvs      CVs
	profiles Profiles
	llm      LLM
}

// New returns a Service that reads the CV from cvs, the user's name and
// contact details from profiles, and has llm write the letter.
func New(cvs CVs, profiles Profiles, llm LLM) *Service {
	return &Service{cvs: cvs, profiles: profiles, llm: llm}
}

// Generate writes a letter for req.JobDescription from the user's newest CV,
// signed with the name on their profile. It returns ErrNoCV when the user
// has no CV.
func (s *Service) Generate(ctx context.Context, userID ulid.ULID, req Request) (*Letter, error) {
	ctx, span := telemetry.Start(ctx, "coverletter.Service.Generate", telemetry.ID(telemetry.AttrUserID, userID))
	defer span.End()
	cv, err := s.cvs.LatestCVText(ctx, userID)
	if err != nil {
		return nil, err
	}
	profile, err := s.profiles.GetProfile(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}

	return s.Write(ctx, Input{
		Candidate: Candidate{
			Name:  strings.TrimSpace(profile.FirstName + " " + profile.LastName),
			Email: profile.Email,
			Phone: profile.Phone,
		},
		CV:             cv,
		JobDescription: req.JobDescription,
		Evaluation:     req.Evaluation,
	})
}

// Write writes a letter from in. A letter that fails the checks is sent
// back once with the failed checks; ErrGenerationFailed is returned when
// the revision fails too.
func (s *Service) Write(ctx context.Context, in Input) (*Letter, error) {
	ctx, span := telemetry.Start(ctx, "coverletter.Service.Write")
	defer span.End()
	if strings.TrimSpace(in.CV) == "" {
		return nil, span.RecordError(ErrNoCV)
	}
	candidate := in.Candidate
	candidate.Name = singleLine(prompts.StripTags(candidate.Name))

	input := &entities.CoverLetterInput{
		JobDescription: truncate(strings.TrimSpace(prompts.StripTags(in.JobDescription)), MaxJobDescriptionChars),
		CV:             strings.TrimSpace(prompts.StripTags(in.CV)),
		Evaluation:     renderEvaluation(in.Evaluation),
		CandidateName:  candidate.Name,
	}

	var failed []string
	for attempt := 1; attempt <= attempts; attempt++ {
		raw, err := s.llm.CoverLetter(ctx, input)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("%w: %w", ErrGenerationFailed, err)
		}

		letter, violations := review(raw, candidate)
		if len(violations) == 0 {
			if attempt > 1 {
				slog.InfoContext(ctx, "cover letter passed the checks after a revision", "failed_checks", failed)
			}
			return letter, nil
		}

		// Log the names of the failed checks only: the messages may quote
		// the letter, and the letter is personal data.
		failed = Names(violations)
		if attempt < attempts {
			// Sent back for a revision: an event only, the revision may pass.
			telemetry.RecordRetry(ctx, fmt.Errorf("failed checks %s", strings.Join(failed, ", ")), attempt)
		}
		slog.WarnContext(ctx, "cover letter failed the checks", "attempt", attempt, "failed_checks", failed)
		input.Revision = &entities.CoverLetterRevision{Previous: raw, Violations: messages(violations)}
	}

	slog.ErrorContext(ctx, "cover letter rejected after a revision", "failed_checks", failed)
	return nil, span.RecordError(fmt.Errorf("%w: failed checks %s", ErrGenerationFailed, strings.Join(failed, ", ")))
}

// review decodes the LLM's answer and checks the letter.
func review(raw string, candidate Candidate) (*Letter, []Violation) {
	var answer struct {
		Subject string `json:"subject"`
		Letter  string `json:"letter"`
	}
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&answer); err != nil {
		return nil, []Violation{{Check: CheckFormat, Message: `answer with one JSON object {"subject": "...", "letter": "..."} and nothing else`}}
	}

	letter := &Letter{
		Subject: strings.TrimSpace(answer.Subject),
		Letter:  normalizeLetter(answer.Letter),
	}
	letter.WordCount = WordCount(letter.Letter)
	return letter, Check(*letter, candidate)
}

var blankLines = regexp.MustCompile(`\n{3,}`)

// normalizeLetter trims every line and collapses runs of blank lines, so
// paragraphs are separated by exactly one blank line.
func normalizeLetter(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func messages(violations []Violation) []string {
	out := make([]string, len(violations))
	for i, v := range violations {
		out[i] = v.Message
	}
	return out
}

// evaluationData is what the LLM sees of an evaluation.
type evaluationData struct {
	OverallScore  int                      `json:"overall_score"`
	Requirements  []matcher.Requirement    `json:"requirements,omitempty"`
	Breakdown     []matcher.CriterionScore `json:"breakdown,omitempty"`
	Strengths     []string                 `json:"strengths,omitempty"`
	MissingSkills []string                 `json:"missing_skills,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
}

// renderEvaluation renders an evaluation as JSON for the prompt, with the
// prompt's tags stripped from every string and long lists and strings cut,
// or "" when there is none. A client-supplied evaluation is untrusted.
func renderEvaluation(e *matcher.Response) string {
	if e == nil {
		return ""
	}
	clean := func(s string, limit int) string {
		return truncate(strings.TrimSpace(prompts.StripTags(s)), limit)
	}
	cleanList := func(in []string) []string {
		var out []string
		for _, s := range in {
			if s = clean(s, maxEvaluationText); s != "" && len(out) < maxEvaluationItems {
				out = append(out, s)
			}
		}
		return out
	}

	data := evaluationData{
		OverallScore:  max(0, min(100, e.OverallScore)),
		Strengths:     cleanList(e.Strengths),
		MissingSkills: cleanList(e.MissingSkills),
		Summary:       clean(e.Summary, maxSummaryText),
	}
	for _, r := range e.Requirements {
		if len(data.Requirements) == maxEvaluationItems {
			break
		}
		if r.Requirement = clean(r.Requirement, maxEvaluationText); r.Requirement == "" {
			continue
		}
		data.Requirements = append(data.Requirements, matcher.Requirement{
			Requirement: r.Requirement,
			Importance:  clean(r.Importance, 20),
			Met:         clean(r.Met, 20),
			Evidence:    clean(r.Evidence, maxEvaluationText),
		})
	}
	for _, b := range e.Breakdown {
		if len(data.Breakdown) == maxEvaluationItems {
			break
		}
		data.Breakdown = append(data.Breakdown, matcher.CriterionScore{
			Criterion: clean(b.Criterion, 40),
			Score:     max(0, min(100, b.Score)),
			Reason:    clean(b.Reason, maxEvaluationText),
		})
	}

	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		// Encoding this struct of strings and ints cannot fail.
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func truncate(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit])
	}
	return s
}

func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
