package oauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/fakemcp"
	"github.com/miraassist/mira/internal/testsupport/fakeoauth"
	"github.com/miraassist/mira/internal/tools"
)

// env is a skill "dienst" with a login at a fake service, its MCP server
// wanting the service's current token, and the connections in a store of
// their own. The clock moves only when a test says so.
type env struct {
	svc     *fakeoauth.Service
	mcpSrv  *fakemcp.Server
	st      *store.Store
	sec     config.Secrets
	skill   skills.Skill
	m       *oauth.Manager
	host    *mcp.Host
	logs    *syncBuffer
	now     time.Time
	mu      sync.Mutex
	changes chan string
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{svc: fakeoauth.New("id-1", "geheim-secret"), mcpSrv: fakemcp.New("dienst"), logs: &syncBuffer{},
		now: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), changes: make(chan string, 8)}
	t.Cleanup(e.svc.Close)
	t.Cleanup(e.mcpSrv.Close)
	e.svc.Basic = true
	e.mcpSrv.Authorize = e.svc.Valid
	e.mcpSrv.AddTool("suche", "Sucht.", `{"type":"object","properties":{}}`, true, func(map[string]any) (string, bool) { return "gefunden", false })
	var err error
	e.st, err = store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.st.Close() })
	e.sec = config.Secrets{Key: "schluessel"}
	srv := config.MCPServer{Name: "dienst", Skill: "dienst", Transport: config.TransportHTTP, URL: e.mcpSrv.Endpoint(),
		OAuth: true, WebContent: true}
	e.skill = skills.Skill{Name: "dienst", Description: "Ein Dienst.", Body: "Nimm dienst__suche.", MCP: &srv,
		OAuth: &skills.OAuth{Name: "Dienst", AuthorizeURL: e.svc.AuthorizeURL(), TokenURL: e.svc.TokenURL(),
			PKCE: true, TokenAuth: skills.TokenAuthBasic, ClientID: "id-1", ClientSecret: "geheim-secret",
			API: []skills.APIPrefix{{URL: e.svc.APIURL() + "v2/", Methods: []string{"GET"}},
				{URL: e.svc.APIURL() + "v2/posts", Methods: []string{"POST"}}}}}
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.m, err = oauth.New(context.Background(), oauth.Options{Store: e.st, Secrets: e.sec, Skills: []skills.Skill{e.skill},
		HTTP: e.svc.Client(), Now: e.clock, Logger: log,
		OnChange: func(skill string) {
			if e.host != nil {
				e.host.Reconnect(context.Background(), skill)
			}
			e.changes <- skill
		}})
	if err != nil {
		t.Fatal(err)
	}
	e.host = mcp.New(mcp.Options{Servers: []config.MCPServer{srv}, Connections: e.m, Logger: log})
	t.Cleanup(e.host.Close)
	if err := e.host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}

// connect hands over a login's tokens as the integration does; it waits
// until the server is connected again.
func (e *env) connect(t *testing.T, life time.Duration) (access, refresh string) {
	t.Helper()
	access, refresh = e.svc.Grant()
	if err := e.m.Put(context.Background(), "dienst", oauth.Handover{AccessToken: access, RefreshToken: refresh,
		TokenType: "bearer", ExpiresIn: life.Seconds()}); err != nil {
		t.Fatal(err)
	}
	<-e.changes
	return access, refresh
}

func (e *env) call(t *testing.T) (mcp.Result, error) {
	t.Helper()
	return e.host.Call(context.Background(), "dienst__suche", json.RawMessage(`{}`))
}

