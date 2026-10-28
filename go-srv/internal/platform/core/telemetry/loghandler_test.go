package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/ctxkey"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/reqctx"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/telemetry"
)

func newLogger(buf *bytes.Buffer) *slog.Logger {
	inner := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(telemetry.NewLogHandler(inner, "svc-test"))
}

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(strings.SplitN(buf.String(), "\n", 2)[0])
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return record
}

func principalContext(ctx context.Context) (context.Context, reqctx.TenantID) {
	tenant := reqctx.TenantID("0a1b2c3d-0000-4000-8000-abcdef012345")
	principal := reqctx.Principal{Kind: reqctx.ActorAccount, ActorID: "actor-1", TenantID: &tenant}
	return context.WithValue(ctx, ctxkey.Principal, principal), tenant
}

func TestLogHandlerAddsServiceOnlyWithoutSpanOrPrincipal(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf).InfoContext(context.Background(), "event_happened")
	record := decodeLine(t, &buf)
	if record["service"] != "svc-test" {
		t.Fatalf("service = %v", record["service"])
	}
	for _, key := range []string{"trace_id", "span_id", "tenant_id", "actor_id"} {
		if _, found := record[key]; found {
			t.Fatalf("%s fabricated without a span or principal: %v", key, record)
		}
	}
}

func TestLogHandlerAddsTraceAndPrincipal(t *testing.T) {
	ctx, span := newTelemetry(t).StartJob(context.Background(), "job", validParent)
	defer span.End()
	ctx, tenant := principalContext(ctx)

	var buf bytes.Buffer
	newLogger(&buf).InfoContext(ctx, "event_happened")
	record := decodeLine(t, &buf)
	want := map[string]string{
		"trace_id":  validTraceID,
		"span_id":   telemetry.SpanID(ctx),
		"tenant_id": string(tenant),
		"actor_id":  "actor-1",
	}
	for key, value := range want {
		if record[key] != value {
			t.Fatalf("%s = %v, want %s", key, record[key], value)
		}
	}
}

func TestLogHandlerKeepsContextFieldsTopLevelInsideGroups(t *testing.T) {
	ctx, span := newTelemetry(t).StartJob(context.Background(), "job", validParent)
	defer span.End()

	var buf bytes.Buffer
	logger := newLogger(&buf).With("component", "billing").WithGroup("req")
	logger.InfoContext(ctx, "event_happened", "size", 3)
	record := decodeLine(t, &buf)
	if record["trace_id"] != validTraceID || record["service"] != "svc-test" || record["component"] != "billing" {
		t.Fatalf("top-level fields wrong: %v", record)
	}
	group, ok := record["req"].(map[string]any)
	if !ok || group["size"] != float64(3) {
		t.Fatalf("group fields wrong: %v", record)
	}
}

func TestLogHandlerNeverLogsPrincipalFieldsOfOtherRequests(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf)
	ctxA, _ := principalContext(context.Background())
	logger.InfoContext(ctxA, "first")
	buf.Reset()
	logger.InfoContext(context.Background(), "second")
	record := decodeLine(t, &buf)
	if _, found := record["actor_id"]; found {
		t.Fatalf("actor_id leaked into a record without a principal: %v", record)
	}
}
