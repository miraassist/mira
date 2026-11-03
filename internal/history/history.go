// Package history is the tool state_history: questions about the past,
// answered from Home Assistant's recorder and condensed in code, so the
// model never reads a raw history (docs/spec/history.md).
package history

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

// Tool is the tool's name.
const Tool = "state_history"

const (
	// maxDays is the longest period (decision 2).
	maxDays = 31
	// listed periods: more than maxListed show the first head and the last
	// tail, with the totals of all (decision 5).
	maxListed, head, tail = 12, 3, 6
)

// Source is what the tool reads from Home Assistant.
type Source interface {
	History(ctx context.Context, id string, start, end time.Time) ([]haws.Change, error)
	GetStates(ctx context.Context, f haws.StateFilter) ([]haws.State, error)
	Statistics(ctx context.Context, ids []string, start time.Time, period string) (map[string][]haws.StatRow, error)
}

// Options are the tool's sources.
type Options struct {
	Lang   i18n.Lang
	Source Source
	// Structure finds the entity by its name (or id), among what Home
	// Assistant exposes to Assist only (decision 6).
	Structure func() structure.Snapshot
	Clock     func() time.Time
	Location  *time.Location
}

// New is the tool.
func New(o Options) tools.InternalTool {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	t := textsFor(o.Lang)
	schema := fmt.Sprintf(`{"type":"object","properties":{"name":{"type":"string","description":%q},"area":{"type":"string","description":%q},`+
		`"period":{"type":"string","enum":["today","yesterday","last_24h","last_7_days","last_31_days"],"description":%q},`+
		`"from":{"type":"string","description":%q},"to":{"type":"string","description":%q},`+
		`"above":{"type":"number","description":%q}},"required":["name"]}`,
		t.argName, t.argArea, t.argPeriod, t.argFrom, t.argTo, t.argAbove)
	h := &tool{o: o, t: t}
	return tools.InternalTool{Name: Tool, Description: t.desc, Schema: schema, ReadOnly: true, Run: h.run}
}

type tool struct {
	o Options
	t texts
}

