package skillha

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
)

// fakeHA is Home Assistant as the sync sees it: its blueprints by path with
// their YAML, its automations by id, and every call made.
type fakeHA struct {
	mu          sync.Mutex
	blueprints  map[string]string // path → yaml
	automations map[string]map[string]any
	calls       []string
	saves       []map[string]any
	// listFails, saveRefused and configRefused make Home Assistant fail.
	listFails     error
	saveRefused   map[string]bool
	configRefused bool
	reloads       []string
	labels        []string
}

func newFakeHA() *fakeHA {
	return &fakeHA{blueprints: map[string]string{}, automations: map[string]map[string]any{}, saveRefused: map[string]bool{}}
}

func (h *fakeHA) Call(ctx context.Context, cmd map[string]any) (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	typ, _ := cmd["type"].(string)
	h.calls = append(h.calls, typ)
	switch typ {
	case "blueprint/list":
		if h.listFails != nil {
			return nil, h.listFails
		}
		out := map[string]any{}
		for p, y := range h.blueprints {
			var doc struct {
				Blueprint map[string]any `yaml:"blueprint"`
			}
			if err := yaml.Unmarshal([]byte(y), &doc); err != nil {
				out[p] = map[string]any{"error": err.Error()}
				continue
			}
			out[p] = map[string]any{"metadata": doc.Blueprint}
		}
		return json.Marshal(out)
	case "blueprint/save":
		bpPath := cmd["path"].(string) + ".yaml"
		h.saves = append(h.saves, cmd)
		if h.saveRefused[bpPath] {
			return nil, &haws.Error{Command: typ, Code: "invalid_format", Message: "refused for the test"}
		}
		if _, exists := h.blueprints[bpPath]; exists && cmd["allow_override"] != true {
			return nil, &haws.Error{Command: typ, Code: "home_assistant_error", Message: "File already exists"}
		}
		h.blueprints[bpPath] = cmd["yaml"].(string)
		return json.RawMessage(`{"overrides_existing":false}`), nil
	case "mira/automation/label":
		h.labels = append(h.labels, cmd["automation_id"].(string))
		return json.RawMessage(`{}`), nil
	}
	return nil, &haws.Error{Command: typ, Code: "unknown_command", Message: "not in the fake"}
}

func (h *fakeHA) CallService(ctx context.Context, domain, service string, data map[string]any, target *haws.Target) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, domain+"."+service)
	h.reloads = append(h.reloads, data["id"].(string))
	return nil
}

func (h *fakeHA) SaveAutomationConfig(ctx context.Context, id string, config map[string]any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, "POST "+id)
	if h.configRefused {
		return &haws.ConfigError{Method: "POST", ID: id, Status: "400 Bad Request", Answer: `{"message":"Message malformed"}`, Code: 400}
	}
	h.automations[id] = config
	return nil
}

func (h *fakeHA) DeleteAutomationConfig(ctx context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, "DELETE "+id)
	delete(h.automations, id)
	return nil
}

func (h *fakeHA) AutomationConfig(ctx context.Context, id string) (map[string]any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, "GET "+id)
	a, ok := h.automations[id]
	if !ok {
		return nil, haws.ErrNoAutomation
	}
	// As JSON, the way Home Assistant answers.
	raw, _ := json.Marshal(maps.Clone(a))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	out["id"] = id
	return out, nil
}

func (h *fakeHA) count(call string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.calls {
		if c == call {
			n++
		}
	}
	return n
}

type fakeOwn struct{ ids []string }

func (o *fakeOwn) RecordOwnAutomation(ctx context.Context, id, kind string) error {
	o.ids = append(o.ids, id+":"+kind)
	return nil
}
func (o *fakeOwn) IsOwnAutomation(context.Context, string) (bool, error)           { return true, nil }
func (o *fakeOwn) ForgetOwnAutomation(context.Context, string) error               { return nil }
func (o *fakeOwn) OwnAutomationsTakenOver(context.Context) (bool, error)           { return true, nil }
func (o *fakeOwn) TakeOverOwnAutomations(context.Context, map[string]string) error { return nil }

var noWait = &retry.Policy{Attempts: 3, First: time.Millisecond, Growth: retry.Exponential}

// blueprintV is the skill's blueprint "zu_ruhig.yaml" in a version with the
// inputs given as YAML lines.
func blueprintV(version string, inputs string) string {
	return "blueprint:\n  name: Zu ruhig\n  description: \"Meldet Ruhe. mira-version: " + version + "\"\n  domain: automation\n  input:\n" + inputs +
		"triggers:\n  - trigger: time\n    at: !input window_end\nactions:\n  - event: zu_ruhig\n"
}

const inputsV1 = `    window_end:
      name: Ende
      selector: { time: {} }
    minutes:
      name: Minuten
      default: 30
      selector: { number: { min: 1, max: 120 } }
    sensor:
      name: Sensor
      default: binary_sensor.flur
      selector: { entity: { filter: [ { domain: binary_sensor } ] } }
`

