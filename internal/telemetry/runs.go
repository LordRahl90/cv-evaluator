package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// StartRun starts measuring one background run of the given kind (for
// example "scrape.linkedin" or "pipeline.match"). It starts a span named
// "background_run <kind>" and returns a context carrying it; call end exactly
// once with the run's error (nil on success) to finish the span and record
// cv_evaluator.background_run.duration.
//
//	ctx, end := telemetry.StartRun(ctx, "scrape.linkedin")
//	err := doRun(ctx)
//	end(err)
func StartRun(ctx context.Context, kind string) (context.Context, func(err error)) {
	start := time.Now()
	ctx, span := tracer().Start(ctx, "background_run "+kind,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String(AttrRunKind, kind)),
	)
	return ctx, func(err error) {
		finishRun(ctx, span, kind, time.Since(start), err)
	}
}

// RecordRun records a run that has already finished, for callers that
// measure the duration themselves. The span is back-dated to cover the run.
func RecordRun(ctx context.Context, kind string, duration time.Duration, err error) {
	end := time.Now()
	ctx, span := tracer().Start(ctx, "background_run "+kind,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithTimestamp(end.Add(-duration)),
		trace.WithAttributes(attribute.String(AttrRunKind, kind)),
	)
	finishRun(ctx, span, kind, duration, err, trace.WithTimestamp(end))
}

func finishRun(ctx context.Context, span trace.Span, kind string, duration time.Duration, err error, opts ...trace.SpanEndOption) {
	out := outcome(err)
	span.SetAttributes(attribute.String(AttrRunOutcome, out))
	// Status only, from the run's outcome: the error is recorded as an
	// exception where it happened (see Start).
	setErrorStatus(span, err)
	span.End(opts...)

	if h := getInstruments().runDuration; h != nil {
		h.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String(AttrRunKind, kind),
			attribute.String(AttrRunOutcome, out),
		))
	}
}

type runSpanKey struct{}

type runSpan struct {
	kind string
	span trace.Span
}

// RunObserver adapts observer-style run hooks to telemetry. It has no state,
// so the zero value is ready to use. It implements both the context-aware
// RunStarted/RunFinished pair (internal/pipeline.RunObserver) and the
// context-free OnRunStart/OnRunEnd pair:
//
//	pipeline.Config{..., Observer: telemetry.RunObserver{}}
type RunObserver struct{}

// RunStarted starts the run span and returns a context carrying it, so work
// done by the run nests under it.
func (RunObserver) RunStarted(ctx context.Context, kind string) context.Context {
	ctx, span := tracer().Start(ctx, "background_run "+kind,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String(AttrRunKind, kind)),
	)
	return context.WithValue(ctx, runSpanKey{}, runSpan{kind: kind, span: span})
}

// RunFinished ends the span started by RunStarted and records
// cv_evaluator.background_run.duration with the measured duration. Given a
// context that RunStarted did not return, it records a back-dated span.
func (RunObserver) RunFinished(ctx context.Context, kind string, duration time.Duration, err error) {
	if rs, ok := ctx.Value(runSpanKey{}).(runSpan); ok && rs.kind == kind {
		finishRun(ctx, rs.span, kind, duration, err)
		return
	}
	RecordRun(ctx, kind, duration, err)
}

// OnRunStart is part of the context-free observer contract. Nothing is
// recorded at start: OnRunEnd emits the span and metric from the duration.
func (RunObserver) OnRunStart(string) {}

// OnRunEnd records the finished run as a back-dated span and a
// cv_evaluator.background_run.duration sample.
func (RunObserver) OnRunEnd(kind string, duration time.Duration, err error) {
	RecordRun(context.Background(), kind, duration, err)
}
