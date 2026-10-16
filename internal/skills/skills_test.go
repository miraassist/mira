package skills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/ical"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tools"
)

const secretURL = "https://example.org/cal?StandortID=geheim"

func loadAbfall(t *testing.T, url string) Skill {
	t.Helper()
	list, err := Load([]string{"testdata/skills"}, map[string]map[string]any{
		"abfall": {"calendar_url": url, "types": []any{"Restabfall", "Wertstoff"}},
	})
	if err != nil || len(list) != 1 {
		t.Fatalf("load: %v %v", list, err)
	}
	return list[0]
}

func TestLoad(t *testing.T) {
	sk := loadAbfall(t, secretURL)
	if sk.Calendars["abfall"] != secretURL || len(sk.Schedules) != 1 || sk.Schedules[0].Cron != "0 18 * * *" {
		t.Errorf("skill: %+v", sk)
	}
	if !strings.Contains(sk.Body, "Interessante Abfallarten: Restabfall, Wertstoff") {
		t.Errorf("setting not filled in: %s", sk.Body)
	}
	if strings.Contains(sk.Body, "geheim") {
		t.Error("secret setting reached the body")
	}

	for name, settings := range map[string]map[string]any{
		"abfall": {}, // required setting missing
		"kaputt": {}, // name does not match the folder
		"fehlt":  {}, // no such skill
	} {
		if _, err := Load([]string{"testdata/skills"}, map[string]map[string]any{name: settings}); err == nil {
			t.Errorf("%s loaded", name)
		}
	}
	if _, err := Load([]string{"testdata/skills"}, map[string]map[string]any{"abfall": {"calendar_url": "x", "farbe": "blau"}}); err == nil {
		t.Error("unknown setting accepted")
	}
}

func TestShippedSkillsLoad(t *testing.T) {
	entries, _ := os.ReadDir("../../skills")
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join("../../skills", e.Name(), "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := split(raw); err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
	}
}

func calendarServer(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../ical/testdata/abfall.ics")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	t.Cleanup(srv.Close)
	return srv.URL + "/cal?StandortID=geheim"
}

func berlinSet(t *testing.T, sk Skill) *Set {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	return &Set{Lang: i18n.DE, Skills: []Skill{sk}, Calendar: &ical.Cache{}, Location: berlin,
		Clock: func() time.Time { return time.Date(2026, 10, 21, 18, 0, 0, 0, berlin) }}
}

func toolByName(list []tools.InternalTool, name string) tools.InternalTool {
	for _, it := range list {
		if it.Name == name {
			return it
		}
	}
	return tools.InternalTool{}
}

