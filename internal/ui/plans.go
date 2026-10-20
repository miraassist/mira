package ui

import (
	"context"
	"net/http"
	"strings"
)

// Plan is a running plan (docs/spec/vorgaenge.md, Pläne) as the page shows
// it: everyone in the house may follow it, nobody changes it here.
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
