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
	domains, _ := vars["domain"].([]string)
	if one, ok := vars["domain"].(string); ok {
		domains = []string{one}
	}
	onlyUnavailable, _ := vars["only_unavailable"].(bool)
	for _, id := range sortedKeys(h.entities) {
		mb := h.entities[id]
		domain, _, _ := strings.Cut(id, ".")
		if !slices.Contains(domains, domain) || len(classes) > 0 && !slices.Contains(classes, h.classes[id]) ||
			onlyUnavailable && mb.State != "unavailable" ||
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
			// Batteries and devices (docs/spec/group-watch.md, decisions 36 to 48).
			"sensor.handy_anna_batterie":          {ID: "sensor.handy_anna_batterie", Name: "Handy Anna Batterie", State: "15", Area: "", Device: "Handy Anna"},
			"sensor.rauchmelder_batterie":         {ID: "sensor.rauchmelder_batterie", Name: "Rauchmelder Flur Batterie", State: "12", Area: "flur", Device: "Rauchmelder Flur"},
			"binary_sensor.wassermelder_batterie": {ID: "binary_sensor.wassermelder_batterie", Name: "Wassermelder Keller Batterie", State: "off", Area: "flur", Device: "Wassermelder Keller"},
			"switch.steckdose_garage":             {ID: "switch.steckdose_garage", Name: "Steckdose Garage", State: "unavailable", Area: "flur", Device: "Steckdose Garage"},
			"sensor.steckdose_garage_signal":      {ID: "sensor.steckdose_garage_signal", Name: "Steckdose Garage Signal", State: "unavailable", Area: "flur", Device: "Steckdose Garage"},
			"sensor.klima_bad_feuchte":            {ID: "sensor.klima_bad_feuchte", Name: "Klima Bad Feuchte", State: "55", Area: "bad", Device: "Klima Bad"},
			"sensor.rest":                         {ID: "sensor.rest", Name: "Rest", State: "unavailable", Area: "bad", Device: "Klima Bad", Restored: true},
		},
		classes: map[string]string{"binary_sensor.fenster_bad": "window", "binary_sensor.haustur": "door",
			"binary_sensor.fenster_buro": "window", "binary_sensor.bewegung_bad": "motion",
			"sensor.handy_anna_batterie": "battery", "sensor.rauchmelder_batterie": "battery", "binary_sensor.wassermelder_batterie": "battery"},
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

// GW-17 Ist alles zu?
func TestGW17_IstAllesZu(t *testing.T) {
	e, h, _ := groupSetup(t)
	// Given the fake bathroom window is open and every other opening closed
	mb := h.entities["binary_sensor.fenster_bad"]
	mb.State, mb.Area = "on", "Bad"
	h.entities["binary_sensor.fenster_bad"] = mb
	// When the user asks "Ist alles zu?"
	out, err := toolNamed(t, e.m, ToolHouseCheck).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{}`)})
	// Then Mira hears that all is closed but the bathroom window
	if err != nil || out != "Offen: Fenster Bad (Bad). Geprüft: 3 Fenster und Türen." {
		t.Errorf("%q %v", out, err)
	}
	// Nothing open: said so; a floor by its name narrows it.
	mb.State = "off"
	h.entities["binary_sensor.fenster_bad"] = mb
	out, err = toolNamed(t, e.m, ToolHouseCheck).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"floors":["Dachgeschoss"]}`)})
	if err != nil || out != "Alles zu: 1 Fenster und Türen geprüft, nichts offen." {
		t.Errorf("%q %v", out, err)
	}
	// A floor that does not exist is said with the floors there are.
	if _, err := toolNamed(t, e.m, ToolHouseCheck).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"floors":["Turm"]}`)}); err == nil ||
		!strings.Contains(err.Error(), `eine Etage "Turm" gibt es nicht; die Etagen: Erdgeschoss, Dachgeschoss`) {
		t.Errorf("%v", err)
	}
}

// GW-18 The check names what does not report
func TestGW18_TheCheckNamesWhatDoesNotReport(t *testing.T) {
	e, h, _ := groupSetup(t)
	// Given a fake window is unavailable
	mb := h.entities["binary_sensor.fenster_buro"]
	mb.State = "unavailable"
	h.entities["binary_sensor.fenster_buro"] = mb
	// When the user asks "Ist alles zu?"
	out, err := toolNamed(t, e.m, ToolHouseCheck).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{}`)})
	// Then Mira names it as not readable
	if err != nil || !strings.Contains(out, "Alles zu: 3 Fenster und Türen geprüft") ||
		!strings.Contains(out, "Meldet sich nicht, also nicht geprüft (sag das dazu): Fenster Büro (buro).") {
		t.Errorf("%q %v", out, err)
	}
}

