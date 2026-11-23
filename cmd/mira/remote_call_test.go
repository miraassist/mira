package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/a2a"
	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/logcap"
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
	log  *logcap.Lines
	mira *httptest.Server
	// clk is what the threads' waits and the calls' attempts wait on.
	clk *clock.Fake
	// toldN signals the number of messages told so far ("1", "2", …).
	toldN signals

	mu   sync.Mutex
	told []store.Message
	// host is the service's remote agents, for the push route.
	host *a2a.Host
	// runs is the store as the remote agents use it, telling the test each
	// state of a call it writes; finished tells each finished thread.
	runs     *signalRuns
	finished *signals
}

// signals are what happened, for a test to wait on instead of a time
// (docs: no timing in tests): await is closed once something matching was
// added, at once when it already was.
type signals struct {
	mu      sync.Mutex
	seen    []string
	waiters []sigWaiter
}

type sigWaiter struct {
	match func(string) bool
	done  chan struct{}
}

func (s *signals) add(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, v)
	kept := s.waiters[:0]
	for _, w := range s.waiters {
		if w.match(v) {
			close(w.done)
			continue
		}
		kept = append(kept, w)
	}
	s.waiters = kept
}

func (s *signals) await(match func(string) bool) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(chan struct{})
	for _, v := range s.seen {
		if match(v) {
			close(done)
			return done
		}
	}
	s.waiters = append(s.waiters, sigWaiter{match: match, done: done})
	return done
}

// signalRuns is the store for the remote agents, adding each state of a
// call it writes to its signals.
type signalRuns struct {
	a2a.Runs
	saved signals
	ended signals
}

func (r *signalRuns) SetAgentRunRemote(ctx context.Context, callID string, remote json.RawMessage) error {
	err := r.Runs.SetAgentRunRemote(ctx, callID, remote)
	r.saved.add(string(remote))
	return err
}

// EndAgentRun tells when a call ended (its call id).
func (r *signalRuns) EndAgentRun(ctx context.Context, callID, status, output, errText string, done bool, usage store.AgentUsage) error {
	err := r.Runs.EndAgentRun(ctx, callID, status, output, errText, done, usage)
	r.ended.add(callID)
	return err
}

// signalFinisher tells when a thread's finish has worked.
type signalFinisher struct {
	threads.Finisher
	done *signals
}

func (f signalFinisher) Work(ctx context.Context, th store.Thread) (threads.Result, error) {
	res, err := f.Finisher.Work(ctx, th)
	f.done.add(th.ID)
	return res, err
}

func (e *remoteEnv) setHost(h *a2a.Host) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.host = h
}

func (e *remoteEnv) currentHost() *a2a.Host {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.host
}

// newRemoteEnv: card is the fake's card (a2afake.CardPushOnly, …), skill
// the behaviour (a2afake.SkillComplete, …); withURL sets server.url.
func newRemoteEnv(t *testing.T, card, skill string, withURL bool) *remoteEnv {
	return newRemoteEnvBefore(t, card, skill, withURL, nil)
}

// newRemoteEnvBefore is newRemoteEnv with a handler before the fake.
func newRemoteEnvBefore(t *testing.T, card, skill string, withURL bool, before func(http.Handler) http.Handler) *remoteEnv {
	return newRemoteEnvWait(t, card, skill, withURL, before, 50*time.Millisecond)
}

