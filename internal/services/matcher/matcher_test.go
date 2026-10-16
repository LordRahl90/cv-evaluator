package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeEmbedder) CreateEmbedding(context.Context, string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return unitVector(0), nil
}

type fakeJobMatch struct {
	raw    string
	err    error
	inputs []*entities.MatchInput
}

func (f *fakeJobMatch) JobMatch(_ context.Context, in *entities.MatchInput) (string, error) {
	f.inputs = append(f.inputs, in)
	return f.raw, f.err
}

func unitVector(i int) []float32 {
	v := make([]float32, 768)
	v[i] = 1
	return v
}

func newMatcherTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)
	return db
}

func seedSection(t *testing.T, db *gorm.DB, userID, cvID ulid.ULID, heading, content string) {
	t.Helper()
	require.NoError(t, db.Create(&models.SectionEmbedding{
		UserID:         userID,
		CVID:           cvID,
		SectionHeading: heading,
		Section:        content,
		Embedding:      pgvector.NewVector(unitVector(0)),
	}).Error)
}

const validResponse = `{"overall_score":80,"breakdown":[` +
	`{"criterion":"skills","score":70,"reason":"Go matches"},` +
	`{"criterion":"location","score":90,"reason":"same city"}],` +
	`"strengths":["go"],"missing_skills":["rust"],"summary":"good fit"}`

func TestDecodeResponse_KeepsKnownCriteriaOnly(t *testing.T) {
	resp, err := decodeResponse(`{"overall_score":140,"breakdown":[
		{"criterion":"Skills","score":75,"reason":" strong Go "},
		{"criterion":"vibes","score":99,"reason":"made up"},
		{"criterion":"skills","score":10,"reason":"duplicate"},
		{"criterion":"education","score":-5,"reason":"none"}
	],"summary":"ok"}`)
	require.NoError(t, err)

	assert.Equal(t, 100, resp.OverallScore, "scores are clamped to 0-100")
	assert.Equal(t, []CriterionScore{
		{Criterion: "skills", Score: 75, Reason: "strong Go"},
		{Criterion: "education", Score: 0, Reason: "none"},
	}, resp.Breakdown)

	_, ok := resp.Score("experience")
	assert.False(t, ok, "an omitted criterion is absent, not 0")
	score, ok := resp.Score("skills")
	assert.True(t, ok)
	assert.Equal(t, 75, score)
}

func TestSimilarityHints_LabelsAndBestScore(t *testing.T) {
	hints := similarityHints([]MatchResult{
		{SectionHeading: "skills", Score: 0.5},
		{SectionHeading: "skills", Score: 0.8},
		{SectionHeading: "title", Score: 0.6},
	})
	require.Len(t, hints, 3)
	assert.Equal(t, "Skills section similarity", hints[0].Label)
	assert.InDelta(t, 80, hints[0].Score, 0.01, "the best match per section, not the last")
	assert.Equal(t, "Title similarity", hints[1].Label)
	assert.InDelta(t, 60, hints[1].Score, 0.01)
	assert.Equal(t, "Weighted similarity across sections", hints[2].Label)
	assert.InDelta(t, (0.8*0.4+0.6*0.1)/0.5*100, hints[2].Score, 0.01)

	for _, h := range hints {
		assert.NotContains(t, strings.ToLower(h.Label), "seniority")
	}
	assert.Empty(t, similarityHints(nil))
}

func TestDecodeResponse_KeepsDecodeError(t *testing.T) {
	_, err := decodeResponse("not json")
	require.Error(t, err)

	var syntaxErr *json.SyntaxError
	assert.ErrorAs(t, err, &syntaxErr, "the decode error must be wrapped, not lost")
	assert.Contains(t, err.Error(), "failed to decode LLM response")
}

func TestDecodeResponse_RejectsNull(t *testing.T) {
	_, err := decodeResponse("null")
	require.Error(t, err)
}

func TestMatchByJobDescription_DecodeFailureReturnsError(t *testing.T) {
	db := newMatcherTestDB(t)
	llm := &fakeJobMatch{raw: "{broken"}
	svc := New(db, &fakeEmbedder{}, llm)

	resp, err := svc.MatchByJobDescription(t.Context(), ulid.Make(), "a job")
	require.Error(t, err, "a bad LLM answer must not be reported as success")
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to decode LLM response")
	assert.NotEqual(t, "failed to decode LLM response: %!w(<nil>)", err.Error())
}

