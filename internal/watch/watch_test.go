package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
)

// fakeHA plays Home Assistant's blueprint commands.
type fakeHA struct {
	have     map[string]string // path -> description
	saves    []map[string]any
	commands []string
	// saveErrs are the answers of the next blueprint/save calls, in order.
	saveErrs []error
	listErr  error
	subst    func(input map[string]any) (json.RawMessage, error)
}

func (h *fakeHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	h.commands = append(h.commands, fmt.Sprint(cmd["type"]))
	switch cmd["type"] {
	case "blueprint/list":
		if h.listErr != nil {
			return nil, h.listErr
		}
		out := map[string]any{"homeassistant/motion_light.yaml": map[string]any{"metadata": map[string]any{"name": "Motion"}}}
		for path, desc := range h.have {
			out[path] = map[string]any{"metadata": map[string]any{"description": desc, "domain": "automation"}}
		}
		return json.Marshal(out)
	case "blueprint/save":
		if len(h.saveErrs) > 0 {
			err := h.saveErrs[0]
			h.saveErrs = h.saveErrs[1:]
			if err != nil {
				return nil, err
			}
		}
		h.saves = append(h.saves, cmd)
		path := cmd["path"].(string) + ".yaml"
		_, exists := h.have[path]
		if exists && cmd["allow_override"] != true {
			return nil, &haws.Error{Command: "blueprint/save", Code: "already_exists", Message: "File already exists"}
		}
		if h.have == nil {
			h.have = map[string]string{}
		}
		b, _ := ByName(strings.TrimPrefix(cmd["path"].(string), Folder))
		h.have[path] = fmt.Sprintf("mira-version: %d", b.Version)
		return json.Marshal(map[string]any{"overrides_existing": exists})
	case "blueprint/substitute":
		return h.subst(cmd["input"].(map[string]any))
	}
	return nil, fmt.Errorf("unexpected %v", cmd["type"])
}

func current() map[string]string {
	out := map[string]string{}
	for _, b := range All {
		out[b.Path()] = fmt.Sprintf("Mira's. mira-version: %d", b.Version)
	}
	return out
}

func newTest(ha *fakeHA) (*Blueprints, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	return New(Options{HA: ha, Lang: i18n.DE, Logger: log, Policy: &retry.Policy{Attempts: 3, Growth: retry.Fixed}}), &buf
}

func TestBlueprintsParse(t *testing.T) {
	if len(All) != 3 {
		t.Fatalf("blueprints: %d", len(All))
	}
	b, ok := ByName("water_runs_on")
	if !ok || b.Version < 1 || b.Path() != "mira/water_runs_on.yaml" {
		t.Fatalf("water_runs_on: %+v", b)
	}
	meter, _ := b.Input("meter")
	if meter.Kind != "entity" || meter.Domain != "sensor" || meter.DeviceClass != "water" {
		t.Errorf("meter: %+v", meter)
	}
	d, _ := b.Input("duration")
	if d.Kind != "number" || d.Min != 5 || d.Max != 1440 || d.Default != 30 {
		t.Errorf("duration: %+v", d)
	}
	w, _ := ByName("daily_routine")
	if in, _ := w.Input("windows"); in.Kind != "text" || in.HasDefault {
		t.Errorf("windows: %+v", in)
	}
}

// Every blueprint and input has its texts in every language.
func TestBlueprintTextsComplete(t *testing.T) {
	for _, l := range i18n.All {
		tx := catalog[l]
		for _, b := range All {
			if tx.about[b.Name] == "" || tx.messages[b.Name] == "" {
				t.Errorf("%s: %s lacks about or message", l, b.Name)
			}
			for _, in := range b.Inputs {
				if in.Name != MessageInput && tx.labels[in.Name] == "" {
					t.Errorf("%s: no label for %s.%s", l, b.Name, in.Name)
				}
			}
		}
	}
}

