package alarm

import (
	"encoding/json"
	"os"
	"testing"
)

// The alarm's automations as the service builds them, kept in testdata for
// the integration's tests: Home Assistant checks them there
// (tests/ha/test_built_automations.py). UPDATE=1 writes them anew.
func TestTheBuiltAlarmsAreKeptForTheIntegrationsCheck(t *testing.T) {
	base := spec{ID: "mira_0123456789ab", Title: "Wecker Schlafzimmer", Time: "06:00", Owner: "anna", Area: "schlafzimmer",
		Satellite: "assist_satellite.show_assist_satellit", Speaker: "media_player.show_speaker",
		RiseURL:    "http://homeassistant.local:8123/api/mira/panel/ui/sounds/wake_rise.mp3",
		LoopURL:    "http://homeassistant.local:8123/api/mira/panel/ui/sounds/wake_loop.mp3",
		FailedText: "Dein Wecker morgen um 06:00 im Raum Schlafzimmer konnte nicht klingeln."}
	once := base
	once.Date = "2099-01-01"
	weekdays := base
	weekdays.Days = []string{"mon", "tue", "wed", "thu", "fri"}
	weekdays.Lights, weekdays.LightAt = []string{"light.farblampe"}, "05:50"
	radio := once
	radio.Music = &Music{Player: "media_player.wohnzimmer_ma", ContentID: "library://radio/1", ContentType: "music", Title: "SWR3"}
	configs := []any{build(once), build(weekdays), build(radio)}
	got, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "testdata/alarms.json"
	if os.Getenv("UPDATE") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if want, err := os.ReadFile(path); err != nil || string(want) != string(got) {
		t.Errorf("%s differs from what is built; run the test with UPDATE=1 and check the diff (%v)", path, err)
	}
}
