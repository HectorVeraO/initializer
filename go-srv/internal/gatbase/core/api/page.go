package api

import (
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultPageSize = 20
	defaultMaxSize  = 100
	maxPageNumber   = math.MaxInt32
)

// Page is the data shape of every list response.
type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalItems int64 `json:"totalItems"`
}

// NewPage builds a Page; nil items become [].
func NewPage[T any](items []T, q PageQuery, totalItems int64) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Page: q.Page, Size: q.Size, TotalItems: totalItems}
}

// PageSpec is a list route's paging contract.
type PageSpec struct {
	// DefaultSize is 20 when 0.
	DefaultSize int
	// MaxSize is 100 when 0.
	MaxSize int
	// Sorts lists the allowed sort fields as camelCase wire names.
	Sorts []string
	// DefaultSort is "<field>" or "-<field>"; its field must be in Sorts.
	DefaultSort string
}

// PageQuery is the parsed paging request.
type PageQuery struct {
	Page      int
	Size      int
	SortField string
	SortDesc  bool
}

// Offset is (Page-1)*Size.
func (q PageQuery) Offset() int {
	return (q.Page - 1) * q.Size
}

func splitSort(s string) (string, bool) {
	field, isDesc := strings.CutPrefix(s, "-")
	return field, isDesc
}

// ParsePageQuery reads ?page, ?size and ?sort ("<field>" or "-<field>"). Problems
// go to p: a non-integer page or size FIELD_TYPE_INVALID; page < 1 or size
// outside 1..MaxSize FIELD_OUT_OF_RANGE; an unknown sort field FIELD_NOT_ALLOWED.
// A spec whose DefaultSort field is not in Sorts is a programmer error and panics.
func ParsePageQuery(r *http.Request, p *Problems, spec PageSpec) PageQuery {
	if spec.DefaultSize == 0 {
		spec.DefaultSize = defaultPageSize
	}
	if spec.MaxSize == 0 {
		spec.MaxSize = defaultMaxSize
	}
	q := PageQuery{Page: 1, Size: spec.DefaultSize}
	q.SortField, q.SortDesc = splitSort(spec.DefaultSort)
	if spec.DefaultSort != "" && !slices.Contains(spec.Sorts, q.SortField) {
		panic("api: PageSpec.DefaultSort field is not in Sorts")
	}

	query := r.URL.Query()
	if page, ok := parsePageInt(p, query.Get("page"), "page", 1, maxPageNumber); ok {
		q.Page = page
	}
	if size, ok := parsePageInt(p, query.Get("size"), "size", 1, spec.MaxSize); ok {
		q.Size = size
	}
	if sort := query.Get("sort"); sort != "" {
		field, isDesc := splitSort(sort)
		if slices.Contains(spec.Sorts, field) {
			q.SortField, q.SortDesc = field, isDesc
		} else {
			p.Add("sort", CodeFieldNotAllowed, "Must be one of: "+strings.Join(spec.Sorts, ", "))
		}
	}
	return q
}

// parsePageInt returns ok false when the parameter is absent or invalid.
func parsePageInt(p *Problems, raw, name string, min, max int) (int, bool) {
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		p.Add(name, CodeFieldTypeInvalid, "Must be an integer")
		return 0, false
	}
	if err != nil || v < min || v > max {
		p.Add(name, CodeFieldOutOfRange, "Must be between "+strconv.Itoa(min)+" and "+strconv.Itoa(max))
		return 0, false
	}
	return v, true
}
