// Package transport sends to and receives from a remote agent for one call
// (docs/spec/remote-agent-calls.md): a stream with SSE, or a send with
// Mira's push route. It has only its own state (open, closed) and reports
// what it sent and received, in order, to its call; what a message means,
// when the call ends and what is stored is the call loop's (package a2a).
// It imports neither the store, the threads nor the call logic (RM-27).
package transport

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/retry"
)

// Client is the part of the SDK's client a transport uses.
type Client interface {
	SendMessage(ctx context.Context, req *sdk.SendMessageRequest) (sdk.SendMessageResult, error)
	SendStreamingMessage(ctx context.Context, req *sdk.SendMessageRequest) iter.Seq2[sdk.Event, error]
	SubscribeToTask(ctx context.Context, req *sdk.SubscribeToTaskRequest) iter.Seq2[sdk.Event, error]
	GetTask(ctx context.Context, req *sdk.GetTaskRequest) (*sdk.Task, error)
	CancelTask(ctx context.Context, req *sdk.CancelTaskRequest) (*sdk.Task, error)
}

// A Report is what a transport tells its call: one of the types below.
// Every operation (Send, Follow, Get, Cancel) ends with exactly one report
// that says it is over (Answered, SendFailed, StreamEnded, Got, CancelDone).
type Report interface{ report() }

// Answered: a send with push was answered; Event is the answer (a task, or
// a message).
type Answered struct {
	MessageID string
	Event     sdk.Event
}

// SendFailed: a send got no answer, after its retries. Reached tells
// whether it may have reached the agent (decision 26, 38).
type SendFailed struct {
	MessageID string
	Err       error
	Reached   bool
}

// Received is an event of the call: from a stream or a push.
type Received struct {
	Event sdk.Event
}

// StreamEnded: a stream is over. MessageID is the send that opened it, ""
// for a Follow. Events counts what came through it; Err is why it ended
// early (nil when the agent closed it), after the retries of a follow.
type StreamEnded struct {
	MessageID string
	Events    int
	Err       error
}

// Got is the answer of a GetTask, or why there is none.
type Got struct {
	Task *sdk.Task
	Err  error
}

// CancelDone is the outcome of a CancelTask.
type CancelDone struct {
	TaskID string
	Err    error
}

func (Answered) report()    {}
func (SendFailed) report()  {}
func (Received) report()    {}
func (StreamEnded) report() {}
func (Got) report()         {}
func (CancelDone) report()  {}

// Options make a transport.
type Options struct {
	Client Client
	// Push: the agent pushes to PushURL with Token; else Mira streams.
	Push    bool
	PushURL string
	Token   string
	// Base is the Mira service's life: its end cuts everything, also a send
	// on its way (decision 10).
	Base context.Context
	// Retry is the policy of a send and a read (3 attempts, exponential).
	Retry retry.Policy
	// Report gets the reports, in order; it must not block.
	Report func(Report)
	// Agent and Call name the transport in its log lines.
	Agent, Call string
	Log         *slog.Logger
}

// T is the transport of one call.
type T struct {
	o   Options
	log *slog.Logger
	// recv ends receiving at Close: a stream, a follow, a get.
	recv     context.Context
	stopRecv context.CancelFunc
	wg       *sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// Open opens a transport; wg counts its goroutines, for the service's stop.
func Open(o Options, wg *sync.WaitGroup) *T {
	t := &T{o: o, wg: wg, log: o.Log.With("agent", o.Agent, "call_id", o.Call)}
	t.recv, t.stopRecv = context.WithCancel(o.Base)
	return t
}

// Push reports whether the agent pushes (else Mira streams).
func (t *T) Push() bool { return t.o.Push }

// Token is the call's push token (decision 14).
func (t *T) Token() string { return t.o.Token }

// Open reports whether the transport still receives.
func (t *T) Open() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.closed
}

// Close ends receiving: a stream, a follow and a get end, a push is no
// longer handed on. A send with push on its way is finished and its answer
// reported (decision 10).
func (t *T) Close() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.stopRecv()
}

// Deliver hands on a push for this call, already checked by the push
// route; false when the transport is closed.
func (t *T) Deliver(ev sdk.Event) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.o.Report(Received{Event: ev})
	return true
}

func (t *T) goOp(fn func()) { t.wg.Go(fn) }

// Send sends a message: with push SendMessage (returnImmediately, the push
// configuration), else SendStreamingMessage, whose events are reported.
// Tried again only when it did not reach the agent (decision 26).
func (t *T) Send(m *sdk.Message) {
	t.goOp(func() {
		if t.o.Push {
			t.sendPush(m)
			return
		}
		t.sendStream(m)
	})
}

func (t *T) warn(what string) retry.OnRetry {
	return retry.Warn(t.log, what, t.o.Retry)
}

func (t *T) sendPush(m *sdk.Message) {
	req := &sdk.SendMessageRequest{Message: m, Config: &sdk.SendMessageConfig{ReturnImmediately: true,
		PushConfig: &sdk.PushConfig{ID: t.o.Call, URL: t.o.PushURL, Token: t.o.Token}}}
	var res sdk.SendMessageResult
	// On the service's context, not the receiving one: closing does not cut
	// it (decision 10).
	err := retry.Do(t.o.Base, t.o.Retry, func(ctx context.Context) error {
		var err error
		res, err = t.o.Client.SendMessage(ctx, req)
		if err != nil {
			return ClassifySend(err)
		}
		return nil
	}, t.warn("sending to the remote agent"))
	if err != nil {
		t.o.Report(SendFailed{MessageID: m.ID, Err: err, Reached: !NotReached(err)})
		return
	}
	t.o.Report(Answered{MessageID: m.ID, Event: res})
}

