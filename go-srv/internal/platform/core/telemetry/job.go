package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const traceparentKey = "traceparent"

// StartJob continues traceparent when it is a valid W3C value, else opens a
// new trace, and starts a span called name. Async jobs call it at dequeue.
func (t *Telemetry) StartJob(ctx context.Context, name, traceparent string) (context.Context, trace.Span) {
	// A clean base keeps a span already in ctx from being mistaken for the remote parent.
	base := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	carrier := propagation.MapCarrier{traceparentKey: traceparent}
	extracted := propagation.TraceContext{}.Extract(base, carrier)
	remote := trace.SpanContextFromContext(extracted)
	isValidRemote := remote.IsValid() && remote.IsRemote()
	tracer := t.provider.Tracer("platform/core/telemetry")
	if isValidRemote {
		return tracer.Start(extracted, name)
	}
	return tracer.Start(base, name, trace.WithNewRoot())
}
