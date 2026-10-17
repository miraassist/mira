// Package timeline renders a list of entries with a time for the model
// (docs/spec/kontext.md, section 2.2): calendar events, logbook changes,
// forecast hours, prices per hour. Mira decides how the model sees them:
// a window, a selection, a limit, short fields by default, grouped by day,
// times in the language of the conversation, and short labels instead of
// Home Assistant's ids where the model may act on an entry.
package timeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
)

// Entry is one item on the timeline.
type Entry struct {
	Start, End time.Time
	// AllDay entries have a day, not a time; End is the day after.
	AllDay bool
	Title  string
	// Place is shown shortened; Source names where it comes from (a
	// calendar's name).
	Place, Source string
	// Details are shown only on request, shortened and without links.
	Details string
	// Ref is what the code needs to act on the entry; the model never sees
	// it, only the label it gets in its place.
	Ref any
}

// Options shape the rendering.
type Options struct {
	Lang     i18n.Lang
	Location *time.Location
	// Now marks today and tomorrow.
	Now time.Time
	// Limit is the most entries shown; the rest is counted. Default 40.
	Limit int
	// Details shows the details of each entry.
	Details bool
	// Labels gives each entry a short label ("t1") from Label, so the model
	// can name it; nil means no labels.
	Label func(Entry) string
}

const (
	placeMax   = 40
	detailsMax = 300
)

var links = regexp.MustCompile(`https?://\S+`)

// Render writes the entries, sorted by time, grouped by day.
func Render(entries []Entry, o Options) string {
	t := i18n.Pick(catalog, o.Lang)
	if len(entries) == 0 {
		return t.none
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Limit <= 0 {
		o.Limit = 40
	}
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var b strings.Builder
	day := ""
	for i, e := range sorted {
		if i == o.Limit {
			fmt.Fprintf(&b, t.more, len(sorted)-o.Limit)
			break
		}
		start := e.Start.In(o.Location)
		if d := dayLabel(t, o, start); d != day {
			if day != "" {
				b.WriteString("\n")
			}
			b.WriteString(d + "\n")
			day = d
		}
		b.WriteString("- ")
		if o.Label != nil {
			if l := o.Label(e); l != "" {
				fmt.Fprintf(&b, "[%s] ", l)
			}
		}
		switch {
		case e.AllDay:
			b.WriteString(t.allDay)
		case e.End.After(e.Start):
			fmt.Fprintf(&b, "%s–%s", clock(start), clock(e.End.In(o.Location)))
		default:
			b.WriteString(clock(start))
		}
		b.WriteString(" " + strings.TrimSpace(e.Title))
		if p := shorten(e.Place, placeMax); p != "" {
			b.WriteString(" · " + p)
		}
		if e.Source != "" {
			fmt.Fprintf(&b, " (%s)", e.Source)
		}
		if o.Details {
			if d := shorten(links.ReplaceAllString(e.Details, t.link), detailsMax); d != "" {
				b.WriteString("\n  " + d)
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func clock(t time.Time) string { return t.Format("15:04") }

// dayLabel is "Montag, 20. Oktober 2026", with "heute"/"morgen" in front
// where it helps.
func dayLabel(t texts, o Options, at time.Time) string {
	label := i18n.FormatDate(o.Lang, at)
	if o.Now.IsZero() {
		return label
	}
	today := o.Now.In(o.Location)
	y1, m1, d1 := today.Date()
	y2, m2, d2 := at.Date()
	days := int(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	switch days {
	case 0:
		return t.today + ", " + label
	case 1:
		return t.tomorrow + ", " + label
	}
	return label
}

// shorten cuts text to max runes on a word boundary, on one line.
func shorten(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + " …"
}

type texts struct{ none, more, allDay, today, tomorrow, link string }

var catalog = map[i18n.Lang]texts{
	i18n.DE: {none: "Keine Einträge.", more: "… und %d weitere\n", allDay: "ganztägig", today: "heute", tomorrow: "morgen", link: "(Link)"},
	i18n.EN: {none: "No entries.", more: "… and %d more\n", allDay: "all day", today: "today", tomorrow: "tomorrow", link: "(link)"},
}
