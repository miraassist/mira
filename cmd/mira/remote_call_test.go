package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/a2a"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
	a2afake "github.com/miraassist/mira/testenv/a2a_fake"
)

// remoteEnv is the app with a skill whose target's field "fact" a remote
// agent fills: the fake A2A agent in the process, Mira's push route on a
// server of its own.
type remoteEnv struct {
	a    *app
	fake *a2afake.Fake
	log  *lockedBuffer
	mira *httptest.Server

	mu   sync.Mutex
	told []store.Message
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// newRemoteEnv: card is the fake's card (a2afake.CardPushOnly, …), skill
// the behaviour (a2afake.SkillComplete, …); withURL sets server.url.
func newRemoteEnv(t *testing.T, card, skill string, withURL bool) *remoteEnv {
	t.Helper()
	a, _, _ := deliveryApp(t)
	env := &remoteEnv{a: a, log: &lockedBuffer{}}
	env.fake = a2afake.New(a2afake.Options{})
	agentSrv := httptest.NewServer(env.fake)
	t.Cleanup(agentSrv.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+a2a.PushPath+"{call}", func(w http.ResponseWriter, r *http.Request) {
		a.remote.ServePush(w, r)
	})
	env.mira = httptest.NewServer(mux)
	t.Cleanup(env.mira.Close)
	base := agentSrv.URL
	if card != "" {
		base += "/" + card
	}
	sk := skills.Skill{Name: "recherche", Description: "Recherche", Body: "Recherchiere.",
		Agents: map[string]config.AgentDef{"recherche": {Name: "Hermes",
			Remote: &config.RemoteDef{URL: base, Token: a2afake.DefaultToken, A2ASkill: skill}}},
		Target: &skills.Target{Finish: skills.Finish{Name: "memory.save"}, Fields: []skills.TargetField{
			{Name: "fact", Description: "Die Übersicht", Agent: "recherche",
				Input: map[string]any{"input": "{{ request }}"}}}}}
	serverURL := ""
	if withURL {
		serverURL = env.mira.URL
	}
	logger := slog.New(slog.NewTextHandler(env.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a.remote = a2a.New(a2a.Options{Skills: []skills.Skill{sk}, ServerURL: serverURL, Runs: a.store,
		Deliver: a.remoteDelivered, CallRetry: 10 * time.Millisecond, Logger: logger})
	a.remote.Start(context.Background())
	t.Cleanup(a.remote.Close)
	a.threads = threads.New(threads.Options{Lang: i18n.DE, Store: a.store, Targets: []skills.Skill{sk},
		Finishers: map[string]threads.Finisher{"memory.save": threads.MemorySave(a.store, i18n.DE)},
		Dispatch:  a.dispatchForThread, RemoteFailure: a2a.Failure, AgentWait: 50 * time.Millisecond,
		Cancel: func(id string) bool { return a.agents.Cancel(id) || a.remote.Cancel(id) },
		Deliver: func(_ context.Context, n store.Message, _ string) error {
			env.mu.Lock()
			defer env.mu.Unlock()
			env.told = append(env.told, n)
			return nil
		}, Logger: logger})
	return env
}

// start starts the skill's thread from a chat; its id comes back.
func (e *remoteEnv) start(t *testing.T, request string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := e.a.store.UpsertSession(ctx, store.Session{Key: "telegram:1", Channel: string(config.ChannelTelegram)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	if _, err := e.a.threads.Start(ctx, inv, "recherche", request); err != nil {
		t.Fatal(err)
	}
	sess, err := e.a.store.GetSession(ctx, "telegram:1")
	if err != nil || sess.ThreadID == "" {
		t.Fatalf("no thread: %+v %v", sess, err)
	}
	return sess.ThreadID
}

// waitFor waits until cond holds, up to 5 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; !cond(); i++ {
		if i > 500 {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// fact is the fact the thread saved, "" while none.
func (e *remoteEnv) fact(t *testing.T) string {
	facts, err := e.a.store.AllFacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 {
		return ""
	}
	return facts[0].Text
}

func (e *remoteEnv) received(kind string) []a2afake.Received {
	var out []a2afake.Received
	for _, r := range e.fake.Received() {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func (e *remoteEnv) messages() []store.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]store.Message(nil), e.told...)
}

// RM-05 A field filled by a remote agent, by push: one message with a push
// configuration and a token of the call; the pushed task's artifacts fill
// the field.
func TestRM05_AFieldFilledByARemoteAgentByPush(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardPushOnly, a2afake.SkillComplete, true)
	env.start(t, "Welche Wärmepumpe passt?")
	waitFor(t, "the field", func() bool { return env.fact(t) != "" })
	if got := env.fact(t); got != a2afake.Result {
		t.Errorf("field = %q, want %q", got, a2afake.Result)
	}
	sends := env.received("SendMessage")
	if len(sends) != 1 {
		t.Fatalf("sends = %+v", sends)
	}
	s := sends[0]
	if !strings.HasPrefix(s.PushURL, env.mira.URL+a2a.PushPath) || len(s.PushToken) < 32 {
		t.Errorf("push configuration %q %q", s.PushURL, s.PushToken)
	}
	if s.Text != "Welche Wärmepumpe passt?" || s.Skill != a2afake.SkillComplete {
		t.Errorf("message %+v", s)
	}
	if len(env.received("push")) == 0 {
		t.Error("the fake pushed nothing")
	}
	if len(env.received("SendStreamingMessage")) != 0 {
		t.Error("a push agent was streamed")
	}
}

// RM-21 The stream first: a card with push and streaming, server.url set:
// Mira reads the stream and sends no push configuration.
func TestRM21_TheStreamFirst(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardFull, a2afake.SkillComplete, true)
	env.start(t, "Welche Wärmepumpe passt?")
	waitFor(t, "the field", func() bool { return env.fact(t) != "" })
	streams := env.received("SendStreamingMessage")
	if len(streams) != 1 || streams[0].PushURL != "" || streams[0].PushToken != "" {
		t.Errorf("streams = %+v", streams)
	}
	if n := len(env.received("SendMessage")); n != 0 {
		t.Errorf("%d sends besides the stream", n)
	}
}

// RM-06 A field filled by a stream: an agent with streaming and no push;
// Mira reads the stream until the task is completed.
func TestRM06_AFieldFilledByAStream(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillSlow, false)
	id := env.start(t, "Welche Wärmepumpe passt?")
	waitFor(t, "the task working", func() bool {
		return len(env.received("SendStreamingMessage")) == 1 && strings.Contains(env.log.String(), "remote agent called")
	})
	th, err := env.a.store.GetThread(context.Background(), id)
	if err != nil || th.Calls["fact"] == "" {
		t.Fatalf("the field is not being filled: %+v %v", th.Calls, err)
	}
	time.Sleep(100 * time.Millisecond)
	env.fake.Release()
	waitFor(t, "the field", func() bool { return env.fact(t) != "" })
	if got := env.fact(t); got != a2afake.Result {
		t.Errorf("field = %q", got)
	}
}

// RM-07 The remote agent fails: the log names the agent, the task and the
// status text, and the thread asks its owner for that field.
func TestRM07_TheRemoteAgentFails(t *testing.T) {
	for _, c := range []struct{ card, skill, text string }{
		{a2afake.CardStreamOnly, a2afake.SkillFail, a2afake.FailText},
		{a2afake.CardPushOnly, a2afake.SkillFail, a2afake.FailText},
		{a2afake.CardStreamOnly, a2afake.SkillReject, a2afake.RejectText},
	} {
		t.Run(c.card+"/"+c.skill, func(t *testing.T) {
			env := newRemoteEnv(t, c.card, c.skill, true)
			env.start(t, "Welche Wärmepumpe passt?")
			waitFor(t, "the question", func() bool { return len(env.messages()) == 1 })
			say := env.messages()[0].Spoken
			if !strings.HasPrefix(say, "Hermes konnte das nicht: "+c.text) || !strings.Contains(say, "Die Übersicht") {
				t.Errorf("said %q", say)
			}
			log := env.log.String()
			if !strings.Contains(log, "remote agent failed") || !strings.Contains(log, "agent=recherche") ||
				!strings.Contains(log, "task_id=") || !strings.Contains(log, c.text) {
				t.Errorf("log:\n%s", log)
			}
			if env.fact(t) != "" {
				t.Error("a failed agent filled the field")
			}
		})
	}
}

// RM-08 A push with a wrong token: 401, the log names the call and the
// sender, the thread does not change.
func TestRM08_APushWithAWrongToken(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardPushOnly, a2afake.SkillSlow, true)
	id := env.start(t, "Welche Wärmepumpe passt?")
	waitFor(t, "the send", func() bool { return len(env.received("SendMessage")) == 1 })
	callID := strings.TrimPrefix(env.received("SendMessage")[0].PushURL, env.mira.URL+a2a.PushPath)
	body := `{"statusUpdate":{"taskId":"x","contextId":"y","status":{"state":"TASK_STATE_COMPLETED"}}}`
	for _, c := range []struct{ call, header, value string }{
		{callID, "A2A-Notification-Token", "wrong"},
		{callID, "Authorization", "Bearer wrong"},
		{callID, "", ""},
		{"unknown", "A2A-Notification-Token", "wrong"},
	} {
		req, _ := http.NewRequest(http.MethodPost, env.mira.URL+a2a.PushPath+c.call, strings.NewReader(body))
		if c.header != "" {
			req.Header.Set(c.header, c.value)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%+v: %d", c, resp.StatusCode)
		}
	}
	log := env.log.String()
	if !strings.Contains(log, "push refused: wrong token") || !strings.Contains(log, "call_id="+callID) ||
		!strings.Contains(log, "from=127.0.0.1") || !strings.Contains(log, "push refused: no such call") {
		t.Errorf("log:\n%s", log)
	}
	th, _ := env.a.store.GetThread(context.Background(), id)
	if th.Calls["fact"] != callID || env.fact(t) != "" {
		t.Errorf("the thread changed: %+v", th)
	}
	// The right token in either header is taken; a body that is no event
	// is 400.
	for _, c := range []struct {
		header, value, body string
		want                int
	}{
		{"Authorization", "Bearer " + env.received("SendMessage")[0].PushToken, "{not json", http.StatusBadRequest},
		{"A2A-Notification-Token", env.received("SendMessage")[0].PushToken, "{}", http.StatusBadRequest},
	} {
		req, _ := http.NewRequest(http.MethodPost, env.mira.URL+a2a.PushPath+callID, strings.NewReader(c.body))
		req.Header.Set(c.header, c.value)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%+v: %d", c, resp.StatusCode)
		}
	}
	if !strings.Contains(env.log.String(), "push not readable") {
		t.Errorf("log:\n%s", env.log.String())
	}
	env.fake.Release()
	waitFor(t, "the field", func() bool { return env.fact(t) != "" })
}

