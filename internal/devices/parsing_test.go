package devices

import (
	"encoding/json"
	"testing"
)

// PA-03 A unique_id that is a number is read (docs/spec/parsing.md): Home
// Assistant keeps it so; the entity is there with its digits.
func TestPA03_AUniqueIDThatIsANumberIsRead(t *testing.T) {
	var entries map[string]*entry
	raw := `{"sensor.power":{"entity_id":"sensor.power","device_id":"d1","unique_id":4712},
		"light.lamp":{"entity_id":"light.lamp","device_id":"d1","unique_id":"lamp-1"}}`
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("the entries do not read: %v", err)
	}
	if got := entries["sensor.power"]; got == nil || got.UniqueID != "4712" {
		t.Errorf("sensor.power: %+v", got)
	}
	if got := entries["light.lamp"]; got == nil || got.UniqueID != "lamp-1" {
		t.Errorf("light.lamp: %+v", got)
	}
}
