package account

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

// seeded is one user's data: every kind of row a user owns, including
// soft-deleted ones.
type seeded struct {
	user       models.User
	cv         models.CV
	oldCV      models.CV // soft-deleted
	search     models.JobSearch
	oldSearch  models.JobSearch // soft-deleted
	result     models.SearchResult
	evaluation models.Evaluation
	key        models.APIKey
	revoked    models.APIKey // soft-deleted
	suggestion models.PortalSuggestion
}

const (
	passwordHash = "$2a$10$notarealhashbutlookslikeoneXXXXXXXXXXXXXXXXXXXXXXXXXX"
)

func seedUser(t *testing.T, db *gorm.DB, email string, jobs ...models.Job) *seeded {
	t.Helper()
	s := &seeded{}
	s.user = models.User{FirstName: "Ada", LastName: "Lovelace", Email: email, Phone: email + "-phone", Password: passwordHash}
	require.NoError(t, db.Create(&s.user).Error)

	vec := pgvector.NewVector(make([]float32, 768))
	s.cv = models.CV{UserID: s.user.ID, ExtractedContent: "Go engineer from " + email, FullEmbedding: vec}
	s.oldCV = models.CV{UserID: s.user.ID, ExtractedContent: "old cv", FullEmbedding: vec}
	require.NoError(t, db.Create(&s.cv).Error)
	require.NoError(t, db.Create(&s.oldCV).Error)
	require.NoError(t, db.Delete(&s.oldCV).Error)
	for _, cv := range []models.CV{s.cv, s.oldCV} {
		require.NoError(t, db.Create(&models.SectionEmbedding{
			UserID: s.user.ID, CVID: cv.ID, SectionHeading: "skills", Section: `["go"]`, Embedding: vec,
		}).Error)
	}

	s.search = models.JobSearch{UserID: s.user.ID, Name: "Go", Query: "go", Location: "Copenhagen", Frequency: models.FrequencyDaily, Enabled: true}
	s.oldSearch = models.JobSearch{UserID: s.user.ID, Name: "Old", Query: "rust", Frequency: models.FrequencyWeekly}
	require.NoError(t, db.Create(&s.search).Error)
	require.NoError(t, db.Create(&s.oldSearch).Error)
	require.NoError(t, db.Delete(&s.oldSearch).Error)

	for i, job := range jobs {
		search := s.search
		if i == 1 {
			search = s.oldSearch
		}
		r := models.SearchResult{JobSearchID: search.ID, UserID: s.user.ID, JobID: job.ID}
		require.NoError(t, db.Create(&r).Error)
		e := models.Evaluation{
			SearchResultID: r.ID, CVID: s.cv.ID, OverallScore: 80 + i, Status: models.StatusCompleted,
			Strengths: models.StringList{"go"}, MissingSkills: models.StringList{"rust"}, Summary: "good fit",
			RawResponse: "raw llm output",
		}
		require.NoError(t, db.Create(&e).Error)
		require.NoError(t, db.Create(&[]models.EvaluationScore{
			{EvaluationID: e.ID, Criterion: "skills", Score: 85, Reason: "knows Go"},
			{EvaluationID: e.ID, Criterion: "location", Score: 70, Reason: "nearby"},
		}).Error)
		if i == 0 {
			s.result, s.evaluation = r, e
		}
	}

	s.key = models.APIKey{UserID: s.user.ID, Name: "laptop", Prefix: "sk_test_abcd", KeyHash: "hash-of-" + email}
	s.revoked = models.APIKey{UserID: s.user.ID, Name: "old", Prefix: "sk_test_wxyz", KeyHash: "hash-of-revoked-" + email}
	require.NoError(t, db.Create(&s.key).Error)
	require.NoError(t, db.Create(&s.revoked).Error)
	require.NoError(t, db.Delete(&s.revoked).Error)

	s.suggestion = models.PortalSuggestion{UserID: s.user.ID, Name: "Jobnet", URL: "https://jobnet.dk/" + email}
	require.NoError(t, db.Create(&s.suggestion).Error)
	return s
}

