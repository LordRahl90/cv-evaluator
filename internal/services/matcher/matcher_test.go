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

const validResponse = `{"overall_score":80,"skills_score":70,"experience_score":60,"seniority_score":50,` +
	`"domain_score":40,"location_score":30,"strengths":["go"],"missing_skills":["rust"],"summary":"good fit"}`

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

	vec := pgvector.NewVector(unitVector(0))
	job := &models.Job{ID: ulid.Make(), Detail: "a Go job", Embedding: &vec}

	resp, raw, err := svc.MatchJob(t.Context(), cvA, job)
	require.NoError(t, err)
	assert.Equal(t, validResponse, raw)
	assert.Equal(t, 80, resp.OverallScore)
	assert.Equal(t, []string{"rust"}, resp.MissingSkills)

	assert.Equal(t, 0, emb.calls, "an embedded job must not be embedded again")
	require.Len(t, llm.inputs, 1)
	assert.Contains(t, llm.inputs[0].RelevantCV, "skill from CV A")
	assert.NotContains(t, llm.inputs[0].RelevantCV, "skill from CV B")
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
