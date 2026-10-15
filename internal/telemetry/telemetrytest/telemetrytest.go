// Package telemetrytest installs in-memory OpenTelemetry providers so tests
// can assert on the spans and metrics that code under test produces.
package telemetrytest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Providers gives access to what was recorded while installed.
type Providers struct {
	reader *sdkmetric.ManualReader
	spans  *tracetest.SpanRecorder
}

// Install replaces the global TracerProvider and MeterProvider with
// in-memory SDK providers and restores the previous ones when the test ends.
// Tests that call it must not run in parallel with each other.
//
// Instrumented HTTP clients capture the tracer when they are built, so call
// Install before constructing the code under test.
func Install(t testing.TB) *Providers {
	t.Helper()
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()

	p := &Providers{
		reader: sdkmetric.NewManualReader(),
		spans:  tracetest.NewSpanRecorder(),
	}
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(p.reader)))
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p.spans)))

	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})
	return p
}

// Spans returns the spans that have ended, in end order.
func (p *Providers) Spans() []sdktrace.ReadOnlySpan {
	return p.spans.Ended()
}

// Histogram returns the data points of the named float64 histogram, failing
// the test when it was not recorded.
func (p *Providers) Histogram(t testing.TB, name string) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, p.reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			require.Equal(t, "s", m.Unit, "%s unit", name)
			h, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "%s is not a float64 histogram", name)
			return h.DataPoints
		}
	}
	require.FailNow(t, "metric not recorded", name)
	return nil
}

// Attrs flattens an attribute set (a data point's or a resource's) to strings.
func Attrs(set attribute.Set) map[string]string {
	return KeyValues(set.ToSlice())
}

// KeyValues flattens attributes (for example a span's) to strings.
func KeyValues(kvs []attribute.KeyValue) map[string]string {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}
