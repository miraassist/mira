package a2afake

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

type withHeader struct {
	name, value string
}

func (h withHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	c := r.Clone(r.Context())
	c.Header.Set(h.name, h.value)
	return http.DefaultTransport.RoundTrip(c)
}

// client reads a card of the fake and makes the SDK's client for it.
func client(t *testing.T, srv *httptest.Server, card string, header, value string) (*a2aclient.Client, *a2a.AgentCard) {
	t.Helper()
	ctx := context.Background()
	base := srv.URL
	if card != "" {
		base += "/" + card
	}
	resp, err := http.Get(base + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	c, err := agentcard.DefaultCardParser(body)
	if err != nil {
		t.Fatalf("card %q: %v (%s)", card, err, body)
	}
	hc := &http.Client{Transport: withHeader{header, value}}
	cl, err := a2aclient.NewFromCard(ctx, c, a2aclient.WithJSONRPCTransport(hc))
	if err != nil {
		t.Fatal(err)
	}
	return cl, c
}

func message(skill, text string) *a2a.Message {
	m := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))
	m.SetMeta(SkillMeta, skill)
	return m
}

func sendTask(t *testing.T, cl *a2aclient.Client, req *a2a.SendMessageRequest) *a2a.Task {
	t.Helper()
	res, err := cl.SendMessage(context.Background(), req)
	return taskOf(t, res, err)
}

func taskOf(t *testing.T, res a2a.SendMessageResult, err error) *a2a.Task {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	task, ok := res.(*a2a.Task)
	if !ok {
		t.Fatalf("answer is %T, want a task", res)
	}
	return task
}

func artifactText(task *a2a.Task) string {
	var out []string
	for _, a := range task.Artifacts {
		out = append(out, textOf(a.Parts))
	}
	return strings.Join(out, "\n")
}

func TestFakeBehaviours(t *testing.T) {
	f := New(Options{})
	srv := httptest.NewServer(f)
	defer srv.Close()
	cl, card := client(t, srv, CardFull, "Authorization", "Bearer "+DefaultToken)
	if !card.Capabilities.PushNotifications || !card.Capabilities.Streaming || len(card.Skills) != len(Skills) {
		t.Fatalf("card = %+v", card)
	}
	ctx := context.Background()
	send := func(m *a2a.Message) *a2a.Task {
		t.Helper()
		res, err := cl.SendMessage(ctx, &a2a.SendMessageRequest{Message: m})
		return taskOf(t, res, err)
	}

	if task := send(message(SkillComplete, "Wärmepumpe")); task.Status.State != a2a.TaskStateCompleted || artifactText(task) != Result {
		t.Errorf("complete: %s %q", task.Status.State, artifactText(task))
	}
	for skill, want := range map[string]a2a.TaskState{SkillFail: a2a.TaskStateFailed, SkillReject: a2a.TaskStateRejected,
		SkillLogin: a2a.TaskStateAuthRequired, "": a2a.TaskStateFailed} {
		if task := send(message(skill, "x")); task.Status.State != want {
			t.Errorf("%q: state %s, want %s", skill, task.Status.State, want)
		}
	}

	asked := send(message(SkillAsk, "Wärmepumpe"))
	if asked.Status.State != a2a.TaskStateInputRequired || textOf(asked.Status.Message.Parts) != Question {
		t.Fatalf("ask: %s %+v", asked.Status.State, asked.Status.Message)
	}
	answer := a2a.NewMessageForTask(a2a.MessageRoleUser, asked, a2a.NewTextPart("140 Quadratmeter"))
	if done := send(answer); done.Status.State != a2a.TaskStateCompleted || artifactText(done) != AnswerResult+"140 Quadratmeter." {
		t.Errorf("answer: %s %q", done.Status.State, artifactText(done))
	}

	res, err := cl.SendMessage(ctx, &a2a.SendMessageRequest{Message: message(SkillMessage, "x")})
	if msg, ok := res.(*a2a.Message); err != nil || !ok || textOf(msg.Parts) != Result {
		t.Errorf("message: %T %v", res, err)
	}

	// Slow: returned at once while it works, done after Release.
	slow := sendTask(t, cl, &a2a.SendMessageRequest{Message: message(SkillSlow, "x"),
		Config: &a2a.SendMessageConfig{ReturnImmediately: true}})
	if slow.Status.State.Terminal() {
		t.Fatalf("slow: %s", slow.Status.State)
	}
	f.Release()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := cl.GetTask(ctx, &a2a.GetTaskRequest{ID: slow.ID})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status.State == a2a.TaskStateCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slow did not complete after Release: %s", got.Status.State)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel a slow one.
	slow = sendTask(t, cl, &a2a.SendMessageRequest{Message: message(SkillSlow, "x"),
		Config: &a2a.SendMessageConfig{ReturnImmediately: true}})
	if got, err := cl.CancelTask(ctx, &a2a.CancelTaskRequest{ID: slow.ID}); err != nil || got.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("cancel: %v %+v", err, got)
	}
	cancelled := false
	for _, r := range f.Received() {
		cancelled = cancelled || (r.Kind == "cancel" && r.TaskID == string(slow.ID))
	}
	if !cancelled {
		t.Errorf("the cancel is not recorded: %+v", f.Received())
	}

	// The stream ends with the completed task.
	var last a2a.TaskState
	for ev, err := range cl.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: message(SkillComplete, "x")}) {
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
			last = s.Status.State
		}
	}
	if last != a2a.TaskStateCompleted {
		t.Errorf("stream ended with %s", last)
	}

	got := f.Received()
	if got[0].Kind != "card" || got[1].Kind != "SendMessage" || got[1].Skill != SkillComplete || got[1].Text != "Wärmepumpe" ||
		got[1].Auth != "Bearer "+DefaultToken {
		t.Errorf("received = %+v", got[:2])
	}
}

