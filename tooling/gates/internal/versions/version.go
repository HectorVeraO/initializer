package versions

import (
	"fmt"
	"regexp"
	"strconv"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type semver [3]uint64

func parseVersion(text string) (semver, error) {
	var parsed semver
	groups := versionPattern.FindStringSubmatch(text)
	if groups == nil {
		return parsed, fmt.Errorf("version %q is not X.Y.Z (non-negative integers, no leading zeros)", text)
	}
	for i := range parsed {
		number, err := strconv.ParseUint(groups[i+1], 10, 63)
		if err != nil {
			return parsed, fmt.Errorf("version %q has a part that is too large", text)
		}
		parsed[i] = number
	}
	return parsed, nil
}

func (a semver) compare(b semver) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// CompareVersions orders two X.Y.Z versions numerically, part by part: it
// returns -1, 0 or 1 when a is lower than, equal to or higher than b. It
// returns an error when either text is not a valid version.
func CompareVersions(a, b string) (int, error) {
	left, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	return left.compare(right), nil
}
