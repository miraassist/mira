package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/watch"
)

// groupHouse plays Home Assistant for the group watch: its blueprints, the
// house's floors, areas and group helpers, the members of a selection as
// its templates render them, and the label and entity registries.
type groupHouse struct {
	*blueprintHA
	entities map[string]watch.Member // id -> member, with its domain and class below
	classes  map[string]string
	floors   map[string][]string // floor id -> areas
	labelled map[string][]string // entity -> labels
	noLabel  bool                // an integration older than the label
	refuse   *haws.Error         // the entity registry's update refused
	updates  []map[string]any
}

func (h *groupHouse) Call(ctx context.Context, cmd map[string]any) (json.RawMessage, error) {
	switch cmd["type"] {
	case "config/label_registry/list":
		labels := []map[string]any{{"label_id": "mira"}}
		if !h.noLabel {
			labels = append(labels, map[string]any{"label_id": watch.LabelDontWatch})
		}
		return json.Marshal(labels)
	case "config/entity_registry/get":
		return json.Marshal(map[string]any{"entity_id": cmd["entity_id"], "labels": nonNil(h.labelled[cmd["entity_id"].(string)])})
	case "config/entity_registry/update":
		if h.refuse != nil {
			return nil, h.refuse
		}
		h.updates = append(h.updates, cmd)
		h.labelled[cmd["entity_id"].(string)] = cmd["labels"].([]string)
		return json.Marshal(map[string]any{"entity_entry": map[string]any{}})
	}
	return h.blueprintHA.Call(ctx, cmd)
}

// RenderTemplate answers the house's structure, or the members of the
// selection the variables name (the label leaves a member out, unless a
// group helper is chosen).
func (h *groupHouse) RenderTemplate(_ context.Context, tpl string, vars map[string]any) (json.RawMessage, error) {
	if strings.HasPrefix(tpl, `{"floors"`) {
		var floors []map[string]any
		areas := map[string]string{"garten": "Garten", "terrasse": "Terrasse"}
		for _, f := range []string{"erdgeschoss", "dachgeschoss"} {
			floors = append(floors, map[string]any{"id": f, "name": strings.ToUpper(f[:1]) + f[1:], "areas": h.floors[f]})
			for _, a := range h.floors[f] {
				areas[a] = strings.ToUpper(a[:1]) + a[1:]
			}
		}
		return json.Marshal(map[string]any{"floors": floors, "areas": areas,
			"groups": []map[string]any{{"id": "binary_sensor.fenster_gast", "name": "Fenster Gast", "device_class": "window"}}})
	}
	var out []watch.Member
	places := slices.Clone(vars["areas"].([]string))
	for _, f := range vars["floors"].([]string) {
		places = append(places, h.floors[f]...)
	}
	classes := vars["classes"].([]string)
	for _, id := range sortedKeys(h.entities) {
		mb := h.entities[id]
		domain, _, _ := strings.Cut(id, ".")
		if domain != vars["domain"] || len(classes) > 0 && !slices.Contains(classes, h.classes[id]) ||
			slices.Contains(h.labelled[id], watch.LabelDontWatch) && strings.Contains(tpl, "label_entities") {
			continue
		}
		if len(places) > 0 && !slices.Contains(places, strings.ToLower(mb.Area)) {
			continue
		}
		out = append(out, mb)
	}
	return json.Marshal(out)
}