// newRemoteEnvWait is newRemoteEnvBefore with how long a voice turn waits
// for the thread's agents.
func newRemoteEnvWait(t *testing.T, card, skill string, withURL bool, before func(http.Handler) http.Handler, wait time.Duration) *remoteEnv {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	a, _, _ := deliveryAppOn(t, clk.Now)
	logger, lines := logcap.Logger()
	env := &remoteEnv{a: a, log: lines, runs: &signalRuns{Runs: a.store}, finished: &signals{}, clk: clk}
	env.fake = a2afake.New(a2afake.Options{})
	var agent http.Handler = env.fake
	if before != nil {
		agent = before(env.fake)
	}
	agentSrv := httptest.NewServer(agent)
	t.Cleanup(agentSrv.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+a2a.PushPath+"{call}", func(w http.ResponseWriter, r *http.Request) {
		env.currentHost().ServePush(w, r)
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
	a.remote = a2a.New(a2a.Options{Skills: []skills.Skill{sk}, ServerURL: serverURL, Runs: env.runs,
		Deliver: a.remoteDelivered, Ask: a.remoteAsked, CallRetry: 10 * time.Millisecond, Logger: logger, Clock: env.clk})
	a.remote.Start(context.Background())
	t.Cleanup(a.remote.Close)
	env.setHost(a.remote)
	a.threads = threads.New(threads.Options{Lang: i18n.DE, Store: a.store, Targets: []skills.Skill{sk},
		Finishers: map[string]threads.Finisher{"memory.save": signalFinisher{threads.MemorySave(a.store, i18n.DE), env.finished}},
		Dispatch:  a.dispatchForThread, RemoteFailure: a2a.Failure, AgentWait: wait, Clock: env.clk,
		Asking: a.remote.Asking, Answer: a.remote.Answer,
		Cancel: func(id string) bool { return a.agents.Cancel(id) || a.remote.Cancel(id) },
		Deliver: func(ctx context.Context, n store.Message, _ string) error {
			_, _, err := a.store.PutMessage(ctx, n)
			env.mu.Lock()
			env.told = append(env.told, n)
			count := len(env.told)
			env.mu.Unlock()
			env.toldN.add(fmt.Sprint(count))
			return err
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
	var err error
	e.clk.Drive(func() { _, err = e.a.threads.Start(ctx, inv, "recherche", request) }) // the turn's wait runs out
	if err != nil {
		t.Fatal(err)
	}
	sess, err := e.a.store.GetSession(ctx, "telegram:1")
	if err != nil || sess.ThreadID == "" {
		t.Fatalf("no thread: %+v %v", sess, err)
	}
	return sess.ThreadID
}

// filled waits until the thread's finish worked: the field is filled.
func (e *remoteEnv) filled() { <-e.finished.await(func(string) bool { return true }) }

// toldAt waits until n messages were told.
func (e *remoteEnv) toldAt(n int) {
	want := fmt.Sprint(n)
	<-e.toldN.await(func(v string) bool { return v == want })
}

// got waits until the fake received n things of a kind.
func (e *remoteEnv) got(kind string, n int) {
	seen := 0
	<-e.fake.Await(func(r a2afake.Received) bool {
		if r.Kind == kind {
			seen++
		}
		return seen >= n
	})
}

// taskKnown waits until the call's state holds the task's id.
func (e *remoteEnv) taskKnown() {
	<-e.runs.saved.await(func(v string) bool { return strings.Contains(v, "task_id") })
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
	env.filled()
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
	env.filled()
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
	env.got("SendStreamingMessage", 1)
	<-env.log.Await("remote agent called")
	th, err := env.a.store.GetThread(context.Background(), id)
	if err != nil || th.Calls["fact"] == "" {
		t.Fatalf("the field is not being filled: %+v %v", th.Calls, err)
	}
	env.fake.Release()
	env.filled()
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
			env.toldAt(1)
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
	env.got("SendMessage", 1)
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
	env.filled()
}

// RM-09 A push twice: the same completed task pushed twice fills the field
// once.
func TestRM09_APushTwice(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardPushOnly, a2afake.SkillPushTwice, true)
	id := env.start(t, "Welche Wärmepumpe passt?")
	env.filled()
	env.got("pushed", 6) // Mira answered every push
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
			// The task id is known once the agent answered the send.
			env.taskKnown()
			inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
			if _, err := env.a.threads.Cancel(context.Background(), inv); err != nil {
				t.Fatal(err)
			}
			env.got("cancel", 1)
			runs, _ := env.a.store.RunningAgentRuns(context.Background())
			if len(runs) != 0 {
				t.Errorf("still running: %+v", runs)
			}
			// Mira logs the cancel once the agent answered it, after the
			// fake recorded it: wait for her line, not for the fake's.
			<-env.log.Await("remote agent's task cancelled")
		})
	}
}

// RM-11 Login asked during the work: the call ends as failed with the
// remote agent's status text, and the thread asks its owner.
func TestRM11_LoginAskedDuringTheWork(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillLogin, false)
	env.start(t, "Welche Wärmepumpe passt?")
	env.toldAt(1)
	if say := env.messages()[0].Spoken; !strings.HasPrefix(say, "Hermes konnte das nicht: "+a2afake.LoginText) {
		t.Errorf("said %q", say)
	}
	if !strings.Contains(env.log.String(), "needs a login: "+a2afake.LoginText) {
		t.Errorf("log:\n%s", env.log.String())
	}
}

