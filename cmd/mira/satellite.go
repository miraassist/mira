package main

import (
	"context"
	"strings"
)

// speakerOf is the media player of a satellite's device, where Mira plays
// her sounds (docs/spec/satellit.md); "" if the device has none.
func (a *app) speakerOf(ctx context.Context, deviceID string) string {
	entities, err := a.ha.Entities(ctx)
	if err != nil {
		return ""
	}
	for _, e := range entities {
		if e.DeviceID == deviceID && e.DisabledBy == "" && strings.HasPrefix(e.EntityID, "media_player.") {
			return e.EntityID
		}
	}
	return ""
}

// cardActionOf is the picture card action of a satellite's device
// (esphome.<node>_notify_picture, TECHO5); "" if the device has none.
func (a *app) cardActionOf(ctx context.Context, deviceID string) string {
	devices, err := a.ha.Devices(ctx)
	if err != nil {
		return ""
	}
	node := ""
	for _, d := range devices {
		if d.ID == deviceID {
			node = slug(d.Name)
		}
	}
	if node == "" {
		return ""
	}
	services, err := a.ha.Services(ctx)
	if err != nil {
		return ""
	}
	if _, ok := services["esphome"][node+"_notify_picture"]; !ok {
		return ""
	}
	return "esphome." + node + "_notify_picture"
}
