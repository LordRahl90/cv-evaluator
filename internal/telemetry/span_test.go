package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"cv-evaluator/internal/telemetry/telemetrytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// failing is a function that detects a failure itself and records it.
func failing(ctx context.Context, fail error) error {
	_, span := Start(ctx, "pkg.Service.validate", attribute.String(AttrUserID, "u1"))
	defer span.End()
	if fail != nil {
		return span.RecordError(fail)
	}
	return nil
}

// passing only passes up its callee's error.
func passing(ctx context.Context, fail error) error {
	ctx, span := Start(ctx, "pkg.Service.Do")
	defer span.End()
	return failing(ctx, fail)
}

func TestSpan_RecordErrorWhereItFails(t *testing.T) {
	p := telemetrytest.Install(t)

	require.NoError(t, passing(context.Background(), nil))
	ok := p.Span(t, "pkg.Service.validate")
	assert.Equal(t, codes.Unset, ok.Status().Code)
	assert.Zero(t, telemetrytest.Exceptions(ok))
	assert.Equal(t, "u1", telemetrytest.KeyValues(ok.Attributes())["user.id"])

	sentinel := errors.New("bad input")
	err := passing(context.Background(), fmt.Errorf("validate: %w", sentinel))
	require.ErrorIs(t, err, sentinel, "the error is returned unchanged")
	assert.Equal(t, "validate: bad input", err.Error())

	spans := p.Spans()
	require.Len(t, spans, 4)
	origin, caller := spans[2], spans[3]
	assert.Equal(t, "pkg.Service.validate", origin.Name())
	assert.Equal(t, codes.Error, origin.Status().Code)
	assert.Equal(t, "*errors.errorString", origin.Status().Description, "the status never carries the message")
	assert.Equal(t, 1, telemetrytest.Exceptions(origin))

	assert.Equal(t, "pkg.Service.Do", caller.Name())
	assert.Equal(t, codes.Unset, caller.Status().Code, "a span that passes the error up stays neutral")
	assert.Zero(t, telemetrytest.Exceptions(caller))
}

func TestSpan_NilSafe(t *testing.T) {
	var span *Span
	sentinel := errors.New("x")
	assert.Same(t, sentinel, span.RecordError(sentinel))
	assert.NoError(t, span.RecordError(nil))
	span.SetAttributes(attribute.Int(AttrCount, 1))
	span.End()
}

func TestSpan_RecordErrorNil(t *testing.T) {
	p := telemetrytest.Install(t)

	_, span := Start(context.Background(), "pkg.op")
	assert.NoError(t, span.RecordError(nil))
	span.SetAttributes(attribute.Int(AttrCount, 3))
	span.End()

	s := p.Span(t, "pkg.op")
	assert.Equal(t, codes.Unset, s.Status().Code)
	assert.Zero(t, telemetrytest.Exceptions(s))
	assert.Equal(t, "3", telemetrytest.KeyValues(s.Attributes())["count"])
}

func TestRecordError(t *testing.T) {
	p := telemetrytest.Install(t)

	ctx, span := Start(context.Background(), "handler")
	RecordError(ctx, nil)
	RecordError(ctx, errors.New(strings.Repeat("x", 2000)))
	span.End()

	s := p.Span(t, "handler")
	require.Equal(t, 1, telemetrytest.Exceptions(s))
	assert.Equal(t, codes.Error, s.Status().Code)
	ev := telemetrytest.KeyValues(s.Events()[0].Attributes)
	assert.Equal(t, "*errors.errorString", ev["exception.type"])
	assert.Len(t, ev["exception.message"], maxErrorMessage+3, "long messages are truncated")
}

func TestRecordRetry_LeavesStatus(t *testing.T) {
	p := telemetrytest.Install(t)

	ctx, span := Start(context.Background(), "portal")
	RecordRetry(ctx, nil, 1)
	RecordRetry(ctx, errors.New("timeout"), 1)
	span.End()

	s := p.Span(t, "portal")
	require.Equal(t, 1, telemetrytest.Exceptions(s))
	assert.Equal(t, codes.Unset, s.Status().Code, "a retried attempt does not fail the span")
	assert.Equal(t, "1", telemetrytest.KeyValues(s.Events()[0].Attributes)["attempt"])
}

