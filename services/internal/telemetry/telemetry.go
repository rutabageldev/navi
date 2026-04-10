// Package telemetry initialises the OpenTelemetry SDK and provides helpers for
// trace context propagation over NATS.
package telemetry

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// Config holds the configuration required to initialise the OTEL SDK.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
}

// InitTracer initialises the OTEL TracerProvider and MeterProvider, configures
// OTLP gRPC exporters pointing at cfg.OTLPEndpoint, registers both as globals,
// and sets the W3C TraceContext propagator. The returned shutdown function must
// be called on service exit.
func InitTracer(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating otel resource: %w", err)
	}

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		if shutErr := tp.Shutdown(ctx); shutErr != nil {
			return nil, fmt.Errorf("creating metric exporter: %w; trace provider shutdown: %w", err, shutErr)
		}
		return nil, fmt.Errorf("creating metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		tpErr := tp.Shutdown(ctx)
		mpErr := mp.Shutdown(ctx)
		if tpErr != nil && mpErr != nil {
			return fmt.Errorf("trace provider shutdown: %w; metric provider shutdown: %w", tpErr, mpErr)
		}
		if tpErr != nil {
			return fmt.Errorf("trace provider shutdown: %w", tpErr)
		}
		if mpErr != nil {
			return fmt.Errorf("metric provider shutdown: %w", mpErr)
		}
		return nil
	}

	return shutdown, nil
}

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// natsHeaderCarrier adapts a nats.Header to the propagation.TextMapCarrier interface.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string {
	vals := nats.Header(c)[key]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (c natsHeaderCarrier) Set(key, value string) {
	nats.Header(c)[key] = []string{value}
}

func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// ExtractNATSTraceContext extracts W3C traceparent/tracestate from a NATS
// message header into a context.
func ExtractNATSTraceContext(headers nats.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(context.Background(), natsHeaderCarrier(headers))
}

// InjectNATSTraceContext injects the active span context from ctx into a NATS
// message header as W3C traceparent/tracestate.
func InjectNATSTraceContext(ctx context.Context, headers nats.Header) {
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(headers))
}
