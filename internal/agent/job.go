package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/tools"
)

// Job runs code as a run of the runtime: code with model calls in between
// that is no model loop, such as the task planner (draft, check with Home
// Assistant, draft again). It gets what every run gets: a call ID that the
// agents it calls hang under, one log line with the totals of their model
// calls, one of the maxAsync places, the time limit, and it can be
// cancelled by its call ID. It is not stored: what runs it starts it anew
// after a restart. It runs in the caller's goroutine.
func (r *Runtime) Job(ctx context.Context, spec Spec, cc CallContext, fn func(ctx context.Context) error) error {
	began := time.Now()
	if parent := current(ctx); parent != nil {
		cc = inherit(parent, cc)
	}
	if cc.CallID == "" {
		cc.CallID = strings.ToLower(ulid.Make().String())
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	ctx, stop := context.WithTimeout(ctx, maxRunTime)
	defer stop()
	r.mu.Lock()
	r.cancels[cc.CallID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.cancels, cc.CallID)
		r.mu.Unlock()
	}()

	me := &run{agent: spec.Name, profile: spec.Profile, cc: cc, rights: cc.Rights.And(spec.Rights),
		ts: &tools.TurnState{SessionKey: sessionKey(cc), Channel: config.ChannelBackground, Person: cc.Person,
			Area: cc.Place, ForeignContext: cc.Foreign, ByRights: true},
		usage: &usage{}}
	var err error
	select {
	case r.async <- struct{}{}:
		err = fn(context.WithValue(ctx, runKey{}, me))
		<-r.async
	case <-ctx.Done():
		err = ctx.Err()
	}

	res := me.usage.result()
	res.CallID, res.Status = cc.CallID, Done
	switch {
	case errors.Is(context.Cause(ctx), ErrCancelled):
		res.Status, err = Cancelled, ErrCancelled
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Status, err = Expired, ErrExpired
	case err != nil:
		res.Status = Failed
	}
	r.logRun(spec, cc, res, time.Since(began), err)
	return err
}

// usage adds up the model calls of the runs a job calls.
type usage struct {
	mu  sync.Mutex
	res Result
}

func (u *usage) add(r Result) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if r.Model != "" {
		u.res.Model = r.Model
	}
	u.res.ModelCalls += r.ModelCalls
	u.res.ModelTime += r.ModelTime
	u.res.PromptTokens += r.PromptTokens
	u.res.Completion += r.Completion
}

func (u *usage) result() Result {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.res
}
