package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Report is one eval run of a model setup (docs/spec/evals.md).
type Report struct {
	Setup     string           `json:"setup"`
	Models    []string         `json:"models"`
	Mira      string           `json:"mira"`
	Date      string           `json:"date"`
	Runs      int              `json:"runs"`
	Scenarios []ScenarioResult `json:"scenarios"`
}

type ScenarioResult struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Core        bool        `json:"core"`
	PassedRuns  int         `json:"passed_runs"`
	AnswerRuns  int         `json:"answer_ok_runs"`
	Passed      bool        `json:"passed"`
	StepTimes   []StepTimes `json:"step_times"`
	Tokens      int         `json:"tokens"`
	Runs        []RunResult `json:"runs"`
}

// StepTimes is a step's answer time over the runs.
type StepTimes struct {
	Say     string  `json:"say"`
	Median  float64 `json:"median_seconds"`
	Max     float64 `json:"max_seconds"`
	ModelMs int64   `json:"model_ms_median"`
	Rating  string  `json:"rating"`
}

func (s *ScenarioResult) finish(runs int) {
	for _, r := range s.Runs {
		if r.Passed {
			s.PassedRuns++
		}
		if r.AnswerOK {
			s.AnswerRuns++
		}
		for _, st := range r.Steps {
			s.Tokens += st.Tokens
		}
	}
	need := runs
	if !s.Core {
		need = (runs*2 + 2) / 3 // 2 of 3
	}
	s.Passed = s.PassedRuns >= need
	steps := 0
	for _, r := range s.Runs {
		steps = max(steps, len(r.Steps))
	}
	for i := 0; i < steps; i++ {
		var secs []float64
		var model []int64
		say := ""
		for _, r := range s.Runs {
			if i < len(r.Steps) {
				secs = append(secs, r.Steps[i].Seconds)
				model = append(model, r.Steps[i].ModelMs)
				say = r.Steps[i].Say
			}
		}
		sort.Float64s(secs)
		sort.Slice(model, func(a, b int) bool { return model[a] < model[b] })
		st := StepTimes{Say: say, Median: secs[len(secs)/2], Max: secs[len(secs)-1], ModelMs: model[len(model)/2]}
		st.Rating = rating(st.Median)
		s.StepTimes = append(s.StepTimes, st)
	}
}

// rating sorts a spoken answer's time: good up to 3 s, usable up to 8 s.
func rating(sec float64) string {
	switch {
	case sec <= 3:
		return "gut"
	case sec <= 8:
		return "brauchbar"
	}
	return "langsam"
}

func (r Report) passed() (passed, total int) {
	for _, s := range r.Scenarios {
		total++
		if s.Passed {
			passed++
		}
	}
	return
}

func (r Report) print() {
	p, t := r.passed()
	fmt.Printf("\n%s: %d of %d scenarios passed\n", r.Setup, p, t)
}

func (r Report) markdown() string {
	var b strings.Builder
	p, t := r.passed()
	fmt.Fprintf(&b, "# Eval %s – %s\n\n", r.Setup, r.Date)
	fmt.Fprintf(&b, "Mira %s · %d Durchläufe je Szenario · **%d von %d Szenarien bestanden**\n\n", r.Mira, r.Runs, p, t)
	b.WriteString("Modelle:\n")
	for _, m := range r.Models {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	b.WriteString("\n| Szenario | Kern | Bestanden | Ergebnis + Verhalten | Antwort | Zeit je Schritt (Median / Max, Modell) | Tokens |\n|---|---|---|---|---|---|---|\n")
	for _, s := range r.Scenarios {
		var times []string
		for _, st := range s.StepTimes {
			times = append(times, fmt.Sprintf("%.1f / %.1f s, %.1f s (%s)", st.Median, st.Max, float64(st.ModelMs)/1000, st.Rating))
		}
		core, ok := "", "✗"
		if s.Core {
			core = "ja"
		}
		if s.Passed {
			ok = "✓"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d/%d | %d/%d | %s | %d |\n", s.Name, core, ok, s.PassedRuns, len(s.Runs), s.AnswerRuns, len(s.Runs), strings.Join(times, "<br>"), s.Tokens)
	}
	var notes []string
	for _, s := range r.Scenarios {
		for i, run := range s.Runs {
			for _, f := range run.Failures {
				notes = append(notes, fmt.Sprintf("- %s, Lauf %d: %s", s.Name, i+1, f))
			}
			for _, f := range run.Answers {
				notes = append(notes, fmt.Sprintf("- %s, Lauf %d (Antwort): %s", s.Name, i+1, f))
			}
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n## Auffälligkeiten\n\n" + strings.Join(notes, "\n") + "\n")
	}
	return b.String()
}

// write stores the report under evals/results/ and renews the overview in
// evals/README.md: the latest run per setup.
func (r Report) write(dir string) error {
	results := filepath.Join(dir, "results")
	if err := os.MkdirAll(results, 0o755); err != nil {
		return err
	}
	base := filepath.Join(results, strings.ReplaceAll(r.Date[:10], " ", "")+"-"+r.Setup)
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", raw, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(base+".md", []byte(r.markdown()), 0o644); err != nil {
		return err
	}
	fmt.Printf("report: %s.md\n", base)
	return writeOverview(dir)
}

func writeOverview(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "results", "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	latest := map[string]Report{}
	file := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var r Report
		if json.Unmarshal(raw, &r) == nil {
			latest[r.Setup] = r
			file[r.Setup] = strings.TrimSuffix(filepath.Base(f), ".json") + ".md"
		}
	}
	var b strings.Builder
	b.WriteString(`# Evals: womit Mira geprüft ist

Szenarien Ende zu Ende mit echtem Modell und Test-Home-Assistant, ohne Sprache
(docs/spec/evals.md). Je Szenario 3 Durchläufe; Kern-Szenarien bestehen bei 3 von 3,
die übrigen bei 2 von 3. Ein Schritt über dem Antwort-Timeout (Sprache 15 s) gilt als
nicht bestanden.

Ausführen: ` + "`go run ./cmd/testha eval --setup evals/setups/<setup>.yaml`" + `

| Setup | Stand | Mira | bestanden | Bericht |
|---|---|---|---|---|
`)
	for _, name := range keys(latest) {
		r := latest[name]
		p, t := r.passed()
		fmt.Fprintf(&b, "| %s | %s | %s | %d von %d | [%s](results/%s) |\n", name, r.Date, r.Mira, p, t, file[name], file[name])
	}
	return os.WriteFile(filepath.Join(dir, "README.md"), []byte(b.String()), 0o644)
}
