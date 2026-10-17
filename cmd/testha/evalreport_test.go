package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A report is named after its setup only and replaces the last one; the
// overview links it without a date in the name.
func TestReportNamedAfterSetup(t *testing.T) {
	dir := t.TempDir()
	r := Report{Setup: "qwen-lokal", Models: []string{"main: qwen"}, Mira: "0.6.19", Date: "2026-10-17 12:00", Runs: 3,
		Scenarios: []ScenarioResult{{Name: "licht", Core: true, Passed: true, PassedRuns: 3, Runs: []RunResult{{Passed: true}}}}}
	for _, date := range []string{"2026-10-17 12:00", "2026-10-18 09:00"} {
		r.Date = date
		if err := r.write(dir); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "results", "*"))
	if len(files) != 2 || filepath.Base(files[0]) != "qwen-lokal.json" || filepath.Base(files[1]) != "qwen-lokal.md" {
		t.Errorf("files: %v", files)
	}
	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if !strings.Contains(string(readme), "[qwen-lokal.md](results/qwen-lokal.md)") || !strings.Contains(string(readme), "2026-10-18 09:00") ||
		!strings.Contains(string(readme), "main: qwen") {
		t.Errorf("overview:\n%s", readme)
	}
}
