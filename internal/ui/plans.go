package ui

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/miraassist/mira/internal/store"
)

// Plan is a running plan (docs/spec/threads.md, Pläne) as the page shows
// it: everyone in the house may follow it and end it (docs/spec/plan-end.md),
// nobody changes it here.
type Plan struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	Request  string `json:"request"`
	Place    string `json:"place"`
	Plan     string `json:"plan"`
	Done     int    `json:"done"`
	Steps    int    `json:"steps"`
	Finished bool   `json:"finished"`
	Started  string `json:"started"`
	Updated  string `json:"updated"`
}

// PlanSource lists the running plans; Place is "area:<id>" for a room.
type PlanSource func(ctx context.Context) ([]Plan, error)

func (h *Handler) plans(w http.ResponseWriter, r *http.Request) {
	out := []Plan{}
	if h.opts.Plans != nil {
		list, err := h.opts.Plans(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, p := range list {
			if area, ok := strings.CutPrefix(p.Place, "area:"); ok {
				p.Place = area
				if h.opts.AreaName != nil {
					if name := h.opts.AreaName(area); name != "" {
						p.Place = name
					}
				}
			} else {
				p.Place = ""
			}
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
}

// endPlan ends a running plan from the page (docs/spec/plan-end.md): only an
// id among the running plans, through the same CancelThread as the
// conversation's thread_cancel.
func (h *Handler) endPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.opts.Plans == nil || h.opts.Threads == nil {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	}
	list, err := h.opts.Plans(r.Context())
	if err != nil {
		h.log.Error("could not end a plan on the page", "plan", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	target, found := "", false
	for _, p := range list {
		if p.ID == id {
			target, found = p.Target, true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	}
	err = h.opts.Threads.CancelThread(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	}
	if err != nil {
		h.log.Error("could not end a plan on the page", "plan", id, "target", target, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log.Info("plan ended on the page", "plan", id, "target", target)
	writeJSON(w, http.StatusOK, map[string]bool{"ended": true})
}
