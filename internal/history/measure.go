package history

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// A row of the statistics with how fine it is.
type row struct {
	haws.StatRow
	span time.Duration
}

// measurement answers for an entity with a state_class from Home
// Assistant's statistics, never from its raw history: the 5-minute ones
// where they reach, the hourly ones before (docs/spec/history.md,
// Measurements).
func (h *tool) measurement(ctx context.Context, live haws.State, class string, start, end time.Time, above *float64) (string, error) {
	id := live.EntityID
	fine, err := h.o.Source.Statistics(ctx, []string{id}, start, "5minute")
	if err != nil {
		return fmt.Sprintf(h.t.failed, err), nil
	}
	var rows []row
	five := within(fine[id], start, end)
	// Where the 5-minute statistics begin; before, the hourly ones.
	cut := end
	if len(five) > 0 {
		cut = five[0].Start
	}
	if cut.After(start.Add(5 * time.Minute)) {
		hourly, err := h.o.Source.Statistics(ctx, []string{id}, start, "hour")
		if err != nil {
			return fmt.Sprintf(h.t.failed, err), nil
		}
		for _, r := range within(hourly[id], start, end) {
			if r.Start.Before(cut) {
				rows = append(rows, row{r, time.Hour})
			}
		}
	}
	for _, r := range five {
		rows = append(rows, row{r, 5 * time.Minute})
	}
	if len(rows) == 0 {
		return h.t.noStatistics, nil
	}
	counter := class == "total" || class == "total_increasing"
	unit, _ := live.Attributes["unit_of_measurement"].(string)

	var b strings.Builder
	h.header(&b, live, start, end)
	b.WriteString(h.resolution(rows, start, end) + "\n")
	if above != nil {
		h.above(&b, rows, counter, *above, unit, start, end)
	} else {
		h.days(&b, rows, counter, unit)
	}
	return strings.TrimSpace(b.String()), nil
}

func within(rows []haws.StatRow, start, end time.Time) []haws.StatRow {
	var out []haws.StatRow
	for _, r := range rows {
		if !r.Start.Before(start.Truncate(time.Hour)) && r.Start.Before(end) {
			out = append(out, r)
		}
	}
	return out
}

// resolution names how fine each part is, so the model can say that older
// values are coarser (decision 7).
func (h *tool) resolution(rows []row, start, end time.Time) string {
	first, last := rows[0].span, rows[len(rows)-1].span
	switch {
	case first == last && first == time.Hour:
		return h.t.resHour
	case first == last:
		return h.t.res5
	}
	for _, r := range rows {
		if r.span != time.Hour {
			return fmt.Sprintf(h.t.resBoth, h.when(r.Start, start, end))
		}
	}
	return h.t.resHour
}

// above lists the periods over a threshold: from 5-minute rows to five
// minutes, from hourly rows as whole hours whose maximum lay above, an upper
// bound.
func (h *tool) above(b *strings.Builder, rows []row, counter bool, limit float64, unit string, start, end time.Time) {
	label := fmt.Sprintf(h.t.above, h.num(limit), unit)
	var fine, hourly []period
	for _, r := range rows {
		v := r.Mean
		if counter {
			v = r.Change
		} else if r.span == time.Hour && r.Max != nil {
			v = r.Max
		}
		if v == nil || *v <= limit {
			continue
		}
		ps := &fine
		if r.span == time.Hour {
			ps = &hourly
		}
		p := period{state: "above", start: r.Start, end: r.Start.Add(r.span)}
		if n := len(*ps); n > 0 && !(*ps)[n-1].end.Before(p.start) {
			(*ps)[n-1].end = p.end
			continue
		}
		*ps = append(*ps, p)
	}
	if len(fine)+len(hourly) == 0 {
		fmt.Fprintf(b, h.t.times+"\n", label, h.duration(0), 0)
		return
	}
	if len(hourly) > 0 {
		h.list(b, hourly, start, end, label, true)
		h.totals(b, hourly, label, true)
	}
	if len(fine) > 0 {
		h.list(b, fine, start, end, label, false)
		h.totals(b, fine, label, false)
	}
}

// days writes per day the minimum, maximum and mean, or for a counter its
// change: at most 31 lines.
func (h *tool) days(b *strings.Builder, rows []row, counter bool, unit string) {
	type day struct {
		key           string
		min, max, sum float64
		n             int
	}
	var ds []*day
	for _, r := range rows {
		key := r.Start.In(h.o.Location).Format("2006-01-02")
		if len(ds) == 0 || ds[len(ds)-1].key != key {
			ds = append(ds, &day{key: key, min: 1e300, max: -1e300})
		}
		d := ds[len(ds)-1]
		if counter {
			if r.Change != nil {
				d.sum += *r.Change
				d.n++
			}
			continue
		}
		if r.Mean == nil {
			continue
		}
		lo, hi := *r.Mean, *r.Mean
		if r.Min != nil {
			lo = *r.Min
		}
		if r.Max != nil {
			hi = *r.Max
		}
		d.min, d.max = min(d.min, lo), max(d.max, hi)
		d.sum += *r.Mean
		d.n++
	}
	for _, d := range ds {
		if d.n == 0 {
			continue
		}
		at, _ := time.ParseInLocation("2006-01-02", d.key, h.o.Location)
		date := at.Format(strings.Fields(h.t.dateLayout)[0])
		if counter {
			fmt.Fprintf(b, h.t.dayCounter+"\n", date, h.num(d.sum), unit)
			continue
		}
		fmt.Fprintf(b, h.t.day+"\n", date, h.num(d.min), h.num(d.max), h.num(d.sum/float64(d.n)), unit)
	}
}

// num writes a value with one decimal at most, in the language's way.
func (h *tool) num(v float64) string {
	s := strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
	return strings.Replace(s, ".", h.t.decimal, 1)
}
