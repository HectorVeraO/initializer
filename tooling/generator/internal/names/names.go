// Package names holds the naming grammar shared by the selection, the plan and
// the lock. It imports no other package of the generator.
package names

import "fmt"

const (
	minLabelLength = 2
	maxLabelLength = 63
	maxDigitRun    = 4
)

// ValidLabel reports whether s is a label: the project, component and service
// grammar ^[a-z][a-z0-9-]*[a-z0-9]$, 2-63 bytes, without five or more digits in
// a row.
func ValidLabel(s string) bool {
	if len(s) < minLabelLength || len(s) > maxLabelLength {
		return false
	}
	digitRun := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		switch {
		case i == 0 && !isLower:
			return false
		case i == len(s)-1 && !isLower && !isDigit:
			return false
		case !isLower && !isDigit && c != '-':
			return false
		}
		if isDigit {
			digitRun++
		} else {
			digitRun = 0
		}
		if digitRun > maxDigitRun {
			return false
		}
	}
	return true
}

// LabelMessage returns the NAME_INVALID message for a value that fails the
// label grammar.
func LabelMessage(s string) string {
	return fmt.Sprintf("%q must be lowercase letters, digits and inner hyphens, 2-63 characters, starting with a letter, without five digits in a row", s)
}

// IsDeviceName reports whether s is a Windows device name: con, prn, aux, nul,
// com0-com9 or lpt0-lpt9, compared to the whole value.
func IsDeviceName(s string) bool {
	switch s {
	case "con", "prn", "aux", "nul":
		return true
	}
	if len(s) != 4 {
		return false
	}
	prefix, digit := s[:3], s[3]
	isPort := prefix == "com" || prefix == "lpt"
	isDigit := digit >= '0' && digit <= '9'
	return isPort && isDigit
}

// maxCloseDistance is the largest edit distance Closest accepts.
const maxCloseDistance = 2

// Closest returns the candidate within edit distance 2 of word, the nearest
// first and the earliest on a tie, and "" when none qualifies. An identical
// candidate is not a close match.
func Closest(word string, candidates []string) string {
	best, bestDistance := "", maxCloseDistance+1
	for _, candidate := range candidates {
		distance := editDistance(word, candidate)
		if distance > 0 && distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

// editDistance returns the Levenshtein distance between a and b in runes.
func editDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

// ValidPGIdent reports whether s is a lowercase unquoted SQL identifier of at
// most 63 bytes: ^[a-z_][a-z0-9_]*$ without five or more digits in a row.
func ValidPGIdent(s string) bool {
	if len(s) < 1 || len(s) > maxLabelLength {
		return false
	}
	digitRun := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		if !isLower && !isDigit && c != '_' {
			return false
		}
		if i == 0 && isDigit {
			return false
		}
		if isDigit {
			digitRun++
		} else {
			digitRun = 0
		}
		if digitRun > maxDigitRun {
			return false
		}
	}
	return true
}

// PGIdentMessage returns the NAME_INVALID message for a value that fails the
// pgident grammar.
func PGIdentMessage(s string) string {
	return fmt.Sprintf("%q must be lowercase letters, digits and underscores, 1-63 characters, starting with a letter or underscore", s)
}