// skill is the switched-on skill "zu-ruhig" with the blueprint and one
// automation filling inputs.
func skill(bp string, inputs map[string]any) skills.Skill {
	return skills.Skill{Name: "zu-ruhig", Files: fstest.MapFS{"SKILL.md": {Data: []byte("---\n")},
		"blueprints/zu_ruhig.yaml": {Data: []byte(bp)}},
		Automations: []skills.Automation{{ID: "nachts", Blueprint: "zu_ruhig.yaml", Alias: "Zu ruhig in der Nacht", Inputs: inputs}}}
}

func run(t *testing.T, ha *fakeHA, list ...skills.Skill) (*Sync, *fakeOwn) {
	t.Helper()
	own := &fakeOwn{}
	s := New(Options{HA: ha, Own: own, Lang: i18n.DE, Policy: noWait})
	s.Run(context.Background(), list)
	return s, own
}

func problemTexts(s *Sync) []string {
	var out []string
	for _, p := range s.Problems(i18n.DE) {
		out = append(out, p.Skill+": "+p.Text(i18n.DE))
	}
	return out
}

func wantProblem(t *testing.T, s *Sync, parts ...string) {
	t.Helper()
	texts := problemTexts(s)
	if len(texts) != 1 {
		t.Fatalf("problems %q, want one", texts)
	}
	for _, p := range parts {
		if !strings.Contains(texts[0], p) {
			t.Errorf("problem %q does not name %q", texts[0], p)
		}
	}
}

const bpPath = "mira_skills/zu-ruhig/zu_ruhig.yaml"

func TestSB03_ANewSkillBlueprintIsSaved(t *testing.T) {
	ha := newFakeHA()
	s, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"}))
	if len(ha.saves) != 1 || ha.saves[0]["path"] != "mira_skills/zu-ruhig/zu_ruhig" || ha.saves[0]["allow_override"] != nil {
		t.Fatalf("saves %v", ha.saves)
	}
	if ha.blueprints[bpPath] == "" || len(problemTexts(s)) > 0 {
		t.Errorf("blueprint %q, problems %v", ha.blueprints[bpPath], problemTexts(s))
	}
}

func TestSB04_ABlueprintInTheSameVersionIsLeftAlone(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	run(t, ha, skill(blueprintV("1", inputsV1+"    extra:\n      default: 1\n      selector: { number: {} }\n"), map[string]any{"window_end": "07:00"}))
	if ha.count("blueprint/list") != 1 || len(ha.saves) != 0 {
		t.Errorf("lists %d, saves %v", ha.count("blueprint/list"), ha.saves)
	}
}

func TestSB05_ACompatibleUpdateIsSavedOverTheOld(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	// The same inputs, a new one with a default, an entity selector widened.
	v2 := strings.Replace(inputsV1, "filter: [ { domain: binary_sensor } ]", "filter: [ { domain: [binary_sensor, sensor] } ]", 1) +
		"    quiet:\n      name: Leise\n      default: false\n      selector: { boolean: {} }\n"
	s, _ := run(t, ha, skill(blueprintV("2", v2), map[string]any{"window_end": "07:00"}))
	if len(ha.saves) != 1 || ha.saves[0]["allow_override"] != true || !strings.Contains(ha.blueprints[bpPath], "mira-version: 2") {
		t.Fatalf("saves %v", ha.saves)
	}
	if len(problemTexts(s)) > 0 {
		t.Errorf("problems %v", problemTexts(s))
	}
	// Home Assistant reloads the automations itself: the sync does not.
	if ha.count("automation.reload") != 1 || ha.reloads[0] != AutomationID("zu-ruhig", "nachts") {
		t.Errorf("reloads %v: only the new automation's", ha.reloads)
	}
}

func TestSB06_ARemovedInputIsRefused(t *testing.T) {
	ha := newFakeHA()
	old := blueprintV("1", inputsV1)
	ha.blueprints[bpPath] = old
	v2 := strings.Replace(inputsV1, "    window_end:\n      name: Ende\n      selector: { time: {} }\n", "", 1)
	s, _ := run(t, ha, skill(blueprintV("2", v2), map[string]any{}))
	if len(ha.saves) != 0 || ha.blueprints[bpPath] != old {
		t.Errorf("saved %v", ha.saves)
	}
	wantProblem(t, s, "zu-ruhig", bpPath, "„window_end“ fehlt", "neue Blueprint-Datei")
}

func TestSB07_ANewInputWithoutADefaultIsRefused(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	s, _ := run(t, ha, skill(blueprintV("2", inputsV1+"    room:\n      name: Raum\n      selector: { area: {} }\n"), map[string]any{"window_end": "07:00"}))
	if len(ha.saves) != 0 {
		t.Errorf("saved %v", ha.saves)
	}
	wantProblem(t, s, bpPath, "„room“ ist neu und hat keinen Vorgabewert")
}

