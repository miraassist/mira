package tasks

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	haws "github.com/sker65/mira/internal/ha/ws"
)

// baselineDays is how far back the same hour is compared.
const baselineDays = 14

// minBaseline is how many earlier values a comparison needs; with fewer the
// rule is still learning and stays silent.
const minBaseline = 5

// Candidate is something the numbers say is out of the ordinary.
type Candidate struct {
	Rule     int
	Evidence string // for the judge, in plain words
}

type stats struct {
	median, mad, min, max float64
	n                     int
}

func describe(vals []float64) stats {
	if len(vals) == 0 {
		return stats{}
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	med := quantile(s, 0.5)
	dev := make([]float64, len(s))
	for i, v := range s {
		dev[i] = math.Abs(v - med)
	}
	sort.Float64s(dev)
	return stats{median: med, mad: quantile(dev, 0.5), min: s[0], max: s[len(s)-1], n: len(s)}
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// hourly splits statistics rows into the value of hour and the values of
// the same hour of day on earlier days.
func hourly(rows []haws.StatRow, hour time.Time, loc *time.Location, pick func(haws.StatRow) *float64) (float64, []float64, bool) {
	var current float64
	found := false
	var base []float64
	h := hour.In(loc).Hour()
	for _, r := range rows {
		v := pick(r)
		if v == nil {
			continue
		}
		start := r.Start.In(loc)
		switch {
		case start.Equal(hour):
			current, found = *v, true
		case start.Before(hour) && start.Hour() == h && hour.Sub(start) <= baselineDays*24*time.Hour:
			base = append(base, *v)
		}
	}
	return current, base, found
}

// usageCandidate: a counter rose much more in the hour than it usually does
// at that hour. If it usually does not rise at all (water at three in the
// morning), any use counts.
func usageCandidate(current float64, base []float64) (bool, stats) {
	s := describe(base)
	if s.n < minBaseline || current <= 0 {
		return false, s
	}
	limit := math.Max(1.5*s.max, s.median+4*s.mad)
	return current > limit, s
}

// levelCandidate: an hourly mean outside everything seen at that hour.
func levelCandidate(current float64, base []float64, direction string) (bool, stats) {
	s := describe(base)
	if s.n < minBaseline {
		return false, s
	}
	margin := math.Max(3*s.mad, 0.05*math.Abs(s.median))
	high := current > s.max+margin
	low := current < s.min-margin
	switch direction {
	case "high":
		return high, s
	case "low":
		return low, s
	default:
		return high || low, s
	}
}

func usageEvidence(t texts, e Entity, hour time.Time, current float64, s stats) string {
	return fmt.Sprintf(t.usageEvidence, e.Name, e.ID, hour.Format("15:04"), hour.Add(time.Hour).Format("15:04"),
		num(current), e.Unit, s.n, num(s.median), e.Unit, num(s.max), e.Unit)
}

func levelEvidence(t texts, e Entity, hour time.Time, current float64, s stats) string {
	return fmt.Sprintf(t.levelEvidence, e.Name, e.ID, hour.Format("15:04"), hour.Add(time.Hour).Format("15:04"),
		num(current), e.Unit, s.n, num(s.min), e.Unit, num(s.max), e.Unit)
}

// stateHolds reports whether the entity is in one of the rule's states.
func stateHolds(r Rule, state string) bool {
	for _, s := range r.States {
		if strings.EqualFold(strings.TrimSpace(s), state) {
			return true
		}
	}
	return false
}

// thresholdHolds reports whether a value is beyond the rule's limits.
func thresholdHolds(r Rule, state string) (bool, float64) {
	v, err := strconv.ParseFloat(state, 64)
	if err != nil {
		return false, 0
	}
	return (r.Above != nil && v > *r.Above) || (r.Below != nil && v < *r.Below), v
}

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}
