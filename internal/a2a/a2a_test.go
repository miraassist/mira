package a2a

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	a2afake "github.com/miraassist/mira/testenv/a2a_fake"
)

// skillFile writes the skill recherche with a remote agent; agent is the
// agent's entry below its remote section, extra goes under metadata.mira.
func skillFile(t *testing.T, agent, extra string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "recherche"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := `---
name: recherche
description: Recherchiert im Netz.
metadata:
  mira:
    settings:
      url:   { description: Adresse des Agenten, required: true }
      token: { description: Token des Agenten, secret: true }
    agents:
      recherche:
        remote:
          url: "{{ url }}"
          token: "{{ token }}"
` + agent + extra + `
---
Recherche.
`
	if err := os.WriteFile(filepath.Join(dir, "recherche", "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// load loads the skill with the agent's address and the fake's token.
func load(t *testing.T, dir, url string) []skills.Skill {
	t.Helper()
	loaded, problems := skills.Load([]string{dir}, map[string]map[string]any{
		"recherche": {"url": url, "token": a2afake.DefaultToken}})
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems[0].Text(i18n.EN))
	}
	return loaded
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

func logger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func fake(t *testing.T, o a2afake.Options) (*a2afake.Fake, *httptest.Server) {
	t.Helper()
	f := a2afake.New(o)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func only(t *testing.T, h *Host) Status {
	t.Helper()
	st := h.Status()
	if len(st) != 1 {
		t.Fatalf("status = %+v", st)
	}
	return st[0]
}

// RM-01 A skill with a remote agent: its card is read, the agent has the
// card's description, or the skill's when it gives one.
func TestRM01_ASkillWithARemoteAgent(t *testing.T) {
	f, srv := fake(t, a2afake.Options{})
	for _, c := range []struct {
		name, agent, desc, label string
	}{
		{"card", "", a2afake.Description, a2afake.Name},
		{"card's skill", "          a2a_skill: complete\n", "Verhalten complete.", a2afake.Name},
		{"skill's own", "          a2a_skill: complete\n        name: Hermes\n        description: Recherchiert ausführlich.\n",
			"Recherchiert ausführlich.", "Hermes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.Reset()
			log, _ := logger()
			h := New(Options{Skills: load(t, skillFile(t, c.agent, ""), srv.URL), Logger: log})
			h.Start(context.Background())
			got := f.Received()
			if len(got) != 1 || got[0].Kind != "card" || got[0].Card != a2afake.CardFull {
				t.Fatalf("received %+v", got)
			}
			if st := only(t, h); !st.Ready || st.Problem != nil {
				t.Fatalf("status %+v", st)
			}
			a, _ := h.Agent("recherche")
			info, ok := a.Info()
			if !ok || info.Description != c.desc || info.Label != c.label || !info.Stream || info.Push ||
				info.Scheme != SchemeBearer || info.Transport != "JSONRPC" || info.Endpoint != srv.URL+"/a2a" {
				t.Errorf("info %+v", info)
			}
		})
	}
}

// RM-02 A card without push and without streaming: the skill has a
// problem naming the agent and stays, without the agent.
func TestRM02_ACardWithoutPushAndWithoutStreaming(t *testing.T) {
	_, srv := fake(t, a2afake.Options{})
	log, buf := logger()
	loaded := load(t, skillFile(t, "        name: Hermes\n", ""), srv.URL+"/"+a2afake.CardNeither)
	if len(loaded) != 1 {
		t.Fatalf("the skill is not there: %+v", loaded)
	}
	h := New(Options{Skills: loaded, ServerURL: "http://mira.example:8787", Logger: log})
	h.Start(context.Background())
	st := only(t, h)
	if st.Ready || st.Problem == nil {
		t.Fatalf("status %+v", st)
	}
	text := st.Problem.Text(i18n.DE)
	if st.Problem.Skill != "recherche" || !strings.HasPrefix(text, "Hermes ") || !strings.Contains(text, "weder") {
		t.Errorf("problem %q", text)
	}
	if a, _ := h.Agent("recherche"); a.Client() != nil {
		t.Error("an agent with a problem has a client")
	}
	if !strings.Contains(buf.String(), "remote agent not usable") {
		t.Errorf("log:\n%s", buf)
	}
}

// RM-03 A remote agent with a model field: the skill has a problem and is
// left out.
func TestRM03_ARemoteAgentWithAModelField(t *testing.T) {
	for _, field := range []string{"model: main", "instructions: Tu was.", "tools: read_only", "conversation: true"} {
		dir := skillFile(t, "        "+field+"\n", "")
		loaded, problems := skills.Load([]string{dir}, map[string]map[string]any{"recherche": {"url": "http://agent.example"}})
		if len(loaded) != 0 || len(problems) != 1 {
			t.Fatalf("%s: loaded %d, problems %d", field, len(loaded), len(problems))
		}
		if text := problems[0].Text(i18n.EN); !strings.Contains(text, strings.SplitN(field, ":", 2)[0]) || !strings.Contains(text, "remote") {
			t.Errorf("%s: %q", field, text)
		}
	}
	// A name without remote, likewise.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "lokal"), 0o755)
	os.WriteFile(filepath.Join(dir, "lokal", "SKILL.md"), []byte("---\nname: lokal\ndescription: x\nmetadata:\n  mira:\n    agents:\n      helfer:\n        name: Hermes\n        description: x\n        instructions: y\n---\n"), 0o644)
	if loaded, problems := skills.Load([]string{dir}, map[string]map[string]any{"lokal": nil}); len(loaded) != 0 || len(problems) != 1 {
		t.Errorf("name without remote: loaded %d, problems %d", len(loaded), len(problems))
	}
}

// RM-18 A remote agent that asks for OAuth2: with the skill's connection
// made, the request carries the connection's token.
func TestRM18_ARemoteAgentThatAsksForOAuth2(t *testing.T) {
	const token = "connection-token"
	f, srv := fake(t, a2afake.Options{Token: token})
	oauthSection := `    oauth:
      name: Agent
      authorize_url: https://login.example/authorize
      token_url: https://login.example/token
`
	loaded := load(t, skillFile(t, "          a2a_skill: complete\n", oauthSection), srv.URL+"/"+a2afake.CardOAuth2)
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	conns, err := oauth.New(ctx, oauth.Options{Store: st, Skills: loaded})
	if err != nil {
		t.Fatal(err)
	}
	log, _ := logger()
	h := New(Options{Skills: loaded, Connections: conns, Logger: log})
	h.Start(ctx)
	// Not connected yet: the agent waits for the connection, no problem.
	if s := only(t, h); s.Ready || !s.Waiting || s.Problem != nil {
		t.Fatalf("before the connection: %+v", s)
	}
	if err := conns.Put(ctx, "recherche", oauth.Handover{AccessToken: token, ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	if s := only(t, h); !s.Ready {
		t.Fatalf("after the connection: %+v", s)
	}
	a, _ := h.Agent("recherche")
	if info, _ := a.Info(); info.Scheme != SchemeOAuth2 {
		t.Errorf("scheme %q", info.Scheme)
	}
	msg := sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart("Wärmepumpe"))
	msg.SetMeta(a2afake.SkillMeta, a2afake.SkillComplete)
	if _, err := a.Client().SendMessage(ctx, &sdk.SendMessageRequest{Message: msg}); err != nil {
		t.Fatal(err)
	}
	got := f.Received()
	var last a2afake.Received
	for _, r := range got {
		if r.Kind == "SendMessage" {
			last = r
		}
	}
	if last.Kind != "SendMessage" || last.Auth != "Bearer "+token || last.Refused {
		t.Errorf("received %+v", got)
	}
}

// RM-19 OAuth2 without a connection in the skill: a problem naming the
// scheme.
func TestRM19_OAuth2WithoutAConnectionInTheSkill(t *testing.T) {
	_, srv := fake(t, a2afake.Options{})
	log, _ := logger()
	h := New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL+"/"+a2afake.CardOAuth2), Logger: log})
	h.Start(context.Background())
	st := only(t, h)
	if st.Ready || st.Problem == nil {
		t.Fatalf("status %+v", st)
	}
	if text := st.Problem.Text(i18n.DE); !strings.Contains(text, "OAuth2") || !strings.Contains(text, "oauth") {
		t.Errorf("problem %q", text)
	}
}