func seedJobs(t *testing.T, db *gorm.DB) []models.Job {
	t.Helper()
	jobs := []models.Job{
		{Provider: "linkedin", JobID: "1", Title: "Go Engineer", Company: "ACME", Location: "Copenhagen", DetailsURL: "https://example.com/1"},
		{Provider: "jobindex", JobID: "2", Title: "Backend Developer", Company: "Initech"},
	}
	require.NoError(t, db.Create(&jobs).Error)
	return jobs
}

// owned counts every row (soft-deleted ones too) that belongs to userID.
func owned(t *testing.T, db *gorm.DB, userID ulid.ULID) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	countRows := func(table string, q *gorm.DB) {
		var n int64
		require.NoError(t, q.Count(&n).Error)
		counts[table] = n
	}
	u := db.Session(&gorm.Session{NewDB: true}).Unscoped
	countRows("users", u().Model(&models.User{}).Where("id = ?", userID))
	countRows("cvs", u().Model(&models.CV{}).Where("user_id = ?", userID))
	countRows("section_embeddings", u().Model(&models.SectionEmbedding{}).Where("user_id = ?", userID))
	countRows("job_searches", u().Model(&models.JobSearch{}).Where("user_id = ?", userID))
	countRows("search_results", u().Model(&models.SearchResult{}).Where("user_id = ?", userID))
	countRows("evaluations", u().Model(&models.Evaluation{}).
		Where("search_result_id IN (?)", db.Model(&models.SearchResult{}).Select("id").Where("user_id = ?", userID)))
	countRows("evaluation_scores", u().Model(&models.EvaluationScore{}).
		Where("evaluation_id IN (?)", db.Model(&models.Evaluation{}).Select("evaluations.id").
			Joins("JOIN search_results sr ON sr.id = evaluations.search_result_id").Where("sr.user_id = ?", userID)))
	countRows("api_keys", u().Model(&models.APIKey{}).Where("user_id = ?", userID))
	countRows("portal_suggestions", u().Model(&models.PortalSuggestion{}).Where("user_id = ?", userID))
	return counts
}