func (t *T) sendStream(m *sdk.Message) {
	req := &sdk.SendMessageRequest{Message: m}
	got := 0
	err := retry.Do(t.recv, t.o.Retry, func(ctx context.Context) error {
		n, err := t.consume(t.o.Client.SendStreamingMessage(ctx, req))
		got += n
		switch {
		case err == nil:
			return nil
		case n > 0:
			// The send reached the agent; what broke is the stream.
			return retry.Fatal(&BrokenStream{err})
		}
		return ClassifySend(err)
	}, t.warn("sending to the remote agent"))
	var broken *BrokenStream
	switch {
	case err != nil && got == 0 && !errors.As(err, &broken) && t.recv.Err() == nil:
		t.o.Report(SendFailed{MessageID: m.ID, Err: err, Reached: !NotReached(err)})
	default:
		t.o.Report(StreamEnded{MessageID: m.ID, Events: got, Err: err})
	}
}

// consume reports a stream's events in order.
func (t *T) consume(events iter.Seq2[sdk.Event, error]) (int, error) {
	n := 0
	for ev, err := range events {
		if err != nil {
			return n, err
		}
		n++
		t.o.Report(Received{Event: ev})
	}
	return n, nil
}

// Follow subscribes to a task again (SubscribeToTask, decision 35), after
// the wait of the attempt-th try of a read (0: at once); its errors are
// tried again as a read. Only for a stream.
func (t *T) Follow(task string, attempt int) {
	t.goOp(func() {
		if attempt > 0 && !t.wait(attempt) {
			t.o.Report(StreamEnded{Err: t.recv.Err()})
			return
		}
		got := 0
		err := retry.Do(t.recv, t.o.Retry, func(ctx context.Context) error {
			n, err := t.consume(t.o.Client.SubscribeToTask(ctx, &sdk.SubscribeToTaskRequest{ID: sdk.TaskID(task)}))
			got += n
			if err != nil {
				return Classify(err)
			}
			return nil
		}, t.warn("following the remote agent's task"))
		t.o.Report(StreamEnded{Events: got, Err: err})
	})
}

// wait is the wait before the attempt-th try of a read; false when the
// transport closed meanwhile.
func (t *T) wait(attempt int) bool {
	clk := clock.Or(t.o.Retry.Clock)
	next, _ := t.o.Retry.Next(clk.Now(), attempt, retry.Transient(errors.New("the stream ended before the task")))
	timer := clk.NewTimer(clk.Until(next))
	defer timer.Stop()
	select {
	case <-timer.C():
		return true
	case <-t.recv.Done():
		return false
	}
}

// Get asks a task once (GetTask), tried again as a read.
func (t *T) Get(task string) {
	t.goOp(func() {
		var got *sdk.Task
		err := retry.Do(t.recv, t.o.Retry, func(ctx context.Context) error {
			var err error
			got, err = t.o.Client.GetTask(ctx, &sdk.GetTaskRequest{ID: sdk.TaskID(task)})
			if err != nil {
				return Classify(err)
			}
			return nil
		}, t.warn("asking the remote agent's task"))
		t.o.Report(Got{Task: got, Err: err})
	})
}

// Cancel sends CancelTask once (decision 19), also after Close: within
// 30 seconds, unless the service stops.
func (t *T) Cancel(task string) {
	t.goOp(func() {
		ctx, cancel := context.WithTimeout(t.o.Base, 30*time.Second)
		defer cancel()
		_, err := t.o.Client.CancelTask(ctx, &sdk.CancelTaskRequest{ID: sdk.TaskID(task)})
		t.o.Report(CancelDone{TaskID: task, Err: err})
	})
}

// BrokenStream is a stream that broke off after the send reached the agent.
type BrokenStream struct{ Err error }

func (b *BrokenStream) Error() string { return "the stream broke off: " + b.Err.Error() }
func (b *BrokenStream) Unwrap() error { return b.Err }

// NotReached says that a send did not reach the remote agent, so sending it
// again cannot make the work twice (decision 26): the connection was
// refused, no route, the name unknown, or the agent answered 429 or 503.
func NotReached(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusTooManyRequests || se.Status == http.StatusServiceUnavailable
	}
	var dns *net.DNSError
	return errors.As(err, &dns) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH)
}

// Classify marks an error for internal/retry: an A2A error in the answer
// and a 4xx are fatal, a lost connection, a timeout, a 408, 429 and 5xx
// retryable (Retries); a wait the agent named is kept.
func Classify(err error) error {
	var ae *sdk.Error
	var se *StatusError
	switch {
	case errors.As(err, &ae):
		return retry.Fatal(err)
	case errors.As(err, &se) && se.Wait > 0:
		return retry.After(err, se.Wait)
	case errors.As(err, &se) && se.Status != http.StatusRequestTimeout && se.Status != http.StatusTooManyRequests &&
		se.Status < 500:
		return retry.Fatal(err)
	}
	return retry.Transient(err)
}

// ClassifySend is Classify for a send: tried again only when it did not
// reach the agent (decision 26).
func ClassifySend(err error) error {
	if !NotReached(err) {
		return retry.Fatal(err)
	}
	return Classify(err)
}
