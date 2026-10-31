package main

import (
	"context"
	"testing"

	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// A satellite whose own ring shows the room's messages gets no screen hint
// on top; another satellite of the room keeps its screen
// (docs/spec/satelliten-raeume.md, Rings and screens).
func TestASatelliteWhoseRingShowsGetsNoScreenHint(t *testing.T) {
	a, ha, _ := deliveryApp(t)
	ha.SetRegistry(nil,
		[]haws.Device{{ID: "dot"}, {ID: "dot-ring", ViaDeviceID: "dot"}, {ID: "show"}, {ID: "echo"}},
		[]haws.Entity{
			{EntityID: "assist_satellite.dot", DeviceID: "dot"},
			{EntityID: "light.dot_ring", DeviceID: "dot-ring"},
			{EntityID: "assist_satellite.show", DeviceID: "show"},
			{EntityID: "assist_satellite.echo", DeviceID: "echo"},
			{EntityID: "light.echo_ring", DeviceID: "echo"},
		})
	a.structure = structure.NewCache(structure.Options{Source: a.ha, Logger: a.log})
	if _, err := a.structure.Rebuild(context.Background()); err != nil {
		t.Fatal(err)
	}
	lights := []string{"light.dot_ring", "light.echo_ring"}
	for sat, want := range map[string]bool{"assist_satellite.dot": true, "assist_satellite.echo": true, "assist_satellite.show": false} {
		if got := a.ownLight(sat, lights); got != want {
			t.Errorf("ownLight(%s) = %v", sat, got)
		}
	}
}
