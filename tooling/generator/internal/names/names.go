// Package names holds the naming grammar shared by the selection, the plan and
// the lock. It imports no other package of the generator.
package names

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
