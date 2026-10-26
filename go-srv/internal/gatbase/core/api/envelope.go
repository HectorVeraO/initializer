// Package api is the wire format of every /api/v1 response: the envelope,
// errors and their code registry, accumulated validation, body decoding,
// pagination and the Writer that stamps responses.
package api

import (
	"encoding/json"
	"net/http"
)

// Envelope is ApiEnvelope<T>: Data is null on failure, Error is null on success.
// Handlers never build it; the Writer does. Tests decode responses into it.
type Envelope[T any] struct {
	Data  *T     `json:"data"`
	Meta  Meta   `json:"meta"`
	Error *Error `json:"error"`
}

// Meta is the response metadata.
type Meta struct {
	// TraceID is 32 lowercase hex characters, never all zeros.
	TraceID string `json:"traceId"`
	// Timestamp is RFC 3339 UTC in whole seconds ending in "Z".
	Timestamp string `json:"timestamp"`
}

// Code is a SCREAMING_SNAKE error or issue code; part of the contract.
type Code string

// Error is the wire error object and a Go error. Status and the cause are
// never serialized.
type Error struct {
	Status  int     `json:"-"`
	Code    Code    `json:"code"`
	Message string  `json:"message"`
	Issues  []Issue `json:"issues"`
	cause   error
}

// Issue is one validation problem. Field is a JSON path into the request body
// ("name", "scopes[1]", "limits.maxValue") or a query parameter name; ""
// means the whole body.
type Issue struct {
	Field   string `json:"field"`
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

// NewError builds an Error with an empty issue list.
func NewError(status int, code Code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message, Issues: []Issue{}}
}

// Wrap is NewError that keeps cause for the log line of Writer.Fail; the wire
// shows message only.
func Wrap(cause error, status int, code Code, message string) *Error {
	wrapped := NewError(status, code, message)
	wrapped.cause = cause
	return wrapped
}

// Error returns "<CODE>: <message>".
func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}

// Unwrap returns the cause given to Wrap, or nil.
func (e *Error) Unwrap() error {
	return e.cause
}

// MarshalJSON writes issues as [] when there are none.
func (e Error) MarshalJSON() ([]byte, error) {
	type wire Error
	out := wire(e)
	if out.Issues == nil {
		out.Issues = []Issue{}
	}
	return json.Marshal(out)
}

// NotFound is 404 RESOURCE_NOT_FOUND "Resource not found": the single answer
// for a missing, malformed or foreign :id or key (tenant isolation).
func NotFound() *Error {
	return NewError(http.StatusNotFound, CodeResourceNotFound, "Resource not found")
}
