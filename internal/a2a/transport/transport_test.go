package transport

import (
	"context"
	"go/parser"
	"go/token"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/miraassist/mira/internal/retry"
)

// RM-27 The transport knows nothing of the call
// (docs/spec/remote-agent-calls.md, decision 7): its package imports
// neither the store, the threads nor the call logic.
func TestRM27_TheTransportKnowsNothingOfTheCall(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"/internal/store", "/internal/threads", "/internal/agent", "/internal/conversation"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasSuffix(path, "/internal/a2a") {
				t.Errorf("%s imports the call logic %s", f, path)
			}
			for _, bad := range forbidden {
				if strings.Contains(path, bad) {
					t.Errorf("%s imports %s", f, path)
				}
			}
		}
	}
}

// blockingClient answers SendMessage only once released.
type blockingClient struct {
	entered chan struct{}
	release chan struct{}
}

func (c *blockingClient) SendMessage(ctx context.Context, req *sdk.SendMessageRequest) (sdk.SendMessageResult, error) {
	close(c.entered)
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &sdk.Task{ID: "t1", Status: sdk.TaskStatus{State: sdk.TaskStateSubmitted}}, nil
}

func (c *blockingClient) SendStreamingMessage(ctx context.Context, req *sdk.SendMessageRequest) iter.Seq2[sdk.Event, error] {
	return func(yield func(sdk.Event, error) bool) {}
}

func (c *blockingClient) SubscribeToTask(ctx context.Context, req *sdk.SubscribeToTaskRequest) iter.Seq2[sdk.Event, error] {
	return func(yield func(sdk.Event, error) bool) {}
}

func (c *blockingClient) GetTask(ctx context.Context, req *sdk.GetTaskRequest) (*sdk.Task, error) {
	return nil, nil
}

func (c *blockingClient) CancelTask(ctx context.Context, req *sdk.CancelTaskRequest) (*sdk.Task, error) {
	return nil, nil
}

// RM-30 Closing the transport does not cut a send (decision 10): the send
// with push on its way is finished and its answer reported; nothing that
// comes after the close is received.
func TestRM30_ClosingTheTransportDoesNotCutASend(t *testing.T) {
	client := &blockingClient{entered: make(chan struct{}), release: make(chan struct{})}
	reports := make(chan Report, 4)
	var wg sync.WaitGroup
	tr := Open(Options{Client: client, Push: true, PushURL: "http://mira/v1/a2a/push/c1", Token: "tok",
		Base: context.Background(), Retry: retry.Policy{Attempts: 1}, Report: func(r Report) { reports <- r },
		Agent: "recherche", Call: "c1", Log: slog.New(slog.DiscardHandler)}, &wg)

	// Given a send of a call is on its way
	m := sdk.NewMessage(sdk.MessageRoleUser, sdk.NewTextPart("140 Quadratmeter"))
	m.ID = "c1-2"
	tr.Send(m)
	<-client.entered
	// When the transport is closed
	tr.Close()
	// Then nothing that comes after the close is received
	if tr.Deliver(&sdk.TaskStatusUpdateEvent{TaskID: "t1", Status: sdk.TaskStatus{State: sdk.TaskStateCompleted}}) {
		t.Error("a push was taken after the close")
	}
	// And the send is finished and its answer reported
	close(client.release)
	wg.Wait()
	r := <-reports
	a, ok := r.(Answered)
	if !ok || a.MessageID != "c1-2" {
		t.Fatalf("report %#v", r)
	}
	if task, ok := a.Event.(*sdk.Task); !ok || task.ID != "t1" {
		t.Errorf("answer %#v", a.Event)
	}
	if len(reports) != 0 {
		t.Errorf("more reports: %d", len(reports))
	}
}
