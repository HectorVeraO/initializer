// Package ports finds and rewrites host port tokens in declared port files.
package ports

import (
	"errors"
	"fmt"
)

// Bounds of the host port band: band*100+99 must stay a valid TCP port.
const (
	// MinBand is the lowest band a token may carry (10000).
	MinBand = 100
	// MaxBand is the highest band a token may carry (65499).
	MaxBand = 654
	// DefaultBand is the band of the reference output.
	DefaultBand = 101
)

// tokenWidth is the number of digits of a port token.
const tokenWidth = 5

// Port is one host port a module declares.
type Port struct {
	// Name is a label unique in the catalog ("srv", "postgres", "kratos-public").
	Name string `json:"name"`
	// Offset is the port's offset inside the band, 0-98.
	Offset int `json:"offset"`
	// Origin marks the public origin port of its stack.
	Origin bool `json:"origin,omitempty"`
	// Stride is the distance between the ports of two components of one
	// stack: the host offset of the component at index i is Offset + i*Stride.
	// A shared port has no stride.
	Stride int `json:"stride,omitempty"`
	// Shared marks a port every component of the stack uses: the host offset
	// is Offset for every index (the shared database server).
	Shared bool `json:"shared,omitempty"`
}

// OffsetAt returns the host offset of the port for the component at index.
func (p Port) OffsetAt(index int) int {
	if p.Shared {
		return p.Offset
	}
	return p.Offset + index*p.Stride
}

// Token is one port token: five ASCII digits with no ASCII digit directly before or after.
type Token struct {
	// Start and End are byte offsets of the token in the scanned source.
	Start, End int
	// Line is the 1-based line of the token.
	Line int
	// Band is the first three digits (101 for 10132); Offset the last two (32).
	Band, Offset int
}

// ValidBand reports whether band is a band a project may use.
func ValidBand(band int) bool {
	return band >= MinBand && band <= MaxBand
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// Scan returns every port token of src in source order.
func Scan(src []byte) []Token {
	var tokens []Token
	line := 1
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if !isDigit(c) {
			i++
			continue
		}
		end := i
		for end < len(src) && isDigit(src[end]) {
			end++
		}
		if end-i == tokenWidth {
			value := 0
			for _, digit := range src[i:end] {
				value = value*10 + int(digit-'0')
			}
			tokens = append(tokens, Token{Start: i, End: end, Line: line, Band: value / 100, Offset: value % 100})
		}
		i = end
	}
	return tokens
}

// Rewrite returns src with every token's band replaced by to and its offset mapped
// through offsets (an offset absent from offsets keeps its value). It returns src
// unchanged when from == to and offsets is empty. It returns an error when a token's
// band is not from, a band is outside 100-654, or a mapped offset is outside 0-99.
func Rewrite(src []byte, from, to int, offsets map[int]int) ([]byte, error) {
	if !ValidBand(from) || !ValidBand(to) {
		return nil, fmt.Errorf("band %d to %d: both must be from %d to %d", from, to, MinBand, MaxBand)
	}
	if from == to && len(offsets) == 0 {
		return src, nil
	}
	out := make([]byte, len(src))
	copy(out, src)
	var failures []error
	for _, token := range Scan(src) {
		if token.Band != from {
			failures = append(failures, fmt.Errorf("line %d: token %s has band %d, not %d", token.Line, src[token.Start:token.End], token.Band, from))
			continue
		}
		offset := token.Offset
		if mapped, found := offsets[offset]; found {
			offset = mapped
		}
		if offset < 0 || offset > 99 {
			failures = append(failures, fmt.Errorf("line %d: token %s maps to offset %d, outside 0-99", token.Line, src[token.Start:token.End], offset))
			continue
		}
		value := to*100 + offset
		for at := token.End - 1; at >= token.Start; at-- {
			out[at] = byte('0' + value%10)
			value /= 10
		}
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return out, nil
}
