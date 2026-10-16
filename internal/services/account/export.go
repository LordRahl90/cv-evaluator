package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/telemetry"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Export is everything stored about a user, as handed to them by
// GET /me/export. It is a document format of its own: field names are
// stable and it never contains password hashes, API key hashes or secrets.
// Soft-deleted rows (a deleted CV or job search, a revoked key) are still
// stored, so they are included with their deletion time.
//
// Embedding vectors are left out: they are derived from the CV text, which
// is included, and are meaningless to a reader.
type Export struct {
	Object            string                   `json:"object"`
	ExportedAt        time.Time                `json:"exported_at"`
	Profile           ExportProfile            `json:"profile"`
	CVs               []ExportCV               `json:"cvs"`
	JobSearches       []ExportJobSearch        `json:"job_searches"`
	Results           []ExportResult           `json:"results"`
	APIKeys           []ExportAPIKey           `json:"api_keys"`
	PortalSuggestions []ExportPortalSuggestion `json:"portal_suggestions"`
}

// ExportProfile is the account itself, without the password hash.
type ExportProfile struct {
	ID                ulid.ULID  `json:"id"`
	FirstName         string     `json:"first_name"`
	LastName          string     `json:"last_name"`
	Email             string     `json:"email"`
	Phone             string     `json:"phone"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	PasswordChangedAt *time.Time `json:"password_changed_at"`
}

// ExportCV is an uploaded CV: its extracted text and the sections it was
// split into.
type ExportCV struct {
	ID               ulid.ULID         `json:"id"`
	ExtractedContent string            `json:"extracted_content"`
	Sections         []ExportCVSection `json:"sections"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	DeletedAt        *time.Time        `json:"deleted_at"`
}

// ExportCVSection is one section of a CV.
type ExportCVSection struct {
	Heading string `json:"heading"`
	Content string `json:"content"`
}

// ExportJobSearch is a saved job search.
type ExportJobSearch struct {
	ID        ulid.ULID  `json:"id"`
	Name      string     `json:"name"`
	Query     string     `json:"query"`
	Location  string     `json:"location"`
	Frequency string     `json:"frequency"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

// ExportResult is a job one of the user's searches found, with its
// evaluations against the user's CVs.
type ExportResult struct {
	ID               ulid.ULID          `json:"id"`
	JobSearchID      ulid.ULID          `json:"job_search_id"`
	Status           string             `json:"status"`
	EvaluationStatus string             `json:"evaluation_status"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	Job              *ExportJob         `json:"job"`
	Evaluations      []ExportEvaluation `json:"evaluations"`
}

// ExportJob is the posting a result points at. Jobs are shared between
// users and are not deleted with an account.
type ExportJob struct {
	ID         ulid.ULID `json:"id"`
	Provider   string    `json:"provider"`
	Title      string    `json:"title"`
	Company    string    `json:"company"`
	Location   string    `json:"location"`
	PostedAt   string    `json:"posted_at"`
	DetailsURL string    `json:"details_url"`
}