func TestTools(t *testing.T) {
	set := berlinSet(t, loadAbfall(t, calendarServer(t)))
	list := set.Tools()
	if len(list) != 2 {
		t.Fatalf("tools: %d", len(list))
	}
	for _, it := range list {
		if strings.Contains(it.Description+it.Schema, "geheim") || !json.Valid([]byte(it.Schema)) || !it.ReadOnly {
			t.Errorf("%s: secret, bad schema or not read-only", it.Name)
		}
	}
	open := toolByName(list, ToolSkillOpen)
	if !strings.Contains(open.Description, "- abfall: Abfuhrtermine") {
		t.Errorf("skill list: %s", open.Description)
	}
	body, err := open.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"abfall"}`)})
	if err != nil || !strings.Contains(body, "calendar_events") {
		t.Errorf("open: %q %v", body, err)
	}

	cal := toolByName(list, ToolCalendarEvents)
	out, err := cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"abfall","days":2,"match":"restabfall, wertstoff"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Heute ist Mittwoch, 21. Oktober 2026.") || !strings.Contains(out, "- morgen, Donnerstag, 22. Oktober 2026: Wertstoff") || strings.Contains(out, "Papier") {
		t.Errorf("events:\n%s", out)
	}
	out, _ = cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"abfall","days":7,"match":"schadstoff"}`)})
	if !strings.Contains(out, "Samstag, 24. Oktober 2026, 09:00 Uhr: Mobile Schadstoffsammlung") {
		t.Errorf("timed event:\n%s", out)
	}
	// A short window still shows the next matches: here the window of one
	// day is empty, the following two come anyway.
	out, _ = cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"abfall","days":1,"match":"wertstoff, schadstoff"}`)})
	if !strings.Contains(out, "Wertstoff") || !strings.Contains(out, "Schadstoff") {
		t.Errorf("no look-ahead:\n%s", out)
	}
	// One day only: tomorrow's collection, and nothing else.
	out, _ = cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"abfall","day":"tomorrow"}`)})
	if !strings.Contains(out, "- morgen, Donnerstag, 22. Oktober 2026: Wertstoff") || strings.Contains(out, "Schadstoff") {
		t.Errorf("tomorrow:\n%s", out)
	}
	if _, err := cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"nope"}`)}); err == nil {
		t.Error("unknown calendar accepted")
	}
	if len((&Set{}).Tools()) != 0 {
		t.Error("tools without skills")
	}
}

type fakeTasker struct {
	answer      string
	instruction string
}

func (f *fakeTasker) RunTask(_ context.Context, instruction string) (string, error) {
	f.instruction = instruction
	return f.answer, nil
}

func TestRunJob(t *testing.T) {
	sk := loadAbfall(t, secretURL)
	set := berlinSet(t, sk)
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tasker := &fakeTasker{answer: `{"importance":3,"spoken":"Morgen wird der Wertstoff abgeholt.","expires_in_hours":14}`}
	r := &Runner{Set: set, Tasker: tasker, Store: st}

	queued, err := r.RunJob(context.Background(), sk, 0)
	if err != nil || !queued {
		t.Fatalf("run: %v %v", queued, err)
	}
	if !strings.Contains(tasker.instruction, "Prüfe mit calendar_events") || strings.Contains(tasker.instruction, "geheim") {
		t.Errorf("instruction:\n%s", tasker.instruction)
	}
	list, _ := st.ListMessages(context.Background(), store.MessageFilter{States: []string{store.MessagePending}})
	if len(list) != 1 || list[0].Importance != 2 || list[0].Spoken != "Morgen wird der Wertstoff abgeholt." {
		t.Errorf("message (importance capped at 2): %+v", list)
	}

	tasker.answer = `{"importance":0,"spoken":""}`
	if queued, err := r.RunJob(context.Background(), sk, 0); err != nil || queued {
		t.Errorf("nothing to report queued a message: %v %v", queued, err)
	}
}

func TestTextsComplete(t *testing.T) {
	if missing := i18n.Missing(catalog); len(missing) > 0 {
		t.Errorf("texts missing: %v", missing)
	}
}

// Seen live: on collection day at 18:00 the model was given today's
// collection and the next one, and announced today's for tomorrow. Asked
// for tomorrow only, the tool says there is nothing.
func TestCalendarTomorrowOnCollectionDay(t *testing.T) {
	set := berlinSet(t, loadAbfall(t, calendarServer(t)))
	set.Clock = func() time.Time { return time.Date(2026, 10, 15, 18, 0, 0, 0, set.Location) }
	cal := toolByName(set.Tools(), ToolCalendarEvents)
	out, err := cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"abfall","day":"tomorrow","match":"restabfall, wertstoff"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "morgen, Freitag, 16. Oktober 2026: keine passenden Termine.") || strings.Contains(out, "Restabfall") {
		t.Errorf("tomorrow:\n%s", out)
	}
	out, _ = cal.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"calendar":"abfall","day":"today"}`)})
	if !strings.Contains(out, "- heute, Donnerstag, 15. Oktober 2026: Restabfall") || strings.Contains(out, "Wertstoff") {
		t.Errorf("today:\n%s", out)
	}
}
