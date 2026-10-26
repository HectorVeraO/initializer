package telemetry

import (
	"context"
	"log/slog"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/reqctx"
)

// op is one WithAttrs or WithGroup call. The ops are kept so context attributes
// can be added at the top level of a record whose handler sits inside a group.
type op struct {
	group string
	attrs []slog.Attr
}

type logHandler struct {
	root     slog.Handler
	ops      []op
	built    slog.Handler
	hasGroup bool
}

// NewLogHandler wraps inner (the stdlib JSON handler) and adds service, plus
// trace_id and span_id while a span is active (never fabricated) and tenant_id,
// actor_id when a principal is in the context.
func NewLogHandler(inner slog.Handler, service string) slog.Handler {
	if inner == nil {
		panic("telemetry: NewLogHandler requires an inner handler")
	}
	root := inner.WithAttrs([]slog.Attr{slog.String("service", service)})
	return &logHandler{root: root, built: root}
}

func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.built.Enabled(ctx, level)
}

func (h *logHandler) Handle(ctx context.Context, rec slog.Record) error {
	attrs := contextAttrs(ctx)
	if len(attrs) == 0 {
		return h.built.Handle(ctx, rec)
	}
	if !h.hasGroup {
		rec.AddAttrs(attrs...)
		return h.built.Handle(ctx, rec)
	}
	target := h.root.WithAttrs(attrs)
	for _, o := range h.ops {
		target = applyOp(target, o)
	}
	return target.Handle(ctx, rec)
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.extend(op{attrs: attrs})
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.extend(op{group: name})
}

func (h *logHandler) extend(next op) *logHandler {
	ops := make([]op, 0, len(h.ops)+1)
	ops = append(ops, h.ops...)
	ops = append(ops, next)
	return &logHandler{
		root:     h.root,
		ops:      ops,
		built:    applyOp(h.built, next),
		hasGroup: h.hasGroup || next.group != "",
	}
}

func applyOp(base slog.Handler, o op) slog.Handler {
	if o.group != "" {
		return base.WithGroup(o.group)
	}
	return base.WithAttrs(o.attrs)
}

func contextAttrs(ctx context.Context) []slog.Attr {
	var attrs []slog.Attr
	if traceID := TraceID(ctx); traceID != "" {
		attrs = append(attrs, slog.String("trace_id", traceID))
	}
	if spanID := SpanID(ctx); spanID != "" {
		attrs = append(attrs, slog.String("span_id", spanID))
	}
	principal, ok := reqctx.PrincipalFrom(ctx)
	if !ok {
		return attrs
	}
	if principal.TenantID != nil {
		attrs = append(attrs, slog.String("tenant_id", string(*principal.TenantID)))
	}
	if principal.ActorID != "" {
		attrs = append(attrs, slog.String("actor_id", principal.ActorID))
	}
	return attrs
}