type args struct {
	Name   string   `json:"name"`
	Area   string   `json:"area"`
	Period string   `json:"period"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Above  *float64 `json:"above"`
}

func (h *tool) run(ctx context.Context, inv tools.Invocation) (string, error) {
	var a args
	if err := json.Unmarshal(inv.Args, &a); err != nil {
		return h.t.badTime, nil
	}
	// The entity by its name, alias, device name or id, as Home Assistant's
	// intents find it; one not exposed to Assist is answered like one that
	// does not exist, and Home Assistant is not asked.
	var snap structure.Snapshot
	if h.o.Structure != nil {
		snap = h.o.Structure()
	}
	found := snap.ResolveName(a.Name, a.Area)
	switch {
	case len(found) == 0:
		return h.t.unknown, nil
	case len(found) > 1:
		var names []string
		for _, f := range found {
			names = append(names, fmt.Sprintf("%s (%s)", snap.EntityLabel[f], snap.AreaNames[snap.EntityArea[f]]))
		}
		return fmt.Sprintf(h.t.several, strings.Join(names, ", ")), nil
	}
	id := found[0]
	start, end, msg := h.window(a)
	if msg != "" {
		return msg, nil
	}
	states, err := h.o.Source.GetStates(ctx, haws.StateFilter{EntityIDs: []string{id}})
	if err != nil {
		return fmt.Sprintf(h.t.failed, err), nil
	}
	if len(states) == 0 {
		return h.t.unknown, nil
	}
	live := states[0]
	if class, _ := live.Attributes["state_class"].(string); class != "" {
		return h.measurement(ctx, live, class, start, end, a.Above)
	}
	if strings.HasPrefix(id, "sensor.") && isNumber(live.State) {
		// A number without statistics: its raw history would be thousands of
		// values (decision 3, Measurements 5).
		return h.t.noStatistics, nil
	}
	return h.states(ctx, live, start, end)
}

// window is the period asked for, in Home Assistant's time zone, or why it
// is refused.
func (h *tool) window(a args) (start, end time.Time, refused string) {
	loc := h.o.Location
	now := h.o.Clock().In(loc)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	end = now
	switch {
	case a.From != "":
		var ok bool
		if start, ok = parseLocal(a.From, now); !ok {
			return start, end, h.t.badTime
		}
		if a.To != "" {
			if end, ok = parseLocal(a.To, now); !ok {
				return start, end, h.t.badTime
			}
		}
		if end.After(now) {
			end = now
		}
	case a.Period == "yesterday":
		start, end = midnight.AddDate(0, 0, -1), midnight
	case a.Period == "last_24h":
		start = now.Add(-24 * time.Hour)
	case a.Period == "last_7_days":
		start = now.AddDate(0, 0, -7)
	case a.Period == "last_31_days":
		start = now.AddDate(0, 0, -maxDays)
	default: // today, or nothing said
		start = midnight
	}
	switch {
	case !start.Before(now) || !start.Before(end):
		return start, end, h.t.future
	case start.AddDate(0, 0, maxDays).Before(end.Add(-time.Minute)):
		return start, end, h.t.tooLong
	}
	return start, end, ""
}

// parseLocal reads a point in time in now's time zone; a time of day alone
// is today.
func parseLocal(s string, now time.Time) (time.Time, bool) {
	loc := now.Location()
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	if t, err := time.ParseInLocation("15:04", s, loc); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc), true
	}
	return time.Time{}, false
}

// period is a stretch of one state (or of a reading above a threshold).
type period struct {
	state      string
	start, end time.Time
	// open: it lasts until now.
	open bool
}

// states condenses a history of states into periods with durations and
// totals (docs/spec/history.md, States).
func (h *tool) states(ctx context.Context, live haws.State, start, end time.Time) (string, error) {
	changes, err := h.o.Source.History(ctx, live.EntityID, start, end)
	if err != nil {
		return fmt.Sprintf(h.t.failed, err), nil
	}
	if len(changes) == 0 {
		return h.t.noHistory, nil
	}
	now := h.o.Clock()
	toNow := !end.Before(now.Add(-time.Minute))
	// The recorder writes with a delay of seconds: the live state ends the
	// history when the period lasts until now.
	if last := changes[len(changes)-1]; toNow && live.State != last.State {
		if at, err := time.Parse(time.RFC3339Nano, live.LastChanged); err == nil && at.After(last.At) {
			changes = append(changes, haws.Change{State: live.State, At: at})
		}
	}
	var ps []period
	for _, c := range changes {
		if len(ps) > 0 && ps[len(ps)-1].state == c.State {
			continue
		}
		ps = append(ps, period{state: c.State, start: maxTime(c.At, start)})
	}
	for i := range ps {
		if i+1 < len(ps) {
			ps[i].end = ps[i+1].start
		} else {
			ps[i].end, ps[i].open = end, toNow
		}
	}
	var b strings.Builder
	h.header(&b, live, start, end)
	if first := changes[0].At; first.After(start.Add(time.Minute)) {
		fmt.Fprintf(&b, "%s\n", fmt.Sprintf(h.t.covered, h.when(first, start, end)))
	}
	h.list(&b, ps, start, end, "", false)
	h.totals(&b, ps, "", false)
	return strings.TrimSpace(b.String()), nil
}

func (h *tool) header(b *strings.Builder, live haws.State, start, end time.Time) {
	name := live.FriendlyName()
	if name == "" {
		name = live.EntityID
	}
	what := live.EntityID
	if class, _ := live.Attributes["device_class"].(string); class != "" {
		what += ", " + class
	}
	// With dates unless the period began today, so "yesterday" does not read
	// as 00:00 to 00:00.
	layout := h.t.dateLayout
	if s, now := start.In(h.o.Location), h.o.Clock().In(h.o.Location); s.YearDay() == now.YearDay() && s.Year() == now.Year() {
		layout = h.t.timeLayout
	}
	to := end.In(h.o.Location).Format(layout)
	if !end.Before(h.o.Clock().Add(-time.Minute)) {
		to = h.t.now
	}
	fmt.Fprintf(b, h.t.head+"\n", name, what, start.In(h.o.Location).Format(layout), to)
}

// when is a point in time: with its date when the period spans more than
// one day.
func (h *tool) when(at, start, end time.Time) string {
	return at.In(h.o.Location).Format(h.dateOrTime(at, start, end))
}

func (h *tool) dateOrTime(_, start, end time.Time) string {
	s, e := start.In(h.o.Location), end.In(h.o.Location)
	if s.YearDay() == e.YearDay() && s.Year() == e.Year() || e.Sub(s) <= 24*time.Hour && e.Hour() == 0 && e.Minute() == 0 {
		return h.t.timeLayout
	}
	return h.t.dateLayout
}

// list writes the periods, the first and the last of them when there are
// too many. With a label (a threshold) it stands once in front, as does
// "to the hour" for coarse periods.
func (h *tool) list(b *strings.Builder, ps []period, start, end time.Time, label string, coarse bool) {
	item := func(p period) string {
		from := h.when(p.start, start, end)
		to := h.when(p.end, start, end)
		if s, e := p.start.In(h.o.Location), p.end.In(h.o.Location); s.YearDay() == e.YearDay() && s.Year() == e.Year() {
			to = e.Format(h.t.timeLayout)
		}
		if p.open {
			to = h.t.now
		}
		d := h.duration(p.end.Sub(p.start))
		if coarse {
			d = fmt.Sprintf(h.t.atMost, d)
		}
		if label != "" {
			return fmt.Sprintf("%s–%s (%s)", from, to, d)
		}
		return fmt.Sprintf("%s %s–%s (%s)", p.state, from, to, d)
	}
	var parts []string
	if len(ps) <= maxListed {
		for _, p := range ps {
			parts = append(parts, item(p))
		}
	} else {
		for _, p := range ps[:head] {
			parts = append(parts, item(p))
		}
		parts = append(parts, fmt.Sprintf(h.t.more, len(ps)-head-tail))
		for _, p := range ps[len(ps)-tail:] {
			parts = append(parts, item(p))
		}
	}
	if label != "" {
		if coarse {
			label += ", " + h.t.coarse
		}
		b.WriteString(label + ": ")
	}
	b.WriteString(strings.Join(parts, " · ") + "\n")
}

// totals writes, per state, its whole time and how often it began; the
// states the longest first.
func (h *tool) totals(b *strings.Builder, ps []period, label string, coarse bool) {
	sum := map[string]time.Duration{}
	count := map[string]int{}
	var order []string
	for _, p := range ps {
		if _, ok := sum[p.state]; !ok {
			order = append(order, p.state)
		}
		sum[p.state] += p.end.Sub(p.start)
		count[p.state]++
	}
	slices.SortStableFunc(order, func(x, y string) int { return int(sum[y] - sum[x]) })
	var parts []string
	for _, s := range order {
		d := h.duration(sum[s])
		if coarse {
			d = fmt.Sprintf(h.t.atMost, d)
		}
		name := s
		if label != "" {
			name = label
		}
		parts = append(parts, fmt.Sprintf(h.t.times, name, d, count[s]))
	}
	if len(parts) > 0 {
		b.WriteString(h.t.together + strings.Join(parts, ", ") + ".\n")
	}
}

func (h *tool) duration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return h.t.under
	}
	days, hours, mins := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf(h.t.days, days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf(h.t.hours, hours))
	}
	if mins > 0 && days == 0 {
		parts = append(parts, fmt.Sprintf(h.t.minutes, mins))
	}
	return strings.Join(parts, " ")
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
