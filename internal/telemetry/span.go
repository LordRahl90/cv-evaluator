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

// Span is an internal span started by Start. Its methods are safe to call
// on a nil *Span.
type Span struct {
	span trace.Span
}

// Start starts an internal span named name as a child of the span in ctx and
// returns a context carrying it with a handle to end it. Instrument a
// function with two lines at its top:
//
//	func (s *Service) DeleteCV(ctx context.Context, userID, cvID ulid.ULID) error {
//		ctx, span := telemetry.Start(ctx, "users.Service.DeleteCV", telemetry.ID(telemetry.AttrCVID, cvID))
//		defer span.End()
//		res := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", cvID, userID).Delete(&models.CV{})
//		if res.Error != nil {
//			return res.Error // the database span recorded it: pass it up as it is
//		}
//		if res.RowsAffected == 0 {
//			return span.RecordError(ErrCVNotFound) // detected here: recorded here
//		}
//		return nil
//	}
//
// Span names are "<package>.<Type>.<Method>" for methods and
// "<package>.<func>" for functions.
//
// Attributes follow the rule above: ids and counts only.
//
// Error rule: record an error where it happens, once. A function calls
// span.RecordError only for a failure it creates or first detects (a
// validation or decoding failure, an unexpected response); errors from a
// callee are returned unchanged, because the callee's span (or the database
// or HTTP client span, for external failures) has recorded them already.
// Spans that only pass an error up stay neutral. The request span turns red
// from its HTTP 5xx status, and a background run span from its outcome.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, *Span) {
	ctx, span := tracer().Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	return ctx, &Span{span: span}
}

// StartClaimed starts a span named name for a polling step that claimed a
// row: the claim ran before the span existed, so the many polls that find
// nothing create no trace at all. The span is back-dated to start (when the
// claim began) and gets a child span claimName covering the claim, so the
// trace still shows claim, work and write in order. End the returned span
// once the step is done.
func StartClaimed(ctx context.Context, name string, start time.Time, claimName string, attrs ...attribute.KeyValue) (context.Context, *Span) {
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
	return ctx, &Span{span: span}
}

// End ends the span.
func (s *Span) End() {
	if s != nil {
		s.span.End()
	}
}

// RecordError records err on the span, where the failure happened: it sets
// the status to Error with a short label (see ErrorLabel, never the
// message) and adds one exception event. It returns err unchanged, so it
// fits on a return line; a nil err does nothing. The message is stored
// truncated to 512 bytes: errors whose text could carry user data (CV text,
// emails, names, LLM output) or secrets must be scrubbed first.
func (s *Span) RecordError(err error) error {
	if s != nil {
		recordError(s.span, err)
	}
	return err
}

// SetAttributes adds attributes (ids and counts only) to the span, for
// values known only after it started, such as row counts.
func (s *Span) SetAttributes(attrs ...attribute.KeyValue) {
	if s != nil {
		s.span.SetAttributes(attrs...)
	}
}

// RecordError records err on the span in ctx like Span.RecordError, for code
// that handles an error and has no handle of its own: the HTTP 5xx path on
// the request span, or the pipeline scheduling a row for retry, marking it
// failed or giving up on a portal. A nil err does nothing.
func RecordError(ctx context.Context, err error) {
	recordError(trace.SpanFromContext(ctx), err)
}

// RecordRetry records a failed attempt that will be retried as an exception
// event with the attempt number, without setting the span status: a later
// attempt may still succeed. A nil err does nothing.
func RecordRetry(ctx context.Context, err error, attempt int) {
	if err != nil {
		addException(trace.SpanFromContext(ctx), err, attribute.Int(AttrAttempt, attempt))
	}
}

func recordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	addException(span, err)
	setErrorStatus(span, err)
}

func setErrorStatus(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, ErrorLabel(err))
	}
}

// maxErrorMessage bounds the exception message stored on a span.
const maxErrorMessage = 512

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
