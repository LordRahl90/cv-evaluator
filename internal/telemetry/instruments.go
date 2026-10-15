package telemetry

import (
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Metric names.
const (
	MetricBackgroundRunDuration   = "cv_evaluator.background_run.duration"
	MetricExternalRequestDuration = "cv_evaluator.external_request.duration"
)

// Attribute keys and values.
const (
	AttrRunKind     = "run.kind"
	AttrRunOutcome  = "run.outcome"
	AttrPeerService = "peer.service"
	AttrOperation   = "operation"
	AttrOutcome     = "outcome"
	// AttrLLMTask distinguishes what an LLM call was for (job_match, cv_cleanup).
	AttrLLMTask = "llm.task"

	OutcomeSuccess = "success"
	OutcomeError   = "error"
)

// durationBuckets suits operations that take from milliseconds (HTTP calls)
// up to tens of minutes (LLM calls, scraping runs).
var durationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800,
}

type instruments struct {
	provider        metric.MeterProvider
	runDuration     metric.Float64Histogram
	externalRequest metric.Float64Histogram
}

var (
	instMu  sync.Mutex
	current *instruments
)

// getInstruments returns the histograms for the current global
// MeterProvider, creating them again whenever the provider changes (Setup in
// production, a fresh SDK provider in each test).
func getInstruments() *instruments {
	mp := otel.GetMeterProvider()

	instMu.Lock()
	defer instMu.Unlock()
	if current != nil && current.provider == mp {
		return current
	}

	meter := mp.Meter(instrumentationName)
	runDuration, err := meter.Float64Histogram(MetricBackgroundRunDuration,
		metric.WithDescription("Duration of background runs (scrapes, pipeline steps)."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		slog.Warn("create background run histogram", "error", err)
	}
	externalRequest, err := meter.Float64Histogram(MetricExternalRequestDuration,
		metric.WithDescription("Duration of requests to external APIs (LLMs, job boards)."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		slog.Warn("create external request histogram", "error", err)
	}

	current = &instruments{
		provider:        mp,
		runDuration:     runDuration,
		externalRequest: externalRequest,
	}
	return current
}

func tracer() trace.Tracer {
	return otel.Tracer(instrumentationName)
}

func outcome(err error) string {
	if err != nil {
		return OutcomeError
	}
	return OutcomeSuccess
}