func TestMatchByJobDescription_PropagatesLLMError(t *testing.T) {
	db := newMatcherTestDB(t)
	want := errors.New("llm down")
	svc := New(db, &fakeEmbedder{}, &fakeJobMatch{err: want})

	_, err := svc.MatchByJobDescription(t.Context(), ulid.Make(), "a job")
	require.ErrorIs(t, err, want)
}

func TestMatchJob_UsesStoredEmbeddingAndCVScope(t *testing.T) {
	db := newMatcherTestDB(t)
	userID := ulid.Make()
	cvA, cvB := ulid.Make(), ulid.Make()
	seedSection(t, db, userID, cvA, "skills", "skill from CV A")
	seedSection(t, db, userID, cvB, "skills", "skill from CV B")

	emb := &fakeEmbedder{}
	llm := &fakeJobMatch{raw: validResponse}
	svc := New(db, emb, llm)

	require.NoError(t, db.Create(&models.CV{
		ID: cvA, UserID: userID, ExtractedContent: "  Jane Doe\nAarhus, Denmark\n" + strings.Repeat("x", 2000),
		FullEmbedding: pgvector.NewVector(unitVector(0)),
	}).Error)

	vec := pgvector.NewVector(unitVector(0))
	job := &models.Job{ID: ulid.Make(), Detail: "a Go job", Location: "Copenhagen", Embedding: &vec}

	resp, raw, err := svc.MatchJob(t.Context(), cvA, job)
	require.NoError(t, err)
	assert.Equal(t, validResponse, raw)
	assert.Equal(t, 80, resp.OverallScore)
	assert.Equal(t, []string{"rust"}, resp.MissingSkills)
	assert.Equal(t, []CriterionScore{
		{Criterion: "skills", Score: 70, Reason: "Go matches"},
		{Criterion: "location", Score: 90, Reason: "same city"},
	}, resp.Breakdown)

	assert.Equal(t, 0, emb.calls, "an embedded job must not be embedded again")
	require.Len(t, llm.inputs, 1)
	in := llm.inputs[0]
	assert.Contains(t, in.RelevantCV, "skill from CV A")
	assert.NotContains(t, in.RelevantCV, "skill from CV B")
	assert.Equal(t, "Copenhagen", in.JobLocation)
	assert.True(t, strings.HasPrefix(in.CVHeader, "Jane Doe\nAarhus, Denmark"))
	assert.Len(t, []rune(in.CVHeader), cvHeaderLength)
	assert.Equal(t, entities.EvaluationCriteria, in.Criteria)
	require.NotEmpty(t, in.Hints)
	assert.Equal(t, "Skills section similarity", in.Hints[0].Label)
}

func TestMatchJob_ReturnsRawOnDecodeFailure(t *testing.T) {
	db := newMatcherTestDB(t)
	svc := New(db, &fakeEmbedder{}, &fakeJobMatch{raw: "oops"})

	_, raw, err := svc.MatchJob(t.Context(), ulid.Make(), &models.Job{Detail: "x"})
	require.Error(t, err)
	assert.Equal(t, "oops", raw)
}

func TestMatchByJobID_FindsGlobalJob(t *testing.T) {
	db := newMatcherTestDB(t)
	userID := ulid.Make()
	seedSection(t, db, userID, ulid.Make(), "skills", "golang")

	vec := pgvector.NewVector(unitVector(0))
	require.NoError(t, db.Create(&models.Job{
		Provider: "linkedin", JobID: "123", Detail: "global job", Embedding: &vec,
	}).Error)

	emb := &fakeEmbedder{}
	llm := &fakeJobMatch{raw: validResponse}
	svc := New(db, emb, llm)

	resp, err := svc.MatchByJobID(t.Context(), userID, "123")
	require.NoError(t, err)
	assert.Equal(t, "good fit", resp.Summary)
	assert.Equal(t, 0, emb.calls)
	require.Len(t, llm.inputs, 1)
	assert.True(t, strings.Contains(llm.inputs[0].RelevantCV, "golang"))

	_, err = svc.MatchByJobID(t.Context(), userID, "missing")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
