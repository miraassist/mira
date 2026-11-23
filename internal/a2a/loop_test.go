package a2a

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/miraassist/mira/internal/a2a/transport"
	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// fakeWire stands for a call's transport: it records what the loop asks
// and signals it.
type fakeWire struct {
	mu     sync.Mutex
	sent   []string
	closed bool
	asked  chan string
}

func newFakeWire() *fakeWire { return &fakeWire{asked: make(chan string, 16)} }

func (w *fakeWire) Send(m *sdk.Message) {
	w.mu.Lock()
	w.sent = append(w.sent, m.ID)
	w.mu.Unlock()
	w.asked <- "send " + m.ID
}
func (w *fakeWire) Follow(task string, attempt int) { w.asked <- "follow " + task }
func (w *fakeWire) Get(task string)                 { w.asked <- "get " + task }
func (w *fakeWire) Cancel(task string)              { w.asked <- "cancel " + task }
func (w *fakeWire) Close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
}

// loopHost is a host with a store, the questions asked and the runs
// delivered; its log is captured.
func loopHost(t *testing.T) (*Host, *[]string, chan store.AgentRun, *logcap.Lines) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t), Clock: clock.Real().Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log, lines := logcap.Logger()
	var mu sync.Mutex
	questions := &[]string{}
	got := make(chan store.AgentRun, 4)
	h := New(Options{Skills: load(t, skillFile(t, "          a2a_skill: ask\n", ""), "http://127.0.0.1:1"), Runs: st,
		Ask: func(_ context.Context, q Question) error {
			mu.Lock()
			defer mu.Unlock()
			*questions = append(*questions, q.Text)
			return nil
		},
		Deliver: func(_ context.Context, rec store.AgentRun) { got <- rec }, Logger: log})
	t.Cleanup(h.Close)
	return h, questions, got, lines
}

