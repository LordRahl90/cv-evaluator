package telemetry

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Peer service names for peer.service.
const (
	PeerDeepSeek = "deepseek"
	PeerOllama   = "ollama"
	PeerLinkedIn = "linkedin"
	PeerJobindex = "jobindex"
)

// Operation names for the operation attribute.
const (
	OpChatCompletion = "chat_completion"
	OpEmbedding      = "embedding"
	OpCVCleanup      = "cv_cleanup"
	OpSearch         = "search"
	OpDetails        = "details"
)

// defaultOperation is used when a request's context carries no operation.
const defaultOperation = "request"

type operationKey struct{}

type operationInfo struct {
	name  string
	attrs []attribute.KeyValue
}

// WithOperation returns a context that labels outgoing requests made through
// a Transport with the given operation and extra low-cardinality attributes
// (for example attribute.String(AttrLLMTask, "job_match")).
func WithOperation(ctx context.Context, operation string, attrs ...attribute.KeyValue) context.Context {
	return context.WithValue(ctx, operationKey{}, operationInfo{name: operation, attrs: attrs})
}

// TrackExternal measures one call to an external API that does not go
// through an instrumented HTTP client (for example an SDK call). It starts a
// client span named "<service> <operation>"; call end exactly once with the
// call's error to finish it and record cv_evaluator.external_request.duration.
//
//	ctx, end := telemetry.TrackExternal(ctx, telemetry.PeerDeepSeek, telemetry.OpChatCompletion)
//	res, err := client.Call(ctx)
//	end(err)
func TrackExternal(ctx context.Context, service, operation string, attrs ...attribute.KeyValue) (context.Context, func(err error)) {
	start := time.Now()
	base := externalAttrs(service, operation, attrs)
	ctx, span := tracer().Start(ctx, service+" "+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(base...),
	)
	return ctx, func(err error) {
		out := outcome(err)
		span.SetAttributes(attribute.String(AttrOutcome, out))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		recordExternal(ctx, time.Since(start), base, out)
	}
}

func externalAttrs(service, operation string, extra []attribute.KeyValue) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2+len(extra))
	attrs = append(attrs,
		attribute.String(AttrPeerService, service),
		attribute.String(AttrOperation, operation),
	)
	return append(attrs, extra...)
}

func recordExternal(ctx context.Context, d time.Duration, attrs []attribute.KeyValue, out string) {
	h := getInstruments().externalRequest
	if h == nil {
		return
	}
	all := make([]attribute.KeyValue, 0, len(attrs)+1)
	all = append(all, attrs...)
	all = append(all, attribute.String(AttrOutcome, out))
	h.Record(ctx, d.Seconds(), metric.WithAttributes(all...))
}

// TransportOption configures NewTransport.
type TransportOption func(*recordingTransport)

// WithPeerFunc derives peer.service from each request, for a transport
// shared by several APIs. An empty result falls back to the service passed
// to NewTransport.
func WithPeerFunc(f func(*http.Request) string) TransportOption {
	return func(t *recordingTransport) { t.peerFunc = f }
}

// WithOperationFunc derives the operation from each request when its
// context carries none (see WithOperation), for clients such as colly that
// do not pass a context. An empty result falls back to "request".
func WithOperationFunc(f func(*http.Request) string) TransportOption {
	return func(t *recordingTransport) { t.operationFunc = f }
}

// NewTransport wraps base (http.DefaultTransport when nil) with otelhttp, so
// each request gets a client span named "<peer.service> <operation>" and
// trace propagation headers, and records
// cv_evaluator.external_request.duration with peer.service set to service.
// The operation comes from the request context (see WithOperation), then
// WithOperationFunc, and defaults to "request". A transport error or a
// status of 400 or above counts as outcome=error.
func NewTransport(service string, base http.RoundTripper, opts ...TransportOption) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	rt := &recordingTransport{service: service, base: base}
	for _, opt := range opts {
		opt(rt)
	}
	return otelhttp.NewTransport(rt,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			peer, op := rt.labels(r)
			return peer + " " + op.name
		}),
	)
}

// NewHTTPClient returns a copy of client (a new client when nil) whose
// transport is wrapped with NewTransport.
func NewHTTPClient(service string, client *http.Client, opts ...TransportOption) *http.Client {
	c := &http.Client{}
	if client != nil {
		*c = *client
	}
	c.Transport = NewTransport(service, c.Transport, opts...)
	return c
}

// recordingTransport runs inside the otelhttp span: it labels that span and
// records the external request histogram.
type recordingTransport struct {
	service       string
	base          http.RoundTripper
	peerFunc      func(*http.Request) string
	operationFunc func(*http.Request) string
}

func (t *recordingTransport) labels(req *http.Request) (string, operationInfo) {
	peer := t.service
	if t.peerFunc != nil {
		if p := t.peerFunc(req); p != "" {
			peer = p
		}
	}
	if op, ok := req.Context().Value(operationKey{}).(operationInfo); ok {
		return peer, op
	}
	if t.operationFunc != nil {
		if name := t.operationFunc(req); name != "" {
			return peer, operationInfo{name: name}
		}
	}
	return peer, operationInfo{name: defaultOperation}
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	peer, op := t.labels(req)
	attrs := externalAttrs(peer, op.name, op.attrs)

	span := trace.SpanFromContext(req.Context())
	span.SetAttributes(attrs...)

	start := time.Now()
	res, err := t.base.RoundTrip(req)

	out := OutcomeSuccess
	if err != nil || res.StatusCode >= http.StatusBadRequest {
		out = OutcomeError
	}
	span.SetAttributes(attribute.String(AttrOutcome, out))
	recordExternal(req.Context(), time.Since(start), attrs, out)

	return res, err
}
