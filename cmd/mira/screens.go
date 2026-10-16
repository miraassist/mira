package main

import (
	"context"
	"slices"
	"strings"

	"github.com/sker65/mira/internal/config"
)

// screens finds, for satellites without an indicator light, an on-screen
// overlay their device offers: an ESPHome action "<node>_notify" with a
// message, a tag and a duration (an Echo Show with TECHO5 has
// esphome.show_notify). Nobody configures it; the notifier uses it to show
// that messages wait (internal/notifier/screen.go).
func (a *app) screens(ctx context.Context, cfg config.Config) map[string]string {
	out := map[string]string{}
	if a.ha == nil || len(cfg.Satellites) == 0 {
		return out
	}
	services, err := a.ha.Services(ctx)
	if err != nil {
		a.log.Warn("could not read home assistant's actions; no screen hints", "error", err)
		return out
	}
	entities, err := a.ha.Entities(ctx)
	if err != nil {
		return out
	}
	devices, err := a.ha.Devices(ctx)
	if err != nil {
		return out
	}
	deviceOf := map[string]string{}
	for _, e := range entities {
		deviceOf[e.EntityID] = e.DeviceID
	}
	nameOf := map[string]string{}
	for _, d := range devices {
		nameOf[d.ID] = d.Name
	}
	for area, sat := range cfg.Satellites {
		if cfg.Notifier.Indicators[area] != "" {
			continue
		}
		node := slug(nameOf[deviceOf[sat]])
		if node == "" {
			continue
		}
		fields, ok := services["esphome"][node+"_notify"]
		if ok && slices.Contains(fields, "message") && slices.Contains(fields, "tag") && slices.Contains(fields, "seconds") {
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
