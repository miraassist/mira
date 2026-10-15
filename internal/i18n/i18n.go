// Package i18n holds the languages Mira speaks and what all packages share
// about them: the language code and how a point in time is written.
//
// The texts themselves live next to the code that uses them, one table per
// language in each package (prompt, conversation, tools, watchers, ...), so a
// prompt and its wording stay in one place. Every package has a test that
// fails when a language lacks a text.
//
// The language is prompt.language in the config. It is the language of
// everything the model reads and of everything Mira says or writes on her
// own; the web UI picks its language from the browser.
package i18n

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

type Lang string

const (
	DE Lang = "de"
	EN Lang = "en"
)

// Default is the language when none is configured.
const Default = DE

// All lists the supported languages.
var All = []Lang{DE, EN}

// Parse accepts "de", "en" and regional forms such as "de-AT" or "en_GB".
func Parse(code string) (Lang, error) {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		return Default, nil
	}
	if i := strings.IndexAny(c, "-_"); i > 0 {
		c = c[:i]
	}
	for _, l := range All {
		if string(l) == c {
			return l, nil
		}
	}
	return "", fmt.Errorf("unsupported language %q (supported: de, en)", code)
}

// Of is Parse for values that were validated already; anything unknown is
// the default language.
func Of(code string) Lang {
	l, err := Parse(code)
	if err != nil {
		return Default
	}
	return l
}

// Pick returns the entry of a per-language table, falling back to the
// default language.
func Pick[T any](table map[Lang]T, l Lang) T {
	if v, ok := table[l]; ok {
		return v
	}
	return table[Default]
}

var weekdays = map[Lang][7]string{
	DE: {"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	EN: {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
}

var months = map[Lang][12]string{
	DE: {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	EN: {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
}

// FormatTime writes a timestamp the way the model should read it back:
// "Mittwoch, 14. Oktober 2026, 22:15 Uhr" or "Wednesday, 14 October 2026, 22:15".
func FormatTime(l Lang, t time.Time) string {
	wd, mo := Pick(weekdays, l)[t.Weekday()], Pick(months, l)[t.Month()-1]
	if l == EN {
		return fmt.Sprintf("%s, %d %s %d, %02d:%02d", wd, t.Day(), mo, t.Year(), t.Hour(), t.Minute())
	}
	return fmt.Sprintf("%s, %d. %s %d, %02d:%02d Uhr", wd, t.Day(), mo, t.Year(), t.Hour(), t.Minute())
}

// Missing lists, for a per-language table of text structs, every language
// that lacks the table or one of its texts (an empty string field or an
// empty list), as "en.fieldName". Each package's tests call it on their
// table, so a text added in one language cannot be forgotten in the other.
func Missing[T any](table map[Lang]T) []string {
	var out []string
	for _, l := range All {
		v, ok := table[l]
		if !ok {
			out = append(out, string(l))
			continue
		}
		rv := reflect.ValueOf(v)
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Field(i)
			if (f.Kind() == reflect.String || f.Kind() == reflect.Slice) && f.Len() == 0 {
				out = append(out, string(l)+"."+rv.Type().Field(i).Name)
			}
		}
	}
	return out
}

// FormatDate writes a day: "Donnerstag, 22. Oktober 2026" or
// "Thursday, 22 October 2026".
func FormatDate(l Lang, t time.Time) string {
	wd, mo := Pick(weekdays, l)[t.Weekday()], Pick(months, l)[t.Month()-1]
	if l == EN {
		return fmt.Sprintf("%s, %d %s %d", wd, t.Day(), mo, t.Year())
	}
	return fmt.Sprintf("%s, %d. %s %d", wd, t.Day(), mo, t.Year())
}