func TestErrorLabel(t *testing.T) {
	assert.Empty(t, ErrorLabel(nil))
	assert.Equal(t, "canceled", ErrorLabel(fmt.Errorf("x: %w", context.Canceled)))
	assert.Equal(t, "deadline_exceeded", ErrorLabel(context.DeadlineExceeded))
	assert.Equal(t, "*errors.errorString", ErrorLabel(fmt.Errorf("a: %w", fmt.Errorf("b: %w", errors.New("c")))))
}

func TestStartClaimed(t *testing.T) {
	p := telemetrytest.Install(t)

	start := time.Now().Add(-time.Second)
	ctx, span := StartClaimed(context.Background(), "pkg.Worker.StepOne", start, "pkg.claim", attribute.Int(AttrAttempt, 2))
	_, workSpan := Start(ctx, "pkg.work")
	workSpan.End()
	span.End()

	step, claim, work := p.Span(t, "pkg.Worker.StepOne"), p.Span(t, "pkg.claim"), p.Span(t, "pkg.work")
	assert.Equal(t, start, step.StartTime(), "the step is back-dated to the claim")
	assert.Equal(t, start, claim.StartTime())
	telemetrytest.RequireChild(t, step, claim)
	telemetrytest.RequireChild(t, step, work)
	assert.Equal(t, "2", telemetrytest.KeyValues(claim.Attributes())["attempt"])
}

func TestWithParentSpan(t *testing.T) {
	p := telemetrytest.Install(t)

	rt := NewJobBoardTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	}))
	ctx, span := Start(context.Background(), "linkedin.Service.SearchJobs")
	// colly builds its requests without a context.
	req, err := http.NewRequest(http.MethodGet, "https://www.linkedin.com/jobs/search?keywords=go", nil)
	require.NoError(t, err)
	res, err := WithParentSpan(ctx, rt).RoundTrip(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	span.End()

	telemetrytest.RequireChild(t, p.Span(t, "linkedin.Service.SearchJobs"), p.Span(t, "linkedin search"))
	assert.Same(t, rt, WithParentSpan(context.Background(), rt), "without a span the transport is unchanged")
}

func TestRecordingTransport_RecordsTransportErrorsOnClientSpan(t *testing.T) {
	p := telemetrytest.Install(t)

	sentinel := errors.New("connection refused")
	rt := NewTransport(PeerOllama, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, sentinel
	}))
	ctx, span := Start(context.Background(), "ollama.Service.CreateEmbedding")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://ollama.invalid/api", nil)
	require.NoError(t, err)
	_, err = (&http.Client{Transport: rt}).Do(req) //nolint:bodyclose // no response on error
	span.End()
	require.ErrorIs(t, err, sentinel, "the error is returned unchanged")

	client := p.SpansOfKind(trace.SpanKindClient)
	require.Len(t, client, 1)
	assert.Equal(t, codes.Error, client[0].Status().Code, "the call where it failed")
	assert.Equal(t, 1, telemetrytest.Exceptions(client[0]))
	caller := p.Span(t, "ollama.Service.CreateEmbedding")
	assert.Equal(t, codes.Unset, caller.Status().Code)
	assert.Zero(t, telemetrytest.Exceptions(caller))
}

func TestParseSQL(t *testing.T) {
	for sql, want := range map[string][2]string{
		"UPDATE jobs SET status = $1 WHERE id = (SELECT id FROM jobs)": {"update", "jobs"},
		"\nINSERT INTO \"users\" (a) VALUES ($1)":                      {"insert", "users"},
		"delete from api_keys where id = $1":                           {"delete", "api_keys"},
		"SELECT count(*) FROM search_results sr JOIN jobs j":           {"select", "search_results"},
		"CREATE EXTENSION IF NOT EXISTS vector":                        {"create", ""},
		"":                                                             {"", ""},
	} {
		verb, table := parseSQL(sql)
		assert.Equal(t, want, [2]string{verb, table}, sql)
	}
}

func TestTableName(t *testing.T) {
	assert.Equal(t, "search_results", tableName("search_results sr"))
	assert.Equal(t, "users", tableName(`"users"`))
	assert.Empty(t, tableName(""))
}
