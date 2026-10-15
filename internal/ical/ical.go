// Package ical reads the events of an iCalendar (.ics) file: waste
// collection calendars, school holidays, club dates. It understands what
// such published calendars use — folded lines, all-day dates, UTC and TZID
// times, escaped text — and simple yearly, monthly, weekly and daily
// RRULEs. Calendar text is third-party content and reaches the model only
// as data.
package ical

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is one occurrence.
type Event struct {
	Start   time.Time
	End     time.Time
	AllDay  bool
	Summary string
}

// maxBody bounds a download; a household calendar is a few hundred KB.
const maxBody = 4 << 20

// Parse returns the events of a calendar, recurring ones expanded within
// [from, to). Times without a zone are read in loc.
func Parse(data []byte, loc *time.Location, from, to time.Time) ([]Event, error) {
	if !bytes.Contains(data, []byte("BEGIN:VCALENDAR")) {
		return nil, fmt.Errorf("not an iCalendar file")
	}
	var out []Event
	var cur map[string]prop
	for _, line := range unfold(data) {
		name, p := parseLine(line)
		switch {
		case name == "BEGIN" && p.value == "VEVENT":
			cur = map[string]prop{}
		case name == "END" && p.value == "VEVENT":
			if cur != nil {
				out = append(out, expand(cur, loc, from, to)...)
			}
			cur = nil
		case cur != nil:
			if _, seen := cur[name]; !seen {
				cur[name] = p
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

type prop struct {
	params map[string]string
	value  string
}

// unfold joins continuation lines (starting with a space or tab).
func unfold(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func parseLine(l string) (string, prop) {
	head, value, ok := strings.Cut(l, ":")
	if !ok {
		return "", prop{}
	}
	parts := strings.Split(head, ";")
	p := prop{params: map[string]string{}, value: value}
	for _, kv := range parts[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return strings.ToUpper(parts[0]), p
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(r.Replace(s))
}

func parseTime(p prop, loc *time.Location) (time.Time, bool, error) {
	v := strings.TrimSpace(p.value)
	if p.params["VALUE"] == "DATE" || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, loc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	zone := loc
	if tz := p.params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			zone = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, zone)
	return t, false, err
}

func expand(ev map[string]prop, loc *time.Location, from, to time.Time) []Event {
	startProp, ok := ev["DTSTART"]
	if !ok {
		return nil
	}
	start, allDay, err := parseTime(startProp, loc)
	if err != nil {
		return nil
	}
	end := start
	if e, ok := ev["DTEND"]; ok {
		if t, _, err := parseTime(e, loc); err == nil {
			end = t
		}
	} else if allDay {
		end = start.AddDate(0, 0, 1)
	}
	base := Event{Start: start, End: end, AllDay: allDay, Summary: unescape(ev["SUMMARY"].value)}
	rule, ok := ev["RRULE"]
	if !ok {
		if base.End.After(from) && base.Start.Before(to) {
			return []Event{base}
		}
		return nil
	}
	return recur(base, rule.value, loc, from, to)
}

// recur expands FREQ=YEARLY|MONTHLY|WEEKLY|DAILY with INTERVAL, COUNT and
// UNTIL. BYDAY and the other BY rules are not supported; such events
// appear only at their first date.
func recur(base Event, rule string, loc *time.Location, from, to time.Time) []Event {
	parts := map[string]string{}
	for _, kv := range strings.Split(rule, ";") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			parts[strings.ToUpper(k)] = v
		}
	}
	for k := range parts {
		if strings.HasPrefix(k, "BY") {
			parts = map[string]string{"FREQ": ""} // unsupported: first date only
			break
		}
	}
	interval, _ := strconv.Atoi(parts["INTERVAL"])
	if interval < 1 {
		interval = 1
	}
	count, _ := strconv.Atoi(parts["COUNT"])
	until := to
	if u := parts["UNTIL"]; u != "" {
		if t, _, err := parseTime(prop{value: u}, loc); err == nil && t.Before(until) {
			until = t.Add(time.Second)
		}
	}
	step := map[string]func(time.Time, int) time.Time{
		"YEARLY":  func(t time.Time, n int) time.Time { return t.AddDate(n, 0, 0) },
		"MONTHLY": func(t time.Time, n int) time.Time { return t.AddDate(0, n, 0) },
		"WEEKLY":  func(t time.Time, n int) time.Time { return t.AddDate(0, 0, 7*n) },
		"DAILY":   func(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) },
	}[parts["FREQ"]]
	if step == nil {
		if base.End.After(from) && base.Start.Before(to) {
			return []Event{base}
		}
		return nil
	}
	dur := base.End.Sub(base.Start)
	var out []Event
	for i := 0; i < 5000; i++ {
		if count > 0 && i >= count {
			break
		}
		s := step(base.Start, i*interval)
		if !s.Before(until) {
			break
		}
		e := base
		e.Start, e.End = s, s.Add(dur)
		if e.End.After(from) && e.Start.Before(to) {
			out = append(out, e)
		}
	}
	return out
}

// Cache downloads calendars and keeps them for TTL, so a skill that asks
// several times a day does not hit the server each time.
type Cache struct {
	Client *http.Client
	TTL    time.Duration

	mu      sync.Mutex
	entries map[string]cached
}

type cached struct {
	data    []byte
	fetched time.Time
}

// Get returns the calendar at url, from the cache when fresh. When the
// download fails, a stale copy is better than nothing.
func (c *Cache) Get(ctx context.Context, url string) ([]byte, error) {
	ttl := c.TTL
	if ttl == 0 {
		ttl = 6 * time.Hour
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cached{}
	}
	e, ok := c.entries[url]
	c.mu.Unlock()
	if ok && time.Since(e.fetched) < ttl {
		return e.data, nil
	}
	data, err := fetch(ctx, c.Client, url)
	if err != nil {
		if ok {
			return e.data, nil
		}
		return nil, err
	}
	c.mu.Lock()
	c.entries[url] = cached{data: data, fetched: time.Now()}
	c.mu.Unlock()
	return data, nil
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mira (household agent)")
	resp, err := client.Do(req)
	if err != nil {
		// The URL may carry an address or a token: never echo it.
		var uerr *neturl.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("calendar download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("calendar download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}
