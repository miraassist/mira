package oauth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/tools"
)

// The shipped skill strava (docs/spec/strava.md): Strava's login and its
// web API, read only, on Mira's connections.

// requests records what reaches the network; it answers nothing.
type requests struct {
	mu   sync.Mutex
	urls []string
}

func (r *requests) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	return &http.Response{StatusCode: http.StatusTeapot, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func (r *requests) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.urls)
}

// stravaSkill loads skills/strava with a client id and a secret.
func stravaSkill(t *testing.T) skills.Skill {
	t.Helper()
	loaded, problems := skills.Load([]string{"../../skills"},
		map[string]map[string]any{"strava": {"client_id": "12345", "client_secret": "geheim"}})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("strava did not load: %v", problems)
	}
	return loaded[0]
}

// stravaManager holds the connection of skills/strava; its HTTP client
// only records.
func stravaManager(t *testing.T) (*oauth.Manager, *requests) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rec := &requests{}
	m, err := oauth.New(context.Background(), oauth.Options{Store: st, Secrets: config.Secrets{Key: "schluessel"},
		Skills: []skills.Skill{stravaSkill(t)}, HTTP: &http.Client{Transport: rec},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return m, rec
}

// stravaTools are the skill's Starlark tools as Mira offers them, with the
// manager's connection.
func stravaTools(t *testing.T, m *oauth.Manager) []tools.InternalTool {
	t.Helper()
	return skills.StarlarkTools([]skills.Skill{stravaSkill(t)}, m, nil, nil)
}

func toolNames(list []tools.InternalTool) []string {
	var out []string
	for _, tl := range list {
		if tl.Enabled == nil || tl.Enabled() {
			out = append(out, tl.Name)
		}
	}
	return out
}

// SV-01 The skill brings Strava's login
func TestSV01_TheSkillBringsStravasLogin(t *testing.T) {
	// Given the skill strava with a client id and a secret
	// When the skills load
	s := stravaSkill(t)
	// Then its login is Strava's authorize and token address, the scopes,
	// no PKCE and the secret in the body
	o := s.OAuth
	if o == nil {
		t.Fatal("no oauth section")
	}
	if o.Name != "Strava" || o.AuthorizeURL != "https://www.strava.com/oauth/authorize" ||
		o.TokenURL != "https://www.strava.com/oauth/token" || o.PKCE || o.TokenAuth != skills.TokenAuthPost {
		t.Errorf("login %+v", o)
	}
	if !slices.Equal(o.Scopes, []string{"read", "activity:read_all", "profile:read_all"}) {
		t.Errorf("scopes %v", o.Scopes)
	}
	// And it is ready to connect, with no MCP server of its own
	if !o.Ready() || o.ClientID != "12345" || s.MCP != nil {
		t.Errorf("ready %v, client %q, mcp %+v", o.Ready(), o.ClientID, s.MCP)
	}
}

// SV-02 Only reading below Strava's web API
func TestSV02_OnlyReadingBelowStravasWebAPI(t *testing.T) {
	// Given the skill strava connected
	m, rec := stravaManager(t)
	if err := m.Put(context.Background(), "strava", oauth.Handover{AccessToken: "a-1", RefreshToken: "r-1",
		TokenType: "Bearer", ExpiresIn: (6 * time.Hour).Seconds()}); err != nil {
		t.Fatal(err)
	}
	// When Mira offers its tools
	list := stravaTools(t, m)
	// Then they are its Starlark tools, all reading, and there is no strava__api_get
	want := []string{"strava__ausruestung", "strava__bestleistungen", "strava__letzte_tour", "strava__serie", "strava__zeitraum"}
	if got := toolNames(list); !slices.Equal(got, want) {
		t.Fatalf("tools %v", got)
	}
	for _, tl := range list {
		if !tl.ReadOnly || !tl.WebContent || tl.AskAfterWeb || tl.Skill != "strava" || !tl.Background {
			t.Errorf("%s: %+v", tl.Name, tl)
		}
	}
	// And an address outside Strava's web API, or a method other than GET, is refused without a request
	for _, c := range [][2]string{{"GET", "https://www.strava.com/oauth/deauthorize"}, {"GET", "https://www.strava.com/api/v2/athlete"},
		{"GET", "https://strava.example/api/v3/athlete"}, {"POST", "https://www.strava.com/api/v3/activities"}} {
		if _, _, err := m.Request(context.Background(), "strava", c[0], c[1], nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%v: %v", c, err)
		}
	}
	if n := rec.count(); n != 0 {
		t.Errorf("%d requests went out", n)
	}
}

// SV-03 Without a connection there is no Strava tool
func TestSV03_WithoutAConnectionThereIsNoStravaTool(t *testing.T) {
	// Given the skill strava not connected
	m, _ := stravaManager(t)
	// When Mira offers its tools
	// Then none is offered
	if got := toolNames(stravaTools(t, m)); len(got) != 0 {
		t.Errorf("offered without a connection: %v", got)
	}
}

// SV-04 No Strava data into memory
func TestSV04_NoStravaDataIntoMemory(t *testing.T) {
	// Given the skill strava
	s := stravaSkill(t)
	// When its instructions are read
	// Then they forbid saving activities, distances or times with memory_save
	if !strings.Contains(s.Body, "Speichere nie Aktivitäten, Strecken, Zeiten oder\nKilometer mit `memory_save`") {
		t.Errorf("no rule against memory_save in:\n%s", s.Body)
	}
}

// strava answers the skill's requests from fixed data, by path.
type stravaFake map[string]string

func (f stravaFake) Request(_ context.Context, _, method, raw string, _ []byte) (int, []byte, error) {
	u, _ := url.Parse(raw)
	body, ok := f[strings.TrimPrefix(u.Path, "/api/v3/")]
	if !ok {
		return 404, []byte(`{"message":"Record Not Found"}`), nil
	}
	if strings.HasPrefix(body, "HTTP ") {
		var code int
		fmt.Sscanf(body, "HTTP %d", &code)
		return code, []byte(body[9:]), nil
	}
	return 200, []byte(body), nil
}

// activities are the newest first, as Strava gives them.
const activities = `[
 {"sport_type":"Ride","name":"Feierabend","start_date_local":"2026-11-19T17:30:00Z","distance":21500,"moving_time":3900,"total_elevation_gain":180.4,"average_speed":5.51},
 {"sport_type":"Run","name":"Runde","start_date_local":"2026-11-17T07:00:00Z","distance":8000,"moving_time":2700,"total_elevation_gain":40,"average_speed":2.96},
 {"sport_type":"Ride","name":"Berg","start_date_local":"2026-11-08T10:00:00Z","distance":64000,"moving_time":10800,"total_elevation_gain":1210,"average_speed":5.9},
 {"sport_type":"Ride","name":"Alt","start_date_local":"2026-10-28T10:00:00Z","distance":90000,"moving_time":12600,"total_elevation_gain":600,"average_speed":7.1}
]`

func runStrava(t *testing.T, fake stravaFake, tool, args string) (map[string]any, error) {
	t.Helper()
	s := stravaSkill(t)
	// Thursday, 20 November 2026, in Berlin.
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("CET", 3600))
	res := s.Starlark.Call(context.Background(), tool, json.RawMessage(args), startools.Env{Requester: fake, Now: func() time.Time { return now }})
	if res.Err != nil {
		return nil, res.Err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Text), &out); err != nil {
		t.Fatalf("%s: %q", tool, res.Text)
	}
	return out, nil
}