// RM-09 A push twice: the same completed task pushed twice fills the field
// once.
func TestRM09_APushTwice(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardPushOnly, a2afake.SkillPushTwice, true)
	id := env.start(t, "Welche Wärmepumpe passt?")
	waitFor(t, "the field", func() bool { return env.fact(t) != "" })
	waitFor(t, "every push", func() bool { return len(env.received("push")) == 6 })
	time.Sleep(100 * time.Millisecond)
	facts, _ := env.a.store.AllFacts(context.Background())
	if len(facts) != 1 || facts[0].Text != a2afake.Result {
		t.Errorf("facts %+v", facts)
	}
	th, _ := env.a.store.GetThread(context.Background(), id)
	filled := 0
	for _, e := range th.Log {
		if strings.HasPrefix(e.Text, "Agent recherche: fact") {
			filled++
		}
	}
	if filled != 1 {
		t.Errorf("filled %d times: %+v", filled, th.Log)
	}
	// A push for the call that has ended: 200, nothing done.
	push := env.received("SendMessage")[0]
	req, _ := http.NewRequest(http.MethodPost, push.PushURL, strings.NewReader(`{"statusUpdate":{"taskId":"x","status":{"state":"TASK_STATE_FAILED"}}}`))
	req.Header.Set("A2A-Notification-Token", push.PushToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(env.log.String(), "push for a remote agent's call that has ended") {
		t.Errorf("ended call: %d\n%s", resp.StatusCode, env.log.String())
	}
}

