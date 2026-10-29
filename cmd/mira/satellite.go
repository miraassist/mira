package main

import (
	"context"
)

// speakerOf is the media player of a satellite's device, where Mira plays
// her sounds (docs/spec/satellit.md); "" if the device has none.
func (a *app) speakerOf(_ context.Context, deviceID string) string {
	return a.deviceEntity(deviceID, "media_player")
}

// cardActionOf is the picture card action of a satellite's device
// (esphome.<node>_notify_picture, TECHO5); "" if the device has none.
func (a *app) cardActionOf(ctx context.Context, deviceID string) string {
	node := a.deviceNode(deviceID)
	if node == "" || !a.hasAction(ctx, "esphome", node+"_notify_picture") {
		return ""
	}
	return "esphome." + node + "_notify_picture"
}
