package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End(opts...)

	if h := getInstruments().runDuration; h != nil {
		h.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String(AttrRunKind, kind),
			attribute.String(AttrRunOutcome, out),
		))
	}
}

// RunObserver adapts observer-style run hooks to telemetry. It has no state,
// so the zero value is ready to use:
//
//	pipeline.New(..., pipeline.WithObserver(telemetry.RunObserver{}))
type RunObserver struct{}

// OnRunStart is part of the observer contract. Nothing is recorded at start:
// the span and metric are emitted by OnRunEnd from the measured duration.
func (RunObserver) OnRunStart(string) {}

// OnRunEnd records the finished run as a back-dated span and a
// cv_evaluator.background_run.duration sample.
func (RunObserver) OnRunEnd(kind string, duration time.Duration, err error) {
	RecordRun(context.Background(), kind, duration, err)
}