// RM-12 An agent request: the remote agent's question goes to the thread's
// owner as a message of the kind agent_request naming the agent; the
// answer goes back with the same task id and the agent goes on.
func TestRM12_AnAgentRequest(t *testing.T) {
	for _, card := range []string{a2afake.CardStreamOnly, a2afake.CardPushOnly} {
		t.Run(card, func(t *testing.T) {
			env := newRemoteEnv(t, card, a2afake.SkillAsk, true)
			ctx := context.Background()
			id := env.start(t, "Welche Wärmepumpe passt?")
			env.toldAt(1)
			n := env.messages()[0]
			if n.Kind != store.KindAgentRequest || n.ThreadID != id || n.Spoken != "Hermes fragt: "+a2afake.Question {
				t.Fatalf("message %+v", n)
			}
			// The thread shows the model the question and the tool.
			inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
			shown, err := env.a.threads.Take(ctx, inv, id)
			if err != nil || !strings.Contains(shown, "Hermes fragt: „"+a2afake.Question+"“") ||
				!strings.Contains(shown, "agent_answer") {
				t.Errorf("thread shown:\n%s %v", shown, err)
			}
			said, err := env.a.threads.AgentAnswer(ctx, inv, "", "140 Quadratmeter")
			if err != nil || said != "Antwort an Hermes weitergegeben." {
				t.Fatalf("answer: %q %v", said, err)
			}
			env.filled()
			if got := env.fact(t); got != a2afake.AnswerResult+"140 Quadratmeter." {
				t.Errorf("field %q", got)
			}
			var first, answer a2afake.Received
			for _, r := range env.fake.Received() {
				if r.Kind != "SendMessage" && r.Kind != "SendStreamingMessage" {
					continue
				}
				if r.Text == "140 Quadratmeter" {
					answer = r
				} else {
					first = r
				}
			}
			runs, _ := env.a.store.RecentAgentRuns(ctx, env.clk.Now().Add(-time.Hour))
			if answer.TaskID == "" || answer.ContextID == "" || len(runs) != 1 || !strings.Contains(string(runs[0].Remote), answer.TaskID) ||
				answer.MessageID == first.MessageID {
				t.Errorf("answer %+v, first %+v, runs %+v", answer, first, runs)
			}
			if m, err := env.a.store.GetMessage(ctx, n.ID); err != nil || m.State != store.MessageAcked {
				t.Errorf("the agent request is not done: %+v %v", m, err)
			}
		})
	}
}

// RM-26 The agent's result comes by push before the answer to Mira's message
// (docs/spec/remote-agents.md, decision 64): the fake holds the HTTP answer to
// the owner's answer until the push of the result has ended the call; the
// send is not cut, and Mira says the answer was passed on.
func TestRM26_TheAgentsResultComesByPushBeforeTheAnswerToMirasMessage(t *testing.T) {
	var env *remoteEnv
	holdAnswer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if !strings.Contains(string(body), "140 Quadratmeter") {
				next.ServeHTTP(w, r)
				return
			}
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, r)
			// The push of the result ends the call before the answer is back.
			<-env.runs.ended.await(func(string) bool { return true })
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
		})
	}
	env = newRemoteEnvBefore(t, a2afake.CardPushOnly, a2afake.SkillAsk, true, holdAnswer)
	ctx := context.Background()
	id := env.start(t, "Welche Wärmepumpe passt?")
	env.toldAt(1)
	inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	if _, err := env.a.threads.Take(ctx, inv, id); err != nil {
		t.Fatal(err)
	}
	said, err := env.a.threads.AgentAnswer(ctx, inv, "", "140 Quadratmeter")
	if err != nil || said != "Antwort an Hermes weitergegeben." {
		t.Fatalf("answer: %q %v", said, err)
	}
	env.filled()
	if got := env.fact(t); got != a2afake.AnswerResult+"140 Quadratmeter." {
		t.Errorf("field %q", got)
	}
}

