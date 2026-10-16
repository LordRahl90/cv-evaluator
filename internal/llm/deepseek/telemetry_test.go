package deepseek

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cv-evaluator/internal/telemetry"
	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/cohesion-org/deepseek-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
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

func TestComplete_ScrubsAPIErrors(t *testing.T) {
	p := telemetrytest.Install(t)

	// What DeepSeek answers to a bad key: the message echoes its masked
	// suffix.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails, Your api key: ****demo is invalid","type":"authentication_error","code":"invalid_request_error"}}`))
	}))
	defer api.Close()

	client := NewClient("sk-not-a-real-key-demo")
	require.NotNil(t, client)
	client.BaseURL = api.URL + "/"

	_, err := New(client).CleanupCV(t.Context(), "Jane Doe, engineer")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "demo", "the error text carries no key")
	assert.Contains(t, err.Error(), "HTTP 401")
	var apiErr *deepseek.APIError
	require.ErrorAs(t, err, &apiErr, "the original error is still reachable")
	require.Contains(t, apiErr.Error(), "****demo", "the unscrubbed text would leak the key suffix")

	call := p.Span(t, "deepseek.Service.complete")
	assert.Equal(t, codes.Error, call.Status().Code)
	require.Equal(t, 1, telemetrytest.Exceptions(call), "recorded where the call failed")
	for _, s := range p.Spans() {
		for _, e := range s.Events() {
			for _, kv := range e.Attributes {
				assert.NotContains(t, kv.Value.String(), "demo", "span %q event %s", s.Name(), kv.Key)
			}
		}
		assert.NotContains(t, s.Status().Description, "demo", s.Name())
	}
	cleanup := p.Span(t, "deepseek.Service.CleanupCV")
	assert.Equal(t, codes.Unset, cleanup.Status().Code, "the caller only passes the error up")
	assert.Zero(t, telemetrytest.Exceptions(cleanup))
}
