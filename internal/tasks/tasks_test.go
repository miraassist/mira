package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
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
	if v.NoticeID == "" || v.Importance != 3 {
		t.Fatalf("verdict: %+v", v)
	}
	notices, _ := e.st.ListNotices(context.Background(), store.NoticeFilter{States: []string{store.NoticePending}})
	if len(notices) != 1 || notices[0].Source != store.SourceTask || !strings.Contains(notices[0].Details, "40 L") {
		t.Fatalf("notices: %+v", notices)
	}

	// An hour later the same verdict is withheld (cooldown), and the model
	// sees what it said before.
	e.now = e.now.Add(time.Hour)
	v, _ = e.m.Assess(context.Background(), req)
	if v.Withheld != "cooldown" || v.NoticeID != "" {
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

func TestDryRunAndDecideMakeNoNotice(t *testing.T) {
	e := setup(t)
	e.nightWater()
	off := false
	e.model.answers = []string{`{"importance":2,"spoken":"Viel Wasser."}`}
	v, _ := e.m.Assess(context.Background(), Request{Goal: "Wasser", Notify: &off})
	if v.Withheld != "dry_run" || v.NoticeID != "" {
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
	notices, _ := e.st.ListNotices(context.Background(), store.NoticeFilter{})
	if len(notices) != 0 {
		t.Errorf("notices: %+v", notices)
	}
	if _, err := e.m.Assess(context.Background(), Request{}); err == nil {
		t.Error("assess without goal")
	}
}

const draft = `{"title":"Wasser","summary":"Ich vergleiche stündlich den Wasserverbrauch.","automation":{
"triggers":[{"trigger":"time_pattern","minutes":15}],
"actions":[{"action":"mira.assess","data":{"goal":"Achte auf den Wasserverbrauch.","data":[{"entity_id":"sensor.wasser","view":"same_hour"}]}}]}}`

func TestDraftConfirmManage(t *testing.T) {
	e := setup(t)
	// The first draft names an entity Home Assistant does not know.
	e.ha.invalid = "sensor.ausgedacht"
	e.model.answers = []string{strings.Replace(draft, "sensor.wasser", "sensor.ausgedacht", 1), draft}
	inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	out, err := e.m.Tools()[0].Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"goal":"Achte auf den Wasserverbrauch."}`),
		SessionKey: inv.SessionKey, Channel: inv.Channel})
	if err != nil || !strings.Contains(out, "Soll ich so vorgehen?") {
		t.Fatalf("create: %q %v", out, err)
	}
	if !strings.Contains(e.model.instructions[1], "sensor.ausgedacht is unknown") {
		t.Error("redraft lacks the reason")
	}
	if !strings.Contains(e.model.instructions[0], "sensor.wasser | Wasserzähler") {
		t.Error("catalog missing from the draft prompt")
	}

	// Yes from the same chat saves it as an automation, tagged with its id.
	reply, ok := e.m.Shortcut(context.Background(), inv, "Ja.")
	if !ok || !strings.Contains(reply, "Wasser") {
		t.Fatalf("shortcut: %q %v", reply, ok)
	}
	if len(e.ha.automations) != 1 {
		t.Fatalf("saved: %+v", e.ha.automations)
	}
	var id string
	for k, cfg := range e.ha.automations {
		id = k
		raw, _ := json.Marshal(cfg)
		if !strings.Contains(string(raw), `"task":"{{ this.attributes.id }}"`) || !strings.Contains(string(raw), `"description":"Achte auf den Wasserverbrauch."`) {
			t.Errorf("not tagged: %s", raw)
		}
	}
	if !strings.HasPrefix(id, "mira_") {
		t.Errorf("id %q", id)
	}
	items, _ := e.m.List(context.Background())
	if len(items) != 1 || items[0].State != "on" || items[0].EditURL != "http://ha:8123/config/automation/edit/"+id {
		t.Fatalf("list: %+v", items)
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
	if !strings.Contains(e.m.DescribeTry(res), "Wichtigkeit 3") {
		t.Error(e.m.DescribeTry(res))
	}
	if err := e.m.Delete(context.Background(), id); err != nil || len(e.ha.automations) != 0 {
		t.Errorf("delete: %v", err)
	}
}

func TestConfirmNeedsAProposalHere(t *testing.T) {
	e := setup(t)
	e.model.answers = []string{draft}
	if _, err := e.m.Create(context.Background(), "Wasser", "telegram:1", "telegram:1"); err != nil {
		t.Fatal(err)
	}
	other := tools.Invocation{SessionKey: "telegram:2", Channel: config.ChannelTelegram}
	if _, err := e.m.Tools()[1].Run(context.Background(), other); err == nil {
		t.Error("confirmed from another chat")
	}
	if _, ok := e.m.Shortcut(context.Background(), other, "Ja"); ok {
		t.Error("shortcut from another chat")
	}
	here := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	if _, ok := e.m.Shortcut(context.Background(), here, "Ja, aber ohne die Tür"); ok {
		t.Error("a yes with a change was handled")
	}
	e.now = e.now.Add(11 * time.Minute)
	if _, ok := e.m.Shortcut(context.Background(), here, "Ja"); ok {
		t.Error("a stale proposal was confirmed by the shortcut")
	}
	if _, err := e.m.Tools()[1].Run(context.Background(), here); err != nil {
		t.Errorf("task_confirm: %v", err)
	}
}

func TestMiraCallsFindsNestedSteps(t *testing.T) {
	var actions any
	json.Unmarshal([]byte(`[{"if":[],"then":[{"action":"mira.decide","data":{"question":"q"}}]},
		{"choose":[{"sequence":[{"action":"mira.assess"}]}]},{"action":"mira.add_notice"}]`), &actions)
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

func TestDraftWithoutTriggerOrEntity(t *testing.T) {
	e := setup(t)
	e.model.answers = []string{
		`{"title":"x","summary":"y","automation":{"triggers":[],"actions":[{"action":"mira.add_notice","data":{"text":"t"}}]}}`,
		`{"error":"Es gibt keinen Gaszähler."}`,
	}
	_, err := e.m.Create(context.Background(), "Achte auf das Gas", "s", "s")
	var cannot errCannot
	if !errors.As(err, &cannot) || cannot.reason != "Es gibt keinen Gaszähler." {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(e.model.instructions[1], "at least one trigger") {
		t.Error("a draft without triggers was not refused")
	}
}

func TestProposalNamesWhatItSwitches(t *testing.T) {
	e := setup(t)
	e.m.opts.ConfirmDomains, e.m.opts.ConfirmEntities = []string{"lock"}, []string{"cover.garage"}
	e.model.answers = []string{`{"title":"Radio","summary":"Wenn morgens Bewegung im Bad ist, entscheide ich, ob Musik passt.","automation":{
"triggers":[{"trigger":"state","entity_id":"binary_sensor.bad","to":"on"}],
"actions":[{"action":"mira.decide","data":{"question":"Passt jetzt Musik?"},"response_variable":"d"},
{"condition":"template","value_template":"{{ d.answer }}"},
{"action":"media_player.turn_on","target":{"entity_id":"media_player.radio_bad"}}]}}`}
	task, err := e.m.Create(context.Background(), "Mach morgens das Radio im Bad an, wenn es passt.", "s", "s")
	if err != nil {
		t.Fatal(err)
	}
	text := e.m.proposalText(task)
	if !strings.Contains(text, "Dabei schaltet sie außerdem: media_player.turn_on (media_player.radio_bad).") {
		t.Errorf("proposal: %s", text)
	}
	// The integration gets the forbidden list with every check.
	validate := e.ha.commands[0]
	if f, _ := validate["forbidden"].(map[string]any); f == nil || len(f["domains"].([]string)) != 1 || f["entities"].([]string)[0] != "cover.garage" {
		t.Errorf("forbidden not sent: %+v", validate)
	}
	if !strings.Contains(e.model.instructions[0], "schaltet: lock, cover.garage.") {
		t.Errorf("draft prompt lacks the forbidden list")
	}
}