// RM-20 Only push, and no address for remote agents: the problem says that
// Mira's address is missing; with it the agent takes push.
func TestRM20_OnlyPushAndNoAddressForRemoteAgents(t *testing.T) {
	_, srv := fake(t, a2afake.Options{})
	loaded := load(t, skillFile(t, "", ""), srv.URL+"/"+a2afake.CardPushOnly)
	log, _ := logger()
	h := New(Options{Skills: loaded, Logger: log})
	h.Start(context.Background())
	st := only(t, h)
	if st.Ready || st.Problem == nil || !strings.Contains(st.Problem.Text(i18n.DE), "Adresse für entfernte Agenten") {
		t.Fatalf("status %+v", st)
	}
	h = New(Options{Skills: loaded, ServerURL: "http://mira.example:8787", Logger: log})
	h.Start(context.Background())
	a, _ := h.Agent("recherche")
	if info, ok := a.Info(); !ok || !info.Push || info.Stream {
		t.Errorf("with server.url: %+v", info)
	}
}

// RM-04 The card cannot be read when Mira starts: the log names the URL
// and the error; Mira reads it again later and the agent becomes ready.
func TestRM04_TheCardCannotBeRead(t *testing.T) {
	f := a2afake.New(a2afake.Options{})
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "Wartung bis heute Abend", http.StatusServiceUnavailable)
			return
		}
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	log, buf := logger()
	h := New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL), Logger: log, RetryInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()

	waitFor(t, func() bool { return only(t, h).Problem != nil })
	st := only(t, h)
	if text := st.Problem.Text(i18n.DE); !strings.Contains(text, "nicht erreichbar") || !strings.Contains(text, "503") {
		t.Errorf("problem %q", text)
	}
	out := buf.String()
	if !strings.Contains(out, "card cannot be read, trying again") || !strings.Contains(out, srv.URL+CardPath) ||
		!strings.Contains(out, "503") || !strings.Contains(out, "Wartung bis heute Abend") {
		t.Errorf("log:\n%s", out)
	}

	up.Store(true)
	waitFor(t, func() bool { return only(t, h).Ready })
	if !strings.Contains(buf.String(), "remote agent ready after its card could be read again") {
		t.Errorf("log:\n%s", buf)
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Errorf("run: %v", err)
	}
}

