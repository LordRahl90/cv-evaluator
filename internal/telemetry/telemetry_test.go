package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cv-evaluator/internal/config"
	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestSetup_NoEndpointUsesNoopProviders(t *testing.T) {
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})

	shutdown, err := Setup(context.Background(), &config.Config{Environment: "test"})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(context.Background(), "noop")
	assert.False(t, span.SpanContext().IsValid(), "no-op tracer must not produce real spans")
	span.End()

	require.NoError(t, shutdown(context.Background()))
}

func TestSetup_WithEndpointInstallsSDKProviders(t *testing.T) {
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})

	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)

	shutdown, err := Setup(context.Background(), &config.Config{
		Environment: "test",
		Telemetry:   config.Telemetry{OTLPEndpoint: collector.URL, ServiceVersion: "1.0.0"},
	})
	require.NoError(t, err)

	_, span := otel.Tracer("test").Start(context.Background(), "real")
	assert.True(t, span.SpanContext().IsValid())
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}

func TestNewResource(t *testing.T) {
	res, err := newResource(context.Background(), &config.Config{
		Environment: "staging",
		Telemetry:   config.Telemetry{ServiceVersion: "1.2.3"},
	})
	require.NoError(t, err)

	attrs := telemetrytest.Attrs(*res.Set())
	assert.Equal(t, "cv-evaluator", attrs["service.name"])
	assert.Equal(t, "1.2.3", attrs["service.version"])
	assert.Equal(t, "staging", attrs["deployment.environment.name"])
	assert.Equal(t, "staging", attrs["deployment.environment"])
}

func TestStartRun_Success(t *testing.T) {
	p := telemetrytest.Install(t)

	ctx, end := StartRun(context.Background(), "scrape.linkedin")
	assert.True(t, trace.SpanFromContext(ctx).SpanContext().IsValid(), "context must carry the run span")
	end(nil)

	points := p.Histogram(t, MetricBackgroundRunDuration)
	require.Len(t, points, 1)
	assert.Equal(t, uint64(1), points[0].Count)
	assert.Equal(t, map[string]string{"run.kind": "scrape.linkedin", "run.outcome": "success"}, telemetrytest.Attrs(points[0].Attributes))

	spans := p.Spans()
	require.Len(t, spans, 1)
	assert.Equal(t, "background_run scrape.linkedin", spans[0].Name())
	assert.Equal(t, trace.SpanKindInternal, spans[0].SpanKind())
	assert.Equal(t, "success", telemetrytest.KeyValues(spans[0].Attributes())["run.outcome"])
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
}

func TestStartRun_Error(t *testing.T) {
	p := telemetrytest.Install(t)

	_, end := StartRun(context.Background(), "pipeline.match")
	end(errors.New("boom"))

	points := p.Histogram(t, MetricBackgroundRunDuration)
	require.Len(t, points, 1)
	assert.Equal(t, map[string]string{"run.kind": "pipeline.match", "run.outcome": "error"}, telemetrytest.Attrs(points[0].Attributes))

	spans := p.Spans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, "*errors.errorString", spans[0].Status().Description, "the status carries a label, not the message")
	assert.Empty(t, spans[0].Events(), "a run records no exception event; the error's origin or handler does")
}

// pipelineObserver mirrors internal/pipeline.RunObserver, so this test fails
// to compile if RunObserver stops satisfying it.
type pipelineObserver interface {
	RunStarted(ctx context.Context, kind string) context.Context
	RunFinished(ctx context.Context, kind string, d time.Duration, err error)
}

var _ pipelineObserver = RunObserver{}

