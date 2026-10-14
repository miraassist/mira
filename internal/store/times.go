package store

import "time"

// timeFormat is the on-disk representation for every timestamp: RFC3339 with
// milliseconds in UTC. It sorts lexicographically, which lets plain string
// comparisons in SQL act as time comparisons.
const timeFormat = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeFormat)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		// Tolerate rows written by an older format rather than failing a read.
		if t2, err2 := time.Parse(time.RFC3339Nano, s); err2 == nil {
			return t2.UTC()
		}
		return time.Time{}
	}
	return t
}
