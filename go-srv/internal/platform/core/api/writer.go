package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/clock"
	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core/internal/ctxkey"
)

const zeroTraceID = "00000000000000000000000000000000"

// Writer writes envelopes, stamping meta from the request context and its clock.
type Writer struct {
	clock  clock.Clock
	logger *slog.Logger
}

// NewWriter builds a Writer; a nil clock or logger is a programmer error and panics.
func NewWriter(c clock.Clock, logger *slog.Logger) *Writer {
	if c == nil || logger == nil {
		panic("api: NewWriter requires a clock and a logger")
	}
	return &Writer{clock: c, logger: logger}
}

// Data writes status with {data, meta, error: null}. A nil data writes
// "data": null. The body is marshalled before any header is written; a
// marshalling failure becomes a 500 through Fail.
func (w *Writer) Data(rw http.ResponseWriter, r *http.Request, status int, data any) {
	if status < 200 || status > 299 {
		w.Fail(rw, r, errors.New("api: Data called with a non-2xx status"))
		return
	}
	meta := w.meta(r)
	envelope := Envelope[any]{Meta: meta}
	if data != nil {
		envelope.Data = &data
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		w.Fail(rw, r, err)
		return
	}
	writeBody(rw, status, meta.TraceID, body)
}

// Fail writes err. An *Error (found with errors.As) is written as is, and a
// 5xx *Error logs its cause at ERROR ("request_failed"). Any other error is
// logged with ErrorContext ("request_failed", attribute "err") and answered
// 500 INTERNAL_ERROR "Internal error". Never an internal message, SQL text or
// stack.
func (w *Writer) Fail(rw http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	isWireError := errors.As(err, &apiErr) && apiErr != nil && apiErr.Status >= 400 && apiErr.Status <= 599
	if !isWireError {
		w.logger.ErrorContext(r.Context(), "request_failed", slog.Any("err", err))
		apiErr = NewError(http.StatusInternalServerError, CodeInternalError, "Internal error")
	} else if apiErr.Status >= 500 {
		w.logger.ErrorContext(r.Context(), "request_failed",
			slog.Int("status_code", apiErr.Status),
			slog.String("code", string(apiErr.Code)),
			slog.Any("err", apiErr.Unwrap()))
	}
	meta := w.meta(r)
	body, marshalErr := json.Marshal(Envelope[any]{Meta: meta, Error: apiErr})
	if marshalErr != nil {
		meta.TraceID = zeroTraceID
		body = []byte(`{"data":null,"meta":{"traceId":"` + zeroTraceID + `","timestamp":"` + w.timestamp() + `"},"error":{"code":"INTERNAL_ERROR","message":"Internal error","issues":[]}}`)
		apiErr = NewError(http.StatusInternalServerError, CodeInternalError, "Internal error")
	}
	writeBody(rw, apiErr.Status, meta.TraceID, body)
}

func writeBody(rw http.ResponseWriter, status int, traceID string, body []byte) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("X-Trace-Id", traceID)
	rw.WriteHeader(status)
	// The status line is already sent; a failed write has no recovery.
	_, _ = rw.Write(body)
}

func (w *Writer) timestamp() string {
	return w.clock.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func (w *Writer) meta(r *http.Request) Meta {
	traceID, ok := ctxkey.TraceIDFrom(r.Context())
	if !ok || !validTraceID(traceID) {
		w.logger.WarnContext(r.Context(), "trace_context_missing")
		traceID = zeroTraceID
	}
	return Meta{TraceID: traceID, Timestamp: w.timestamp()}
}

func validTraceID(s string) bool {
	if len(s) != 32 || s == zeroTraceID {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}
