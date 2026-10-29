package main

import (
	"context"
	"strings"

	"github.com/miraassist/mira/internal/config"
)

// screens finds, for satellites without an indicator light, an on-screen
// overlay their device offers: an ESPHome action "<node>_notify" with a
// message, a tag and a duration (an Echo Show with TECHO5 has
// esphome.show_notify). Nobody configures it; the notifier uses it to show
// that messages wait (internal/notifier/screen.go).
func (a *app) screens(ctx context.Context, cfg config.Config) map[string]string {
	out := map[string]string{}
	for area, sat := range cfg.Satellites {
		if cfg.Notifier.Indicators[area] != "" {
			continue // the ring shows it
		}
		node := a.deviceNode(a.entityDevice(sat))
		if node != "" && a.hasAction(ctx, "esphome", node+"_notify", "message", "tag", "seconds") {
			out[area] = "esphome." + node + "_notify"
			a.log.Info("satellite shows waiting messages on its screen", "area", area, "action", out[area])
		}
	}
	return out
}

// slug is how ESPHome names a node in its actions: lower case, anything
// but letters and digits an underscore.
func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