// GW-19 mira.check from a script
func TestGW19_MiraCheckFromAScript(t *testing.T) {
	e, h, _ := groupSetup(t)
	// Given the fake bathroom window is open
	mb := h.entities["binary_sensor.fenster_bad"]
	mb.State, mb.Area = "on", "Bad"
	h.entities["binary_sensor.fenster_bad"] = mb
	// When a script calls mira.check with a response variable
	ans, err := e.m.Check(context.Background(), CheckCall{})
	// Then the response lists the bathroom window
	if err != nil || ans.Scope != "openings" || len(ans.Open) != 1 || ans.Open[0].ID != "binary_sensor.fenster_bad" || ans.Checked != 3 {
		t.Fatalf("%+v %v", ans, err)
	}
	// And an entry for the receiver names it
	n, err := e.st.GetMessage(context.Background(), ans.MessageID)
	if err != nil || n.Receiver != store.ReceiverHouse || n.Kind != store.KindSimple || n.Spoken != "Noch offen: Fenster Bad (Bad)." {
		t.Errorf("%+v %v", n, err)
	}
	// For a person, and not at all with notify off; an unknown receiver is the caller's error.
	off := false
	if ans, err := e.m.Check(context.Background(), CheckCall{Notify: &off}); err != nil || ans.MessageID != "" || len(ans.Open) != 1 {
		t.Errorf("notify off: %+v %v", ans, err)
	}
	var bad watch.ErrBadCheck
	if _, err := e.m.Check(context.Background(), CheckCall{Receiver: "jemand"}); !errors.As(err, &bad) {
		t.Errorf("receiver: %v", err)
	}
	if ans, err := e.m.Check(context.Background(), CheckCall{Receiver: "anna"}); err != nil || ans.MessageID == "" {
		t.Errorf("anna: %+v %v", ans, err)
	}
}

// GW-20 mira.check and nothing open
func TestGW20_MiraCheckAndNothingOpen(t *testing.T) {
	e, _, _ := groupSetup(t)
	// Given every opening is closed
	// When a script calls mira.check
	ans, err := e.m.Check(context.Background(), CheckCall{})
	// Then the response's list is empty
	if err != nil || ans.Open == nil || len(ans.Open) != 0 || ans.Checked != 3 {
		t.Fatalf("%+v %v", ans, err)
	}
	// And no entry comes
	if ans.MessageID != "" {
		t.Errorf("entry %s", ans.MessageID)
	}
	if msgs, _ := e.st.ListMessages(context.Background(), store.MessageFilter{IncludeExpired: true}); len(msgs) != 0 {
		t.Errorf("messages: %v", msgs)
	}
}

