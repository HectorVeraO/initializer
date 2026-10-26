// Package ctxkey holds the context keys of core. Go's internal-package rule
// makes it importable only from packages under core/, so a module cannot
// install a principal or a trace id by itself.
package ctxkey

import "context"

// Key is the type of every core context key.
type Key int

const (
	// Principal holds the reqctx.Principal the guard installed.
	Principal Key = iota + 1
	// TraceID holds the request's trace id as a string.
	TraceID
)

// WithTraceID returns ctx carrying the trace id.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceID, traceID)
}

// TraceIDFrom returns the trace id stored in ctx, if any.
func TraceIDFrom(ctx context.Context) (string, bool) {
	traceID, ok := ctx.Value(TraceID).(string)
	return traceID, ok
}
