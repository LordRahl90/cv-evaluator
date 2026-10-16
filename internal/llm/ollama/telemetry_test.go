package ollama

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cv-evaluator/internal/telemetry"
	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestService_RecordsExternalRequests(t *testing.T) {
	p := telemetrytest.Install(t)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embeddings":
			_, _ = w.Write([]byte(`{"embedding":[0.1,0.2]}`))
		default:
			_, _ = w.Write([]byte(`{"response":"{}"}`))
		}
	}))
	defer api.Close()

	svc := New(&http.Client{Timeout: 5 * time.Second}, api.URL)

	_, err := svc.CreateEmbedding(t.Context(), "text")
	require.NoError(t, err)
	_, err = svc.CleanupCV(t.Context(), "cv")
	require.NoError(t, err)

	got := map[string]string{}
	for _, dp := range p.Histogram(t, telemetry.MetricExternalRequestDuration) {
		attrs := telemetrytest.Attrs(dp.Attributes)
		assert.Equal(t, "ollama", attrs["peer.service"])
		got[attrs["operation"]] = attrs["outcome"]
	}
	assert.Equal(t, map[string]string{"embedding": "success", "cv_cleanup": "success"}, got)

	spans := p.SpansOfKind(trace.SpanKindClient)
	require.Len(t, spans, 2)
	assert.Equal(t, "ollama embedding", spans[0].Name())
	assert.Equal(t, "ollama cv_cleanup", spans[1].Name())
	telemetrytest.RequireChild(t, p.Span(t, "ollama.Service.CreateEmbedding"), spans[0])
	telemetrytest.RequireChild(t, p.Span(t, "ollama.Service.CleanupCV"), spans[1])
}
