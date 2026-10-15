// Package telemetry wires OpenTelemetry tracing and metrics for the service
// and provides small helpers to measure background runs and calls to
// external APIs.
//
// Setup installs the global providers. Every other helper in this package
// reads the global providers at call time, so it is safe to use before Setup
// runs (the data is simply dropped) and in tests (nothing is exported unless
// a test installs its own SDK providers).
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cv-evaluator/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// ServiceName is reported as service.name and used as the instrumentation scope.
const ServiceName = "cv-evaluator"

const instrumentationName = "cv-evaluator/internal/telemetry"

// Setup installs the global TracerProvider, MeterProvider and propagator.
//
// When cfg.Telemetry is not enabled (OTEL_EXPORTER_OTLP_ENDPOINT unset or
// OTEL_SDK_DISABLED=true), no-op providers are installed and nothing is
// exported. Otherwise traces and metrics are exported over OTLP/HTTP; the
// exporters honour the standard OTEL_EXPORTER_OTLP_* environment variables.
//
// The returned shutdown flushes and stops the providers; call it before exit.
func Setup(ctx context.Context, cfg *config.Config) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	if !cfg.Telemetry.Enabled() {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		return func(context.Context) error { return nil }, nil
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		_ = traceExporter.Shutdown(ctx)
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

func newResource(ctx context.Context, cfg *config.Config) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName(ServiceName),
			semconv.ServiceVersion(cfg.Telemetry.ServiceVersion),
			semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
			// Older backends still key environments on the pre-1.27 name.
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}
	return res, nil
}

// flushTimeout bounds how long Flush waits for pending telemetry to export.
const flushTimeout = 5 * time.Second

// Flush calls shutdown with a short timeout and logs any error. It is meant
// to be deferred right after Setup:
//
//	shutdown, err := telemetry.Setup(ctx, cfg)
//	if err != nil {
//		return err
//	}
//	defer telemetry.Flush(shutdown)
func Flush(shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		slog.Warn("flush telemetry", "error", err)
	}
}
