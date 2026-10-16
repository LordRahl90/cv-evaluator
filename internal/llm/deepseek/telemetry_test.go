package deepseek

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cv-evaluator/internal/telemetry"
	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestNewClient_RecordsChatCompletions(t *testing.T) {
	p := telemetrytest.Install(t)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"{\"ok\":true}"},"finish_reason":"stop"}]}`))
	}))
	defer api.Close()

	client := NewClient("test-key")
	require.NotNil(t, client)
	client.BaseURL = api.URL + "/"
	svc := New(client)

	res, err := svc.CleanupCV(t.Context(), "Jane Doe, engineer")
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, res)

	points := p.Histogram(t, telemetry.MetricExternalRequestDuration)
	require.Len(t, points, 1)
	assert.Equal(t, map[string]string{
		"peer.service": "deepseek",
		"operation":    "chat_completion",
		"llm.task":     "cv_cleanup",
		"outcome":      "success",
	}, telemetrytest.Attrs(points[0].Attributes))

	spans := p.SpansOfKind(trace.SpanKindClient)
	require.Len(t, spans, 1)
	assert.Equal(t, "deepseek chat_completion", spans[0].Name())

	// The HTTP call nests under the client's own spans.
	call := p.Span(t, "deepseek.Service.complete")
	telemetrytest.RequireChild(t, call, spans[0])
	telemetrytest.RequireChild(t, p.Span(t, "deepseek.Service.CleanupCV"), call)
	assert.Equal(t, "cv_cleanup", telemetrytest.KeyValues(call.Attributes())["llm.task"])
}
