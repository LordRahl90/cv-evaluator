package pipeline

import (
	"context"
	"errors"
	"testing"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"
	"cv-evaluator/internal/telemetry"
	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// llmEvaluator makes the LLM client span a real client would, around the
// fake evaluation.
type llmEvaluator struct{ *fakeEvaluator }

func (e llmEvaluator) MatchJob(ctx context.Context, cvID ulid.ULID, job *models.Job) (*matcher.Response, string, error) {
	ctx, end := telemetry.TrackExternal(ctx, telemetry.PeerDeepSeek, telemetry.OpChatCompletion)
	resp, raw, err := e.fakeEvaluator.MatchJob(ctx, cvID, job)
	end(err)
	return resp, raw, err
}

func children(p *telemetrytest.Providers, parent sdktrace.ReadOnlySpan) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range p.Spans() {
		if s.Parent().SpanID() == parent.SpanContext().SpanID() {
			out = append(out, s)
		}
	}
	return out
}

func names(spans []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name())
	}
	return out
}

func TestTracing_EvaluateStep(t *testing.T) {
	p := telemetrytest.Install(t)

	userID := ulid.Make()
	search := newSearch(userID, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.setPosts("linkedin", post("linkedin", "1", "a Go job"))
	var eval llmEvaluator
	h := newHarness(t, newFakeSearches(search), fetcher, func(c *Config) {
		c.Observer = telemetry.RunObserver{}
		eval = llmEvaluator{&fakeEvaluator{}}
		c.Evaluator = eval
	})
	cv := seedCV(t, userID, h.clock.Now())

	h.tick()
	embedded, err := h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	require.True(t, embedded)

	evaluated, err := h.pipeline.EvaluateOne(t.Context())
	require.NoError(t, err)
	require.True(t, evaluated)

	step := p.Span(t, "pipeline.Pipeline.EvaluateOne")
	assert.False(t, step.Parent().IsValid(), "a claimed step starts its own trace")
	attrs := telemetrytest.KeyValues(step.Attributes())
	assert.Equal(t, userID.String(), attrs["user.id"])
	assert.Equal(t, "1", attrs["attempt"])
	assert.NotEmpty(t, attrs["search_result.id"])

	// claim -> evaluate -> write, in that order, under the step.
	parts := children(p, step)
	require.Equal(t, []string{"pipeline.claimResult", "background_run evaluate_job", "pipeline.Pipeline.writeEvaluation"}, names(parts))
	assert.False(t, parts[1].StartTime().Before(parts[0].StartTime()), "the claim comes first")
	assert.False(t, parts[2].StartTime().Before(parts[1].EndTime()), "the write comes after the evaluation")

	run := parts[1]
	evalChildren := names(children(p, run))
	assert.Contains(t, evalChildren, "gorm.query jobs")
	assert.Contains(t, evalChildren, "pipeline.LatestCVSelector.SelectCV")
	assert.Contains(t, evalChildren, "deepseek chat_completion", "the LLM call nests in the evaluate step")

	write := parts[2]
	assert.Equal(t, "done", telemetrytest.KeyValues(write.Attributes())["status"])
	assert.Equal(t, "2", telemetrytest.KeyValues(write.Attributes())["criteria.count"])
	assert.Contains(t, names(children(p, write)), "gorm.create evaluations")
	assert.Equal(t, cv.ID, eval.cvs[0])

	// Idle polls find nothing and leave no trace.
	before := len(p.Spans())
	for range 3 {
		evaluated, err = h.pipeline.EvaluateOne(t.Context())
		require.NoError(t, err)
		require.False(t, evaluated)
		embedded, err = h.pipeline.EmbedOne(t.Context())
		require.NoError(t, err)
		require.False(t, embedded)
	}
	assert.Equal(t, before, len(p.Spans()), "idle polls must not produce spans: %v", p.Names()[before:])
}

func TestTracing_FailedEvaluationIsRecordedWhereHandled(t *testing.T) {
	p := telemetrytest.Install(t)

	userID := ulid.Make()
	search := newSearch(userID, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.setPosts("linkedin", post("linkedin", "1", "a Go job"))
	h := newHarness(t, newFakeSearches(search), fetcher, func(c *Config) { c.Observer = telemetry.RunObserver{} })
	h.evaluator.fail = func(int) (string, error) { return "", errors.New("llm unavailable") }
	seedCV(t, userID, h.clock.Now())

	h.tick()
	_, err := h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	_, err = h.pipeline.EvaluateOne(t.Context())
	require.NoError(t, err)

	step := p.Span(t, "pipeline.Pipeline.EvaluateOne")
	run := p.Span(t, "background_run evaluate_job")
	assert.Equal(t, codes.Error, run.Status().Code, "the failed step is marked")
	assert.Zero(t, telemetrytest.Exceptions(run))
	assert.Equal(t, 1, telemetrytest.Exceptions(step), "recorded once, where the result is scheduled for retry")
	assert.Equal(t, "pending", telemetrytest.KeyValues(p.Span(t, "pipeline.Pipeline.writeEvaluation").Attributes())["status"])
}

func TestTracing_PortalFailureIsRecordedOnThePortalSpan(t *testing.T) {
	p := telemetrytest.Install(t)

	search := newSearch(ulid.Make(), "go")
	fetcher := newFakeFetcher("linkedin", "jobindex")
	fetcher.setPosts("linkedin", post("linkedin", "1", "d"))
	fetcher.searchErr["jobindex"] = errors.New("jobindex is down")
	h := newHarness(t, newFakeSearches(search), fetcher, func(c *Config) { c.Observer = telemetry.RunObserver{} })

	assert.Equal(t, 1, h.tick())

	portals := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range p.Spans() {
		if s.Name() == "background_run run_search" {
			portals[telemetrytest.KeyValues(s.Attributes())["provider"]] = s
		}
	}
	require.Len(t, portals, 2)

	failed := portals["jobindex"]
	assert.Equal(t, codes.Error, failed.Status().Code)
	assert.Equal(t, DefaultPortalAttempts, telemetrytest.Exceptions(failed), "each failed attempt is recorded on the portal span")
	assert.Equal(t, codes.Unset, portals["linkedin"].Status().Code)
	assert.Contains(t, names(children(p, portals["linkedin"])), "pipeline.Pipeline.storePostings")

	runSearch := p.Span(t, "pipeline.Pipeline.runSearch")
	telemetrytest.RequireChild(t, runSearch, failed)
	tick := p.Span(t, "background_run schedule_tick")
	telemetrytest.RequireChild(t, tick, runSearch)
	for _, s := range []sdktrace.ReadOnlySpan{runSearch, tick} {
		assert.Equal(t, codes.Unset, s.Status().Code, "%s: the run succeeds", s.Name())
		assert.Zero(t, telemetrytest.Exceptions(s), s.Name())
	}
}

// flakyFetcher fails the first search of each portal.
type flakyFetcher struct {
	*fakeFetcher
	failed map[string]bool
}

func (f *flakyFetcher) Search(ctx context.Context, provider string, c Criteria) ([]entities.JobPost, error) {
	f.mu.Lock()
	first := !f.failed[provider]
	f.failed[provider] = true
	f.mu.Unlock()
	if first {
		return nil, errors.New("rate limited")
	}
	return f.fakeFetcher.Search(ctx, provider, c)
}

func TestTracing_RecoveredPortalRetryLeavesPortalOK(t *testing.T) {
	p := telemetrytest.Install(t)

	search := newSearch(ulid.Make(), "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.setPosts("linkedin", post("linkedin", "1", "d"))
	h := newHarness(t, newFakeSearches(search), fetcher, func(c *Config) {
		c.Observer = telemetry.RunObserver{}
		c.Fetcher = &flakyFetcher{fakeFetcher: fetcher, failed: map[string]bool{}}
	})
	assert.Equal(t, 1, h.tick())

	portal := p.Span(t, "background_run run_search")
	assert.Equal(t, codes.Unset, portal.Status().Code, "the retry succeeded")
	require.Equal(t, 1, telemetrytest.Exceptions(portal), "the failed attempt is still visible")
	assert.Equal(t, "1", telemetrytest.KeyValues(portal.Events()[0].Attributes)["attempt"])
}
