package conversation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
)

// A model that failed for its key or its address is said in the turn and
// shown as a problem until it answers again (docs/spec/models.md, When a
// model fails).

// TurnError ends a turn with a sentence to say instead of the model's
// answer: the caller answers Say as the turn's text.
type TurnError struct {
	Say string
	Err error
}

func (e *TurnError) Error() string { return e.Err.Error() }
func (e *TurnError) Unwrap() error { return e.Err }

// ModelProblem is a model that failed for its key or its address.
type ModelProblem struct {
	Model, Text string
}

type modelFailure struct {
	cause string
	err   string
}

// modelFailed keeps why a model failed in a turn; one of other causes is
// not a problem the user can fix.
func (e *Engine) modelFailed(ctx context.Context, model string, err error) {
	if ctx.Err() != nil {
		return
	}
	cause := llm.Cause(err)
	if cause == "" {
		return
	}
	e.failMu.Lock()
	defer e.failMu.Unlock()
	if e.failed == nil {
		e.failed = map[string]modelFailure{}
	}
	// The server's answer on one line: it is a repair's text.
	e.failed[model] = modelFailure{cause: cause, err: strings.Join(strings.Fields(err.Error()), " ")}
}

// modelAnswered forgets a model's failure.
func (e *Engine) modelAnswered(model string) {
	e.failMu.Lock()
	defer e.failMu.Unlock()
	delete(e.failed, model)
}

// ModelProblems are the models that failed for their key or address and
// have not answered since, in lang.
func (e *Engine) ModelProblems(lang i18n.Lang) []ModelProblem {
	t := i18n.Pick(catalog, lang)
	e.failMu.Lock()
	defer e.failMu.Unlock()
	var out []ModelProblem
	for model, f := range e.failed {
		format := t.problemUnreachable
		if f.cause == llm.CauseKey {
			format = t.problemKey
		}
		out = append(out, ModelProblem{Model: model, Text: fmt.Sprintf(format, f.err)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// turnFailed is the turn's error: with the sentence to say when the model
// failed for its key or its address, else err as it is.
func (e *Engine) turnFailed(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	switch llm.Cause(err) {
	case llm.CauseKey:
		return &TurnError{Say: e.texts(ctx).keyRefused, Err: err}
	case llm.CauseUnreachable:
		return &TurnError{Say: e.texts(ctx).unreachable, Err: err}
	}
	return err
}

// SayOf is the sentence a failed turn ends with, "" when it has none.
func SayOf(err error) string {
	var te *TurnError
	if errors.As(err, &te) {
		return te.Say
	}
	return ""
}
