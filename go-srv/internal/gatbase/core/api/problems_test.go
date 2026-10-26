package api_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/core/api"
)

func TestEmptyProblemsErrIsNil(t *testing.T) {
	var p api.Problems
	if p.Err() != nil || !p.Empty() || p.Len() != 0 {
		t.Fatal("zero Problems must be empty with nil Err")
	}
}

func TestProblemsKeepInsertionOrderInOne422(t *testing.T) {
	var p api.Problems
	p.Add("c", api.CodeFieldRequired, "c")
	p.Add("a", api.CodeFieldTooLong, "a")
	p.Add("b", api.CodeFieldTypeInvalid, "b")

	var apiErr *api.Error
	if !errors.As(p.Err(), &apiErr) {
		t.Fatal("Err is not *api.Error")
	}
	if apiErr.Status != http.StatusUnprocessableEntity || apiErr.Code != api.CodeValidationFailed || apiErr.Message != "Request validation failed" {
		t.Fatalf("error = %+v", apiErr)
	}
	fields := ""
	for _, issue := range apiErr.Issues {
		fields += issue.Field
	}
	if fields != "cab" || p.Len() != 3 {
		t.Fatalf("fields = %q", fields)
	}
}

func TestMergePrefixesPaths(t *testing.T) {
	var inner api.Problems
	inner.Add("maxValue", api.CodeFieldOutOfRange, "m")
	inner.Add("", api.CodeFieldRequired, "whole")
	inner.Add("[0]", api.CodeFieldTypeInvalid, "idx")
	var outer api.Problems
	outer.Merge("limits", &inner)
	outer.Merge("ignored", nil)

	var apiErr *api.Error
	errors.As(outer.Err(), &apiErr)
	want := []string{"limits.maxValue", "limits", "limits[0]"}
	if len(apiErr.Issues) != len(want) {
		t.Fatalf("issues = %v", apiErr.Issues)
	}
	for i := range want {
		if apiErr.Issues[i].Field != want[i] {
			t.Errorf("issue %d field = %q, want %q", i, apiErr.Issues[i].Field, want[i])
		}
	}
}

func TestErrIssuesAreACopy(t *testing.T) {
	var p api.Problems
	p.Add("a", api.CodeFieldRequired, "a")
	var apiErr *api.Error
	errors.As(p.Err(), &apiErr)
	apiErr.Issues[0].Field = "mutated"
	var again *api.Error
	errors.As(p.Err(), &again)
	if again.Issues[0].Field != "a" {
		t.Fatal("Err shares the issue slice")
	}
}
