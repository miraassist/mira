package main

import (
	"encoding/json"
	"os"
	"testing"
)

// The service's own version is the integration's: a build without the
// release's -X main.version reports it, as the eval reports do. 0.16.0 went
// out with it still 0.15.2.
func TestTheVersionIsTheIntegrations(t *testing.T) {
	raw, err := os.ReadFile("../../custom_components/mira/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Version string }
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if version != m.Version {
		t.Errorf("cmd/mira says %s, manifest.json %s", version, m.Version)
	}
}
