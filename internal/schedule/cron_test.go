package schedule

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, berlin)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		expr, from, want string
	}{
		{"30 3 * * *", "2026-10-14 22:00", "2026-10-15 03:30"},
		{"30 3 * * *", "2026-10-15 03:30", "2026-10-16 03:30"}, // strictly after
		{"0 8 * * *", "2026-10-14 07:59", "2026-10-14 08:00"},
		{"*/15 * * * *", "2026-10-14 10:01", "2026-10-14 10:15"},
		{"0 9 * * 1-5", "2026-10-17 10:00", "2026-10-19 09:00"}, // Saturday -> Monday
		{"0 0 1 * *", "2026-12-15 00:00", "2027-01-01 00:00"},
		{"0 12 * * 0", "2026-10-14 00:00", "2026-10-18 12:00"}, // Sunday
		{"0 12 * * 7", "2026-10-14 00:00", "2026-10-18 12:00"}, // 7 is Sunday too
		{"5,35 6 * * *", "2026-10-14 06:10", "2026-10-14 06:35"},
		// The night the clocks go back: 02:30 exists twice, the first wins.
		{"30 2 * * *", "2026-10-25 01:00", "2026-10-25 02:30"},
	}
	for _, c := range cases {
		cr, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.expr, err)
		}
		got := cr.Next(at(c.from))
		if want := at(c.want); !got.Equal(want) {
			t.Errorf("%q after %s = %s, want %s", c.expr, c.from, got.Format("2006-01-02 15:04 MST"), want.Format("2006-01-02 15:04 MST"))
		}
	}
}

func TestParseRejectsNonsense(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q) accepted", expr)
		}
	}
}