// ExportEvaluation is the assessment of one CV against a result's job.
type ExportEvaluation struct {
	ID            ulid.ULID     `json:"id"`
	CVID          ulid.ULID     `json:"cv_id"`
	Status        string        `json:"status"`
	OverallScore  int           `json:"overall_score"`
	Scores        []ExportScore `json:"scores"`
	Strengths     []string      `json:"strengths"`
	MissingSkills []string      `json:"missing_skills"`
	Summary       string        `json:"summary"`
	Error         string        `json:"error,omitempty"`
	Model         string        `json:"model"`
	PromptVersion string        `json:"prompt_version"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// ExportScore is one criterion's score within an evaluation.
type ExportScore struct {
	Criterion string `json:"criterion"`
	Score     int    `json:"score"`
	Reason    string `json:"reason"`
}

// ExportAPIKey is an API key's metadata. The key itself is never stored
// and its hash is never exported.
type ExportAPIKey struct {
	ID         ulid.ULID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// ExportPortalSuggestion is a job portal the user suggested.
type ExportPortalSuggestion struct {
	ID        ulid.ULID `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Note      string    `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Export collects everything stored about the user. It returns
// ErrUserNotFound when there is no such user. The data is read in one
// read-only transaction so it is a consistent snapshot.
func (s *Service) Export(ctx context.Context, userID ulid.ULID) (_ *Export, err error) {
	ctx, end := telemetry.Start(ctx, "account.Service.Export", telemetry.ID(telemetry.AttrUserID, userID))
	defer end(&err)
	var out *Export
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY").Error; err != nil {
			return fmt.Errorf("start export snapshot: %w", err)
		}
		var err error
		out, err = s.export(tx, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) export(tx *gorm.DB, userID ulid.ULID) (*Export, error) {
	var user models.User
	if err := tx.Where("id = ?", userID).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("load user: %w", err)
	}

	out := &Export{
		Object:     "account_export",
		ExportedAt: time.Now().UTC(),
		Profile: ExportProfile{
			ID:                user.ID,
			FirstName:         user.FirstName,
			LastName:          user.LastName,
			Email:             user.Email,
			Phone:             user.Phone,
			CreatedAt:         user.CreatedAt,
			UpdatedAt:         user.UpdatedAt,
			PasswordChangedAt: user.PasswordChangedAt,
		},
	}

	var err error
	if out.CVs, err = exportCVs(tx, userID); err != nil {
		return nil, err
	}
	if out.JobSearches, err = exportJobSearches(tx, userID); err != nil {
		return nil, err
	}
	if out.Results, err = exportResults(tx, userID); err != nil {
		return nil, err
	}
	if out.APIKeys, err = exportAPIKeys(tx, userID); err != nil {
		return nil, err
	}
	if out.PortalSuggestions, err = exportPortalSuggestions(tx, userID); err != nil {
		return nil, err
	}
	return out, nil
}

func exportCVs(tx *gorm.DB, userID ulid.ULID) ([]ExportCV, error) {
	var cvs []models.CV
	if err := tx.Unscoped().Omit("full_embedding").
		Where("user_id = ?", userID).Order("created_at, id").Find(&cvs).Error; err != nil {
		return nil, fmt.Errorf("load cvs: %w", err)
	}
	var sections []models.SectionEmbedding
	if err := tx.Unscoped().Select("id", "cv_id", "section_heading", "section").
		Where("user_id = ? OR cv_id IN (?)", userID, ownedCVs(tx, userID)).
		Order("section_heading, id").Find(&sections).Error; err != nil {
		return nil, fmt.Errorf("load cv sections: %w", err)
	}
	byCV := map[ulid.ULID][]ExportCVSection{}
	for _, sec := range sections {
		byCV[sec.CVID] = append(byCV[sec.CVID], ExportCVSection{Heading: sec.SectionHeading, Content: sec.Section})
	}

	out := make([]ExportCV, 0, len(cvs))
	for _, cv := range cvs {
		secs := byCV[cv.ID]
		if secs == nil {
			secs = []ExportCVSection{}
		}
		out = append(out, ExportCV{
			ID:               cv.ID,
			ExtractedContent: cv.ExtractedContent,
			Sections:         secs,
			CreatedAt:        cv.CreatedAt,
			UpdatedAt:        cv.UpdatedAt,
			DeletedAt:        deletedAt(cv.DeletedAt),
		})
	}
	return out, nil
}

func exportJobSearches(tx *gorm.DB, userID ulid.ULID) ([]ExportJobSearch, error) {
	var searches []models.JobSearch
	if err := tx.Unscoped().Where("user_id = ?", userID).Order("created_at, id").Find(&searches).Error; err != nil {
		return nil, fmt.Errorf("load job searches: %w", err)
	}
	out := make([]ExportJobSearch, 0, len(searches))
	for _, js := range searches {
		out = append(out, ExportJobSearch{
			ID:        js.ID,
			Name:      js.Name,
			Query:     js.Query,
			Location:  js.Location,
			Frequency: js.Frequency,
			Enabled:   js.Enabled,
			LastRunAt: js.LastRunAt,
			NextRunAt: js.NextRunAt,
			CreatedAt: js.CreatedAt,
			UpdatedAt: js.UpdatedAt,
			DeletedAt: deletedAt(js.DeletedAt),
		})
	}
	return out, nil
}

func exportResults(tx *gorm.DB, userID ulid.ULID) ([]ExportResult, error) {
	var results []models.SearchResult
	if err := tx.Where("user_id = ? OR job_search_id IN (?)", userID, ownedSearches(tx, userID)).
		Order("created_at, id").Find(&results).Error; err != nil {
		return nil, fmt.Errorf("load search results: %w", err)
	}
	if len(results) == 0 {
		return []ExportResult{}, nil
	}

	resultIDs := make([]ulid.ULID, len(results))
	jobIDs := make([]ulid.ULID, len(results))
	for i, r := range results {
		resultIDs[i], jobIDs[i] = r.ID, r.JobID
	}

	var jobs []models.Job
	if err := tx.Unscoped().
		Select("id", "provider", "title", "company", "location", "posted_at", "details_url").
		Where("id IN ?", jobIDs).Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	jobByID := make(map[ulid.ULID]*ExportJob, len(jobs))
	for _, j := range jobs {
		jobByID[j.ID] = &ExportJob{
			ID: j.ID, Provider: j.Provider, Title: j.Title, Company: j.Company,
			Location: j.Location, PostedAt: j.PostedAt, DetailsURL: j.DetailsURL,
		}
	}

	var evals []models.Evaluation
	if err := tx.Omit("raw_response").Where("search_result_id IN ?", resultIDs).
		Order("created_at, id").Find(&evals).Error; err != nil {
		return nil, fmt.Errorf("load evaluations: %w", err)
	}
	evalIDs := make([]ulid.ULID, len(evals))
	for i, e := range evals {
		evalIDs[i] = e.ID
	}
	scoresByEval := map[ulid.ULID][]ExportScore{}
	if len(evalIDs) > 0 {
		var scores []models.EvaluationScore
		if err := tx.Where("evaluation_id IN ?", evalIDs).Order("criterion").Find(&scores).Error; err != nil {
			return nil, fmt.Errorf("load evaluation scores: %w", err)
		}
		for _, sc := range scores {
			scoresByEval[sc.EvaluationID] = append(scoresByEval[sc.EvaluationID],
				ExportScore{Criterion: sc.Criterion, Score: sc.Score, Reason: sc.Reason})
		}
	}
	evalsByResult := map[ulid.ULID][]ExportEvaluation{}
	for _, e := range evals {
		scores := scoresByEval[e.ID]
		if scores == nil {
			scores = []ExportScore{}
		}
		evalsByResult[e.SearchResultID] = append(evalsByResult[e.SearchResultID], ExportEvaluation{
			ID:            e.ID,
			CVID:          e.CVID,
			Status:        e.Status,
			OverallScore:  e.OverallScore,
			Scores:        scores,
			Strengths:     nonNil(e.Strengths),
			MissingSkills: nonNil(e.MissingSkills),
			Summary:       e.Summary,
			Error:         e.Error,
			Model:         e.Model,
			PromptVersion: e.PromptVersion,
			CreatedAt:     e.CreatedAt,
			UpdatedAt:     e.UpdatedAt,
		})
	}

	out := make([]ExportResult, 0, len(results))
	for _, r := range results {
		evs := evalsByResult[r.ID]
		if evs == nil {
			evs = []ExportEvaluation{}
		}
		out = append(out, ExportResult{
			ID:               r.ID,
			JobSearchID:      r.JobSearchID,
			Status:           r.Status,
			EvaluationStatus: r.EvaluationStatus,
			CreatedAt:        r.CreatedAt,
			UpdatedAt:        r.UpdatedAt,
			Job:              jobByID[r.JobID],
			Evaluations:      evs,
		})
	}
	return out, nil
}

func exportAPIKeys(tx *gorm.DB, userID ulid.ULID) ([]ExportAPIKey, error) {
	var keys []models.APIKey
	if err := tx.Unscoped().Omit("key_hash").Where("user_id = ?", userID).
		Order("created_at, id").Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("load api keys: %w", err)
	}
	out := make([]ExportAPIKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, ExportAPIKey{
			ID:         k.ID,
			Name:       k.Name,
			Prefix:     k.Prefix,
			CreatedAt:  k.CreatedAt,
			LastUsedAt: k.LastUsedAt,
			RevokedAt:  deletedAt(k.DeletedAt),
		})
	}
	return out, nil
}

func exportPortalSuggestions(tx *gorm.DB, userID ulid.ULID) ([]ExportPortalSuggestion, error) {
	var suggestions []models.PortalSuggestion
	if err := tx.Where("user_id = ?", userID).Order("created_at, id").Find(&suggestions).Error; err != nil {
		return nil, fmt.Errorf("load portal suggestions: %w", err)
	}
	out := make([]ExportPortalSuggestion, 0, len(suggestions))
	for _, p := range suggestions {
		out = append(out, ExportPortalSuggestion{
			ID: p.ID, Name: p.Name, URL: p.URL, Note: p.Note, Status: p.Status,
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		})
	}
	return out, nil
}

func deletedAt(d gorm.DeletedAt) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}

func nonNil(l models.StringList) []string {
	if l == nil {
		return []string{}
	}
	return l
}
