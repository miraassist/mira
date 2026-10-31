package main

import (
	"context"
	"strings"

	"github.com/miraassist/mira/internal/config"
)

// screens finds the on-screen overlays the satellites' devices offer: an
// ESPHome action "<node>_notify" with a message, a tag and a duration (an
// Echo Show with TECHO5 has esphome.show_notify). Nobody configures it; the
// notifier uses it, per satellite, to show that messages wait in its room
// (internal/notifier/screen.go). A satellite whose own ring shows them is
// left out: its ring is its sign.
func (a *app) screens(ctx context.Context, cfg config.Config) map[string]string {
	out := map[string]string{}
	for area, sats := range cfg.Satellites {
		for _, sat := range sats {
			if a.ownLight(sat, cfg.Notifier.Indicators[area]) {
				continue
			}
			node := a.deviceNode(a.entityDevice(sat))
			if node != "" && a.hasAction(ctx, "esphome", node+"_notify", "message", "tag", "seconds") {
				out[sat] = "esphome." + node + "_notify"
				a.log.Info("satellite shows waiting messages on its screen", "area", area, "satellite", sat, "action", out[sat])
			}
		}
	}
	return out
}

// ownLight says whether one of lights is on the satellite's device or on a
// device that hangs off it.
func (a *app) ownLight(sat string, lights []string) bool {
	device := a.entityDevice(sat)
	if device == "" {
		return false
	}
	devices := a.structure.Snapshot().Devices
	for _, light := range lights {
		if d := a.entityDevice(light); d == device || (d != "" && devices[d].ViaDeviceID == device) {
			return true
		}
	}
	return false
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