// SV-05 The best efforts come from the activities
func TestSV05_TheBestEffortsComeFromTheActivities(t *testing.T) {
	// Given a connected account with activities and stats
	fake := stravaFake{"athlete/activities": activities, "athlete": `{"id":7}`,
		"athletes/7/stats": `{"ytd_ride_totals":{"count":40,"distance":2500000,"elevation_gain":21000},"all_ride_totals":{"count":300,"distance":15000000,"elevation_gain":120000},"biggest_ride_distance":999999}`}
	// When the best efforts are asked for
	out, err := runStrava(t, fake, "bestleistungen", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	// Then the longest ride and the biggest climb come from the activities and the totals from the stats
	l, g := out["laengste"].(map[string]any), out["groesster_anstieg"].(map[string]any)
	if l["name"] != "Alt" || l["km"] != 90.0 || g["name"] != "Berg" || g["hoehenmeter"] != 1210.0 {
		t.Errorf("laengste %v, groesster_anstieg %v", l, g)
	}
	if y := out["ytd_ride_totals"].(map[string]any); y["km"] != 2500.0 || y["anzahl"] != 40.0 {
		t.Errorf("ytd %v", y)
	}
	// And no field biggest_ride_distance is used
	if strings.Contains(fmt.Sprint(out), "999") {
		t.Errorf("biggest_ used: %v", out)
	}
}

// SV-06 The week and the month are counted by the tool
func TestSV06_TheWeekAndTheMonthAreCountedByTheTool(t *testing.T) {
	// Given activities on Wednesday and Monday of this week, earlier this month and last month
	fake := stravaFake{"athlete/activities": activities}
	// When the week and the month are asked for
	week, err := runStrava(t, fake, "zeitraum", `{"zeitraum":"woche"}`)
	if err != nil {
		t.Fatal(err)
	}
	month, _ := runStrava(t, fake, "zeitraum", `{"zeitraum":"monat"}`)
	// Then the week from Monday has the ride and the run, the month also the ride of the 8th
	ws := week["je_sportart"].(map[string]any)
	if week["ab"] != "2026-11-16" || ws["Ride"].(map[string]any)["km"] != 21.5 || ws["Run"].(map[string]any)["anzahl"] != 1.0 {
		t.Errorf("week %v", week)
	}
	mr := month["je_sportart"].(map[string]any)["Ride"].(map[string]any)
	if month["ab"] != "2026-11-01" || mr["anzahl"] != 2.0 || mr["km"] != 85.5 || mr["hoehenmeter"] != 1390.0 || mr["minuten"] != 245.0 {
		t.Errorf("month %v", month)
	}
	if _, err := runStrava(t, fake, "zeitraum", `{"zeitraum":"jahr"}`); err == nil || !strings.Contains(err.Error(), "woche") {
		t.Errorf("jahr: %v", err)
	}
}

// SV-07 The series counts weeks in a row
func TestSV07_TheSeriesCountsWeeksInARow(t *testing.T) {
	// Given activities this week, two weeks ago and three weeks ago, none last week
	fake := stravaFake{"athlete/activities": activities}
	// When the series is asked for
	out, err := runStrava(t, fake, "serie", `{}`)
	// Then it is one week (this one)
	if err != nil || out["wochen"] != 1.0 {
		t.Errorf("%v %v", out, err)
	}
	// Without an activity this week it counts from last week.
	fake["athlete/activities"] = `[{"sport_type":"Ride","start_date_local":"2026-11-12T10:00:00Z","distance":1,"moving_time":60,"total_elevation_gain":0},
		{"sport_type":"Ride","start_date_local":"2026-11-03T10:00:00Z","distance":1,"moving_time":60,"total_elevation_gain":0}]`
	if out, _ := runStrava(t, fake, "serie", `{}`); out["wochen"] != 2.0 {
		t.Errorf("from last week: %v", out)
	}
}

// SV-08 Strava's errors become sentences
func TestSV08_StravasErrorsBecomeSentences(t *testing.T) {
	for body, want := range map[string]string{
		`HTTP 401 {"errors":[{"field":"activity:read_permission"}]}`: "Strava erlaubt die Aktivitäten nicht",
		`HTTP 429 {}`: "keine Anfragen mehr",
		`HTTP 503 {}`: "nicht erreichbar",
	} {
		// Given Strava answers with an error
		// When a tool reads
		_, err := runStrava(t, stravaFake{"athlete/activities": body}, "letzte_tour", `{}`)
		// Then the tool's error is the sentence the skill says
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", body, err)
		}
	}
	// No activity is a hint, not an error.
	if out, err := runStrava(t, stravaFake{"athlete/activities": `[]`}, "letzte_tour", `{}`); err != nil || out["hinweis"] == nil {
		t.Errorf("%v %v", out, err)
	}
}

// SL-18 strava brings its tools in tools.star (docs/spec/api-tools.md, step 4)
func TestSL18_StravaBringsItsToolsInToolsStar(t *testing.T) {
	// Given the skill strava
	// When it loads
	s := stravaSkill(t)
	// Then it has the tools letzte_tour, zeitraum, serie, ausruestung and bestleistungen and no strava__api_get
	if s.Starlark == nil || !slices.Equal(s.Starlark.Names(), []string{"ausruestung", "bestleistungen", "letzte_tour", "serie", "zeitraum"}) {
		t.Fatalf("tools %v", s.Starlark)
	}
	// And none of them writes
	if len(s.Writes) != 0 || len(s.Confirm) != 0 {
		t.Errorf("writes %v, confirm %v", s.Writes, s.Confirm)
	}
}