// CO-03 The login data goes to the integration only
func TestCO03_TheLoginDataGoesToTheIntegrationOnly(t *testing.T) {
	// Given a skill with an oauth section, client id and secret set
	e := newEnv(t)
	api := httptest.NewServer(haconv.New(haconv.Options{Token: "mira-token", Logins: e.m}))
	t.Cleanup(api.Close)
	get := func(token string) (*http.Response, oauth.Login) {
		req, _ := http.NewRequest(http.MethodGet, api.URL+"/v1/oauth/dienst", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var l oauth.Login
		json.NewDecoder(resp.Body).Decode(&l)
		return resp, l
	}
	// When the integration asks GET /v1/oauth/<skill> with the bearer token
	resp, l := get("mira-token")
	// Then it gets addresses, scopes, PKCE, extra parameters, client id and secret
	if resp.StatusCode != 200 || l.AuthorizeURL != e.svc.AuthorizeURL() || l.TokenURL != e.svc.TokenURL() || !l.PKCE ||
		l.TokenAuth != "basic" || l.ClientID != "id-1" || l.ClientSecret != "geheim-secret" || l.Name != "Dienst" {
		t.Errorf("%d %+v", resp.StatusCode, l)
	}
	// And without the bearer token it gets nothing
	if resp, l := get(""); resp.StatusCode != http.StatusUnauthorized || l.ClientSecret != "" {
		t.Errorf("without the token: %d %+v", resp.StatusCode, l)
	}
}

// CO-04 Tokens handed over connect the skill's MCP server
func TestCO04_TokensHandedOverConnectTheSkillsMCPServer(t *testing.T) {
	// Given a skill with an oauth section and an mcp section with token oauth, not connected
	e := newEnv(t)
	if _, ok := e.host.Lookup("dienst__suche"); ok {
		t.Fatal("connected without a connection")
	}
	if st := e.host.Status()[0]; !st.NoConnection || len(e.mcpSrv.Tokens()) != 0 {
		t.Fatalf("a server without a connection was tried: %+v", st)
	}
	// When the integration puts the tokens to /v1/oauth/<skill>/token
	api := httptest.NewServer(haconv.New(haconv.Options{Token: "mira-token", Logins: e.m}))
	t.Cleanup(api.Close)
	access, refresh := e.svc.Grant()
	body, _ := json.Marshal(map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "bearer",
		"expires_in": 7200, "expires_at": float64(e.clock().Add(2 * time.Hour).Unix()), "scope": "read offline.access"})
	req, _ := http.NewRequest(http.MethodPut, api.URL+"/v1/oauth/dienst/token", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer mira-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %v %v", err, resp)
	}
	<-e.changes
	// Then they are stored encrypted in Mira's database
	raw, err := e.st.GetOAuthToken(context.Background(), "dienst")
	if err != nil || !strings.HasPrefix(raw, "mira-secrets:v1:") || strings.Contains(raw, access) || strings.Contains(raw, refresh) {
		t.Errorf("stored %q, %v", raw, err)
	}
	// And Mira connects to the skill's server with the access token
	if _, ok := e.host.Lookup("dienst__suche"); !ok || e.m.State("dienst") != oauth.StateConnected {
		t.Fatal("the server did not connect")
	}
	if res, err := e.call(t); err != nil || res.Text != "gefunden" {
		t.Errorf("call: %+v %v", res, err)
	}
	for _, tok := range e.mcpSrv.Tokens() {
		if tok != access {
			t.Errorf("the server got %q", tok)
		}
	}
	// After a restart the connection is still there.
	again, err := oauth.New(context.Background(), oauth.Options{Store: e.st, Secrets: e.sec, Skills: []skills.Skill{e.skill}})
	if err != nil || again.State("dienst") != oauth.StateConnected {
		t.Errorf("after a restart: %v %s", err, again.State("dienst"))
	}
}

// CO-05 A token that ran out is renewed before the call
func TestCO05_ATokenThatRanOutIsRenewedBeforeTheCall(t *testing.T) {
	// Given a connection whose access token has run out
	e := newEnv(t)
	old, oldRefresh := e.connect(t, time.Hour)
	e.advance(time.Hour)
	// When the skill's MCP server is called
	if _, err := e.call(t); err != nil {
		t.Fatal(err)
	}
	// Then Mira renews it at token_url first and the call carries the new token
	toks := e.mcpSrv.Tokens()
	last := toks[len(toks)-1]
	if e.svc.Renewals() != 1 || last == old || !e.svc.Valid(last) {
		t.Errorf("renewals %d, last token %q (old %q)", e.svc.Renewals(), last, old)
	}
	// And a new refresh token from the service replaces the stored one:
	// the old one is refused by now, and the next renewal works.
	e.advance(2 * time.Hour)
	if _, err := e.call(t); err != nil || e.svc.Renewals() != 2 {
		t.Errorf("second renewal: %v, renewals %d (old refresh token %q)", err, e.svc.Renewals(), oldRefresh)
	}
}

// CO-06 A 401 renews once and repeats once
func TestCO06_A401RenewsOnceAndRepeatsOnce(t *testing.T) {
	// Given a connection whose access token the service refuses
	e := newEnv(t)
	e.connect(t, 2*time.Hour)
	e.svc.Expire()
	// When the skill's MCP server is called
	res, err := e.call(t)
	// Then Mira renews once and repeats the request once
	if err != nil || res.Text != "gefunden" || e.svc.Renewals() != 1 {
		t.Fatalf("call %+v %v, renewals %d", res, err, e.svc.Renewals())
	}
	if e.m.State("dienst") != oauth.StateConnected {
		t.Error("the connection did not stay")
	}
}

