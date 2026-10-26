package api

import (
	"net/http"
	"strings"
)

// Problems accumulates issues. Handlers validate every field, then return
// p.Err() once; they never stop at the first invalid field. The zero value is
// ready.
type Problems struct {
	issues []Issue
}

// Add records one issue.
func (p *Problems) Add(field string, code Code, message string) {
	p.issues = append(p.issues, Issue{Field: field, Code: code, Message: message})
}

// Merge appends other's issues with field prefixed: Merge("limits", q) turns
// "maxValue" into "limits.maxValue" and "" into "limits".
func (p *Problems) Merge(prefix string, other *Problems) {
	if other == nil {
		return
	}
	for _, issue := range other.issues {
		issue.Field = joinPath(prefix, issue.Field)
		p.issues = append(p.issues, issue)
	}
}

// Empty reports whether no issue was recorded.
func (p *Problems) Empty() bool {
	return len(p.issues) == 0
}

// Len returns the number of recorded issues.
func (p *Problems) Len() int {
	return len(p.issues)
}

// Err is nil when empty, else a 422 *Error VALIDATION_FAILED "Request
// validation failed" with every issue in insertion order.
func (p *Problems) Err() error {
	if p.Empty() {
		return nil
	}
	failure := NewError(http.StatusUnprocessableEntity, CodeValidationFailed, "Request validation failed")
	failure.Issues = append(failure.Issues, p.issues...)
	return failure
}

func joinPath(prefix, field string) string {
	switch {
	case prefix == "":
		return field
	case field == "":
		return prefix
	case strings.HasPrefix(field, "["):
		return prefix + field
	}
	return prefix + "." + field
}