// RM-10 The thread is cancelled: Mira sends CancelTask for the task.
func TestRM10_TheThreadIsCancelled(t *testing.T) {
	for _, card := range []string{a2afake.CardStreamOnly, a2afake.CardPushOnly} {
		t.Run(card, func(t *testing.T) {
			env := newRemoteEnv(t, card, a2afake.SkillSlow, true)
			env.start(t, "Welche Wärmepumpe passt?")
			waitFor(t, "the task", func() bool {
				return strings.Contains(env.log.String(), "TASK_STATE_WORKING") ||
					len(env.received("SendMessage"))+len(env.received("SendStreamingMessage")) == 1
			})
			// The task id is known once the agent answered the send.
			waitFor(t, "the task id", func() bool {
				runs, _ := env.a.store.RunningAgentRuns(context.Background())
				return len(runs) == 1 && strings.Contains(string(runs[0].Remote), "task_id")
			})
			inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
			if _, err := env.a.threads.Cancel(context.Background(), inv); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the cancel", func() bool { return len(env.received("cancel")) == 1 })
			runs, _ := env.a.store.RunningAgentRuns(context.Background())
			if len(runs) != 0 {
				t.Errorf("still running: %+v", runs)
			}
			if !strings.Contains(env.log.String(), "remote agent's task cancelled") {
				t.Errorf("log:\n%s", env.log.String())
			}
		})
	}
}

// RM-11 Login asked during the work: the call ends as failed with the
// remote agent's status text, and the thread asks its owner.
func TestRM11_LoginAskedDuringTheWork(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillLogin, false)
	env.start(t, "Welche Wärmepumpe passt?")
	waitFor(t, "the question", func() bool { return len(env.messages()) == 1 })
	if say := env.messages()[0].Spoken; !strings.HasPrefix(say, "Hermes konnte das nicht: "+a2afake.LoginText) {
		t.Errorf("said %q", say)
	}
	if !strings.Contains(env.log.String(), "needs a login: "+a2afake.LoginText) {
		t.Errorf("log:\n%s", env.log.String())
	}
}