func TestFakeRefusesAWrongCredential(t *testing.T) {
	f := New(Options{})
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, c := range []struct{ card, header, value string }{
		{CardFull, "Authorization", "Bearer wrong"},
		{CardAPIKey, "Authorization", "Bearer " + DefaultToken},
	} {
		cl, _ := client(t, srv, c.card, c.header, c.value)
		if _, err := cl.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: message(SkillComplete, "x")}); err == nil {
			t.Errorf("%s: a wrong credential was taken", c.card)
		}
	}
	cl, _ := client(t, srv, CardAPIKey, APIKeyHeader, DefaultToken)
	if _, err := cl.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: message(SkillComplete, "x")}); err != nil {
		t.Errorf("api key: %v", err)
	}
	cl, _ = client(t, srv, CardOpen, "X-Nothing", "")
	if _, err := cl.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: message(SkillComplete, "x")}); err != nil {
		t.Errorf("open: %v", err)
	}
	refused := 0
	for _, r := range f.Received() {
		if r.Refused {
			refused++
		}
	}
	if refused != 2 {
		t.Errorf("refused %d, want 2: %+v", refused, f.Received())
	}
}

func TestFakeCards(t *testing.T) {
	f := New(Options{})
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, c := range []struct {
		card         string
		push, stream bool
	}{{CardPushOnly, true, false}, {CardStreamOnly, false, true}, {CardNeither, false, false}} {
		_, card := client(t, srv, c.card, "", "")
		if card.Capabilities.PushNotifications != c.push || card.Capabilities.Streaming != c.stream {
			t.Errorf("%s: %+v", c.card, card.Capabilities)
		}
		if want := srv.URL + "/" + c.card + "/a2a"; card.SupportedInterfaces[0].URL != want {
			t.Errorf("%s: interface %s, want %s", c.card, card.SupportedInterfaces[0].URL, want)
		}
	}
	_, card := client(t, srv, CardOAuth2, "", "")
	if _, ok := card.SecuritySchemes["login"].(a2a.OAuth2SecurityScheme); !ok {
		t.Errorf("oauth2: %+v", card.SecuritySchemes)
	}
}

func TestFakePushesTwice(t *testing.T) {
	var mu sync.Mutex
	var pushes []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sr a2a.StreamResponse
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &sr); err != nil {
			t.Errorf("push %s: %v", body, err)
		}
		mu.Lock()
		pushes = append(pushes, r.Header.Get("A2A-Notification-Token"))
		mu.Unlock()
	}))
	defer receiver.Close()
	f := New(Options{})
	srv := httptest.NewServer(f)
	defer srv.Close()
	cl, _ := client(t, srv, CardFull, "Authorization", "Bearer "+DefaultToken)
	push := &a2a.PushConfig{URL: receiver.URL + "/push", Token: "call-token"}
	for _, skill := range []string{SkillComplete, SkillPushTwice} {
		mu.Lock()
		pushes = nil
		mu.Unlock()
		sendTask(t, cl, &a2a.SendMessageRequest{Message: message(skill, "x"),
			Config: &a2a.SendMessageConfig{PushConfig: push}})
		mu.Lock()
		n := len(pushes)
		mu.Unlock()
		if n == 0 || pushes[0] != "call-token" {
			t.Fatalf("%s: pushes %v", skill, pushes)
		}
		if skill == SkillPushTwice && n%2 != 0 {
			t.Errorf("push twice sent %d pushes", n)
		}
		t.Logf("%s: %d pushes", skill, n)
	}
}