func groupSetup(t *testing.T) (*env, *groupHouse, *bytes.Buffer) {
	t.Helper()
	e, bp := watchSetup(t)
	h := &groupHouse{blueprintHA: bp,
		entities: map[string]watch.Member{
			"light.kuche":                {ID: "light.kuche", Name: "Licht Küche", State: "off", Area: "kuche"},
			"light.terrasse":             {ID: "light.terrasse", Name: "Licht Terrasse", State: "off", Area: "terrasse"},
			"light.garten":               {ID: "light.garten", Name: "Licht Garten", State: "off", Area: "garten"},
			"binary_sensor.fenster_bad":  {ID: "binary_sensor.fenster_bad", Name: "Fenster Bad", State: "off", Area: "bad"},
			"binary_sensor.haustur":      {ID: "binary_sensor.haustur", Name: "Haustür", State: "off", Area: "flur"},
			"binary_sensor.fenster_buro": {ID: "binary_sensor.fenster_buro", Name: "Fenster Büro", State: "off", Area: "buro"},
			"binary_sensor.bewegung_bad": {ID: "binary_sensor.bewegung_bad", Name: "Bewegung Bad", State: "off", Area: "bad"},
		},
		classes: map[string]string{"binary_sensor.fenster_bad": "window", "binary_sensor.haustur": "door",
			"binary_sensor.fenster_buro": "window", "binary_sensor.bewegung_bad": "motion"},
		floors:   map[string][]string{"erdgeschoss": {"kuche", "bad", "flur"}, "dachgeschoss": {"buro"}},
		labelled: map[string][]string{},
	}
	var log bytes.Buffer
	opts := e.m.opts
	opts.Logger = slog.New(slog.NewTextHandler(&log, nil))
	opts.Watch = watch.New(watch.Options{HA: h, Templates: h, Lang: i18n.DE, Logger: opts.Logger, Policy: &retry.Policy{Attempts: 1}})
	e.m = New(opts)
	return e, h, &log
}

func confirm(t *testing.T, e *env, d Draft) error {
	t.Helper()
	content, _ := json.Marshal(d)
	_, err := e.m.Finisher().Commit(context.Background(), store.Thread{Content: content})
	return err
}

func savedUse(t *testing.T, e *env) (string, map[string]any) {
	t.Helper()
	if len(e.ha.automations) != 1 {
		t.Fatalf("automations: %v", e.ha.automations)
	}
	for _, cfg := range e.ha.automations {
		use, _ := cfg["use_blueprint"].(map[string]any)
		input, _ := use["input"].(map[string]any)
		return fmt.Sprint(use["path"]), input
	}
	return "", nil
}