func TestRunObserver_RunStartedAndFinished(t *testing.T) {
	p := telemetrytest.Install(t)

	var obs pipelineObserver = RunObserver{}
	ctx := obs.RunStarted(context.Background(), "run_search")
	_, child := otel.Tracer("test").Start(ctx, "fetch")
	child.End()
	obs.RunFinished(ctx, "run_search", 1500*time.Millisecond, errors.New("provider down"))

	points := p.Histogram(t, MetricBackgroundRunDuration)
	require.Len(t, points, 1)
	assert.InDelta(t, 1.5, points[0].Sum, 0.001)
	assert.Equal(t, map[string]string{"run.kind": "run_search", "run.outcome": "error"}, telemetrytest.Attrs(points[0].Attributes))

	spans := p.Spans()
	require.Len(t, spans, 2)
	run := spans[1]
	assert.Equal(t, "background_run run_search", run.Name())
	assert.Equal(t, codes.Error, run.Status().Code)
	assert.Equal(t, run.SpanContext().SpanID(), spans[0].Parent().SpanID(), "work in the run must nest under the run span")
}

func TestRunObserver_RunFinishedWithoutStart(t *testing.T) {
	p := telemetrytest.Install(t)

	RunObserver{}.RunFinished(context.Background(), "schedule_tick", time.Second, nil)

	require.Len(t, p.Histogram(t, MetricBackgroundRunDuration), 1)
	require.Len(t, p.Spans(), 1)
}

func TestRunObserver_RecordsBackdatedRun(t *testing.T) {
	p := telemetrytest.Install(t)

	var obs RunObserver
	obs.OnRunStart("pipeline.embed")
	obs.OnRunEnd("pipeline.embed", 2*time.Second, nil)

	points := p.Histogram(t, MetricBackgroundRunDuration)
	require.Len(t, points, 1)
	assert.InDelta(t, 2.0, points[0].Sum, 0.001)
	assert.Equal(t, map[string]string{"run.kind": "pipeline.embed", "run.outcome": "success"}, telemetrytest.Attrs(points[0].Attributes))

	spans := p.Spans()
	require.Len(t, spans, 1)
	assert.Equal(t, "background_run pipeline.embed", spans[0].Name())
	assert.InDelta(t, (2 * time.Second).Seconds(), spans[0].EndTime().Sub(spans[0].StartTime()).Seconds(), 0.001)
}

func TestTrackExternal(t *testing.T) {
	p := telemetrytest.Install(t)

	_, end := TrackExternal(context.Background(), PeerDeepSeek, OpChatCompletion, attribute.String(AttrLLMTask, "job_match"))
	end(nil)
	_, end = TrackExternal(context.Background(), PeerDeepSeek, OpChatCompletion, attribute.String(AttrLLMTask, "job_match"))
	end(errors.New("rate limited"))

	points := p.Histogram(t, MetricExternalRequestDuration)
	require.Len(t, points, 2)
	got := map[string]uint64{}
	for _, dp := range points {
		attrs := telemetrytest.Attrs(dp.Attributes)
		assert.Equal(t, "deepseek", attrs["peer.service"])
		assert.Equal(t, "chat_completion", attrs["operation"])
		assert.Equal(t, "job_match", attrs["llm.task"])
		got[attrs["outcome"]] = dp.Count
	}
	assert.Equal(t, map[string]uint64{"success": 1, "error": 1}, got)

	spans := p.Spans()
	require.Len(t, spans, 2)
	assert.Equal(t, "deepseek chat_completion", spans[0].Name())
	assert.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	assert.Equal(t, codes.Error, spans[1].Status().Code)
}