// CO-07 A refused refresh token makes the connection expired
func TestCO07_ARefusedRefreshTokenMakesTheConnectionExpired(t *testing.T) {
	// Given a connection whose refresh token the service refuses
	e := newEnv(t)
	e.connect(t, time.Hour)
	e.svc.Revoke()
	e.advance(time.Hour)
	// When the token is renewed
	if _, err := e.m.Token(context.Background(), "dienst"); err != oauth.ErrExpired {
		t.Fatalf("token: %v", err)
	}
	if got := <-e.changes; got != "dienst" {
		t.Fatalf("changed %q", got)
	}
	// Then the tokens are deleted, the state is expired
	raw, _ := e.st.GetOAuthToken(context.Background(), "dienst")
	plain, _ := e.sec.Open(raw)
	if e.m.State("dienst") != oauth.StateExpired || strings.Contains(plain, "token") {
		t.Errorf("state %s, stored %q", e.m.State("dienst"), plain)
	}
	if _, ok := e.host.Lookup("dienst__suche"); ok {
		t.Error("the server's tools are still there")
	}
	// (Home Assistant's repair: cmd/mira's TestCO07_AnExpiredConnectionIsARepair.)
	// And when the skill is opened Mira says that the connection must be made again
	set := &skills.Set{Lang: i18n.DE, Skills: []skills.Skill{e.skill}, Connection: e.m.State}
	open := set.Tools()[0]
	out, err := open.Run(context.Background(), tools.Invocation{Args: json.RawMessage(`{"name":"dienst"}`)})
	if err != nil || !strings.Contains(out, "abgelaufen") || !strings.Contains(out, "Verbindungen") {
		t.Errorf("skill_open: %q %v", out, err)
	}
}

// CO-08 The tokens reach no model and no log
func TestCO08_TheTokensReachNoModelAndNoLog(t *testing.T) {
	// Given a connected skill
	e := newEnv(t)
	access, refresh := e.connect(t, time.Hour)
	// When a conversation uses its tools and the log is written
	e.call(t)
	e.advance(time.Hour)
	e.call(t)
	e.svc.Revoke()
	e.advance(2 * time.Hour)
	e.call(t)
	// Then neither the access token, the refresh token nor the secret is in a prompt or in the log
	logs := e.logs.String()
	for _, secret := range append(e.mcpSrv.Tokens(), access, refresh, "geheim-secret") {
		if secret != "" && strings.Contains(logs, secret) {
			t.Errorf("%q is in the log", secret)
		}
	}
	if !strings.Contains(logs, "renewed") {
		t.Error("the log has nothing of the renewal; the test checks nothing")
	}
	set := &skills.Set{Lang: i18n.DE, Skills: []skills.Skill{e.skill}, Connection: e.m.State}
	for _, tl := range append(set.Tools(), e.m.APITools(i18n.DE)...) {
		for _, text := range []string{tl.Description, tl.Schema} {
			if strings.Contains(text, "geheim-secret") || strings.Contains(text, access) {
				t.Errorf("%s carries a secret", tl.Name)
			}
		}
	}
}

