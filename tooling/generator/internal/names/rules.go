package names

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Rule is one name rule: a context-anchored text with placeholders.
type Rule struct {
	Path string
	Text string
}

// Occurrence is one match of a rule's reference expansion in a source file.
type Occurrence struct {
	// Start and End are byte offsets, End exclusive.
	Start, End int
	// Rule is the index of the rule in the slice passed to Find.
	Rule int
}

// token is one piece of a rule text: a literal or a slot placeholder.
type token struct {
	text   string
	isSlot bool
}

// tokenize splits text into literals and placeholders. "%%{" is the literal
// "%{", any other "%" is literal, and an unterminated "%{" is an error.
func tokenize(text string) ([]token, error) {
	var tokens []token
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			tokens = append(tokens, token{text: literal.String()})
			literal.Reset()
		}
	}
	for i := 0; i < len(text); {
		rest := text[i:]
		switch {
		case strings.HasPrefix(rest, "%%{"):
			literal.WriteString("%{")
			i += len("%%{")
		case strings.HasPrefix(rest, "%{"):
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				return nil, errors.New(`unterminated "%{"`)
			}
			flush()
			tokens = append(tokens, token{text: rest[len("%{"):end], isSlot: true})
			i += end + 1
		default:
			literal.WriteByte(text[i])
			i++
		}
	}
	flush()
	return tokens, nil
}

// Slots returns the slots a rule text uses, sorted and deduplicated, or an
// error for bad placeholder syntax, an unknown slot, no placeholder, or an
// empty literal part. A per-stack slot accepts any label as its stack: the
// caller checks the stack against its table.
func Slots(text string) ([]string, error) {
	tokens, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	hasContext := false
	for _, t := range tokens {
		if !t.isSlot {
			hasContext = hasContext || strings.Trim(t.text, " \t") != ""
			continue
		}
		if !isSlotName(t.text, ValidLabel) {
			return nil, fmt.Errorf("unknown slot %q", t.text)
		}
		seen[t.text] = true
	}
	if len(seen) == 0 {
		return nil, errors.New("text has no placeholder")
	}
	if !hasContext {
		return nil, errors.New("text has no literal context besides spaces and tabs")
	}
	slots := make([]string, 0, len(seen))
	for slot := range seen {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	return slots, nil
}

// Expand replaces every placeholder of text by its value and every "%%{" by
// "%{". It returns an error when values lacks a used slot.
func Expand(text string, values map[string]string) (string, error) {
	tokens, err := tokenize(text)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, t := range tokens {
		if !t.isSlot {
			out.WriteString(t.text)
			continue
		}
		value, found := values[t.text]
		if !found {
			return "", fmt.Errorf("no value for slot %q", t.text)
		}
		out.WriteString(value)
	}
	return out.String(), nil
}

// expandAll returns the expansion of every rule.
func expandAll(rules []Rule, values map[string]string) ([]string, error) {
	expansions := make([]string, len(rules))
	for i, rule := range rules {
		expanded, err := Expand(rule.Text, values)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.Text, err)
		}
		if expanded == "" {
			return nil, fmt.Errorf("rule %q: expansion is empty", rule.Text)
		}
		expansions[i] = expanded
	}
	return expansions, nil
}

// Find returns every occurrence of every rule's reference expansion in src,
// sorted by Start. When two occurrences overlap it also returns an error that
// names the first overlap; the occurrences are still returned, so a caller can
// report them. Occurrences of one rule that overlap each other count.
func Find(src []byte, rules []Rule, reference map[string]string) ([]Occurrence, error) {
	expansions, err := expandAll(rules, reference)
	if err != nil {
		return nil, err
	}
	var found []Occurrence
	for index, expansion := range expansions {
		needle := []byte(expansion)
		for from := 0; from < len(src); {
			at := bytes.Index(src[from:], needle)
			if at < 0 {
				break
			}
			start := from + at
			found = append(found, Occurrence{Start: start, End: start + len(needle), Rule: index})
			from = start + 1
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Start != found[j].Start {
			return found[i].Start < found[j].Start
		}
		if found[i].End != found[j].End {
			return found[i].End < found[j].End
		}
		return found[i].Rule < found[j].Rule
	})
	for i := 1; i < len(found); i++ {
		previous := furthestEnd(found[:i])
		if found[i].Start < previous.End {
			return found, fmt.Errorf("rule %q at byte %d overlaps rule %q at byte %d",
				rules[found[i].Rule].Text, found[i].Start, rules[previous.Rule].Text, previous.Start)
		}
	}
	return found, nil
}

// furthestEnd returns the occurrence of occurrences that ends last.
func furthestEnd(occurrences []Occurrence) Occurrence {
	best := occurrences[0]
	for _, occurrence := range occurrences[1:] {
		if occurrence.End > best.End {
			best = occurrence
		}
	}
	return best
}

// Apply rewrites src in one left-to-right pass: every occurrence of
// Expand(rule.Text, from) becomes Expand(rule.Text, to). Bytes outside
// occurrences are copied unchanged. When every from equals its to, the
// result equals src.
func Apply(src []byte, rules []Rule, from, to map[string]string) ([]byte, error) {
	occurrences, err := Find(src, rules, from)
	if err != nil {
		return nil, err
	}
	if len(occurrences) == 0 {
		return src, nil
	}
	replacements := make([]string, len(rules))
	for i, rule := range rules {
		expanded, err := Expand(rule.Text, to)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", rule.Text, err)
		}
		replacements[i] = expanded
	}
	out := make([]byte, 0, len(src))
	cursor := 0
	for _, occurrence := range occurrences {
		out = append(out, src[cursor:occurrence.Start]...)
		out = append(out, replacements[occurrence.Rule]...)
		cursor = occurrence.End
	}
	out = append(out, src[cursor:]...)
	return out, nil
}
