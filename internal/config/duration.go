package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so YAML can carry values like "5m" or "12h".
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"30m\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML writes the short form a person would type: "30m", "1h30m",
// "2m", not Go's "30m0s".
func (d Duration) MarshalYAML() (any, error) { return compact(time.Duration(d)), nil }

func compact(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// ClockTime is a wall-clock time of day without a date, written as "22:30".
type ClockTime struct {
	Hour   int
	Minute int
}

func ParseClockTime(s string) (ClockTime, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return ClockTime{}, fmt.Errorf("invalid time of day %q, want HH:MM: %w", s, err)
	}
	return ClockTime{Hour: t.Hour(), Minute: t.Minute()}, nil
}

func (c ClockTime) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

// MinutesOfDay returns the offset from midnight, which makes range checks trivial.
func (c ClockTime) MinutesOfDay() int { return c.Hour*60 + c.Minute }

func (c *ClockTime) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("time of day must be a string like \"22:30\": %w", err)
	}
	parsed, err := ParseClockTime(s)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

func (c ClockTime) MarshalYAML() (any, error) { return c.String(), nil }