// GW-04 A wish becomes a group watch
func TestGW04_AWishBecomesAGroupWatch(t *testing.T) {
	e, _, _ := groupSetup(t)
	// When the user says "Gib mir Bescheid, wenn ein Fenster offen ist und keiner da ist"
	e.model.answers = []string{`{"title":"Offen, keiner da","summary":"Ich sag euch Bescheid, wenn ein Fenster oder eine Tür offen ist und niemand zu Hause ist.","blueprint":"open_while_away","inputs":{}}`}
	d, err := e.m.DraftAutomation(context.Background(), "Gib mir Bescheid, wenn ein Fenster offen ist und keiner da ist",
		map[string]string{"watch": "Gruppe: alle Fenster und Türen"}, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	// The planner learnt how to choose a watch group and the house's floors.
	if ins := e.model.instructions[0]; !strings.Contains(ins, "open_while_away") || !strings.Contains(ins, "Etage Dachgeschoss (dachgeschoss): Buro (buro)") {
		t.Errorf("instruction lacks the group catalog:\n%s", ins)
	}
	// And the proposal named how many windows and doors it watches today
	text := e.m.ProposalText(d)
	if !strings.Contains(text, "Heute sind das 3: Fenster Bad, Fenster Büro, Haustür.") || !strings.Contains(text, "Verzögerung 5 min") {
		t.Errorf("proposal: %s", text)
	}
	// And agrees to the proposal
	if err := confirm(t, e, d); err != nil {
		t.Fatal(err)
	}
	// Then an automation with use_blueprint "mira/open_while_away.yaml" is saved as a task
	path, input := savedUse(t, e)
	if path != "mira/open_while_away.yaml" || !strings.Contains(fmt.Sprint(input["message"]), "Niemand ist zu Hause") {
		t.Errorf("saved %s %v", path, input)
	}
	// A floor narrows it: "alle Fenster im Dachgeschoss".
	e.model.answers = []string{`{"title":"Fenster oben","summary":"Ich melde, wenn oben ein Fenster 30 Minuten offen ist.","blueprint":"open_too_long","inputs":{"floors":["dachgeschoss"]}}`}
	d, err = e.m.DraftAutomation(context.Background(), "Sag Bescheid, wenn oben ein Fenster lange offen ist", nil, Origin{})
	if err != nil || !slices.Equal(d.Members, []string{"Fenster Büro"}) || !strings.Contains(e.m.ProposalText(d), "Etagen dachgeschoss") {
		t.Errorf("%v %v: %s", d.Members, err, e.m.ProposalText(d))
	}
}

// GW-05 An exception sets the label
func TestGW05_AnExceptionSetsTheLabel(t *testing.T) {
	e, h, _ := groupSetup(t)
	// When the user says "Sag Bescheid, wenn Licht ewig an ist, aber die Terrasse nicht"
	e.model.answers = []string{`{"title":"Licht an","summary":"Ich sag Bescheid, wenn ein Licht drei Stunden an ist, außer auf der Terrasse und im Garten.","blueprint":"left_on","inputs":{"hours":3},"exclude":["light.terrasse","light.garten"]}`}
	d, err := e.m.DraftAutomation(context.Background(), "Sag Bescheid, wenn Licht ewig an ist, aber die Terrasse nicht",
		map[string]string{"watch": "Gruppe: alle Lichter außer Terrasse"}, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	// And the proposal said that it is then in no group watch
	text := e.m.ProposalText(d)
	if !strings.Contains(text, "Ausgenommen: Licht Terrasse, Licht Garten; bekommt das Label „Mira: nicht überwachen“ und ist dann in keinem Wächter mehr.") ||
		!strings.Contains(text, "Heute sind das 1: Licht Küche.") {
		t.Errorf("proposal: %s", text)
	}
	if len(h.updates) != 0 {
		t.Fatalf("the label was set before the yes: %v", h.updates)
	}
	// And agrees
	if err := confirm(t, e, d); err != nil {
		t.Fatal(err)
	}
	// Then the terrace light carries the label "Mira: nicht überwachen"
	if !slices.Contains(h.labelled["light.terrasse"], watch.LabelDontWatch) || !slices.Contains(h.labelled["light.garten"], watch.LabelDontWatch) {
		t.Errorf("labels: %v", h.labelled)
	}
	if path, _ := savedUse(t, e); path != "mira/left_on.yaml" {
		t.Errorf("saved %s", path)
	}
	// An exception that is no member of the watch group goes back to the planner.
	e.model.instructions = nil
	e.model.answers = []string{`{"title":"x","summary":"x","blueprint":"left_on","inputs":{},"exclude":["light.keller"]}`,
		`{"title":"x","summary":"x","blueprint":"left_on","inputs":{}}`}
	if _, err := e.m.DraftAutomation(context.Background(), "x", nil, Origin{}); err != nil || len(e.model.instructions) != 2 ||
		!strings.Contains(e.model.instructions[1], "light.keller in exclude is no member") {
		t.Errorf("%v %d", err, len(e.model.instructions))
	}
	// Taken back on the user's word: watch_exception with watch true, by name.
	tool := toolNamed(t, e.m, ToolWatchException)
	out, err := tool.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"entity":"die Terrasse","watch":true}`)})
	if err != nil || slices.Contains(h.labelled["light.terrasse"], watch.LabelDontWatch) || !strings.Contains(out, "Licht Terrasse wird wieder überwacht") {
		t.Errorf("%q %v %v", out, err, h.labelled)
	}
}

// GW-06 A selection without members
func TestGW06_ASelectionWithoutMembers(t *testing.T) {
	e, _, _ := groupSetup(t)
	// When the user asks to watch the windows on a floor that has none
	e.model.answers = []string{`{"title":"Bewegung oben","summary":"x","blueprint":"motion_while_away","inputs":{"floors":["dachgeschoss"]}}`}
	d := Draft{}
	_, err := e.m.DraftAutomation(context.Background(), "Melde Bewegung im Dachgeschoss, wenn keiner da ist", nil, Origin{})
	// Then Mira says there are none
	var cannot ErrCannot
	if !errors.As(err, &cannot) || !strings.Contains(cannot.Reason, "keinen einzigen Sensor (motion, Dachgeschoss)") {
		t.Errorf("err %v", err)
	}
	// And nothing is saved (drafting ended at once, without a second draft)
	if len(e.ha.automations) != 0 || len(e.model.instructions) != 1 || d.Automation != nil {
		t.Errorf("automations %v, instructions %d", e.ha.automations, len(e.model.instructions))
	}
	// A floor that does not exist goes back to the planner with the floors there are.
	e.model.instructions = nil
	e.model.answers = []string{`{"title":"x","summary":"x","blueprint":"open_too_long","inputs":{"floors":["turm"]}}`, `{"error":"Einen Turm gibt es nicht; es gibt Erdgeschoss und Dachgeschoss."}`}
	_, err = e.m.DraftAutomation(context.Background(), "Fenster im Turm", nil, Origin{})
	if !errors.As(err, &cannot) || !strings.Contains(e.model.instructions[1], `eine Etage "turm" gibt es nicht; die Etagen: Erdgeschoss (erdgeschoss), Dachgeschoss (dachgeschoss)`) {
		t.Errorf("%v %v", err, e.model.instructions)
	}
}

// GW-07 The label cannot be set
func TestGW07_TheLabelCannotBeSet(t *testing.T) {
	e, h, log := groupSetup(t)
	// Given Home Assistant refuses the entity registry's update
	h.refuse = &haws.Error{Command: "config/entity_registry/update", Code: "unauthorized", Message: "Unauthorized"}
	// When the user names an exception
	e.model.answers = []string{`{"title":"Licht an","summary":"x","blueprint":"left_on","inputs":{},"exclude":["light.terrasse"]}`}
	d, err := e.m.DraftAutomation(context.Background(), "Sag Bescheid, wenn Licht ewig an ist, aber die Terrasse nicht", nil, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	err = confirm(t, e, d)
	// Then Mira says the exception could not be set
	if err == nil || !strings.Contains(err.Error(), "Die Ausnahme Licht Terrasse (light.terrasse) konnte ich nicht setzen, der Wächter ist nicht gespeichert") {
		t.Errorf("err %v", err)
	}
	if len(e.ha.automations) != 0 {
		t.Errorf("saved: %v", e.ha.automations)
	}
	// And the log names the entity and Home Assistant's answer
	if !strings.Contains(log.String(), "entity=light.terrasse") || !strings.Contains(log.String(), "Unauthorized") {
		t.Errorf("log: %s", log)
	}
	// An integration without the label: the error says so, nothing is updated.
	h.refuse, h.noLabel = nil, true
	if err := confirm(t, e, d); err == nil || !strings.Contains(err.Error(), "needs an update") || len(h.updates) != 0 {
		t.Errorf("%v %v", err, h.updates)
	}
}

// The field "watch" takes a watch group as words; the drafting checks it.
func TestTheWatchFieldTakesAWatchGroup(t *testing.T) {
	e, _, _ := groupSetup(t)
	for _, v := range []string{"Gruppe: alle Fenster im Dachgeschoss", "group: all lights"} {
		if got, err := e.m.CheckEntities(context.Background(), v); err != nil || got != v {
			t.Errorf("%q: %q %v", v, got, err)
		}
	}
	if _, err := e.m.CheckEntities(context.Background(), "alle Fenster"); err == nil {
		t.Error("words without Gruppe: taken")
	}
}

func toolNamed(t *testing.T, m *Manager, name string) tools.InternalTool {
	t.Helper()
	for _, tool := range m.Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %s", name)
	return tools.InternalTool{}
}
