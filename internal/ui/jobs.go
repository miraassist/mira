package ui

import (
	"context"
	"errors"
	"net/http"
)

// TemplateInfo is a skill's job template as the page shows it
// (docs/spec/scheduled-jobs.md).
type TemplateInfo struct {
	Key         string        `json:"key"`
	Description string        `json:"description,omitempty"`
	PerPerson   bool          `json:"per_person,omitempty"`
	Parameters  []SettingInfo `json:"parameters,omitempty"`
}

// JobInfo is a scheduled job of a skill as the page shows it.
type JobInfo struct {
	Name     string            `json:"name"`
	Template string            `json:"template"`
	Cron     string            `json:"cron"`
	Time     string            `json:"time,omitempty"`
	Days     []int             `json:"days,omitempty"`
	Enabled  bool              `json:"enabled"`
	Params   map[string]string `json:"params,omitempty"`
	Problem  string            `json:"problem,omitempty"`
	Next     string            `json:"next,omitempty"`
}

// JobEdit is a scheduled job as the page saves it: its template, its time
// (time of day and weekdays), on or off, its parameters.
type JobEdit struct {
	Template string            `json:"template"`
	Time     string            `json:"time"`
	Days     []int             `json:"days"`
	Enabled  bool              `json:"enabled"`
	Params   map[string]string `json:"params"`
}

// Jobs makes, changes and deletes a skill's scheduled jobs in mira.yaml.
type Jobs interface {
	SaveJob(ctx context.Context, skill, name string, j JobEdit) error
	DeleteJob(ctx context.Context, skill, name string) error
}

func (h *Handler) saveJob(w http.ResponseWriter, r *http.Request) {
	if h.opts.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "mira.yaml cannot be written here")
		return
	}
	var j JobEdit
	if err := readJSON(r, &j); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.opts.Jobs.SaveJob(r.Context(), r.PathValue("skill"), r.PathValue("name"), j); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.log.Info("scheduled job saved on the page", "skill", r.PathValue("skill"), "job", r.PathValue("name"))
	writeJSON(w, http.StatusOK, map[string]bool{"restart_needed": true})
}

func (h *Handler) deleteJob(w http.ResponseWriter, r *http.Request) {
	if h.opts.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "mira.yaml cannot be written here")
		return
	}
	err := h.opts.Jobs.DeleteJob(r.Context(), r.PathValue("skill"), r.PathValue("name"))
	switch {
	case errors.Is(err, ErrNoJob):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.log.Info("scheduled job deleted on the page", "skill", r.PathValue("skill"), "job", r.PathValue("name"))
	writeJSON(w, http.StatusOK, map[string]bool{"restart_needed": true})
}

// ErrNoJob is a job that is not there.
var ErrNoJob = errors.New("no such scheduled job")
