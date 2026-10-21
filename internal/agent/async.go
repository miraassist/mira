package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
)

// Asynchronous runs (docs/spec/agenten.md, Runtime): each in a goroutine of
// its own, at most maxAsync at once, stored after every step so a restart
// continues them, ended by cancel, time limit or their answer, and delivered
// on their route.

const (
	// maxAsync runs at the same time (Decided 20); conversations do not
	// count.
	maxAsync = 10
	// keepRecords is how long a run record stays after its end (Decided 11).
	keepRecords = 60 * time.Minute
)

// maxRunTime is an asynchronous run's time limit from its start (Decided
// 34); a variable only for the tests.
var maxRunTime = 60 * time.Minute

// Running is the status of a run that has not ended.
const Running Status = "running"

var (
	// ErrUnknownAgent: no agent of that name.
	ErrUnknownAgent = errors.New("no such agent")
	// ErrExpired: the run went beyond its time limit.
	ErrExpired = errors.New("the run went beyond its time limit")
	// ErrCancelled: the run was cancelled.
	ErrCancelled = errors.New("the run was cancelled")
	// ErrNoStore: asynchronous runs need the store.
	ErrNoStore = errors.New("no store for asynchronous runs")
)

// RunStore keeps the asynchronous runs and their history (package store).
type RunStore interface {
	PutAgentRun(ctx context.Context, r store.AgentRun) error
	EndAgentRun(ctx context.Context, callID, status, output, errText string) error
	GetAgentRun(ctx context.Context, callID string) (store.AgentRun, error)
	RunningAgentRuns(ctx context.Context) ([]store.AgentRun, error)
	AddAgentStep(ctx context.Context, callID, kind string, body json.RawMessage) error
	AgentSteps(ctx context.Context, callID string) ([]store.AgentStep, error)
	DeleteAgentRunsEndedBefore(ctx context.Context, t time.Time) (int, error)
}

// Deliverer hands a finished asynchronous run to its route: the call
// context's Mode.Delivery.
type Deliverer func(ctx context.Context, rec store.AgentRun, cc CallContext)

// The kinds of a run's steps.
const (
	stepAnswer  = "answer"  // the model's answer with tool calls
	stepStarted = "started" // a tool about to run
	stepResult  = "result"  // a tool's result
)

// saver stores what a run's rounds add.
type saver struct {
	r      *Runtime
	callID string
}

func (s *saver) add(ctx context.Context, kind string, m llm.Message) {
	if s == nil {
		return
	}
	body, _ := json.Marshal(m)
	// Stored even when the run is being stopped: what happened must be
	// there when it continues.
	if err := s.r.o.Runs.AddAgentStep(context.WithoutCancel(ctx), s.callID, kind, body); err != nil {
		s.r.log.Warn("could not store a run's step", "call_id", s.callID, "kind", kind, "error", err)
	}
}

// start stores the run record and hands the run to a goroutine. An
// asynchronous call returns at once; a synchronous one with run-on waits up
// to its timeout.
func (r *Runtime) start(ctx context.Context, parent *run, spec Spec, input string, cc CallContext) (Result, error) {
	res := Result{CallID: cc.CallID, Status: Running}
	if r.o.Runs == nil {
		res.Status = Failed
		return res, ErrNoStore
	}
	r.cleanup(ctx)
	now := r.now()
	raw, err := json.Marshal(cc)
	if err != nil {
		res.Status = Failed
		return res, err
	}
	rec := store.AgentRun{CallID: cc.CallID, ParentID: cc.ParentID, Agent: spec.Name, Status: string(Running),
		Call: raw, Input: input, StartedAt: now, Deadline: now.Add(maxRunTime)}
	if err := r.o.Runs.PutAgentRun(ctx, rec); err != nil {
		res.Status = Failed
		return res, err
	}
	done := r.launch(rec, spec, cc, nil)
	if cc.Mode.Async {
		return res, nil
	}
	timer := time.NewTimer(cc.Mode.Timeout)
	defer timer.Stop()
	select {
	case out := <-done:
		// In time: as a synchronous call, its third-party text counts for
		// the caller.
		if out.res.Foreign && parent != nil {
			parent.ts.ForeignContext = true
		}
		return out.res, out.err
	case <-timer.C:
	case <-ctx.Done():
	}
	return res, nil
}

type outcome struct {
	res Result
	err error
}