func TestSB08_AChangedSelectorOrALostDefaultIsRefused(t *testing.T) {
	for name, c := range map[string]struct{ v2, want string }{
		"selector": {strings.Replace(inputsV1, "selector: { number: { min: 1, max: 120 } }", "selector: { text: {} }", 1), "„minutes“ ist jetzt text statt number"},
		"domains":  {strings.Replace(inputsV1, "domain: binary_sensor", "domain: light", 1), "„sensor“ nimmt nur noch light statt binary_sensor"},
		"default":  {strings.Replace(inputsV1, "      default: 30\n", "", 1), "„minutes“ hat keinen Vorgabewert mehr"},
	} {
		t.Run(name, func(t *testing.T) {
			ha := newFakeHA()
			ha.blueprints[bpPath] = blueprintV("1", inputsV1)
			s, _ := run(t, ha, skill(blueprintV("2", c.v2), map[string]any{"window_end": "07:00", "minutes": 20}))
			if len(ha.saves) != 0 {
				t.Errorf("saved %v", ha.saves)
			}
			wantProblem(t, s, bpPath, c.want)
		})
	}
}

func TestSB09_ABlueprintThatCannotBeUsed(t *testing.T) {
	for name, c := range map[string]struct{ yaml, want string }{
		"no version": {strings.Replace(blueprintV("1", inputsV1), "mira-version: 1", "", 1), "no mira-version"},
		"a script":   {strings.Replace(blueprintV("1", inputsV1), "domain: automation", "domain: script", 1), `domain "script"`},
		"not yaml":   {"blueprint: [\n", "not YAML"},
	} {
		t.Run(name, func(t *testing.T) {
			ha := newFakeHA()
			s, _ := run(t, ha, skill(c.yaml, map[string]any{"window_end": "07:00"}))
			if len(ha.saves) != 0 || ha.count("POST "+AutomationID("zu-ruhig", "nachts")) != 0 {
				t.Errorf("saved %v, calls %v", ha.saves, ha.calls)
			}
			wantProblem(t, s, "blueprints/zu_ruhig.yaml", c.want, "Der Skill läuft")
		})
	}
}

func TestSB10_HomeAssistantCannotListItsBlueprints(t *testing.T) {
	ha := newFakeHA()
	ha.listFails = errors.New("connection lost")
	s, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"}),
		skills.Skill{Name: "ohne-blueprints"})
	if ha.count("blueprint/list") != 3 || len(ha.saves) != 0 || len(ha.automations) != 0 {
		t.Errorf("lists %d, saves %v", ha.count("blueprint/list"), ha.saves)
	}
	wantProblem(t, s, "zu-ruhig", "Home Assistant konnte nicht nach seinen Blueprints gefragt werden", "connection lost")
}

func TestSB11_ASaveRefused(t *testing.T) {
	ha := newFakeHA()
	ha.saveRefused[bpPath] = true
	sk := skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"})
	sk.Files.(fstest.MapFS)["blueprints/anderer.yaml"] = &fstest.MapFile{Data: []byte(blueprintV("1", inputsV1))}
	s, _ := run(t, ha, sk)
	if ha.blueprints["mira_skills/zu-ruhig/anderer.yaml"] == "" || ha.blueprints[bpPath] != "" {
		t.Errorf("blueprints %v", ha.blueprints)
	}
	if ha.count("blueprint/save") != 2 {
		t.Errorf("a refusal was tried again: %v", ha.calls)
	}
	wantProblem(t, s, bpPath, "refused for the test")
}

func TestSB12_ADeclaredAutomationIsMade(t *testing.T) {
	ha := newFakeHA()
	s, own := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"}))
	id := "mira_skill_zu-ruhig_nachts"
	got := ha.automations[id]
	if got == nil {
		t.Fatalf("no automation; calls %v, problems %v", ha.calls, problemTexts(s))
	}
	want := map[string]any{"path": bpPath, "input": map[string]any{"window_end": "07:00"}}
	if !same(got["use_blueprint"], want) || got["alias"] != "Zu ruhig in der Nacht" || !strings.Contains(got["description"].(string), "zu-ruhig") {
		t.Errorf("automation %v", got)
	}
	if len(ha.labels) != 1 || ha.labels[0] != id || len(own.ids) != 1 || own.ids[0] != id+":task" || len(ha.reloads) != 1 {
		t.Errorf("labels %v, own %v, reloads %v", ha.labels, own.ids, ha.reloads)
	}
}