// CW-01 A missing blueprint is saved in Home Assistant
func TestCW01_AMissingBlueprintIsSavedInHomeAssistant(t *testing.T) {
	// Given Home Assistant has none of Mira's blueprints
	ha := &fakeHA{}
	w, _ := newTest(ha)
	// When the blueprints are made ready
	if err := w.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Then each blueprint is saved under "mira/" with blueprint/save
	if len(ha.saves) != len(All) {
		t.Fatalf("saves: %v", ha.saves)
	}
	for i, b := range All {
		s := ha.saves[i]
		if s["path"] != "mira/"+b.Name || s["domain"] != "automation" || s["allow_override"] != nil || s["yaml"] != b.YAML() {
			t.Errorf("save %d: %v", i, s["path"])
		}
	}
	if got := w.Available(context.Background()); len(got) != len(All) {
		t.Errorf("available: %d", len(got))
	}
}

// CW-02 An unchanged blueprint is not saved again
func TestCW02_AnUnchangedBlueprintIsNotSavedAgain(t *testing.T) {
	// Given Home Assistant has every blueprint in its current version
	ha := &fakeHA{have: current()}
	w, _ := newTest(ha)
	// When the blueprints are made ready
	if err := w.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Then nothing is saved
	if len(ha.saves) != 0 {
		t.Errorf("saves: %v", ha.saves)
	}
	// and the first use asks Home Assistant nothing more.
	w.Available(context.Background())
	if len(ha.commands) != 1 {
		t.Errorf("commands: %v", ha.commands)
	}
}

// CW-03 A changed blueprint replaces the old one and reaches its automations
func TestCW03_AChangedBlueprintReplacesTheOldOne(t *testing.T) {
	// Given Home Assistant has the blueprint "water_runs_on" in an older version
	// And two automations use it (Home Assistant reloads them itself when
	// the blueprint is saved over: checked on the test HA, row 12)
	ha := &fakeHA{have: current()}
	ha.have["mira/water_runs_on.yaml"] = "Mira's. mira-version: 0"
	w, log := newTest(ha)
	// When the blueprints are made ready
	if err := w.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Then the blueprint is saved with allow_override
	if len(ha.saves) != 1 || ha.saves[0]["path"] != "mira/water_runs_on" || ha.saves[0]["allow_override"] != true {
		t.Fatalf("saves: %v", ha.saves)
	}
	// And the reload of its automations is Home Assistant's: nothing of the
	// service's own.
	for _, c := range ha.commands {
		if c != "blueprint/list" && c != "blueprint/save" {
			t.Errorf("command %s", c)
		}
	}
	if !strings.Contains(log.String(), "blueprint updated") {
		t.Errorf("log: %s", log)
	}
}

// CW-04 Saving a blueprint is refused
func TestCW04_SavingABlueprintIsRefused(t *testing.T) {
	// Given Home Assistant refuses blueprint/save
	refusal := &haws.Error{Command: "blueprint/save", Code: "unauthorized", Message: "Unauthorized"}
	ha := &fakeHA{have: current(), saveErrs: []error{refusal}}
	delete(ha.have, "mira/daily_routine.yaml")
	w, log := newTest(ha)
	// When the blueprints are made ready
	err := w.Ready(context.Background())
	// Then a log line names the blueprint's path and Home Assistant's answer
	if err == nil || !strings.Contains(log.String(), "blueprint=mira/daily_routine.yaml") || !strings.Contains(log.String(), "Unauthorized") {
		t.Fatalf("err %v, log %s", err, log)
	}
	// And no watch is drafted from that blueprint: the catalog leaves it
	// out and the check refuses it.
	ha.saveErrs = []error{refusal}
	cat := w.Catalog(context.Background())
	if strings.Contains(cat, "daily_routine") || !strings.Contains(cat, "water_runs_on") {
		t.Errorf("catalog: %s", cat)
	}
	if _, _, err := w.Check("daily_routine", map[string]any{"power": "sensor.p", "windows": "07:00-08:00"}, nil); err == nil || !strings.Contains(err.Error(), "nicht bereit") {
		t.Errorf("check: %v", err)
	}
}