// launch runs a stored run in a goroutine of its own, from the steps it
// already stored, if any.
func (r *Runtime) launch(rec store.AgentRun, spec Spec, cc CallContext, steps []store.AgentStep) <-chan outcome {
	out := make(chan outcome, 1)
	ctx, cancel := context.WithCancelCause(r.base)
	// The time limit as the runtime's clock counts it, also across a restart.
	ctx, stopDeadline := context.WithTimeout(ctx, rec.Deadline.Sub(r.now()))
	r.mu.Lock()
	r.cancels[rec.CallID] = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer stopDeadline()
		defer func() {
			r.mu.Lock()
			delete(r.cancels, rec.CallID)
			r.mu.Unlock()
			cancel(nil)
		}()
		began := time.Now()
		res, status, err := r.drive(ctx, rec, spec, cc, steps)
		switch {
		case r.stopping.Load() && status == Cancelled && !errors.Is(context.Cause(ctx), ErrCancelled):
			// Shut down: the run stays "running" and goes on after the start.
			r.log.Info("agent stopped for shutdown", "agent", spec.Name, "call_id", rec.CallID)
			return
		case status == Cancelled && errors.Is(ctx.Err(), context.DeadlineExceeded):
			status, err = Expired, ErrExpired
		case status == Cancelled:
			err = ErrCancelled
		}
		res.Status = status
		r.end(rec, spec, cc, res, err, time.Since(began))
		out <- outcome{res, err}
	}()
	return out
}

// drive runs a run in its goroutine: it waits for one of the maxAsync
// places, sets the run up, takes up the stored steps and runs the rounds.
func (r *Runtime) drive(ctx context.Context, rec store.AgentRun, spec Spec, cc CallContext, steps []store.AgentStep) (Result, Status, error) {
	select {
	case r.async <- struct{}{}:
		defer func() { <-r.async }()
	case <-ctx.Done():
		return Result{CallID: rec.CallID}, Cancelled, ctx.Err()
	}
	ex, ctx, err := r.prepare(ctx, spec, rec.Input, cc)
	if err != nil {
		return ex.res, Failed, err
	}
	ex.saved = &saver{r: r, callID: rec.CallID}
	r.takeUp(ctx, ex, steps)
	status, err := r.loop(ctx, ex)
	return ex.res, status, err
}

// takeUp continues a run from its stored steps: the answers and results go
// back into what the model sees. A tool that was asked for but has no
// result is run again if it only reads; one that changes something is not,
// the model gets that its result is unknown.
func (r *Runtime) takeUp(ctx context.Context, ex *execution, steps []store.AgentStep) {
	if len(steps) == 0 {
		return
	}
	started, have := map[string]bool{}, map[string]bool{}
	last := -1
	for _, st := range steps {
		var m llm.Message
		if err := json.Unmarshal(st.Body, &m); err != nil {
			continue
		}
		switch st.Kind {
		case stepAnswer:
			ex.msgs = append(ex.msgs, m)
			last = len(ex.msgs) - 1
			ex.round++
		case stepStarted:
			started[m.ToolCallID] = true
		case stepResult:
			ex.msgs = append(ex.msgs, m)
			have[m.ToolCallID] = true
		}
	}
	if last < 0 {
		return
	}
	for _, call := range ex.msgs[last].ToolCalls {
		if have[call.ID] {
			continue
		}
		if started[call.ID] && !r.readOnly(call.Function.Name) {
			ex.addResult(ctx, call, ex.t.interrupted)
			continue
		}
		ex.runTool(ctx, r, call)
	}
	r.log.Info("agent continued", "agent", ex.spec.Name, "call_id", ex.res.CallID, "round", ex.round)
}

// readOnly: a tool that only reads, so running it again does no harm.
func (r *Runtime) readOnly(name string) bool {
	spec, ok := r.o.Tools.Lookup(name)
	return ok && spec.ReadOnly
}

// end writes a run's final status, logs it and delivers it.
func (r *Runtime) end(rec store.AgentRun, spec Spec, cc CallContext, res Result, err error, took time.Duration) {
	ctx := context.Background()
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	if e := r.o.Runs.EndAgentRun(ctx, rec.CallID, string(res.Status), res.Text, errText); e != nil {
		r.log.Error("could not end a run record", "call_id", rec.CallID, "error", e)
	}
	r.logRun(spec, cc, res, took, err)
	if r.o.Deliver == nil {
		return
	}
	final, e := r.o.Runs.GetAgentRun(ctx, rec.CallID)
	if e != nil {
		r.log.Error("could not read a run record to deliver", "call_id", rec.CallID, "error", e)
		return
	}
	r.o.Deliver(ctx, final, cc)
}

