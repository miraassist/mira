package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

func f(v float64) *float64 { return &v }

type fakeSource struct {
	states []haws.State
	stats  map[string][]haws.StatRow
}

func (s *fakeSource) States(context.Context) ([]haws.State, error) { return s.states, nil }
func (s *fakeSource) Statistics(_ context.Context, ids []string, _ time.Time, _ string) (map[string][]haws.StatRow, error) {
	return s.stats, nil
}

// fakeHA plays the Mira integration's automation commands.
type fakeHA struct {
	commands    []map[string]any
	invalid     string // validate refuses configs containing this
	automations map[string]map[string]any
	services    []string
	config      string // get_config's answer
}

func (h *fakeHA) Call(_ context.Context, cmd map[string]any) (json.RawMessage, error) {
	h.commands = append(h.commands, cmd)
	switch cmd["type"] {
	case "mira/automation/validate":
		raw, _ := json.Marshal(cmd["config"])
		if h.invalid != "" && strings.Contains(string(raw), h.invalid) {
			return json.RawMessage(`{"valid":false,"error":"Entity ` + h.invalid + ` is unknown"}`), nil
		}
		return json.RawMessage(`{"valid":true}`), nil
	case "mira/automation/save":
		h.automations[cmd["automation_id"].(string)] = cmd["config"].(map[string]any)
		return json.RawMessage(`{"id":"x"}`), nil
	case "mira/automation/list":
		var list []map[string]any
		for id, cfg := range h.automations {
			list = append(list, map[string]any{"id": id, "entity_id": "automation." + id, "state": "on", "config": cfg})
		}
		raw, _ := json.Marshal(map[string]any{"automations": list})
		return raw, nil
	case "get_config":
		return json.RawMessage(h.config), nil
	case "mira/automation/delete":
		delete(h.automations, cmd["automation_id"].(string))
		return json.RawMessage(`{"deleted":true}`), nil
	}
	return nil, nil
}

func (h *fakeHA) CallService(_ context.Context, domain, service string, _ map[string]any, target *haws.Target) error {
	h.services = append(h.services, domain+"."+service+" "+target.EntityID[0])
	return nil
}

type fakeModel struct {
	answers      []string
	instructions []string
}

func (m *fakeModel) Ask(_ context.Context, _, instruction string, _ json.RawMessage) (string, error) {
	m.instructions = append(m.instructions, instruction)
	a := m.answers[0]
	if len(m.answers) > 1 {
		m.answers = m.answers[1:]
	}
	return a, nil
}

type env struct {
	m     *Manager
	st    *store.Store
	src   *fakeSource
	ha    *fakeHA
	model *fakeModel
	now   time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{now: time.Date(2026, 10, 15, 3, 20, 0, 0, time.UTC), model: &fakeModel{},
		ha: &fakeHA{automations: map[string]map[string]any{}}}
	var err error
	e.st, err = store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db"), Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	e.src = &fakeSource{states: []haws.State{
		{EntityID: "sensor.wasser", State: "32117", Attributes: map[string]any{"friendly_name": "Wasserzähler", "state_class": "total_increasing", "unit_of_measurement": "L"}},
		{EntityID: "person.anna", State: "home"},
		{EntityID: "person.ben", State: "not_home"},
	}}
	e.m = New(Options{Lang: i18n.DE, Store: e.st, Source: e.src, HA: e.ha, Model: e.model,
		QuietHours: config.QuietHours{From: config.ClockTime{Hour: 23, Minute: 30}, To: config.ClockTime{Hour: 6, Minute: 30}},
		Persons: func() map[string]config.Person {
			return map[string]config.Person{"anna": {Entity: "person.anna"}, "ben": {Entity: "person.ben"}}
		},
		HAURL: "http://ha:8123", Clock: func() time.Time { return e.now }, Location: time.UTC})
	return e
}

