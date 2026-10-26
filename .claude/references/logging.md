# Logging

Passive reference, reached from root `AGENTS.md` "Project overview" (log message convention). It is the
normative contract every service's logging follows; subproject `AGENTS.md` files point here instead of
restating it.

---

## Normative summary

1. **The logger is the only stdout writer.** No `console.*` (Node), `print()` (Python) or equivalent in
   application code. Existing calls are removed incrementally, never shimmed. A CLI's own result output (what
   the command prints for its caller) is not logging.
2. **One logger per file or component, bound once.** The binding supplies `component`; call sites never repeat it.
      - Go: `log := logger.With(slog.String("component", "<module>"))` on the `*slog.Logger` that `cmd/server/main.go` builds and passes down; never a package-level logger.
3. **The message is constant; variables are fields.** The event name is the message itself or a constant
   event field, as root `AGENTS.md` "Log message convention" states. Never build the message from variables
   (`'order ' + id + ' saved'`, template literals, f-strings).
4. **Field names are snake_case and reused across services.** Same thing, same name, everywhere
   (`request_id`, `job_id`, `duration_ms`, `status_code`, `count`).
5. **Errors travel as the error object, under `err`, logged once.** Log where the error is handled; do not log
   and rethrow at every layer.
6. **Levels.**
   - `error` - it failed and someone should look.
   - `warn` - handled but abnormal: fallback taken, retry, bad input from outside.
   - `info` - one line per completed unit of work (request, job, message processed).
   - `debug` - code-path detail, never expected in production stdout.
7. **Never log secrets, tokens or PII** (message text, names, emails, phones, national ids). Log ids, counts
   and sizes. No request or response bodies above `debug`.

## What the logger adds

The logger adds `time`, `level`, `service`, `component` and, when a trace span is active,
`trace_id`/`span_id`. Call sites never write those fields; they write the event name and their own fields.

## The PII boundary

The platform may strip what the **transport** attaches on its own - headers, request configuration, and
error response bodies attached by an HTTP client - using an allow-list without inspecting content.
Everything the **caller** writes (the message, the fields, a hand-built `err`) travels as written: rule 7
is enforced by developers and code review, not by the logger.

**No runtime scan of free-form strings for emails, phones or ids exists, and none is to be added.**

## JSON line shape

One JSON object per line:

| Key | Required | Value |
|---|---|---|
| `time` | yes | ISO-8601 UTC ending in `Z` |
| `level` | yes | `debug`, `info`, `warn`, `error` |
| `msg` | yes | a constant string: the event name, or a fixed short text when the event has its own key |
| `service` | yes | the service name |
| `component` | when bound | from the per-file binding |
| `trace_id`, `span_id` | optional | only while a span is active; never fabricated |
| `err` | optional | `{ name, message, stack }` plus `code`, `status_code`, `cause` when present |
| any other key | optional | the caller's fields, top level, snake_case |
