package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/startools"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
)

// plainSkill is a skill "kurse" without a login whose api section names
// prefix, loaded from a folder as Mira loads it.
func plainSkill(t *testing.T, prefix string) skills.Skill {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kurse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: kurse\ndescription: Kurse\nmetadata:\n  mira:\n    api:\n      - url: " + prefix + "\n---\nKurse.\n"
	star := "def kurs(wert):\n    \"\"\"Der Kurs.\n\n    Args:\n        wert (str): Name\n    \"\"\"\n    return api.get(\"" + prefix + "/q\").json()\n"
	for name, body := range map[string]string{"SKILL.md": md, "tools.star": star} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, problems := skills.Load([]string{filepath.Dir(dir)}, map[string]map[string]any{"kurse": {}})
	if len(problems) > 0 || len(loaded) != 1 {
		t.Fatalf("the skill did not load: %v", problems)
	}
	return loaded[0]
}

func plainManager(t *testing.T, sk skills.Skill, hc *http.Client) *oauth.Manager {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := oauth.New(context.Background(), oauth.Options{Store: st, Secrets: config.Secrets{Key: "k"},
		Skills: []skills.Skill{sk}, HTTP: hc, Now: clk.Now, Version: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// BO-01 A skill without a login reaches its web API, and only that
func TestBO01_ASkillWithoutALoginReachesItsWebAPIAndOnlyThat(t *testing.T) {
	var auth []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	// Given a skill without an oauth section names an api prefix
	sk := plainSkill(t, srv.URL+"/finance")
	if sk.OAuth != nil || len(sk.API) != 1 || sk.API[0].Methods[0] != "GET" || sk.Starlark == nil {
		t.Fatalf("skill %+v", sk)
	}
	m := plainManager(t, sk, srv.Client())
	if m.Connected("kurse") {
		t.Error("a skill without a login counts as connected")
	}
	if !sk.StarlarkUsable(m) {
		t.Error("the tools of a skill without a login are not usable")
	}
	// Then its Starlark tool reaches an address below the prefix, without a token
	status, body, err := m.Request(context.Background(), "kurse", "GET", srv.URL+"/finance/q?s=SAP", nil)
	if err != nil || status != 200 || string(body) != `{"ok":true}` {
		t.Fatalf("below the prefix: %d %q %v", status, body, err)
	}
	if len(auth) != 1 || auth[0] != "" {
		t.Errorf("Authorization %q", auth)
	}
	// And an address elsewhere is refused before anything is sent
	for _, c := range [][2]string{{"GET", srv.URL + "/other"}, {"POST", srv.URL + "/finance/q"}} {
		if _, _, err := m.Request(context.Background(), "kurse", c[0], c[1], nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s %s: %v", c[0], c[1], err)
		}
	}
	if len(auth) != 1 {
		t.Errorf("requests sent: %d", len(auth))
	}
}

// BO-02 Every request of a Starlark tool names Mira
func TestBO02_EveryRequestOfAStarlarkToolNamesMira(t *testing.T) {
	var agent string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
	}))
	defer srv.Close()
	m := plainManager(t, plainSkill(t, srv.URL), srv.Client())
	if _, _, err := m.Request(context.Background(), "kurse", "GET", srv.URL+"/q", nil); err != nil {
		t.Fatal(err)
	}
	// Then the request carries the header User-Agent Mira and her version
	if agent != "Mira/1.2.3" {
		t.Errorf("User-Agent %q", agent)
	}
}

// SL-21 A tools.star without an api section loads and reaches no address
func TestSL21_AToolsStarWithoutAnAPISectionLoadsAndReachesNoAddress(t *testing.T) {
	// Given a skill with a tools.star and no api section
	dir := filepath.Join(t.TempDir(), "rechnen")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: rechnen\ndescription: Rechnet\n---\nRechnet.\n"
	star := "def doppelt(x):\n    \"\"\"Verdoppelt.\n\n    Args:\n        x (int): Zahl\n    \"\"\"\n    return {\"x\": x * 2}\n\n" +
		"def holen():\n    \"\"\"Holt.\"\"\"\n    return api.get(\"https://example.com/x\").json\n"
	for name, body := range map[string]string{"SKILL.md": md, "tools.star": star} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, problems := skills.Load([]string{filepath.Dir(dir)}, map[string]map[string]any{"rechnen": {}})
	// Then the skill loads with its tools
	if len(problems) > 0 || len(loaded) != 1 || loaded[0].Starlark == nil {
		t.Fatalf("the skill did not load: %v", problems)
	}
	sk := loaded[0]
	m := plainManager(t, sk, http.DefaultClient)
	for _, tl := range skills.StarlarkTools(loaded, m, nil, nil) {
		if tl.WebContent {
			t.Errorf("%s counts as third-party text", tl.Name)
		}
	}
	if !sk.StarlarkUsable(m) {
		t.Error("its tools are not usable")
	}
	res := sk.Starlark.Call(context.Background(), "doppelt", []byte(`{"x":21}`), startools.Env{Requester: m})
	if res.Err != nil || res.Text != `{"x": 42}` && res.Text != `{"x":42}` {
		t.Errorf("doppelt: %q %v", res.Text, res.Err)
	}
	// And api.get from one of them fails naming that the skill has no api section
	res = sk.Starlark.Call(context.Background(), "holen", []byte(`{}`), startools.Env{Requester: m})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "rechnen has no api section") {
		t.Errorf("holen: %v", res.Err)
	}
}
