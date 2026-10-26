// Package semver implements the X.Y.Z and ^X.Y.Z subset used by Gatbase manifests.
package semver

import (
	"errors"
	"strconv"
	"strings"
)

// Version is MAJOR.MINOR.PATCH.
type Version struct {
	Major, Minor, Patch uint64
}

// Range is an exact version or a caret range.
type Range struct {
	base  Version
	caret bool
}

var (
	errFormat   = errors.New("version must be MAJOR.MINOR.PATCH")
	errNumber   = errors.New("version part must be a non-negative integer without leading zeros")
	errOverflow = errors.New("version part does not fit in 64 bits")
)

// ParseVersion parses X.Y.Z without leading zeros. Nothing else is accepted:
// no prefix, pre-release, build metadata or whitespace.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, errFormat
	}
	var numbers [3]uint64
	for i, part := range parts {
		n, err := parsePart(part)
		if err != nil {
			return Version{}, err
		}
		numbers[i] = n
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, nil
}

func parsePart(part string) (uint64, error) {
	if part == "" {
		return 0, errNumber
	}
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return 0, errNumber
		}
	}
	if len(part) > 1 && part[0] == '0' {
		return 0, errNumber
	}
	n, err := strconv.ParseUint(part, 10, 64)
	if err != nil {
		return 0, errOverflow
	}
	return n, nil
}

// String returns MAJOR.MINOR.PATCH.
func (v Version) String() string {
	return strconv.FormatUint(v.Major, 10) + "." +
		strconv.FormatUint(v.Minor, 10) + "." +
		strconv.FormatUint(v.Patch, 10)
}

// Compare returns -1, 0 or 1 when v is lower than, equal to or higher than other.
func (v Version) Compare(other Version) int {
	if c := compareUint(v.Major, other.Major); c != 0 {
		return c
	}
	if c := compareUint(v.Minor, other.Minor); c != 0 {
		return c
	}
	return compareUint(v.Patch, other.Patch)
}

func compareUint(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ParseRange parses X.Y.Z (exact) or ^X.Y.Z (caret).
func ParseRange(s string) (Range, error) {
	caret := strings.HasPrefix(s, "^")
	v, err := ParseVersion(strings.TrimPrefix(s, "^"))
	if err != nil {
		return Range{}, err
	}
	return Range{base: v, caret: caret}, nil
}

// Base returns the version written in the range.
func (r Range) Base() Version {
	return r.base
}

// String returns the range in the form ParseRange accepts.
func (r Range) String() string {
	if r.caret {
		return "^" + r.base.String()
	}
	return r.base.String()
}

// Contains reports whether v is inside r. A caret range follows npm: it keeps
// the leftmost non-zero part fixed.
func (r Range) Contains(v Version) bool {
	if v.Compare(r.base) < 0 {
		return false
	}
	if !r.caret {
		return v == r.base
	}
	switch {
	case r.base.Major >= 1:
		return v.Major == r.base.Major
	case r.base.Minor >= 1:
		return v.Major == 0 && v.Minor == r.base.Minor
	default:
		return v == r.base
	}
}
