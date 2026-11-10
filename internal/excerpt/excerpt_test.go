package excerpt

import (
	"strings"
	"testing"
)

func TestOf(t *testing.T) {
	if got := Of(""); got != "(empty)" {
		t.Errorf("empty = %q", got)
	}
	if got := Of("ok\n  so  "); got != "ok so" {
		t.Errorf("one line = %q", got)
	}
	long := strings.Repeat("ä", Max+10)
	if got := Of(long); got != strings.Repeat("ä", Max)+"…" {
		t.Errorf("long cut wrong: %d runes", len([]rune(got)))
	}
}
