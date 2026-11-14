package main

import "testing"

// The steps of the messages' kinds wait for these lines of the eval service
// (docs/spec/evals.md, Messages and their kinds, decision 3).
func TestKindStepsKnowTheirLines(t *testing.T) {
	found := `time=2026-11-14T17:53:24.336+01:00 level=INFO msg="found devices said" component=found-devices count=2`
	if m := foundSaid.FindStringSubmatch(found); m == nil || m[1] != "2" {
		t.Errorf("found devices: %v", m)
	}
	kept := `time=2026-11-14T17:53:24.336+01:00 level=INFO msg="answering machine thread kept" thread=01m7 started=true recordings=2 expires=2026-11-19T17:53:24+01:00`
	if m := machineKept.FindStringSubmatch(kept); m == nil || m[1] != "2" {
		t.Errorf("answering machine: %v", m)
	}
}

// Every scenario file parses with the runner's fields; the sixteen of the
// messages' kinds are there.
func TestScenarioFilesParse(t *testing.T) {
	all, err := loadScenarios("../../evals/scenarios", "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := 0
	for _, sc := range all {
		for _, st := range sc.Steps {
			if st.Message != nil || len(st.Devices) > 0 || len(st.Recordings) > 0 {
				kinds++
				break
			}
		}
	}
	if kinds != 12 { // the four of the inbox put mails
		t.Errorf("%d scenarios put a message, a device or a recording, want 12", kinds)
	}
}