func TestSB13_AnAutomationWithTheSameInputsIsLeftAlone(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	id := AutomationID("zu-ruhig", "nachts")
	ha.automations[id] = map[string]any{"alias": "x", "use_blueprint": map[string]any{"path": bpPath, "input": map[string]any{"window_end": "07:00", "minutes": 20}}}
	run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00", "minutes": 20}))
	if ha.count("GET "+id) != 1 || ha.count("POST "+id) != 0 {
		t.Errorf("calls %v", ha.calls)
	}
}

func TestSB14_AChangedSettingMakesTheAutomationAnew(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	id := AutomationID("zu-ruhig", "nachts")
	ha.automations[id] = map[string]any{"alias": "Meine Ruhe", "description": "von mir", "use_blueprint": map[string]any{"path": bpPath, "input": map[string]any{"window_end": "07:00"}}}
	run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "08:00"}))
	got := ha.automations[id]
	if !same(got["use_blueprint"], map[string]any{"path": bpPath, "input": map[string]any{"window_end": "08:00"}}) {
		t.Errorf("use_blueprint %v", got["use_blueprint"])
	}
	if got["alias"] != "Meine Ruhe" || got["description"] != "von mir" || got["id"] != nil {
		t.Errorf("automation %v: the user's alias and description stay, no id in the config", got)
	}
}

func TestSB15_AnEmptySettingLeavesTheInputToTheBlueprintsDefault(t *testing.T) {
	// The skill side (an empty setting leaves the input out) is
	// skills.TestAutomationInputsFromSettings; here the automation is made
	// without the input, which has a default.
	ha := newFakeHA()
	s, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"}))
	in := ha.automations[AutomationID("zu-ruhig", "nachts")]["use_blueprint"].(map[string]any)["input"].(map[string]any)
	if _, has := in["minutes"]; has || len(problemTexts(s)) > 0 {
		t.Errorf("inputs %v, problems %v", in, problemTexts(s))
	}
}

func TestSB16_ARequiredInputWithoutAValue(t *testing.T) {
	ha := newFakeHA()
	s, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{}))
	if len(ha.automations) != 0 {
		t.Errorf("made %v", ha.automations)
	}
	wantProblem(t, s, "„nachts“", "„window_end“", bpPath)
}

func TestSB17_AnInputTheBlueprintDoesNotHave(t *testing.T) {
	ha := newFakeHA()
	s, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00", "fenster": "x"}))
	if len(ha.automations) != 0 {
		t.Errorf("made %v", ha.automations)
	}
	wantProblem(t, s, "„nachts“", "„fenster“", bpPath)
}

func TestSB19_NothingIsDeleted(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	ha.blueprints["mira_skills/alt/alt.yaml"] = blueprintV("1", inputsV1)
	id := AutomationID("zu-ruhig", "nachts")
	ha.automations[id] = map[string]any{"use_blueprint": map[string]any{"path": bpPath, "input": map[string]any{"window_end": "07:00"}}}
	// "alt" is switched off; "zu-ruhig" no longer declares the automation.
	sk := skill(blueprintV("1", inputsV1), nil)
	sk.Automations = nil
	run(t, ha, sk)
	for _, c := range ha.calls {
		if strings.HasPrefix(c, "DELETE") || c == "blueprint/delete" || c == "blueprint/save" || strings.HasPrefix(c, "POST") {
			t.Errorf("call %s", c)
		}
	}
	if len(ha.blueprints) != 2 || len(ha.automations) != 1 {
		t.Errorf("blueprints %v, automations %v", ha.blueprints, ha.automations)
	}
}

func TestSB20_ARefusedBlueprintsAutomationsStayAsTheyAre(t *testing.T) {
	ha := newFakeHA()
	ha.blueprints[bpPath] = blueprintV("1", inputsV1)
	id := AutomationID("zu-ruhig", "nachts")
	ha.automations[id] = map[string]any{"use_blueprint": map[string]any{"path": bpPath, "input": map[string]any{"window_end": "07:00"}}}
	v2 := inputsV1 + "    room:\n      selector: { area: {} }\n"
	run(t, ha, skill(blueprintV("2", v2), map[string]any{"window_end": "08:00", "room": "kueche"}))
	if ha.count("GET "+id) != 0 || ha.count("POST "+id) != 0 {
		t.Errorf("calls %v", ha.calls)
	}
}

func TestSB21_SavingTheAutomationFails(t *testing.T) {
	ha := newFakeHA()
	ha.configRefused = true
	s, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"}))
	wantProblem(t, s, "„nachts“", "Message malformed", "nächsten Start")
	// At the next start: tried again.
	ha.configRefused = false
	s2, _ := run(t, ha, skill(blueprintV("1", inputsV1), map[string]any{"window_end": "07:00"}))
	if ha.automations[AutomationID("zu-ruhig", "nachts")] == nil || len(problemTexts(s2)) > 0 {
		t.Errorf("not made at the next start: %v", problemTexts(s2))
	}
}
