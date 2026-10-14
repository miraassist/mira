// Package schedule parses the five-field cron expressions of the config
// ("30 3 * * *") and finds their next run. It covers what a household agent
// needs — numbers, lists, ranges, steps and * — and nothing more, which is
// less to get wrong than a dependency.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed expression: minute, hour, day of month, month, weekday.
type Cron struct {
	expr                          string
	minute, hour, dom, month, dow map[int]bool
	domAny, dowAny                bool
}

// Parse reads "minute hour day-of-month month weekday". Weekday 0 and 7 are
// Sunday.
func Parse(expr string) (Cron, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Cron{}, fmt.Errorf("cron %q: want 5 fields, got %d", expr, len(fields))
	}
	c := Cron{expr: expr}
	var err error
	if c.minute, err = field(fields[0], 0, 59); err != nil {
		return Cron{}, fmt.Errorf("cron %q minute: %w", expr, err)
	}
	if c.hour, err = field(fields[1], 0, 23); err != nil {
		return Cron{}, fmt.Errorf("cron %q hour: %w", expr, err)
	}
	if c.dom, err = field(fields[2], 1, 31); err != nil {
		return Cron{}, fmt.Errorf("cron %q day of month: %w", expr, err)
	}
	if c.month, err = field(fields[3], 1, 12); err != nil {
		return Cron{}, fmt.Errorf("cron %q month: %w", expr, err)
	}
	if c.dow, err = field(fields[4], 0, 7); err != nil {
		return Cron{}, fmt.Errorf("cron %q weekday: %w", expr, err)
	}
	if c.dow[7] {
		c.dow[0] = true
	}
	c.domAny, c.dowAny = fields[2] == "*", fields[4] == "*"
	return c, nil
}

func (c Cron) String() string { return c.expr }

func field(s string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("bad step in %q", part)
			}
			step, part = n, part[:i]
		}
		lo, hi := min, max
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			var err1, err2 error
			lo, err1 = strconv.Atoi(a)
			hi, err2 = strconv.Atoi(b)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("bad range %q", part)
			}
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("bad value %q", part)
			}
			lo, hi = n, n
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("%q outside %d-%d", part, min, max)
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	return out, nil
}

// Next returns the first matching minute strictly after t, in t's location.
func (c Cron) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	// A valid expression matches within a few years (29 February on a
	// Monday is the worst case); the bound only guards against a bug.
	for limit := t.AddDate(5, 0, 0); t.Before(limit); t = t.Add(time.Minute) {
		if !c.month[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location()).Add(-time.Minute)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location()).Add(-time.Minute)
			continue
		}
		if !c.hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location()).Add(-time.Minute)
			continue
		}
		if c.minute[t.Minute()] {
			return t
		}
	}
	return time.Time{}
}

// dayMatches follows cron: when both day fields are restricted, either
// matching is enough.
func (c Cron) dayMatches(t time.Time) bool {
	dom, dow := c.dom[t.Day()], c.dow[int(t.Weekday())]
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	default:
		return dom || dow
	}
}