// apiTool is the skill's web API tool of that name.
func (e *env) apiTool(t *testing.T, name string) tools.InternalTool {
	t.Helper()
	for _, tl := range e.m.APITools(i18n.DE) {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return tools.InternalTool{}
}

func run(tl tools.InternalTool, args string) (string, error) {
	return tl.Run(context.Background(), tools.Invocation{Name: tl.Name, Args: json.RawMessage(args)})
}

// CO-15 The API tool calls only below the skill's prefixes
func TestCO15_TheAPIToolCallsOnlyBelowTheSkillsPrefixes(t *testing.T) {
	// Given a connected skill whose api names https://api.example/2/ with GET
	e := newEnv(t)
	access, _ := e.connect(t, time.Hour)
	get := e.apiTool(t, "dienst__api_get")
	// When the model calls the API tool with a path below it
	out, err := run(get, `{"url":"`+e.svc.APIURL()+`v2/users/me?fields=name"}`)
	// Then the request carries the token and the answer is third-party text
	calls := e.svc.Calls()
	if err != nil || !strings.HasPrefix(out, "HTTP 200") || len(calls) != 1 || calls[0].Token != access || calls[0].Path != "/api/v2/users/me" {
		t.Fatalf("%q %v %+v", out, err, calls)
	}
	if !get.WebContent || !get.ReadOnly || !get.Background || get.Skill != "dienst" {
		t.Errorf("flags %+v", get)
	}
	// Not connected, the tool is not offered.
	e.m.Disconnect(context.Background(), "dienst")
	<-e.changes
	if get.Enabled() {
		t.Error("offered without a connection")
	}
}

// CO-16 No token outside the prefixes
func TestCO16_NoTokenOutsideThePrefixes(t *testing.T) {
	// Given a connected skill whose api names https://api.example/2/
	e := newEnv(t)
	e.connect(t, time.Hour)
	get := e.apiTool(t, "dienst__api_get")
	// When the model calls an address outside it
	for _, u := range []string{e.svc.APIURL() + "v1/users", e.svc.APIURL() + "v2x", "https://elsewhere.example/api/v2/", e.svc.URL() + "/token"} {
		if out, err := run(get, `{"url":"`+u+`"}`); err == nil {
			t.Errorf("%s: %q", u, out)
		}
	}
	if n := len(e.svc.Calls()); n != 0 {
		t.Errorf("%d requests made", n)
	}
	// or the service redirects to another host
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the redirect was followed, Authorization %q", r.Header.Get("Authorization"))
	}))
	t.Cleanup(other.Close)
	redirect := httptest.NewTLSServer(http.RedirectHandler(other.URL+"/steal", http.StatusFound))
	t.Cleanup(redirect.Close)
	m, _ := oauth.New(context.Background(), oauth.Options{Store: e.st, Secrets: e.sec, HTTP: redirect.Client(), Now: e.clock,
		Skills: []skills.Skill{{Name: "dienst", OAuth: &skills.OAuth{Name: "Dienst", TokenURL: e.svc.TokenURL(),
			TokenAuth: skills.TokenAuthBasic, ClientID: "id-1", ClientSecret: "geheim-secret",
			API: []skills.APIPrefix{{URL: redirect.URL + "/", Methods: []string{"GET"}}}}}}})
	var tl tools.InternalTool
	for _, x := range m.APITools(i18n.DE) {
		tl = x
	}
	// Then no request with the token is made
	if out, err := run(tl, `{"url":"`+redirect.URL+`/api"}`); err == nil || !strings.Contains(err.Error(), "does not go to") {
		t.Errorf("followed: %q %v", out, err)
	}
}

// CO-17 A method the skill does not name is refused
func TestCO17_AMethodTheSkillDoesNotNameIsRefused(t *testing.T) {
	// Given a connected skill whose api allows only GET (below v2/; POST only at v2/posts)
	e := newEnv(t)
	e.connect(t, time.Hour)
	write := e.apiTool(t, "dienst__api_write")
	// When the model calls the API tool with POST
	for _, args := range []string{
		`{"method":"POST","url":"` + e.svc.APIURL() + `v2/users"}`,
		`{"method":"DELETE","url":"` + e.svc.APIURL() + `v2/posts/1"}`,
	} {
		// Then the call is refused with that reason
		if out, err := run(write, args); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s: %q %v", args, out, err)
		}
	}
	if n := len(e.svc.Calls()); n != 0 {
		t.Errorf("%d requests made", n)
	}
}

// CO-18 A method the skill names runs as the skill says
func TestCO18_AMethodTheSkillNamesRunsAsTheSkillSays(t *testing.T) {
	// Given a connected skill whose api allows POST and whose instructions say to post without asking
	e := newEnv(t)
	e.connect(t, time.Hour)
	write := e.apiTool(t, "dienst__api_write")
	// When the model calls the API tool with POST
	out, err := run(write, `{"method":"POST","url":"`+e.svc.APIURL()+`v2/posts","body":{"text":"Guten Morgen"}}`)
	// Then the request is made without a question of Mira's own
	calls := e.svc.Calls()
	if err != nil || !strings.HasPrefix(out, "HTTP 200") || len(calls) != 1 || calls[0].Method != "POST" || calls[0].Body != `{"text":"Guten Morgen"}` {
		t.Fatalf("%q %v %+v", out, err, calls)
	}
	// The tool asks only after third-party text (CO-19): it is not read only
	// and keeps or passes on.
	if write.ReadOnly || !write.AskAfterWeb || !write.WebContent {
		t.Errorf("flags %+v", write)
	}
}