// RM-13 The answer cannot be sent: Mira says she could not pass it on, the
// agent request stays and the log names the agent and the refusal.
func TestRM13_TheAnswerCannotBeSent(t *testing.T) {
	refuse := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "140 Quadratmeter") {
				http.Error(w, "Antworten nehme ich nicht an.", http.StatusForbidden)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(w, r)
		})
	}
	env := newRemoteEnvBefore(t, a2afake.CardStreamOnly, a2afake.SkillAsk, false, refuse)
	ctx := context.Background()
	env.start(t, "Welche Wärmepumpe passt?")
	env.toldAt(1)
	inv := tools.Invocation{SessionKey: "telegram:1", Channel: config.ChannelTelegram}
	_, err := env.a.threads.AgentAnswer(ctx, inv, "", "140 Quadratmeter")
	if err == nil || !strings.HasPrefix(err.Error(), "Ich konnte die Antwort nicht weitergeben: ") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
	if m, err := env.a.store.GetMessage(ctx, env.messages()[0].ID); err != nil || m.State == store.MessageAcked {
		t.Errorf("the agent request did not stay: %+v %v", m, err)
	}
	log := env.log.String()
	if !strings.Contains(log, "the answer did not reach the remote agent") || !strings.Contains(log, "agent=recherche") ||
		!strings.Contains(log, "Antworten nehme ich nicht an") {
		t.Errorf("log:\n%s", log)
	}
	if label, q, ok := env.a.remote.Asking(func() string {
		runs, _ := env.a.store.RunningAgentRuns(ctx)
		return runs[0].CallID
	}()); !ok || label != "Hermes" || q != a2afake.Question {
		t.Errorf("the question is not open any more: %q %q %v", label, q, ok)
	}
}

// restart stops the remote agents as a stopping service does and starts
// them anew on the same store; meanwhile, while is run with no service.
func (e *remoteEnv) restart(t *testing.T, while func()) {
	t.Helper()
	old := e.a.remote
	e.setHost(nil)
	old.Close()
	while()
	logger := slog.New(slog.NewTextHandler(e.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.a.remote = a2a.New(a2a.Options{Skills: old.Skills(), ServerURL: old.ServerURL(), Runs: e.runs,
		Deliver: e.a.remoteDelivered, Ask: e.a.remoteAsked, CallRetry: 10 * time.Millisecond, Logger: logger, Clock: e.clk})
	e.a.remote.Start(context.Background())
	t.Cleanup(e.a.remote.Close)
	e.setHost(e.a.remote)
	e.a.remote.Resume(context.Background())
}

// RM-14 A restart during the work: the remote agent completes the task
// while the service is down; up again, Mira asks the task's state once and
// the field holds the artifacts' text.
func TestRM14_ARestartDuringTheWork(t *testing.T) {
	for _, card := range []string{a2afake.CardStreamOnly, a2afake.CardPushOnly} {
		t.Run(card, func(t *testing.T) {
			env := newRemoteEnv(t, card, a2afake.SkillSlow, true)
			env.start(t, "Welche Wärmepumpe passt?")
			// Signals, no times: the call's state "working" written,
			// the fake's task stored completed while the service is down,
			// the thread finished after the restart.
			<-env.runs.saved.await(func(v string) bool { return strings.Contains(v, "TASK_STATE_WORKING") })
			env.restart(t, func() {
				env.fake.Release()
				// GetTask answers "completed" from now on.
				<-env.fake.Await(func(r a2afake.Received) bool { return r.Kind == "stored" })
				if card == a2afake.CardPushOnly {
					// Its push of the end goes out while no service is there.
					<-env.fake.Await(func(r a2afake.Received) bool { return r.Kind == "push" && r.State == "TASK_STATE_COMPLETED" })
				}
			})
			<-env.finished.await(func(string) bool { return true })
			if got := env.fact(t); got != a2afake.Result {
				t.Errorf("field %q", got)
			}
			if n := len(env.received("GetTask")); n != 1 {
				t.Errorf("GetTask %d times", n)
			}
			if !strings.Contains(env.log.String(), "remote agent's call taken up after the restart") {
				t.Errorf("log:\n%s", env.log.String())
			}
		})
	}
}

// A task the remote agent no longer knows after the restart (Hermes keeps
// its tasks in memory) is a failed agent: the thread asks its owner.
func TestRemoteTaskNotFoundAfterTheRestart(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillSlow, false)
	ctx := context.Background()
	env.start(t, "Welche Wärmepumpe passt?")
	env.taskKnown()
	env.restart(t, func() {
		runs, _ := env.a.store.RunningAgentRuns(ctx)
		var st map[string]any
		_ = json.Unmarshal(runs[0].Remote, &st)
		st["task_id"] = "gone"
		raw, _ := json.Marshal(st)
		if err := env.a.store.SetAgentRunRemote(ctx, runs[0].CallID, raw); err != nil {
			t.Fatal(err)
		}
	})
	env.toldAt(1)
	if say := env.messages()[0].Spoken; !strings.HasPrefix(say, "Hermes konnte das nicht.") {
		t.Errorf("said %q", say)
	}
	if !strings.Contains(env.log.String(), "it no longer knows the task after Mira's restart") {
		t.Errorf("log:\n%s", env.log.String())
	}
	env.fake.Release()
}

// RM-15 mira.agent starts the skill's thread: for a person, with the input
// as the request; the response holds the thread's id.
func TestRM15_MiraAgentStartsTheSkillsThread(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillComplete, false)
	ctx := context.Background()
	env.a.targetSkills = env.a.remote.Skills()
	id, err := env.a.startSkill(ctx, "recherche", "Welche Wärmepumpe passt?", "anna")
	if err != nil {
		t.Fatal(err)
	}
	th, err := env.a.store.GetThread(ctx, id)
	if err != nil || th.Place != "person:anna" || th.Person != "anna" || th.Request != "Welche Wärmepumpe passt?" {
		t.Fatalf("thread %+v %v", th, err)
	}
	env.filled()
	if s := env.received("SendStreamingMessage"); len(s) != 1 || s[0].Text != "Welche Wärmepumpe passt?" {
		t.Errorf("sent %+v", s)
	}
	// Over the service's API, as the integration calls it.
	srv := haconv.New(haconv.Options{StartSkill: env.a.startSkill})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/agent/skill",
		strings.NewReader(`{"skill":"recherche","input":{"input":"Und für die Garage?"},"receiver":"anna"}`)))
	var out struct {
		ThreadID string `json:"thread_id"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.ThreadID == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if th, err := env.a.store.GetThread(ctx, out.ThreadID); err != nil || th.Request != "Und für die Garage?" {
		t.Errorf("thread %+v %v", th, err)
	}
	// A receiver Mira does not know is refused, naming it.
	if _, err := env.a.startSkill(ctx, "recherche", "x", "niemand"); err == nil || !strings.Contains(err.Error(), `"niemand"`) {
		t.Errorf("unknown receiver: %v", err)
	}
}

// RM-16 mira.agent with a skill without a target fails naming the reason.
func TestRM16_MiraAgentWithASkillWithoutATarget(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillComplete, false)
	env.a.skills = &skills.Set{Skills: []skills.Skill{{Name: "wetter", Description: "Wetter"}}}
	srv := haconv.New(haconv.Options{StartSkill: env.a.startSkill})
	for _, c := range []struct{ body, want string }{
		{`{"skill":"wetter","input":"Wie wird das Wetter?","receiver":"anna"}`, "has no target"},
		{`{"skill":"gibtsnicht","input":"x","receiver":"anna"}`, `no skill \"gibtsnicht\"`},
		{`{"skill":"wetter","input":{},"receiver":"anna"}`, "input must be the request"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/agent/skill", strings.NewReader(c.body)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.body, rec.Code, rec.Body)
		}
	}
}