// GW-27 Sag mir, wenn eine Batterie leer wird
func TestGW27_SagMirWennEineBatterieLeerWird(t *testing.T) {
	e, h, _ := groupSetup(t)
	// When the user says "Sag mir, wenn eine Batterie leer wird"
	e.model.answers = []string{`{"title":"Batterien","summary":"Ich schau jeden Morgen um 9 nach den Batterien und sag dir, welche unter 20 Prozent sind, das Handy nicht.","blueprint":"battery_low","inputs":{},"exclude":["sensor.handy_anna_batterie"]}`}
	d, err := e.m.DraftAutomation(context.Background(), "Sag mir, wenn eine Batterie leer wird",
		map[string]string{"watch": "Gruppe: Batterien"}, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	// And the proposal named how many batteries it watches, by device, and the one already low
	text := e.m.ProposalText(d)
	for _, want := range []string{"Heute sind das 2: Rauchmelder Flur, Wassermelder Keller.", "Schon jetzt schwach: Rauchmelder Flur (12 %).",
		"Ausgenommen: Handy Anna Batterie;", "unter 20 %", "um 09:00"} {
		if !strings.Contains(text, want) {
			t.Errorf("proposal lacks %q: %s", want, text)
		}
	}
	// And agrees to the proposal
	if err := confirm(t, e, d); err != nil {
		t.Fatal(err)
	}
	// Then an automation with use_blueprint "mira/battery_low.yaml" is saved as a task
	path, input := savedUse(t, e)
	if path != "mira/battery_low.yaml" || !strings.HasPrefix(fmt.Sprint(input["message"]), "Batterie fast leer: ") {
		t.Errorf("saved %s %v", path, input)
	}
	// And the phone the planner proposed as an exception carries the label
	if !slices.Contains(h.labelled["sensor.handy_anna_batterie"], watch.LabelDontWatch) {
		t.Errorf("labels %v", h.labelled)
	}
}

// GW-28 Sag Bescheid, wenn ein Gerät nicht mehr erreichbar ist
func TestGW28_SagBescheidWennEinGeratNichtMehrErreichbarIst(t *testing.T) {
	e, _, _ := groupSetup(t)
	// When the user says "Sag Bescheid, wenn ein Gerät nicht mehr erreichbar ist"
	e.model.answers = []string{`{"title":"Erreichbarkeit","summary":"Ich sag dir jeden Morgen um 9, welche Geräte seit einer Stunde nicht erreichbar sind.","blueprint":"unreachable","inputs":{"at":"9:00"}}`}
	d, err := e.m.DraftAutomation(context.Background(), "Sag Bescheid, wenn ein Gerät nicht mehr erreichbar ist",
		map[string]string{"watch": "Gruppe: Geräte, die nicht erreichbar sind"}, Origin{})
	if err != nil {
		t.Fatal(err)
	}
	// And the proposal named how many devices it watches and the one not reachable today (the
	// restored left-over and the entities without a device are none)
	text := e.m.ProposalText(d)
	for _, want := range []string{"Heute sind das 5: Handy Anna, Klima Bad, Rauchmelder Flur, Steckdose Garage, Wassermelder Keller.",
		"Schon jetzt nicht erreichbar: Steckdose Garage.", "Stunden 1 h"} {
		if !strings.Contains(text, want) {
			t.Errorf("proposal lacks %q: %s", want, text)
		}
	}
	if err := confirm(t, e, d); err != nil {
		t.Fatal(err)
	}
	// Then an automation with use_blueprint "mira/unreachable.yaml" is saved as a task
	path, input := savedUse(t, e)
	if path != "mira/unreachable.yaml" || input["at"] != "09:00:00" || !strings.HasPrefix(fmt.Sprint(input["message"]), "Nicht erreichbar: ") {
		t.Errorf("saved %s %v", path, input)
	}
}

// GW-29 The planner sees only the devices lost today
func TestGW29_ThePlannerSeesOnlyTheDevicesLostToday(t *testing.T) {
	e, _, _ := groupSetup(t)
	e.model.answers = []string{`{"title":"x","summary":"x","blueprint":"battery_low","inputs":{}}`}
	// When a group watch is drafted
	if _, err := e.m.DraftAutomation(context.Background(), "Sag mir, wenn eine Batterie leer wird", map[string]string{"watch": "Gruppe: Batterien"}, Origin{}); err != nil {
		t.Fatal(err)
	}
	ins := e.model.instructions[0]
	// Then the planner's catalog lists every battery with its value
	if !strings.Contains(ins, "Batterien (id, Name, Gerät, Bereich, Wert): ") ||
		!strings.Contains(ins, "sensor.rauchmelder_batterie (Rauchmelder Flur Batterie, Rauchmelder Flur, flur, 12)") ||
		!strings.Contains(ins, "sensor.handy_anna_batterie (Handy Anna Batterie, Handy Anna, , 15)") {
		t.Errorf("batteries:\n%s", ins)
	}
	// And of the devices only the entities unavailable today
	i := strings.Index(ins, "Geräte (Erreichbarkeit): heute nicht erreichbar")
	if i < 0 {
		t.Fatalf("no devices line:\n%s", ins)
	}
	line, _, _ := strings.Cut(ins[i:], "\n")
	if !strings.Contains(line, "switch.steckdose_garage (Steckdose Garage, Steckdose Garage, flur)") ||
		!strings.Contains(line, "sensor.steckdose_garage_signal") || strings.Contains(line, "klima_bad_feuchte") || strings.Contains(line, "sensor.rest") {
		t.Errorf("devices: %s", line)
	}
	// The planner learns to propose what is charged as an exception.
	if !strings.Contains(ins, "was geladen statt gewechselt wird") || !strings.Contains(ins, "- battery_low:") || !strings.Contains(ins, "- unreachable:") {
		t.Errorf("catalog:\n%s", ins)
	}
}

// GW-30 Ist alles zu? takes no batteries
func TestGW30_IstAllesZuTakesNoBatteries(t *testing.T) {
	e, _, _ := groupSetup(t)
	// When house_check is asked for the batteries or the devices
	for _, scope := range []string{"batteries", "devices"} {
		_, err := toolNamed(t, e.m, ToolHouseCheck).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"scope":"` + scope + `"}`)})
		// Then it refuses them as a kind there is not, naming the kinds there are
		if err == nil || !strings.Contains(err.Error(), `eine Art "`+scope+`" gibt es hier nicht; die Arten: openings, lights, motion`) {
			t.Errorf("%s: %v", scope, err)
		}
	}
}

// GW-31 An exception among the devices
func TestGW31_AnExceptionAmongTheDevices(t *testing.T) {
	e, h, _ := groupSetup(t)
	// Given the fake plug in the garage is not reachable
	// When the user says "Überwach die Steckdose in der Garage nicht mehr"
	out, err := toolNamed(t, e.m, ToolWatchException).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"entity":"switch.steckdose_garage","watch":false}`)})
	// Then its entity carries the label "Mira: nicht überwachen"
	if err != nil || !slices.Contains(h.labelled["switch.steckdose_garage"], watch.LabelDontWatch) || !strings.Contains(out, "Steckdose Garage") {
		t.Errorf("%q %v %v", out, err, h.labelled)
	}
	// By its words: the plug's two entities both match; the tool names both.
	_, err = toolNamed(t, e.m, ToolWatchException).Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"entity":"Steckdose Garage Signal","watch":false}`)})
	if err != nil || !slices.Contains(h.labelled["sensor.steckdose_garage_signal"], watch.LabelDontWatch) {
		t.Errorf("%v %v", err, h.labelled)
	}
}
