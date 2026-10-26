// Package telemetry builds the OpenTelemetry tracer provider and propagator, the
// request-tracing middleware, the job-context starter and the correlating log handler.
package telemetry

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config of Setup.
type Config struct {
	ServiceName string // OTEL_SERVICE_NAME
}

// Telemetry owns the tracer provider and the propagator of one process.
type Telemetry struct {
	provider   *sdktrace.TracerProvider
	propagator propagation.TextMapPropagator
}

// Setup builds an SDK TracerProvider (always, even with no exporter, so a valid
// trace id exists without an incoming traceparent), sampler
// ParentBased(AlwaysSample), resource service.name, and the composite W3C
// TraceContext + Baggage propagator. It sets no OTel globals.
func Setup(ctx context.Context, cfg Config) (*Telemetry, error) {
	if ctx == nil {
		return nil, errors.New("telemetry: Setup requires a context")
	}
	if cfg.ServiceName == "" {
		return nil, errors.New("telemetry: Config.ServiceName is required")
	}
	res := sdkresource.NewSchemaless(attribute.String("service.name", cfg.ServiceName))
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithResource(res),
	)
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	t := &Telemetry{provider: provider, propagator: propagator}
	return t, nil
}

// TracerProvider returns the provider modules build tracers from.
func (t *Telemetry) TracerProvider() trace.TracerProvider {
	return t.provider
}

// Propagator returns the W3C TraceContext + Baggage propagator.
func (t *Telemetry) Propagator() propagation.TextMapPropagator {
	return t.propagator
}

// Tracer returns a named tracer of the provider.
func (t *Telemetry) Tracer(name string) trace.Tracer {
	return t.provider.Tracer(name)
}

// Transport wraps base (nil: http.DefaultTransport) with otelhttp.NewTransport
// bound to this provider and propagator, so outgoing calls carry traceparent.
func (t *Telemetry) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base,
		otelhttp.WithTracerProvider(t.provider),
		otelhttp.WithPropagators(t.propagator),
	)
}

// Shutdown flushes and stops the provider.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	return t.provider.Shutdown(ctx)
}

// TraceID returns the 32-hex trace id of ctx's span, or "" when none.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanID returns the 16-hex span id of ctx's span, or "" when none.
func SpanID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasSpanID() {
		return ""
	}
	return sc.SpanID().String()
}
