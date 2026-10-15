package ui

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tasks"
)

// Tasks is what the UI does with Mira's tasks (package tasks): Mira's
// automations in Home Assistant.
type Tasks interface {
	List(ctx context.Context) ([]tasks.Item, error)
	SetEnabled(ctx context.Context, id string, on bool) error
	Delete(ctx context.Context, id string) error
	Try(ctx context.Context, id string) (tasks.TryResult, error)
	DescribeTry(res tasks.TryResult) string
}

// Threads confirms or cancels a thread ("Vorgang") by id: a task still
// being planned is one.
type Threads interface {
	ConfirmThread(ctx context.Context, id string) (string, error)
	CancelThread(ctx context.Context, id string) error
}

// threadJSON is an open task thread as the tasks tab shows it.
type threadJSON struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Request string `json:"request"`
	Last    string `json:"last"`
	Updated string `json:"updated"`
}

func (h *Handler) noTasks(w http.ResponseWriter) bool {
	if h.opts.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "tasks need Home Assistant")
		return true
	}
	return false
}

// tasks lists proposals and automations. When Home Assistant cannot be
// asked, the proposals still come, with the error.
func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	if h.noTasks(w) {
		return
	}
	items, err := h.opts.Tasks.List(r.Context())
	out := map[string]any{"items": items}
	if items == nil {
		out["items"] = []tasks.Item{}
	}
	if err != nil {
		out["error"] = err.Error()
	}
	// Tasks still being planned: open threads of the target "aufgabe".
	planning := []threadJSON{}
	if open, err := h.opts.Store.OpenThreads(r.Context(), ""); err == nil {
		for _, th := range open {
			if th.Target != "aufgabe" {
				continue
			}
			last := ""
			for i := len(th.Log) - 1; i >= 0; i-- {
				if th.Log[i].Who == "mira" {
					last = th.Log[i].Text
					break
				}
			}
			planning = append(planning, threadJSON{ID: th.ID, State: th.State, Request: th.Request, Last: last,
				Updated: localTime(th.UpdatedAt)})
		}
	}
	out["planning"] = planning
	writeJSON(w, http.StatusOK, out)
}

// enableTask saves a proposal, or switches an automation on or off.
func (h *Handler) enableTask(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.noTasks(w) {
			return
		}
		id := r.PathValue("id")
		var err error
		if _, terr := h.opts.Store.GetThread(r.Context(), id); terr == nil && h.opts.Threads != nil {
			if !on {
				err = h.opts.Threads.CancelThread(r.Context(), id)
			} else {
				_, err = h.opts.Threads.ConfirmThread(r.Context(), id)
			}
		} else {
			err = h.opts.Tasks.SetEnabled(r.Context(), id, on)
		}
		if h.taskError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"on": on})
	}
}

func (h *Handler) tryTask(w http.ResponseWriter, r *http.Request) {
	if h.noTasks(w) {
		return
	}
	res, err := h.opts.Tasks.Try(r.Context(), r.PathValue("id"))
	if h.taskError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": h.opts.Tasks.DescribeTry(res), "result": res})
}

type assessmentJSON struct {
	At         string `json:"at"`
	Kind       string `json:"kind"`
	Importance int    `json:"importance"`
	Answer     bool   `json:"answer"`
	Spoken     string `json:"spoken"`
	Input      string `json:"input"`
	Notified   bool   `json:"notified"`
	DryRun     bool   `json:"dry_run"`
}

// taskLog is the task's latest judgements: what Mira saw and said.
func (h *Handler) taskLog(w http.ResponseWriter, r *http.Request) {
	list, err := h.opts.Store.Assessments(r.Context(), r.PathValue("id"), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]assessmentJSON, 0, len(list))
	for _, a := range list {
		out = append(out, assessmentJSON{At: localTime(a.CreatedAt), Kind: a.Kind, Importance: a.Importance,
			Answer: a.Answer, Spoken: a.Spoken, Input: a.Input, Notified: a.NoticeID != "", DryRun: a.DryRun})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteTask(w http.ResponseWriter, r *http.Request) {
	if h.noTasks(w) {
		return
	}
	id := r.PathValue("id")
	var err error
	if _, terr := h.opts.Store.GetThread(r.Context(), id); terr == nil && h.opts.Threads != nil {
		err = h.opts.Threads.CancelThread(r.Context(), id)
	} else {
		err = h.opts.Tasks.Delete(r.Context(), id)
	}
	if h.taskError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) taskError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such task")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
	return true
}

func localTime(t time.Time) string { return t.In(time.Local).Format("2006-01-02 15:04") }