func TestNewTransport(t *testing.T) {
	p := telemetrytest.Install(t)
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(prevProp) })
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var gotTraceparent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client := NewHTTPClient(PeerOllama, nil)

	do := func(ctx context.Context, path string) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+path, nil)
		require.NoError(t, err)
		res, err := client.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
	}
	do(WithOperation(context.Background(), OpEmbedding), "/ok")
	assert.NotEmpty(t, gotTraceparent, "trace context must be propagated")
	do(WithOperation(context.Background(), OpCVCleanup), "/missing")
	do(context.Background(), "/ok")

	points := p.Histogram(t, MetricExternalRequestDuration)
	got := map[string]string{}
	for _, dp := range points {
		attrs := telemetrytest.Attrs(dp.Attributes)
		assert.Equal(t, "ollama", attrs["peer.service"])
		got[attrs["operation"]] = attrs["outcome"]
	}
	assert.Equal(t, map[string]string{"embedding": "success", "cv_cleanup": "error", "request": "success"}, got)

	spans := p.Spans()
	require.Len(t, spans, 3)
	assert.Equal(t, "ollama embedding", spans[0].Name())
	assert.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	assert.Equal(t, "embedding", telemetrytest.KeyValues(spans[0].Attributes())["operation"])
	assert.Equal(t, "ollama", telemetrytest.KeyValues(spans[0].Attributes())["peer.service"])
}

func TestNewJobBoardTransport(t *testing.T) {
	p := telemetrytest.Install(t)

	var gotHost string
	rt := NewJobBoardTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotHost = r.URL.Host
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	}))
	client := &http.Client{Transport: rt}

	for _, u := range []string{
		"https://www.linkedin.com/jobs/search?keywords=go",
		"https://www.linkedin.com/jobs/view/senior-engineer-1001",
		"https://www.jobindex.dk/jobsoegning?q=go",
		"https://www.jobindex.dk/jobannonce/2001",
		"https://jobs.example.com/search",
	} {
		res, err := client.Get(u)
		require.NoError(t, err)
		_ = res.Body.Close()
	}
	assert.Equal(t, "jobs.example.com", gotHost)

	got := map[string]bool{}
	for _, dp := range p.Histogram(t, MetricExternalRequestDuration) {
		attrs := telemetrytest.Attrs(dp.Attributes)
		got[attrs["peer.service"]+" "+attrs["operation"]] = true
	}
	assert.Equal(t, map[string]bool{
		"linkedin search":  true,
		"linkedin details": true,
		"jobindex search":  true,
		"jobindex details": true,
		"job_board search": true,
	}, got)

	names := map[string]bool{}
	for _, s := range p.Spans() {
		names[s.Name()] = true
	}
	assert.True(t, names["linkedin details"])
	assert.True(t, names["jobindex search"])
}

func TestNewTransport_ContextOperationWinsOverFunc(t *testing.T) {
	p := telemetrytest.Install(t)

	rt := NewTransport(PeerLinkedIn, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	}), WithOperationFunc(func(*http.Request) string { return OpSearch }))

	req, err := http.NewRequestWithContext(WithOperation(context.Background(), OpDetails), http.MethodGet, "https://www.linkedin.com/x", nil)
	require.NoError(t, err)
	res, err := rt.RoundTrip(req)
	require.NoError(t, err)
	_ = res.Body.Close()

	points := p.Histogram(t, MetricExternalRequestDuration)
	require.Len(t, points, 1)
	assert.Equal(t, "details", telemetrytest.Attrs(points[0].Attributes)["operation"])
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewHTTPClient_KeepsClientSettings(t *testing.T) {
	orig := &http.Client{Timeout: time.Minute}
	c := NewHTTPClient(PeerLinkedIn, orig)

	assert.Equal(t, time.Minute, c.Timeout)
	assert.NotSame(t, orig, c)
	assert.Nil(t, orig.Transport, "the original client must not be modified")
	assert.NotNil(t, c.Transport)
}

func TestSignalURL(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	assert.Equal(t, "http://collector:4318/v1/traces",
		signalURL("http://collector:4318/", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "/v1/traces"))

	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://other:4318/v1/traces")
	assert.Empty(t, signalURL("http://collector:4318", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "/v1/traces"),
		"the per-signal variable must keep precedence")
}

func TestFlush_CallsShutdownWithDeadline(t *testing.T) {
	called := false
	Flush(func(ctx context.Context) error {
		called = true
		_, ok := ctx.Deadline()
		assert.True(t, ok, "shutdown must get a bounded context")
		return errors.New("exporter unreachable")
	})
	assert.True(t, called)
}
