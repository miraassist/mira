// Package excerpt cuts an input for an error message or a log line
// (docs/spec/fallbacks.md, decision 5: an error names its input): the start
// of a text that did not parse, a server's answer, a model's answer.
package excerpt

import (
	"strings"
	"unicode/utf8"
)

// Max is how much of an input an error carries.
const Max = 300

// Of is the start of s, up to Max characters, cut with "…", on one line;
// "(empty)" for an empty one, so an error never ends in nothing.
func Of(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "(empty)"
	}
	if utf8.RuneCountInString(s) <= Max {
		return s
	}
	r := []rune(s)
	return string(r[:Max]) + "…"
}

// Bytes is Of for a body.
func Bytes(b []byte) string { return Of(string(b)) }
