package oauth_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
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
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
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

func toolNames(m *oauth.Manager) []string {
	var out []string
	for _, tl := range m.APITools(i18n.DE) {
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
	// Then there is strava__api_get for Strava's web API and no strava__api_write
	if got := toolNames(m); !slices.Equal(got, []string{"strava__api_get"}) {
		t.Fatalf("tools %v", got)
	}
	var get = m.APITools(i18n.DE)[0]
	if !get.ReadOnly || !get.WebContent || !strings.Contains(get.Description, "https://www.strava.com/api/v3/") {
		t.Errorf("tool %+v", get)
	}
	// And an address outside it is refused without a request
	for _, u := range []string{"https://www.strava.com/oauth/deauthorize", "https://www.strava.com/api/v2/athlete",
		"https://strava.example/api/v3/athlete"} {
		if _, err := run(get, `{"url":"`+u+`"}`); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s: %v", u, err)
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
	// Then strava__api_get is not offered
	if got := toolNames(m); len(got) != 0 {
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

// SV-05 The best efforts come from the activities
func TestSV05_TheBestEffortsComeFromTheActivities(t *testing.T) {
	// Given the skill strava
	s := stravaSkill(t)
	// When its instructions are read
	i := strings.Index(s.Body, "**Bestleistungen**")
	if i < 0 {
		t.Fatalf("no best efforts in:\n%s", s.Body)
	}
	best := s.Body[i:]
	if j := strings.Index(best, "\n\n"); j > 0 {
		best = best[:j]
	}
	// Then the longest ride and the biggest climb come from the last 100
	// activities and the totals from the stats
	for _, want := range []string{"athlete/activities?per_page=100", "`distance`", "`total_elevation_gain`",
		"athletes/<id>/stats", "ytd_ride_totals", "all_ride_totals"} {
		if !strings.Contains(best, want) {
			t.Errorf("best efforts without %s:\n%s", want, best)
		}
	}
	// And they name no field biggest_ride_distance or biggest_climb_elevation_gain
	if strings.Contains(s.Body, "biggest_") {
		t.Errorf("names a biggest_ field:\n%s", s.Body)
	}
}
