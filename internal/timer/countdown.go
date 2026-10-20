package timer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/miraassist/mira/internal/tools"
)

// countdown runs Home Assistant's timer intents on the satellite the user is
// talking to. Nothing is kept here: the timer lives in Home Assistant, which
// tells the satellite to count, show and ring.
func (c *controller) countdown(ctx context.Context, inv tools.Invocation) (string, error) {
	var a struct {
		Action  string `json:"action"`
		Hours   int    `json:"hours"`
		Minutes int    `json:"minutes"`
		Seconds int    `json:"seconds"`
		Name    string `json:"name"`
		All     bool   `json:"all"`
	}
	if err := json.Unmarshal(inv.Args, &a); err != nil {
		return "", err
	}
	device := ""
	if c.o.DeviceOf != nil {
		device = c.o.DeviceOf(ctx, inv.SessionKey)
	}
	if device == "" || c.o.Intents == nil {
		return c.t.noDevice, nil
	}
	data := map[string]any{}
	if n := strings.TrimSpace(a.Name); n != "" {
		data["name"] = n
	}
	amount := func() bool {
		for k, v := range map[string]int{"hours": a.Hours, "minutes": a.Minutes, "seconds": a.Seconds} {
			if v > 0 {
				data[k] = v
			}
		}
		return a.Hours > 0 || a.Minutes > 0 || a.Seconds > 0
	}
	var intent string
	switch a.Action {
	case "start":
		intent = "HassStartTimer"
		if !amount() {
			return c.t.tNeedsTime, nil
		}
	case "add", "remove":
		intent = map[string]string{"add": "HassIncreaseTimer", "remove": "HassDecreaseTimer"}[a.Action]
		if !amount() {
			return c.t.tNeedsTime, nil
		}
	case "status":
		intent = "HassTimerStatus"
	case "cancel":
		intent = "HassCancelTimer"
		if a.All {
			intent, data = "HassCancelAllTimers", map[string]any{}
		}
	case "pause":
		intent = "HassPauseTimer"
	case "resume":
		intent = "HassUnpauseTimer"
	default:
		return fmt.Sprintf(c.t.tFailed, "action"), nil
	}
	res, err := c.o.Intents.Handle(ctx, intent, data, device, string(c.o.Lang))
	if err != nil {
		return fmt.Sprintf(c.t.tFailed, err), nil
	}
	if res.Error != "" {
		return c.t.explain(res.Error), nil
	}
	switch a.Action {
	case "start":
		return fmt.Sprintf(c.t.started, c.describe(strings.TrimSpace(a.Name), a.Hours, a.Minutes, a.Seconds)), nil
	case "status":
		if len(res.Timers) == 0 {
			return c.t.tNone, nil
		}
		var b strings.Builder
		for _, tm := range res.Timers {
			name := tm.Name
			if name == "" {
				name = "–"
			}
			paused := ""
			if !tm.IsActive {
				paused = c.t.tPaused
			}
			fmt.Fprintf(&b, "- "+c.t.tLine+"\n", name, c.span(tm.TotalSecondsLeft), paused)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "cancel":
		if a.All {
			return fmt.Sprintf(c.t.tCanceledAll, res.Canceled), nil
		}
		return c.t.tCanceled, nil
	}
	return c.t.done, nil
}

// explain turns what Home Assistant says when it cannot into a sentence for
// the model; anything else goes through as it is.
func (t texts) explain(speech string) string {
	switch {
	case strings.Contains(speech, "Multiple timers"):
		return t.tSeveral
	case strings.Contains(speech, "Timer not found"):
		return t.tNotFound
	case strings.Contains(speech, "does not support timers"):
		return t.noDevice
	}
	return fmt.Sprintf(t.tFailed, speech)
}

// describe says a new timer: "Nudeln, 10 Minuten".
func (c *controller) describe(name string, h, m, s int) string {
	d := c.span(h*3600 + m*60 + s)
	if name == "" {
		return d
	}
	return name + ", " + d
}

// span writes seconds as "1 Stunde 5 Minuten 3 Sekunden".
func (c *controller) span(total int) string {
	h, m, s := total/3600, total%3600/60, total%60
	var parts []string
	unit := func(n int, one, many string) {
		if n == 0 {
			return
		}
		w := many
		if n == 1 {
			w = one
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, w))
	}
	unit(h, c.t.hourWord, c.t.hoursWord)
	unit(m, c.t.minuteWord, c.t.minutesWord)
	unit(s, c.t.secondWord, c.t.secondsWord)
	if len(parts) == 0 {
		return fmt.Sprintf("0 %s", c.t.secondsWord)
	}
	return strings.Join(parts, " ")
}