// 02:00-03:00: 40 litres; the same hour on the seven days before: nothing.
func (e *env) nightWater() {
	hour := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	var rows []haws.StatRow
	for d := 7; d >= 1; d-- {
		rows = append(rows, haws.StatRow{Start: hour.AddDate(0, 0, -d), Change: f(0)})
	}
	rows = append(rows, haws.StatRow{Start: hour, Change: f(40)})
	e.src.stats = map[string][]haws.StatRow{"sensor.wasser": rows}
}

func TestAssessCondensesDataAndNotifies(t *testing.T) {
	e := setup(t)
	e.nightWater()
	e.st.SaveFact(context.Background(), store.Fact{ID: "f1", Text: "Ben ist bis Sonntag im Urlaub, Wasser läuft nachts sonst nie."})
	e.model.answers = []string{`{"importance":3,"spoken":"Zwischen 2 und 3 Uhr liefen 40 Liter Wasser.","details":""}`}
	req := Request{Goal: "Achte auf den Wasserverbrauch.", Data: []DataRequest{{EntityID: "sensor.wasser", View: ViewSameHour}},
		Task: "mira_wasser", Cooldown: "6h"}
	v, err := e.m.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	prompt := e.model.instructions[0]
	for _, want := range []string{"Achte auf den Wasserverbrauch.", "02:00–03:00 Uhr Verbrauch 40 L.",
		"Dieselbe Stunde an 7 früheren Tagen: Median 0, Bereich 0–0 L.", "Zu Hause: anna. Unterwegs: ben.",
		"Ruhezeit: ja", "Wasser läuft nachts sonst nie"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if v.MessageID == "" || v.Importance != 3 {
		t.Fatalf("verdict: %+v", v)
	}
	messages, _ := e.st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}})
	if len(messages) != 1 || messages[0].Source != store.SourceTask || !strings.Contains(messages[0].Details, "40 L") {
		t.Fatalf("messages: %+v", messages)
	}

	// An hour later the same verdict is withheld (cooldown), and the model
	// sees what it said before.
	e.now = e.now.Add(time.Hour)
	v, _ = e.m.Assess(context.Background(), req)
	if v.Withheld != "cooldown" || v.MessageID != "" {
		t.Errorf("not withheld: %+v", v)
	}
	if !strings.Contains(e.model.instructions[1], "Wichtigkeit 3 – Zwischen 2 und 3 Uhr liefen 40 Liter Wasser.") {
		t.Errorf("earlier verdict missing:\n%s", e.model.instructions[1])
	}
	log, _ := e.st.Assessments(context.Background(), "mira_wasser", 10)
	if len(log) != 2 {
		t.Errorf("log: %d", len(log))
	}
}

func TestDryRunAndDecideMakeNoMessage(t *testing.T) {
	e := setup(t)
	e.nightWater()
	off := false
	e.model.answers = []string{`{"importance":2,"spoken":"Viel Wasser."}`}
	v, _ := e.m.Assess(context.Background(), Request{Goal: "Wasser", Notify: &off})
	if v.Withheld != "dry_run" || v.MessageID != "" {
		t.Errorf("dry run notified: %+v", v)
	}
	e.model.answers = []string{`{"answer":true,"reason":"40 Liter statt 0."}`}
	d, err := e.m.Decide(context.Background(), Request{Question: "Läuft unnötig Wasser?",
		Data: []DataRequest{{EntityID: "sensor.wasser", View: ViewSameHour}}})
	if err != nil || !d.Answer {
		t.Fatalf("decide: %+v %v", d, err)
	}
	if !strings.Contains(e.model.instructions[1], "## Frage\nLäuft unnötig Wasser?") {
		t.Errorf("question missing:\n%s", e.model.instructions[1])
	}
	messages, _ := e.st.ListMessages(context.Background(), store.MessageFilter{})
	if len(messages) != 0 {
		t.Errorf("messages: %+v", messages)
	}
	if _, err := e.m.Assess(context.Background(), Request{}); err == nil {
		t.Error("assess without goal")
	}
}

const draft = `{"title":"Wasser","summary":"Ich vergleiche stündlich den Wasserverbrauch.","automation":{
"triggers":[{"trigger":"time_pattern","minutes":15}],
"actions":[{"action":"mira.assess","data":{"goal":"Achte auf den Wasserverbrauch.","data":[{"entity_id":"sensor.wasser","view":"same_hour"}]}}]}}`

