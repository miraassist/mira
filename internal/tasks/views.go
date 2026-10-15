package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
)

// Views are how Mira condenses data from Home Assistant for a judgement: a
// few numbers that fit the question instead of raw history. That is what
// keeps a judgement at a few hundred tokens.
const (
	// ViewState is the current state and since when.
	ViewState = "state"
	// ViewSameHour is the hour that ended against the same hour on the
	// previous fourteen days: the change of a counter, the mean of a
	// measurement.
	ViewSameHour = "same_hour"
	// ViewDay is today so far against the same weekday of the previous
	// four weeks up to the same time.
	ViewDay = "day"
)

// DataRequest is one entry of mira.assess's data list.
type DataRequest struct {
	EntityID string `json:"entity_id"`
	View     string `json:"view"`
}

// render condenses the requested data into lines for the model. A view
// that cannot be made says why instead of failing the judgement.
func (m *Manager) render(ctx context.Context, data []DataRequest) string {
	if len(data) == 0 {
		return ""
	}
	t := m.texts()
	states, err := m.opts.Source.States(ctx)
	if err != nil {
		return fmt.Sprintf(t.viewFailed, err)
	}
	cat := map[string]Entity{}
	for _, e := range Catalog(states, m.opts.Snapshot()) {
		cat[e.ID] = e
	}
	byID := map[string]haws.State{}
	for _, s := range states {
		byID[s.EntityID] = s
	}
	var b strings.Builder
	for _, d := range data {
		e, ok := cat[d.EntityID]
		if !ok {
			fmt.Fprintf(&b, "- %s: %s\n", d.EntityID, t.viewUnknown)
			continue
		}
		var line string
		switch d.View {
		case ViewSameHour:
			line = m.sameHour(ctx, t, e)
		case ViewDay:
			line = m.day(ctx, t, e)
		default:
			line = m.stateLine(t, e, byID[e.ID])
		}
		fmt.Fprintf(&b, "- %s (%s%s): %s\n", e.Name, e.ID, areaSuffix(e), line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func areaSuffix(e Entity) string {
	if e.Area == "" {
		return ""
	}
	return ", " + e.Area
}

func (m *Manager) stateLine(t texts, e Entity, s haws.State) string {
	value := strings.TrimSpace(s.State + " " + e.Unit)
	since, err := time.Parse(time.RFC3339Nano, s.LastChanged)
	if err != nil {
		return value
	}
	return fmt.Sprintf(t.viewState, value, i18n.FormatTime(m.opts.Lang, since.In(m.opts.Location)))
}

// sameHour compares the hour that ended with the same hour on earlier days.
func (m *Manager) sameHour(ctx context.Context, t texts, e Entity) string {
	now := m.opts.Clock().In(m.opts.Location)
	hour := now.Truncate(time.Hour).Add(-time.Hour)
	rows, err := m.statistics(ctx, e.ID, hour.AddDate(0, 0, -baselineDays))
	if err != nil {
		return fmt.Sprintf(t.viewFailed, err)
	}
	pick := pickFor(e)
	var current *float64
	var base []float64
	for _, r := range rows {
		v := pick(r)
		if v == nil {
			continue
		}
		start := r.Start.In(m.opts.Location)
		switch {
		case start.Equal(hour):
			current = v
		case start.Before(hour) && start.Hour() == hour.Hour():
			base = append(base, *v)
		}
	}
	if current == nil {
		return t.viewNoData
	}
	s := describe(base)
	measure := t.viewMean
	if e.counter() {
		measure = t.viewChange
	}
	line := fmt.Sprintf(t.viewHour, hour.Format("15:04"), hour.Add(time.Hour).Format("15:04"), measure, num(*current), e.Unit)
	if s.n == 0 {
		return line + " " + t.viewNoBaseline
	}
	return line + " " + fmt.Sprintf(t.viewBaseline, s.n, num(s.median), num(s.min), num(s.max), e.Unit)
}

// day compares today so far with the same weekday of earlier weeks.
func (m *Manager) day(ctx context.Context, t texts, e Entity) string {
	now := m.opts.Clock().In(m.opts.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, m.opts.Location)
	rows, err := m.statistics(ctx, e.ID, today.AddDate(0, 0, -28))
	if err != nil {
		return fmt.Sprintf(t.viewFailed, err)
	}
	pick := pickFor(e)
	upTo := now.Sub(today)
	sums := map[string][]float64{} // day -> values up to the same time of day
	for _, r := range rows {
		v := pick(r)
		if v == nil {
			continue
		}
		start := r.Start.In(m.opts.Location)
		dayStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, m.opts.Location)
		if start.Weekday() != now.Weekday() || start.Sub(dayStart) >= upTo {
			continue
		}
		key := dayStart.Format("2006-01-02")
		sums[key] = append(sums[key], *v)
	}
	value := func(vals []float64) float64 {
		var sum float64
		for _, v := range vals {
			sum += v
		}
		if e.counter() || len(vals) == 0 {
			return sum
		}
		return sum / float64(len(vals))
	}
	todayVals, ok := sums[today.Format("2006-01-02")]
	if !ok {
		return t.viewNoData
	}
	var base []float64
	for k, vals := range sums {
		if k != today.Format("2006-01-02") {
			base = append(base, value(vals))
		}
	}
	measure := t.viewMean
	if e.counter() {
		measure = t.viewChange
	}
	line := fmt.Sprintf(t.viewToday, now.Format("15:04"), measure, num(value(todayVals)), e.Unit)
	if len(base) == 0 {
		return line + " " + t.viewNoBaseline
	}
	s := describe(base)
	return line + " " + fmt.Sprintf(t.viewWeekdays, s.n, num(s.median), num(s.min), num(s.max), e.Unit)
}

func (m *Manager) statistics(ctx context.Context, id string, start time.Time) ([]haws.StatRow, error) {
	all, err := m.opts.Source.Statistics(ctx, []string{id}, start, "hour")
	if err != nil {
		return nil, err
	}
	return all[id], nil
}

// pickFor reads the hourly change of a counter and the mean of anything else.
func pickFor(e Entity) func(haws.StatRow) *float64 {
	if e.counter() {
		return func(r haws.StatRow) *float64 { return r.Change }
	}
	return func(r haws.StatRow) *float64 { return r.Mean }
}
