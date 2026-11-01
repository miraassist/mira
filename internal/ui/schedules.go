package ui

import (
	"context"
	"errors"
	"net/http"
)

// Schedules are Mira's jobs at a time (docs/spec/zeitplan.md): the skills'
// schedules and Mira's own.
type Schedules interface {
	List(ctx context.Context) ([]ScheduleItem, error)
	// Change switches a schedule on or off (enabled nil: as it is) and gives
	// it a time of day and weekdays (time "": as it is).
	Change(ctx context.Context, skill, name string, c ScheduleEdit) error
	// RunNow runs it at once, in the background.
	RunNow(skill, name string) error
}

// ErrScheduleRunning: the schedule runs or waits to run already.
var ErrScheduleRunning = errors.New("it is running")

// ScheduleItem is a schedule as the page shows it.
type ScheduleItem struct {
	Skill       string `json:"skill"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// Own: one of Mira's own (never switched off).
	Own bool `json:"own,omitempty"`
	// SkillOff: the skill is off; the schedule is shown, it does not run.
	SkillOff  bool   `json:"skill_off,omitempty"`
	PerPerson bool   `json:"per_person,omitempty"`
	Cron      string `json:"cron"`
	Enabled   bool   `json:"enabled"`
	// Editable: it has a name and a time the fields can show; then Time
	// ("06:45") and Days (0 Sunday … 6 Saturday) are set.
	Editable bool   `json:"editable"`
	Time     string `json:"time,omitempty"`
	Days     []int  `json:"days,omitempty"`
	Next     string `json:"next,omitempty"`
	// State: waiting, running, or "".
	State string    `json:"state,omitempty"`
	Last  []LastRun `json:"last,omitempty"`
}

// LastRun is how a schedule ran last; per person for a per-person one.
type LastRun struct {
	Person  string `json:"person,omitempty"`
	Started string `json:"started"`
	Seconds int    `json:"seconds"`
	Status  string `json:"status"`
	Text    string `json:"text,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ScheduleEdit is what the page changes.
type ScheduleEdit struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Time    string `json:"time,omitempty"`
	Days    []int  `json:"days,omitempty"`
}

func (h *Handler) noSchedules(w http.ResponseWriter) bool {
	if h.opts.Schedules == nil {
		writeError(w, http.StatusServiceUnavailable, "no schedules")
		return true
	}
	return false
}

func (h *Handler) schedules(w http.ResponseWriter, r *http.Request) {
	if h.noSchedules(w) {
		return
	}
	items, err := h.opts.Schedules.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []ScheduleItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) changeSchedule(w http.ResponseWriter, r *http.Request) {
	if h.noSchedules(w) {
		return
	}
	var c ScheduleEdit
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.opts.Schedules.Change(r.Context(), r.PathValue("skill"), r.PathValue("name"), c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (h *Handler) runSchedule(w http.ResponseWriter, r *http.Request) {
	if h.noSchedules(w) {
		return
	}
	switch err := h.opts.Schedules.RunNow(r.PathValue("skill"), r.PathValue("name")); {
	case errors.Is(err, ErrScheduleRunning):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
	}
}