// openCall starts a call with a fake transport and its row.
func openCall(t *testing.T, h *Host, id string) (*call, *fakeWire) {
	t.Helper()
	rec := store.AgentRun{CallID: id, Agent: "recherche", Status: statusRunning}
	if err := h.runs.PutAgentRun(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	c := h.newCall(rec, h.agents[0], "Hermes", state{Agent: "recherche", Sent: 1})
	w := newFakeWire()
	c.tr = w
	h.start(c)
	return c, w
}

// settle returns once the loop handled everything put before it.
func settle(c *call) {
	done := make(chan struct{})
	c.in.put(runIn{func() { close(done) }})
	<-done
}

func question(text string) *sdk.Task {
	return &sdk.Task{ID: "t1", ContextID: "x1", Status: sdk.TaskStatus{State: sdk.TaskStateInputRequired,
		Message: sdk.NewMessage(sdk.MessageRoleAgent, sdk.NewTextPart(text))}}
}

// The send's answer and the pushes come side by side: a state behind the
// one taken since Mira's last message is stale and left
// (docs/spec/remote-agents.md, decision 37a). Seen on GitHub's runner: the
// send's answer already asked, a push "submitted" came after it.
func TestAStaleStateIsLeft(t *testing.T) {
	h, questions, _, _ := loopHost(t)
	c, _ := openCall(t, h, "c1")
	c.in.put(transport.Received{Event: question("Wie groß ist das Haus?")}) // the send's answer, asking already
	c.in.put(transport.Received{Event: &sdk.TaskStatusUpdateEvent{TaskID: "t1", ContextID: "x1", Status: sdk.TaskStatus{State: sdk.TaskStateSubmitted}}})
	c.in.put(transport.Received{Event: &sdk.TaskStatusUpdateEvent{TaskID: "t1", ContextID: "x1", Status: sdk.TaskStatus{State: sdk.TaskStateWorking}}})
	settle(c)
	if label, q, ok := h.Asking("c1"); !ok || label != "Hermes" || q != "Wie groß ist das Haus?" || len(*questions) != 1 {
		t.Errorf("asking: %q %q %v, asked %v", label, q, ok, *questions)
	}
	rec, err := h.runs.GetAgentRun(context.Background(), "c1")
	if err != nil || !strings.Contains(string(rec.Remote), `"state":"TASK_STATE_INPUT_REQUIRED"`) {
		t.Errorf("stored %s: %v", rec.Remote, err)
	}
}

// RM-28 An answer counts as passed on by the first sign of the agent
// (docs/spec/remote-agent-calls.md, decisions 11 and 12): the agent reports
// on the task before the send's answer comes; the owner hears it was passed
// on before the thread gets the result, and a send that fails after that is
// only a log line.
func TestRM28_AnAnswerCountsAsPassedOnByTheFirstSignOfTheAgent(t *testing.T) {
	h, _, got, lines := loopHost(t)
	c, w := openCall(t, h, "c1")
	// Given an agent request waits
	c.in.put(transport.Received{Event: question("Wie groß ist die Wohnfläche?")})
	settle(c)
	answered := make(chan error, 1)
	go func() { answered <- h.Answer(context.Background(), "c1", "140 Quadratmeter") }()
	if s := <-w.asked; s != "send c1-2" {
		t.Fatalf("asked %q", s)
	}
	// When the agent reports on the task before the answer to the send comes
	c.in.put(transport.Received{Event: &sdk.TaskStatusUpdateEvent{TaskID: "t1", ContextID: "x1",
		Status: sdk.TaskStatus{State: sdk.TaskStateCompleted, Message: sdk.NewMessage(sdk.MessageRoleAgent, sdk.NewTextPart("Die Wärmepumpe X."))}}})
	// Then the owner hears that the answer was passed on
	if err := <-answered; err != nil {
		t.Fatalf("answer: %v", err)
	}
	if rec := <-got; rec.Status != statusDone || rec.Output != "Die Wärmepumpe X." {
		t.Errorf("delivered %+v", rec)
	}
	// And a send that fails after that is only a log line
	c.in.put(transport.SendFailed{MessageID: "c1-2", Err: errors.New("context canceled"), Reached: true})
	<-lines.Await("failed after the agent reported on it", "message_id=c1-2")
	if lines.Line("the answer did not reach the remote agent") != "" {
		t.Errorf("the answer was said not to reach the agent:\n%s", lines.String())
	}
}

// RM-29 A cancel while the first send is on its way (decision 13 and
// decision 19 of docs/spec/remote-agents.md): the call ends cancelled at
// once; the send's answer brings the task, and Mira cancels it.
func TestRM29_ACancelWhileTheFirstSendIsOnItsWay(t *testing.T) {
	h, _, _, lines := loopHost(t)
	c, w := openCall(t, h, "c2")
	m := sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart("Finde X"))
	m.ID = "c2"
	c.in.put(runIn{func() { c.send(m) }})
	if s := <-w.asked; s != "send c2" {
		t.Fatalf("asked %q", s)
	}
	// When the thread ends
	if !h.Cancel("c2") {
		t.Fatal("no call to cancel")
	}
	settle(c)
	rec, err := h.runs.GetAgentRun(context.Background(), "c2")
	if err != nil || rec.Status != statusCancelled {
		t.Fatalf("row %+v %v", rec, err)
	}
	// And when the send's answer brings the task, Mira cancels that task
	c.in.put(transport.Answered{MessageID: "c2", Event: &sdk.Task{ID: "t9", Status: sdk.TaskStatus{State: sdk.TaskStateSubmitted}}})
	if s := <-w.asked; s != "cancel t9" {
		t.Fatalf("asked %q", s)
	}
	c.in.put(transport.CancelDone{TaskID: "t9"})
	<-lines.Await("remote agent's task cancelled", "task_id=t9")
}

// RM-31 The stop of the service ends the loops, not the calls: the loop
// and the transport end, the row stays running.
func TestRM31_TheStopOfTheServiceEndsTheLoopsNotTheCalls(t *testing.T) {
	h, _, _, _ := loopHost(t)
	c, _ := openCall(t, h, "c3")
	c.in.put(transport.Received{Event: &sdk.Task{ID: "t3", Status: sdk.TaskStatus{State: sdk.TaskStateWorking}}})
	settle(c)
	// When the Mira service stops (Close waits for the loops)
	h.Close()
	if c.read().ended {
		t.Error("the call was ended")
	}
	rec, err := h.runs.GetAgentRun(context.Background(), "c3")
	if err != nil || rec.Status != statusRunning {
		t.Errorf("row %+v %v", rec, err)
	}
}
