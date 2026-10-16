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
)

func instrumented(ctx context.Context, fail error) (err error) {
	_, end := Start(ctx, "pkg.Service.Do", attribute.String(AttrUserID, "u1"))
	defer end(&err)
	return fail
}

func TestStart_SetsStatusWithoutException(t *testing.T) {
	p := telemetrytest.Install(t)

	require.NoError(t, instrumented(context.Background(), nil))
	require.Error(t, instrumented(context.Background(), fmt.Errorf("load: %w", errors.New("secret@example.com not found"))))

	spans := p.Spans()
	require.Len(t, spans, 2)
	assert.Equal(t, "pkg.Service.Do", spans[0].Name())
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
	assert.Equal(t, "u1", telemetrytest.KeyValues(spans[0].Attributes())["user.id"])

	assert.Equal(t, codes.Error, spans[1].Status().Code)
	assert.Equal(t, "*errors.errorString", spans[1].Status().Description, "the status never carries the message")
	assert.Zero(t, telemetrytest.Exceptions(spans[1]), "end does not record an exception")
}

func TestStartOrigin_RecordsOnceAndMarks(t *testing.T) {
	p := telemetrytest.Install(t)

	origin := func(ctx context.Context) (err error) {
		_, end := StartOrigin(ctx, "pkg.decode")
		defer end(&err)
		return errors.New("bad json")
	}
	ctx, end := Start(context.Background(), "pkg.Service.Call")
	err := origin(ctx)
	require.Error(t, err)
	assert.True(t, IsRecorded(err))
	assert.Equal(t, "bad json", err.Error(), "marking keeps the message")
	RecordError(ctx, err) // a handler further up
	end(&err)

	decode, call := p.Span(t, "pkg.decode"), p.Span(t, "pkg.Service.Call")
	assert.Equal(t, 1, telemetrytest.Exceptions(decode))
	assert.Zero(t, telemetrytest.Exceptions(call), "an error already recorded is not recorded again")
	assert.Equal(t, codes.Error, call.Status().Code)
}

func TestRecordError(t *testing.T) {
	p := telemetrytest.Install(t)

	ctx, end := Start(context.Background(), "handler")
	RecordError(ctx, nil)
	RecordError(ctx, errors.New(strings.Repeat("x", 2000)))
	end(nil)

	span := p.Span(t, "handler")
	require.Equal(t, 1, telemetrytest.Exceptions(span))
	assert.Equal(t, codes.Error, span.Status().Code)
	ev := telemetrytest.KeyValues(span.Events()[0].Attributes)
	assert.Equal(t, "*errors.errorString", ev["exception.type"])
	assert.Len(t, ev["exception.message"], maxErrorMessage+3, "long messages are truncated")
}

func TestMarkRecorded_KeepsErrorChain(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := MarkRecorded(fmt.Errorf("wrap: %w", sentinel))
	assert.ErrorIs(t, err, sentinel)
	assert.Same(t, err, MarkRecorded(err), "marking twice is a no-op")
	assert.True(t, IsRecorded(fmt.Errorf("outer: %w", err)))
	assert.Nil(t, MarkRecorded(nil))
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
	ctx, end := StartClaimed(context.Background(), "pkg.Worker.StepOne", start, "pkg.claim", attribute.Int(AttrAttempt, 2))
	_, endWork := Start(ctx, "pkg.work")
	endWork(nil)
	end(nil)

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
	ctx, end := Start(context.Background(), "linkedin.Service.SearchJobs")
	// colly builds its requests without a context.
	req, err := http.NewRequest(http.MethodGet, "https://www.linkedin.com/jobs/search?keywords=go", nil)
	require.NoError(t, err)
	res, err := WithParentSpan(ctx, rt).RoundTrip(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	end(nil)

	telemetrytest.RequireChild(t, p.Span(t, "linkedin.Service.SearchJobs"), p.Span(t, "linkedin search"))
	assert.Same(t, rt, WithParentSpan(context.Background(), rt), "without a span the transport is unchanged")
}

func TestRecordingTransport_MarksTransportErrors(t *testing.T) {
	telemetrytest.Install(t)

	rt := NewTransport(PeerOllama, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}))
	req, err := http.NewRequest(http.MethodGet, "http://ollama.invalid/api", nil)
	require.NoError(t, err)
	_, err = (&http.Client{Transport: rt}).Do(req) //nolint:bodyclose // no response on error
	require.Error(t, err)
	assert.True(t, IsRecorded(err), "otelhttp recorded it on the client span")
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