// A refused card (a 404) is not read again; a card that is no card names
// what came.
func TestRemoteCardRefusedOrNotACard(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasPrefix(r.URL.Path, "/html") {
			fmt.Fprint(w, "<html>Anmeldung</html>")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	log, buf := logger()
	h := New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL), Logger: log, RetryInterval: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	h.Run(ctx)
	if n := calls.Load(); n != 1 {
		t.Errorf("a refused card was read %d times", n)
	}
	if st := only(t, h); st.Problem == nil || !strings.Contains(st.Problem.Text(i18n.DE), "404") {
		t.Errorf("status %+v", st)
	}
	if !strings.Contains(buf.String(), "card refused; not read again") {
		t.Errorf("log:\n%s", buf)
	}

	h = New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL+"/html"), Logger: log})
	h.Start(context.Background())
	if st := only(t, h); st.Problem == nil || !strings.Contains(st.Problem.Text(i18n.EN), "<html>Anmeldung") {
		t.Errorf("not a card: %+v", st)
	}
}

// The other cards: an API key, no login, a skill the card does not have,
// a token missing, an interface elsewhere.
func TestRemoteCards(t *testing.T) {
	f, srv := fake(t, a2afake.Options{})
	log, _ := logger()
	ctx := context.Background()

	h := New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL+"/"+a2afake.CardAPIKey), Logger: log})
	h.Start(ctx)
	a, _ := h.Agent("recherche")
	if info, ok := a.Info(); !ok || info.Scheme != SchemeAPIKey {
		t.Fatalf("api key: %+v", info)
	}
	msg := sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart("x"))
	msg.SetMeta(a2afake.SkillMeta, a2afake.SkillComplete)
	if _, err := a.Client().SendMessage(ctx, &sdk.SendMessageRequest{Message: msg}); err != nil {
		t.Errorf("api key: %v", err)
	}

	h = New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL+"/"+a2afake.CardOpen), Logger: log})
	h.Start(ctx)
	a, _ = h.Agent("recherche")
	if info, ok := a.Info(); !ok || info.Scheme != SchemeNone {
		t.Errorf("open: %+v", info)
	}

	h = New(Options{Skills: load(t, skillFile(t, "          a2a_skill: zaubern\n", ""), srv.URL), Logger: log})
	h.Start(ctx)
	if st := only(t, h); st.Problem == nil || !strings.Contains(st.Problem.Text(i18n.DE), "zaubern") ||
		!strings.Contains(st.Problem.Text(i18n.DE), a2afake.SkillComplete) {
		t.Errorf("unknown skill: %+v", st)
	}

	noToken, problems := skills.Load([]string{skillFile(t, "", "")}, map[string]map[string]any{"recherche": {"url": srv.URL}})
	if len(problems) > 0 {
		t.Fatal(problems[0].Text(i18n.EN))
	}
	h = New(Options{Skills: noToken, Logger: log})
	h.Start(ctx)
	if st := only(t, h); st.Problem == nil || !strings.Contains(st.Problem.Text(i18n.DE), "Token") {
		t.Errorf("no token: %+v", st)
	}

	// The card names another address for its requests: the token does not
	// go there.
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "agent.example:9999"
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(elsewhere.Close)
	h = New(Options{Skills: load(t, skillFile(t, "", ""), elsewhere.URL), Logger: log})
	h.Start(ctx)
	st := only(t, h)
	if st.Problem == nil || !strings.Contains(st.Problem.Text(i18n.DE), "nur an die Adresse") ||
		!strings.Contains(st.Problem.Text(i18n.DE), "http://agent.example:9999/a2a") {
		t.Errorf("elsewhere: %+v", st)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// An agent whose card was never read is named all the same when its call
// cannot start (decision 22): by the skill's name for it, else its key.
func TestFailureNamesAnAgentThatIsNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	log, _ := logger()
	h := New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL), Logger: log})
	h.Start(context.Background())
	if label, said, ok := h.Failure(store.AgentRun{Agent: "recherche"}); !ok || label != "recherche" || said != "" {
		t.Errorf("not ready: %q %q %v", label, said, ok)
	}
	h = New(Options{Skills: load(t, skillFile(t, "        name: Hermes\n", ""), srv.URL), Logger: log})
	h.Start(context.Background())
	if label, _, ok := h.Failure(store.AgentRun{Agent: "recherche"}); !ok || label != "Hermes" {
		t.Errorf("with a name: %q %v", label, ok)
	}
	if _, _, ok := h.Failure(store.AgentRun{Agent: "wetter"}); ok {
		t.Error("an agent that is no remote agent is named")
	}
}

