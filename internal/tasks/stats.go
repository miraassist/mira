package tasks

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// baselineDays is how far back the same hour is compared.
const baselineDays = 14

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

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}