// CW-31 A lost connection is tried again, a refusal is not
func TestCW31_ALostConnectionIsTriedAgainARefusalIsNot(t *testing.T) {
	// Given Home Assistant's connection is lost at the first blueprint/save, then it succeeds
	ha := &fakeHA{have: current(), saveErrs: []error{haws.ErrNotConnected}}
	delete(ha.have, "mira/water_runs_on.yaml")
	w, log := newTest(ha)
	// When the blueprints are made ready
	if err := w.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Then the blueprint is saved on the second try
	if len(ha.saves) != 1 || !strings.Contains(log.String(), "saving a blueprint failed, trying again") {
		t.Fatalf("saves %v, log %s", ha.saves, log)
	}
	// And when Home Assistant refuses blueprint/save, it is not tried again
	// and the log names the path and the answer
	ha2 := &fakeHA{have: current(), saveErrs: []error{
		&haws.Error{Command: "blueprint/save", Code: "invalid_format", Message: "Invalid blueprint"}, nil}}
	delete(ha2.have, "mira/water_runs_on.yaml")
	w2, log2 := newTest(ha2)
	if err := w2.Ready(context.Background()); err == nil {
		t.Fatal("no error")
	}
	if len(ha2.saves) != 0 || strings.Contains(log2.String(), "trying again") ||
		!strings.Contains(log2.String(), "blueprint=mira/water_runs_on.yaml") || !strings.Contains(log2.String(), "Invalid blueprint") {
		t.Errorf("saves %v, log %s", ha2.saves, log2)
	}
}

// A failed list leaves every blueprint unready and is said with the answer.
func TestListRefused(t *testing.T) {
	ha := &fakeHA{listErr: &haws.Error{Command: "blueprint/list", Code: "unauthorized", Message: "Unauthorized"}}
	w, log := newTest(ha)
	if err := w.Ready(context.Background()); err == nil || len(w.Available(context.Background())) != 0 {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(log.String(), "could not list") || !strings.Contains(log.String(), "Unauthorized") {
		t.Errorf("log %s", log)
	}
}

// The check refuses what Home Assistant would not: inputs it does not have,
// numbers outside the bounds, the wrong entity, times that do not read; it
// fills the entry's text itself.
func TestCheck(t *testing.T) {
	w, _ := newTest(&fakeHA{have: current()})
	if err := w.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	entities := map[string]Entity{"sensor.wasser": {"sensor", "water"}, "sensor.aquarium": {"sensor", "power"}}
	lookup := func(id string) (Entity, bool) { e, ok := entities[id]; return e, ok }
	for _, c := range []struct {
		name   string
		inputs map[string]any
		want   string
	}{
		{"water_runs_on", map[string]any{"meter": "sensor.aquarium"}, "device_class water"},
		{"water_runs_on", map[string]any{"meter": "sensor.wasser", "duration": 2.0}, "außerhalb"},
		{"water_runs_on", map[string]any{"meter": "sensor.wasser", "duration": "viel"}, "braucht eine Zahl"},
		{"water_runs_on", map[string]any{"meter": "sensor.wasser", "gap": 40.0, "duration": 30.0}, "kürzer"},
		{"water_runs_on", map[string]any{"meter": "sensor.wasser", "colour": "red"}, "keine Eingabe"},
		{"daily_routine", map[string]any{"power": "sensor.aquarium", "windows": "morgens"}, "07:40-11:40"},
		{"leak", map[string]any{}, "gibt es nicht"},
	} {
		if _, _, err := w.Check(c.name, c.inputs, lookup); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %v: %v, want %q", c.name, c.inputs, err, c.want)
		}
	}
	_, in, err := w.Check("daily_routine", map[string]any{"power": "sensor.aquarium", "windows": "07:40-11:40, 14:20-21:00", "tolerance": "20", "message": "x"}, lookup)
	if err != nil || in["tolerance"] != 20.0 || !strings.Contains(in["message"].(string), "Tagesablauf") {
		t.Errorf("%v %v", in, err)
	}
}

// A refused substitution is the inputs' fault: not tried again.
func TestSubstituteRefusedOnce(t *testing.T) {
	calls := 0
	ha := &fakeHA{subst: func(map[string]any) (json.RawMessage, error) {
		calls++
		return nil, &haws.Error{Command: "blueprint/substitute", Code: "unknown_error", Message: "Missing input windows"}
	}}
	w, _ := newTest(ha)
	b, _ := ByName("daily_routine")
	_, err := w.Substitute(context.Background(), b, map[string]any{"power": "sensor.p"})
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "mira/daily_routine.yaml") || !strings.Contains(err.Error(), "Missing input windows") {
		t.Errorf("calls %d, err %v", calls, err)
	}
}