// Resume takes up the runs that were running when the agent stopped. A run
// whose time limit passed meanwhile has expired; one whose agent no longer
// exists has failed; the others go on with the agent as it is now.
func (r *Runtime) Resume(ctx context.Context) error {
	if r.o.Runs == nil {
		return nil
	}
	r.cleanup(ctx)
	runs, err := r.o.Runs.RunningAgentRuns(ctx)
	if err != nil {
		return err
	}
	for _, rec := range runs {
		var cc CallContext
		_ = json.Unmarshal(rec.Call, &cc)
		spec, ok := r.lookup(rec.Agent)
		switch {
		case !ok:
			r.end(rec, Spec{Name: rec.Agent}, cc, Result{CallID: rec.CallID, Status: Failed},
				fmt.Errorf("%w: %s no longer exists", ErrUnknownAgent, rec.Agent), 0)
		case !r.now().Before(rec.Deadline):
			r.end(rec, spec, cc, Result{CallID: rec.CallID, Status: Expired}, ErrExpired, 0)
		default:
			steps, err := r.o.Runs.AgentSteps(ctx, rec.CallID)
			if err != nil {
				return err
			}
			r.launch(rec, spec, cc, steps)
		}
	}
	return nil
}

// Close stops the asynchronous runs: no new round starts, what happened is
// stored, and the runs go on after the next start. It waits for them up to
// the deadline of ctx.
func (r *Runtime) Close(ctx context.Context) {
	r.stopping.Store(true)
	r.stop()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Cancel ends a running asynchronous run; false when none runs under that
// call ID.
func (r *Runtime) Cancel(callID string) bool {
	r.mu.Lock()
	cancel, ok := r.cancels[callID]
	r.mu.Unlock()
	if ok {
		cancel(ErrCancelled)
	}
	return ok
}

// Record is a run's record by its call ID, as long as it is kept.
func (r *Runtime) Record(ctx context.Context, callID string) (store.AgentRun, error) {
	if r.o.Runs == nil {
		return store.AgentRun{}, ErrNoStore
	}
	r.cleanup(ctx)
	return r.o.Runs.GetAgentRun(ctx, callID)
}

// cleanup removes the records that ended more than keepRecords ago.
func (r *Runtime) cleanup(ctx context.Context) {
	if _, err := r.o.Runs.DeleteAgentRunsEndedBefore(ctx, r.now().Add(-keepRecords)); err != nil {
		r.log.Warn("could not remove old run records", "error", err)
	}
}

// lookup is a registered agent by its name.
func (r *Runtime) lookup(name string) (Spec, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	spec, ok := r.agents[name]
	return spec, ok
}

// CallByName calls a registered agent from outside (Home Assistant's
// mira.agent): the input is an object as the agent's input schema asks,
// or {"input": text} for an agent that takes text; the run gets the rights
// the agent names (Decided 17).
func (r *Runtime) CallByName(ctx context.Context, name string, input json.RawMessage, cc CallContext) (Result, error) {
	spec, ok := r.lookup(name)
	if !ok {
		return Result{Status: Failed}, fmt.Errorf("%w: %s", ErrUnknownAgent, name)
	}
	text, err := inputFor(spec, input)
	if err != nil {
		return Result{Status: Failed}, err
	}
	cc.Rights = AllRights
	return r.Call(ctx, spec, text, cc)
}

// inputFor is what an agent gets as input from a JSON object.
func inputFor(spec Spec, raw json.RawMessage) (string, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return "", errors.New("the input is not JSON")
	}
	if len(spec.Input) == 0 {
		var in struct {
			Input string `json:"input"`
		}
		if err := json.Unmarshal(raw, &in); err != nil || in.Input == "" {
			return "", errors.New(`the input needs {"input": "…"}`)
		}
		return in.Input, nil
	}
	if err := checkSchema(spec.Input, raw); err != nil {
		return "", err
	}
	return string(raw), nil
}

func (r *Runtime) now() time.Time {
	if r.o.Clock != nil {
		return r.o.Clock()
	}
	return time.Now()
}
