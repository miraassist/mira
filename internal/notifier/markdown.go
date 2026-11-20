package notifier

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	address = regexp.MustCompile(`https?://\S+`)
	// linkTarget is the (address) of a link [text](address): Telegram takes
	// it as it is, so nothing in it is escaped or counted (checked on the
	// test HA, 2026-11-20).
	linkTarget = regexp.MustCompile(`\]\([^)\s]*\)`)
)

// TelegramMarkdown makes text parse as Telegram's first Markdown, which HA's
// telegram_bot sends with (docs/spec/channels.md, Telegram's Markdown): it
// escapes what would open an entity that never ends and keeps balanced
// Markdown as it is; the target of a link stays untouched. In an address
// every _ and *; a _ or * between two
// letters or digits; then a sign (_ * `) still odd in number everywhere; a
// [ that no ]( follows.
func TelegramMarkdown(text string) string {
	r := []rune(text)
	esc := make([]bool, len(r))
	keep := make([]bool, len(r))
	runes := func(loc []int) (int, int) { return len([]rune(text[:loc[0]])), len([]rune(text[:loc[1]])) }
	for _, loc := range linkTarget.FindAllStringIndex(text, -1) {
		from, to := runes(loc)
		for i := from + 2; i < to-1; i++ {
			keep[i] = true
		}
	}
	for _, loc := range address.FindAllStringIndex(text, -1) {
		from, to := runes(loc)
		for i := from; i < to; i++ {
			if (r[i] == '_' || r[i] == '*') && !keep[i] {
				esc[i] = true
			}
		}
	}
	word := func(i int) bool { return i >= 0 && i < len(r) && (unicode.IsLetter(r[i]) || unicode.IsDigit(r[i])) }
	for i, c := range r {
		if (c == '_' || c == '*') && word(i-1) && word(i+1) && !escaped(r, i) && !keep[i] {
			esc[i] = true
		}
	}
	for _, sign := range []rune{'_', '*', '`'} {
		n := 0
		for i, c := range r {
			if c == sign && !esc[i] && !keep[i] && !escaped(r, i) {
				n++
			}
		}
		if n%2 == 1 {
			for i, c := range r {
				if c == sign && !escaped(r, i) && !keep[i] {
					esc[i] = true
				}
			}
		}
	}
	for i, c := range r {
		if c == '[' && !escaped(r, i) && !strings.Contains(string(r[i:]), "](") {
			esc[i] = true
		}
	}
	var b strings.Builder
	for i, c := range r {
		if esc[i] {
			b.WriteRune('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// escaped reports whether r[i] already has a backslash before it.
func escaped(r []rune, i int) bool { return i > 0 && r[i-1] == '\\' }
