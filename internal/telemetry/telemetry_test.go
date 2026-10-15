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
	assert.Equal(t, "boom", spans[0].Status().Description)
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

func TestNewHTTPClient_KeepsClientSettings(t *testing.T) {
	orig := &http.Client{Timeout: time.Minute}
	c := NewHTTPClient(PeerLinkedIn, orig)

	assert.Equal(t, time.Minute, c.Timeout)
	assert.NotSame(t, orig, c)
	assert.Nil(t, orig.Transport, "the original client must not be modified")
	assert.NotNil(t, c.Transport)
}