// RM-25 Without the skill's name, the card's name once read: before the
// card is read the skill's agent has no name; the skill's own name holds
// from the start.
func TestRM25_WithoutTheSkillsNameTheCardsNameOnceRead(t *testing.T) {
	f := a2afake.New(a2afake.Options{})
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	log, _ := logger()
	// The skill's own name: there before the card is read.
	named := New(Options{Skills: load(t, skillFile(t, "        name: Hermes\n", ""), srv.URL), Logger: log})
	if got := named.Names("recherche"); len(got) != 1 || got[0] != "Hermes" {
		t.Errorf("with the skill's name: %v", got)
	}
	// Given the skill gives no name for its remote agent
	// And its card cannot be read yet
	h := New(Options{Skills: load(t, skillFile(t, "", ""), srv.URL), Logger: log, RetryInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	waitFor(t, func() bool { return only(t, h).Problem != nil })
	// Then the entry names no agent (not its key)
	if got := h.Names("recherche"); len(got) != 0 {
		t.Errorf("before the card: %v", got)
	}
	// When the card is read
	up.Store(true)
	waitFor(t, func() bool { return only(t, h).Ready })
	// Then the next turn's entry names the card's name
	if got := h.Names("recherche"); len(got) != 1 || got[0] != a2afake.Name {
		t.Errorf("after the card: %v", got)
	}
	if got := h.Names("merken"); len(got) != 0 {
		t.Errorf("a skill without a remote agent: %v", got)
	}
	cancel()
	<-done
}