func TestService_Delete_RemovesExactlyTheUsersRows(t *testing.T) {
	db := newTestDB(t)
	svc := New(db)
	ctx := context.Background()
	jobs := seedJobs(t, db)
	me := seedUser(t, db, "me@example.com", jobs...)
	other := seedUser(t, db, "other@example.com", jobs...)

	want := map[string]int64{
		"users": 1, "cvs": 2, "section_embeddings": 2, "job_searches": 2, "search_results": 2,
		"evaluations": 2, "evaluation_scores": 4, "api_keys": 2, "portal_suggestions": 1,
	}
	require.Equal(t, want, owned(t, db, me.user.ID), "fixture")
	otherBefore := owned(t, db, other.user.ID)
	require.Equal(t, want, otherBefore, "fixture")

	report, err := svc.Delete(ctx, me.user.ID)
	require.NoError(t, err)

	// Every table is reported, children first, the user last.
	var tables []string
	got := map[string]int64{}
	for _, d := range report {
		tables = append(tables, d.Table)
		got[d.Table] = d.Rows
	}
	assert.Equal(t, []string{
		"evaluation_scores", "evaluations", "search_results", "job_searches",
		"section_embeddings", "cvs", "api_keys", "portal_suggestions", "users",
	}, tables)
	assert.Equal(t, want, got)

	for table, n := range owned(t, db, me.user.ID) {
		assert.Zero(t, n, "%s rows left for the deleted user (hard delete)", table)
	}
	assert.Equal(t, otherBefore, owned(t, db, other.user.ID), "other users are untouched")

	var jobCount int64
	require.NoError(t, db.Unscoped().Model(&models.Job{}).Count(&jobCount).Error)
	assert.Equal(t, int64(len(jobs)), jobCount, "global jobs are never deleted")

	// Deleting again is a clean not-found, not a failure.
	_, err = svc.Delete(ctx, me.user.ID)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestService_Delete_UnknownUser(t *testing.T) {
	svc := New(newTestDB(t))
	_, err := svc.Delete(context.Background(), ulid.Make())
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestService_Delete_SoftDeletedUser(t *testing.T) {
	db := newTestDB(t)
	me := seedUser(t, db, "me@example.com", seedJobs(t, db)...)
	require.NoError(t, db.Delete(&me.user).Error)

	_, err := New(db).Delete(context.Background(), me.user.ID)
	require.NoError(t, err)
	for table, n := range owned(t, db, me.user.ID) {
		assert.Zero(t, n, table)
	}
}

func TestService_Export(t *testing.T) {
	db := newTestDB(t)
	svc := New(db)
	jobs := seedJobs(t, db)
	me := seedUser(t, db, "me@example.com", jobs...)
	seedUser(t, db, "other@example.com", jobs...)

	before := time.Now().Add(-time.Second)
	exp, err := svc.Export(context.Background(), me.user.ID)
	require.NoError(t, err)

	assert.Equal(t, "account_export", exp.Object)
	assert.True(t, exp.ExportedAt.After(before))
	assert.Equal(t, me.user.ID, exp.Profile.ID)
	assert.Equal(t, "me@example.com", exp.Profile.Email)
	assert.Equal(t, "Ada", exp.Profile.FirstName)

	require.Len(t, exp.CVs, 2, "deleted CVs are still stored, so exported")
	assert.Equal(t, me.cv.ID, exp.CVs[0].ID)
	assert.Equal(t, "Go engineer from me@example.com", exp.CVs[0].ExtractedContent)
	assert.Nil(t, exp.CVs[0].DeletedAt)
	assert.Equal(t, []ExportCVSection{{Heading: "skills", Content: `["go"]`}}, exp.CVs[0].Sections)
	assert.NotNil(t, exp.CVs[1].DeletedAt)

	require.Len(t, exp.JobSearches, 2)
	assert.Equal(t, "Go", exp.JobSearches[0].Name)
	assert.Equal(t, "Copenhagen", exp.JobSearches[0].Location)
	assert.NotNil(t, exp.JobSearches[1].DeletedAt)

	require.Len(t, exp.Results, 2)
	r := exp.Results[0]
	assert.Equal(t, me.result.ID, r.ID)
	assert.Equal(t, me.search.ID, r.JobSearchID)
	require.NotNil(t, r.Job)
	assert.Equal(t, "Go Engineer", r.Job.Title)
	assert.Equal(t, "ACME", r.Job.Company)
	require.Len(t, r.Evaluations, 1)
	e := r.Evaluations[0]
	assert.Equal(t, me.cv.ID, e.CVID)
	assert.Equal(t, 80, e.OverallScore)
	assert.Equal(t, []string{"go"}, e.Strengths)
	assert.Equal(t, []string{"rust"}, e.MissingSkills)
	assert.Equal(t, "good fit", e.Summary)
	assert.Equal(t, []ExportScore{
		{Criterion: "location", Score: 70, Reason: "nearby"},
		{Criterion: "skills", Score: 85, Reason: "knows Go"},
	}, e.Scores)

	require.Len(t, exp.APIKeys, 2)
	assert.Equal(t, "laptop", exp.APIKeys[0].Name)
	assert.Equal(t, "sk_test_abcd", exp.APIKeys[0].Prefix)
	assert.Nil(t, exp.APIKeys[0].RevokedAt)
	assert.NotNil(t, exp.APIKeys[1].RevokedAt)

	require.Len(t, exp.PortalSuggestions, 1)
	assert.Equal(t, "Jobnet", exp.PortalSuggestions[0].Name)

	// No secrets, and nothing of the other user's.
	raw, err := json.Marshal(exp)
	require.NoError(t, err)
	body := string(raw)
	for _, secret := range []string{passwordHash, "hash-of-", "raw llm output", "other@example.com", `"password"`, "key_hash", "embedding"} {
		assert.NotContains(t, body, secret)
	}
}

func TestService_Export_EmptyAccount(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Email: "empty@example.com", Password: passwordHash}
	require.NoError(t, db.Create(&user).Error)

	exp, err := New(db).Export(context.Background(), user.ID)
	require.NoError(t, err)
	raw, err := json.Marshal(exp)
	require.NoError(t, err)
	for _, key := range []string{"cvs", "job_searches", "results", "api_keys", "portal_suggestions"} {
		assert.Contains(t, string(raw), `"`+key+`":[]`, "empty lists, not null")
	}

	_, err = New(db).Export(context.Background(), ulid.Make())
	require.ErrorIs(t, err, ErrUserNotFound)
}