func TestDraftSaveManage(t *testing.T) {
	e := setup(t)
	// The first draft names an entity Home Assistant does not know.
	e.ha.invalid = "sensor.ausgedacht"
	e.model.answers = []string{strings.Replace(draft, "sensor.wasser", "sensor.ausgedacht", 1), draft}
	d, err := e.m.DraftAutomation(context.Background(), "Achte auf den Wasserverbrauch.",
		map[string]string{"watch": "Wasserzähler (sensor.wasser)", "measure": "- Mira beurteilt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.model.instructions[1], "sensor.ausgedacht is unknown") {
		t.Error("redraft lacks the reason")
	}
	if !strings.Contains(e.model.instructions[0], "sensor.wasser | Wasserzähler") || !strings.Contains(e.model.instructions[0], "watch: Wasserzähler (sensor.wasser)") {
		t.Errorf("catalog or fields missing from the draft prompt:\n%s", e.model.instructions[0])
	}

	// The finisher proposes and, on yes, saves it, tagged with its id.
	content, _ := json.Marshal(d)
	msg, err := e.m.Finisher().Commit(context.Background(), store.Thread{Content: content})
	if err != nil || !strings.Contains(msg, "Wasser") || len(e.ha.automations) != 1 {
		t.Fatalf("commit: %q %v %+v", msg, err, e.ha.automations)
	}
	var id string
	for k, cfg := range e.ha.automations {
		id = k
		raw, _ := json.Marshal(cfg)
		if !strings.Contains(string(raw), `"task":"{{ this.attributes.id }}"`) || !strings.Contains(string(raw), `"description":"Achte auf den Wasserverbrauch."`) {
			t.Errorf("not tagged: %s", raw)
		}
	}
	items, _ := e.m.List(context.Background())
	if len(items) != 1 || items[0].State != "on" || items[0].EditURL != "http://ha:8123/config/automation/edit/"+id {
		t.Fatalf("list: %+v", items)
	}
	// A browser needs the URL Home Assistant has for itself, not Mira's.
	e.ha.config = `{"internal_url":"http://192.168.1.5:8123/","external_url":""}`
	if items, _ := e.m.List(context.Background()); items[0].EditURL != "http://192.168.1.5:8123/config/automation/edit/"+id {
		t.Errorf("edit url: %s", items[0].EditURL)
	}
	e.ha.config = `{"internal_url":"http://192.168.1.5:8123","external_url":"https://home.example.org"}`
	if items, _ := e.m.List(context.Background()); items[0].EditURL != "https://home.example.org/config/automation/edit/"+id {
		t.Errorf("edit url: %s", items[0].EditURL)
	}

	// Pause, dry run, delete.
	if err := e.m.SetEnabled(context.Background(), id, false); err != nil || e.ha.services[0] != "automation.turn_off automation."+id {
		t.Errorf("pause: %v %v", err, e.ha.services)
	}
	e.nightWater()
	e.model.answers = []string{`{"importance":3,"spoken":"40 Liter nachts."}`}
	res, err := e.m.Try(context.Background(), id)
	if err != nil || len(res.Verdicts) != 1 || res.Verdicts[0].Withheld != "dry_run" {
		t.Fatalf("try: %+v %v", res, err)
	}
	if err := e.m.Delete(context.Background(), id); err != nil || len(e.ha.automations) != 0 {
		t.Errorf("delete: %v", err)
	}
}

func TestCheckEntitiesByIDNameAndAlias(t *testing.T) {
	e := setup(t)
	e.m.opts.Snapshot = func() structure.Snapshot {
		return structure.Snapshot{EntityNames: map[string][]string{"sensor.wasser": {"sensor.wasser", "wasserzähler", "hauptwasser"}}}
	}
	if v, err := e.m.CheckEntities(context.Background(), "sensor.wasser"); err != nil || v != "sensor.wasser" {
		t.Errorf("by id: %q %v", v, err)
	}
	if v, err := e.m.CheckEntities(context.Background(), "der Hauptwasser-Zähler"); err != nil || !strings.Contains(v, "(sensor.wasser)") {
		t.Errorf("by alias: %q %v", v, err)
	}
	if _, err := e.m.CheckEntities(context.Background(), "sensor.gibtsnicht"); err == nil {
		t.Error("unknown id accepted")
	}
	if _, err := e.m.CheckEntities(context.Background(), "der Rasenmäher"); err == nil {
		t.Error("nothing recognised but accepted")
	}
}

func TestMiraCallsFindsNestedSteps(t *testing.T) {
	var actions any
	json.Unmarshal([]byte(`[{"if":[],"then":[{"action":"mira.decide","data":{"question":"q"}}]},
		{"choose":[{"sequence":[{"action":"mira.assess"}]}]},{"action":"mira.send_message"}]`), &actions)
	calls := miraCalls(actions)
	if len(calls) != 2 || calls[0].action != "mira.decide" || calls[1].action != "mira.assess" {
		t.Fatalf("calls: %+v", calls)
	}
	tagTask(actions)
	raw, _ := json.Marshal(actions)
	if strings.Count(string(raw), "this.attributes.id") != 2 {
		t.Errorf("tagged: %s", raw)
	}
}

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

func TestDraftQuestionsAndHonestProposal(t *testing.T) {
	e := setup(t)
	e.m.opts.ConfirmDomains, e.m.opts.ConfirmEntities = []string{"lock"}, []string{"cover.garage"}
	e.st.SaveFact(context.Background(), store.Fact{ID: "f1", Text: "Ungewöhnlich ist Licht im Schlafzimmer zwischen 0 und 6 Uhr."})

	// No trigger: refused; then the planner asks back.
	e.model.answers = []string{
		`{"title":"x","summary":"y","automation":{"triggers":[],"actions":[{"action":"mira.send_message","data":{"text":"t"}}]}}`,
		`{"question":"Ab wann ist es ungewöhnlich?"}`,
	}
	_, err := e.m.DraftAutomation(context.Background(), "Licht zu ungewöhnlicher Zeit", nil)
	var q ErrQuestion
	if !errors.As(err, &q) || q.Question != "Ab wann ist es ungewöhnlich?" {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(e.model.instructions[1], "at least one trigger") {
		t.Error("a draft without triggers was not refused")
	}
	if !strings.Contains(e.model.instructions[0], "zwischen 0 und 6 Uhr") || !strings.Contains(e.model.instructions[0], "schaltet: lock, cover.garage.") {
		t.Error("the planner lacks memory or the forbidden list")
	}

	// A condition is said by the code; switching is named.
	e.model.answers = []string{`{"title":"Licht nachts","summary":"Ich melde mich, wenn nachts das Licht angeht.","automation":{
"triggers":[{"trigger":"state","entity_id":"light.schlafzimmer","to":"on"}],
"conditions":[{"condition":"time","after":"00:00:00","before":"06:00:00"}],
"actions":[{"action":"mira.send_message","data":{"text":"Licht an.","importance":1}},
{"action":"media_player.turn_on","target":{"entity_id":"media_player.radio_bad"}}]}}`}
	d, err := e.m.DraftAutomation(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := e.m.ProposalText(d)
	if !strings.Contains(text, "Bedingung: nur zwischen 00:00 und 06:00 Uhr.") || !strings.Contains(text, "media_player.turn_on (media_player.radio_bad)") {
		t.Errorf("proposal: %s", text)
	}

	// No condition and a judgement: Mira says she decides.
	e.model.answers = []string{`{"title":"Licht","summary":"s","automation":{
"triggers":[{"trigger":"state","entity_id":"light.schlafzimmer","to":"on"}],
"actions":[{"action":"mira.assess","data":{"goal":"g","observation":"o"}}]}}`}
	d, _ = e.m.DraftAutomation(context.Background(), "x", nil)
	if text := e.m.ProposalText(d); !strings.Contains(text, "entscheide ich jedes Mal selbst") {
		t.Errorf("proposal hides that Mira judges: %s", text)
	}
}
