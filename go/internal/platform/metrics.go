package platform

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const meterName = "github.com/atipongsena/erp-town/go"

// Metric names, exactly as exported to Prometheus.
const (
	MetricEventsInvalid         = "town_events_invalid_total"
	MetricEventsDropped         = "town_events_dropped_total"
	MetricEventsStale           = "town_events_stale_total"
	MetricOutboxPublishFailures = "town_outbox_publish_failures_total"
)

// Metrics holds the town's OpenTelemetry counters.
type Metrics struct {
	EventsInvalid, EventsDropped, EventsStale, OutboxPublishFailures metric.Int64Counter
}

// NewMetrics creates the counters on a meter provider exported through a private
// Prometheus registry and returns the registry's scrape handler (mounted at
// /town/metrics). Instrument names already end in _total, so the exporter adds no
// suffixes and the exposed names are exactly the names above.
func NewMetrics() (*Metrics, http.Handler, error) {
	reg := prometheus.NewRegistry()
	exp, err := otelprom.New(
		otelprom.WithRegisterer(reg),
		otelprom.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithoutSuffixes),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("metrics: prometheus exporter: %w", err)
	}
	m, err := newMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp)))
	if err != nil {
		return nil, nil, err
	}
	return m, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), nil
}

// NewTestMetrics returns counters backed by a manual reader and a function that reads
// a counter's current total by its exported name (0 when never incremented).
func NewTestMetrics() (*Metrics, func(name string) int64) {
	reader := sdkmetric.NewManualReader()
	m, err := newMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		panic(err) // instrument creation on the SDK provider cannot fail for these names
	}
	read := func(name string) int64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			panic(err)
		}
		var total int64
		for _, sm := range rm.ScopeMetrics {
			for _, mt := range sm.Metrics {
				if mt.Name != name {
					continue
				}
				if sum, ok := mt.Data.(metricdata.Sum[int64]); ok {
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
				}
			}
		}
		return total
	}
	return m, read
}

func newMetrics(mp metric.MeterProvider) (*Metrics, error) {
	meter := mp.Meter(meterName)
	var m Metrics
	for _, c := range []struct {
		dst        *metric.Int64Counter
		name, desc string
	}{
		{&m.EventsInvalid, MetricEventsInvalid, "Stream entries acked without handling because they failed the event schema."},
		{&m.EventsDropped, MetricEventsDropped, "Stream entries acked without success after the maximum number of deliveries."},
		{&m.EventsStale, MetricEventsStale, "Unique events skipped because their subject sequence was already applied."},
		{&m.OutboxPublishFailures, MetricOutboxPublishFailures, "Failed attempts to publish a town_outbox row to the stream."},
	} {
		ctr, err := meter.Int64Counter(c.name, metric.WithDescription(c.desc))
		if err != nil {
			return nil, fmt.Errorf("metrics: counter %s: %w", c.name, err)
		}
		*c.dst = ctr
	}
	return &m, nil
}