// A question that comes while the voice turn waits is said in that turn,
// the satellite listens again, and agent_answer is refused within it: the
// user has not heard the question yet (decisions 41, 51).
func TestRemoteQuestionInTheWaitingTurn(t *testing.T) {
	env := newRemoteEnv(t, a2afake.CardStreamOnly, a2afake.SkillAsk, false)
	ctx := context.Background()
	if _, err := env.a.store.UpsertSession(ctx, store.Session{Key: "voice:1", Channel: string(config.ChannelVoice)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	inv := tools.Invocation{SessionKey: "voice:1", Channel: config.ChannelVoice, Person: "anna"}
	ts := &tools.TurnState{SessionKey: "voice:1", Channel: config.ChannelVoice}
	turn := tools.WithTurn(ctx, ts)
	// The fake asks at once; the turn waits up to AgentWait for it.
	out, err := env.a.threads.Start(turn, inv, "recherche", "Welche Wärmepumpe passt?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Hermes fragt: "+a2afake.Question) || !ts.WaitsForUser() {
		t.Fatalf("waits %v, said:\n%s", ts.WaitsForUser(), out)
	}
	if n := len(env.messages()); n != 0 {
		t.Errorf("%d messages for a question said in the turn", n)
	}
	if _, err := env.a.threads.AgentAnswer(turn, inv, "", "150 Quadratmeter"); err == nil ||
		!strings.Contains(err.Error(), "gerade erst gefragt") {
		t.Errorf("answered within the asking turn: %v", err)
	}
	// The next turn carries the user's answer.
	next := tools.WithTurn(ctx, &tools.TurnState{SessionKey: "voice:1", Channel: config.ChannelVoice})
	if _, err := env.a.threads.AgentAnswer(next, inv, "", "140 Quadratmeter"); err != nil {
		t.Fatal(err)
	}
	env.filled()
}

// RM-22 agent_answer at a satellite waits for the remote agent: what comes
// within the wait (the result and the finish's answer, or a second question)
// is said in the same answer, and no message comes (decision 52).
func TestRM22_AgentAnswerAtASatelliteWaitsForTheRemoteAgent(t *testing.T) {
	// voiceTurn starts the skill's thread at a satellite; the fake asks at
	// once, within the turn.
	voiceTurn := func(t *testing.T, env *remoteEnv) tools.Invocation {
		t.Helper()
		ctx := context.Background()
		if _, err := env.a.store.UpsertSession(ctx, store.Session{Key: "voice:1", Channel: string(config.ChannelVoice)}, time.Hour); err != nil {
			t.Fatal(err)
		}
		inv := tools.Invocation{SessionKey: "voice:1", Channel: config.ChannelVoice, Person: "anna"}
		out, err := env.a.threads.Start(tools.WithTurn(ctx, &tools.TurnState{SessionKey: "voice:1", Channel: config.ChannelVoice}),
			inv, "recherche", "Welche Wärmepumpe passt?")
		if err != nil || !strings.Contains(out, "Hermes fragt: "+a2afake.Question) {
			t.Fatalf("start: %v\n%s", err, out)
		}
		return inv
	}
	t.Run("the result", func(t *testing.T) {
		env := newRemoteEnvWait(t, a2afake.CardStreamOnly, a2afake.SkillAsk, false, nil, 5*time.Second)
		inv := voiceTurn(t, env)
		ts := &tools.TurnState{SessionKey: "voice:1", Channel: config.ChannelVoice}
		out, err := env.a.threads.AgentAnswer(tools.WithTurn(context.Background(), ts), inv, "", "140 Quadratmeter")
		if err != nil {
			t.Fatal(err)
		}
		result := a2afake.AnswerResult + "140 Quadratmeter."
		if !strings.HasPrefix(out, "Antwort an Hermes weitergegeben.") || !strings.Contains(out, result) {
			t.Errorf("said:\n%s", out)
		}
		if got := env.fact(t); got != result {
			t.Errorf("field %q", got)
		}
		if n := env.messages(); len(n) != 0 {
			t.Errorf("messages for a result said in the turn: %+v", n)
		}
		if ts.WaitsForUser() {
			t.Error("the satellite listens again after a result")
		}
	})
	t.Run("a second question", func(t *testing.T) {
		env := newRemoteEnvWait(t, a2afake.CardStreamOnly, a2afake.SkillAskTwice, false, nil, 5*time.Second)
		inv := voiceTurn(t, env)
		ts := &tools.TurnState{SessionKey: "voice:1", Channel: config.ChannelVoice}
		turn := tools.WithTurn(context.Background(), ts)
		out, err := env.a.threads.AgentAnswer(turn, inv, "", "140 Quadratmeter")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out, "Antwort an Hermes weitergegeben.") || !strings.Contains(out, "Hermes fragt: "+a2afake.Question2) ||
			!ts.WaitsForUser() {
			t.Fatalf("waits %v, said:\n%s", ts.WaitsForUser(), out)
		}
		if n := env.messages(); len(n) != 0 {
			t.Errorf("messages for a question said in the turn: %+v", n)
		}
		// Not answered in the turn that heard the question first.
		if _, err := env.a.threads.AgentAnswer(turn, inv, "", "1925"); err == nil || !strings.Contains(err.Error(), "gerade erst gefragt") {
			t.Errorf("answered within the asking turn: %v", err)
		}
		next := tools.WithTurn(context.Background(), &tools.TurnState{SessionKey: "voice:1", Channel: config.ChannelVoice})
		out, err = env.a.threads.AgentAnswer(next, inv, "", "1925")
		if err != nil || !strings.Contains(out, a2afake.AnswerResult+"1925.") {
			t.Fatalf("second answer: %v\n%s", err, out)
		}
	})
}
