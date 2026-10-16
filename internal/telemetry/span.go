package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Span attribute keys for the ids and counts recorded on internal spans.
//
// Rule for span attributes: ids, counts, enums and status values only. Never
// record names, emails, phone numbers, CV text, job descriptions, prompts,
// LLM output, passwords, tokens, API keys or SQL values: traces are kept
// longer and seen by more people than the database, and they must not become
// a second copy of user data.
const (
	AttrUserID      = "user.id"
	AttrCVID        = "cv.id"
	AttrJobSearchID = "job_search.id"
	AttrJobID       = "job.id"
	AttrResultID    = "search_result.id"
	AttrAPIKeyID    = "api_key.id"
	AttrProvider    = "provider"
	AttrAuthMethod  = "auth.method"
	AttrAttempt     = "attempt"
	AttrStatus      = "status"
	AttrCount       = "count"
	AttrRowsClaimed = "rows.claimed"
)

// Start starts an internal span named name as a child of the span in ctx and
// returns a context carrying it with a function that ends it. Instrument a
// function with two lines at its top, using a named err result to capture
// the error (callers are unaffected), or end(nil) when there is none:
//
//	func (s *Service) DeleteCV(ctx context.Context, userID, cvID ulid.ULID) (err error) {
//		ctx, end := telemetry.Start(ctx, "users.Service.DeleteCV", telemetry.ID(telemetry.AttrCVID, cvID))
//		defer end(&err)
//		...
//	}
//
// Span names are "<package>.<Type>.<Method>" for methods and
// "<package>.<func>" for functions.
//
// Attributes follow the rule above: ids and counts only.
//
// Error convention: an error travels up through many spans, and recording it
// on each would show the same exception N times in one trace. So end only
// sets the span status to Error, with a short label (see ErrorLabel) rather
// than the message, when *errp is not nil. The error itself is recorded as
// an exception event once, with RecordError, either where it originates (a
// database statement, an outbound HTTP call) or where it is handled and
// turned into an outcome (the HTTP 5xx path, a portal failure the pipeline
// logs and skips, a row marked for retry or failed).
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(errp *error)) {
	ctx, span := tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	return ctx, func(errp *error) {
		if errp != nil {
			setErrorStatus(span, *errp)
		}
		span.End()
	}
}

// StartOrigin is Start for a function where errors originate rather than
// pass through (extracting a CV, decoding an LLM response): on failure end
// also records the error as an exception event and replaces *errp with the
// error marked by MarkRecorded, so callers up the stack do not record it
// again. The error message must not carry user data.
func StartOrigin(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(errp *error)) {
	ctx, span := tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	return ctx, func(errp *error) {
		if errp != nil && *errp != nil {
			if !IsRecorded(*errp) {
				addException(span, *errp)
				*errp = MarkRecorded(*errp)
			}
			setErrorStatus(span, *errp)
		}
		span.End()
	}
}

// StartClaimed starts a span named name for a polling step that claimed a
// row: the claim ran before the span existed, so the many polls that find
// nothing create no trace at all. The span is back-dated to start (when the
// claim began) and gets a child span claimName covering the claim, so the
// trace still shows claim, work and write in order. Call end once the step
// is done (a nil errp leaves the status unset).
func StartClaimed(ctx context.Context, name string, start time.Time, claimName string, attrs ...attribute.KeyValue) (context.Context, func(errp *error)) {
	ctx, span := tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
	_, claim := tracer().Start(ctx, claimName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
	claim.End()
	return ctx, func(errp *error) {
		if errp != nil {
			setErrorStatus(span, *errp)
		}
		span.End()
	}
}

func setErrorStatus(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, ErrorLabel(err))
	}
}

// maxErrorMessage bounds the exception message RecordError stores.
const maxErrorMessage = 512

// RecordError records err as an exception event on the span in ctx and sets
// that span's status to Error. Call it once per error, where it is handled
// (see Start); errors recorded where they originated (by the GORM plugin or
// an instrumented transport, see MarkRecorded) only set the status, so an
// error shows up as one exception per trace. The message is stored truncated
// to 512 bytes: wrap errors that could carry user data (CV text, emails,
// names, LLM output) before recording them. A nil err does nothing.
func RecordError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !IsRecorded(err) {
		addException(span, err)
	}
	setErrorStatus(span, err)
}

// RecordRetry records a failed attempt that will be retried, like
// RecordError but without setting the span status: a later attempt may
// still succeed, and if none does the caller's end sets the status. The
// event carries the attempt number. An error already recorded where it
// originated is not recorded again.
func RecordRetry(ctx context.Context, err error, attempt int) {
	if err == nil || IsRecorded(err) {
		return
	}
	addException(trace.SpanFromContext(ctx), err, attribute.Int(AttrAttempt, attempt))
}

func addException(span trace.Span, err error, extra ...attribute.KeyValue) {
	msg := err.Error()
	if len(msg) > maxErrorMessage {
		msg = msg[:maxErrorMessage] + "..."
	}
	// The event span.RecordError would add, with the message bounded and
	// the type of the innermost error rather than of a wrapper.
	attrs := append([]attribute.KeyValue{
		attribute.String("exception.type", ErrorLabel(err)),
		attribute.String("exception.message", msg),
	}, extra...)
	span.AddEvent("exception", trace.WithAttributes(attrs...))
}

// recordedError marks an error already recorded as an exception event.
type recordedError struct{ err error }

func (e *recordedError) Error() string { return e.err.Error() }
func (e *recordedError) Unwrap() error { return e.err }

// MarkRecorded returns err wrapped so that RecordError knows it has already
// been recorded (errors.Is and errors.As still see err). Code that records
// an error where it originates returns the marked error.
func MarkRecorded(err error) error {
	if err == nil || IsRecorded(err) {
		return err
	}
	return &recordedError{err: err}
}

// IsRecorded reports whether err, or an error it wraps, was marked by
// MarkRecorded.
func IsRecorded(err error) bool {
	var r *recordedError
	return errors.As(err, &r)
}

// ErrorLabel returns a short description of err that is safe to put on a
// span status: "canceled" or "deadline_exceeded" for context errors,
// otherwise the Go type of the innermost wrapped error (for example
// "*pgconn.PgError" or "*errors.errorString"). It never includes the message.
func ErrorLabel(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	}
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return fmt.Sprintf("%T", err)
		}
		err = next
	}
}

// SetAttributes adds attributes (ids and counts only) to the span in ctx, for
// values known only after the span started, such as row counts or the user
// an API request authenticated as.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

// ID returns an attribute holding an id's string form (a ulid.ULID, for
// example). It takes a fmt.Stringer so this package need not know id types.
func ID(key string, id fmt.Stringer) attribute.KeyValue {
	return attribute.String(key, id.String())
}

// Count returns an int attribute, for counts and attempt numbers.
func Count(key string, n int) attribute.KeyValue {
	return attribute.Int(key, n)
}
