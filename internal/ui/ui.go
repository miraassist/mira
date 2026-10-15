// Package ui is the agent's small web interface: a single page that edits
// the config sections the agent allows (notifier, persons,
// satellites), shows open notices and the memory.
//
// The page itself is static and holds no data. Everything goes through the
// JSON API under /v1/ui/, which needs the same bearer token as the rest of
// the HTTP API; the page asks for it once and keeps it in the browser.
package ui

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sker65/mira/docs"
	"github.com/sker65/mira/internal/store"
)

//go:embed index.html
var page []byte

//go:embed icon.png
var icon []byte

// Entity is offered for autocompletion in the forms.
type Entity struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Area string `json:"area,omitempty"`
}

// Config is what the agent exposes to the UI.
type Config interface {
	// Section returns a section as JSON and whether the UI changed it.
	Section(name string) (json.RawMessage, bool, error)
	// SetSections changes one or more sections at once; they are validated
	// together, because some depend on each other (a presence sensor needs
	// its area's satellite).
	SetSections(ctx context.Context, sections map[string]json.RawMessage) error
	ResetSection(ctx context.Context, name string) error
}

type Options struct {
	Token    string
	Sections []string
	Config   Config
	Store    *store.Store
	// Entities lists Home Assistant entities for autocompletion.
	Entities func() []Entity
	// CompactProfile rebuilds the profile now.
	CompactProfile func(ctx context.Context) (bool, error)
	// Tasks are Mira's tasks; nil without Home Assistant.
	Tasks Tasks
	// Version is the agent's version, shown in the page header.
	Version string
	Logger  *slog.Logger
}

type Handler struct {
	opts Options
	log  *slog.Logger
	mux  *http.ServeMux
}

func New(opts Options) *Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	h := &Handler{opts: opts, log: opts.Logger.With("component", "ui"), mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /ui", h.page)
	h.mux.HandleFunc("GET /ui/icon.png", h.icon)
	h.mux.HandleFunc("GET /ui/guide/{lang}", h.guide)
	h.mux.HandleFunc("GET /ui/", h.page)
	h.mux.HandleFunc("GET /v1/ui/config", h.auth(h.getConfig))
	h.mux.HandleFunc("PUT /v1/ui/config", h.auth(h.putSections))
	h.mux.HandleFunc("DELETE /v1/ui/config/{section}", h.auth(h.resetSection))
	h.mux.HandleFunc("GET /v1/ui/entities", h.auth(h.entities))
	h.mux.HandleFunc("GET /v1/ui/notices", h.auth(h.notices))
	h.mux.HandleFunc("POST /v1/ui/notices/{id}/ack", h.auth(h.ackNotice))
	h.mux.HandleFunc("GET /v1/ui/memory", h.auth(h.memory))
	h.mux.HandleFunc("DELETE /v1/ui/memory/{id}", h.auth(h.forget))
	h.mux.HandleFunc("POST /v1/ui/memory/compact", h.auth(h.compact))
	h.mux.HandleFunc("GET /v1/ui/tasks", h.auth(h.tasks))
	h.mux.HandleFunc("POST /v1/ui/tasks/{id}/activate", h.auth(h.enableTask(true)))
	h.mux.HandleFunc("POST /v1/ui/tasks/{id}/pause", h.auth(h.enableTask(false)))
	h.mux.HandleFunc("POST /v1/ui/tasks/{id}/try", h.auth(h.tryTask))
	h.mux.HandleFunc("GET /v1/ui/tasks/{id}/log", h.auth(h.taskLog))
	h.mux.HandleFunc("DELETE /v1/ui/tasks/{id}", h.auth(h.deleteTask))
	h.mux.HandleFunc("GET /v1/ui/ha", h.auth(h.getHA))
	h.mux.HandleFunc("PUT /v1/ui/ha", h.auth(h.putHA))
	h.mux.HandleFunc("DELETE /v1/ui/ha", h.auth(h.deleteHA))
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

func (h *Handler) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
	w.Write(bytes.Replace(page, []byte("%%VERSION%%"), []byte(html.EscapeString(h.opts.Version)), 1))
}

// guide serves the user guide as Markdown. Like the page it holds no data,
// so it needs no token.
func (h *Handler) guide(w http.ResponseWriter, r *http.Request) {
	text, err := docs.Guide(r.PathValue("lang"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write(text)
}

func (h *Handler) icon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(icon)
}

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if h.opts.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(h.opts.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong bearer token")
			return
		}
		next(w, r)
	}
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	type section struct {
		Value      json.RawMessage `json:"value"`
		Overridden bool            `json:"overridden"`
		// ManagedBy is "home_assistant" while the integration manages the
		// section; the page shows it read-only then. For the notifier it
		// means only its morning_trigger.
		ManagedBy string `json:"managed_by,omitempty"`
	}
	managed := h.managedByHA(r)
	out := map[string]section{}
	for _, name := range h.opts.Sections {
		v, over, err := h.opts.Config.Section(name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s := section{Value: v, Overridden: over}
		if managed && (slices.Contains(haSections, name) || name == "notifier") {
			s.ManagedBy = "home_assistant"
		}
		out[name] = s
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) putSections(w http.ResponseWriter, r *http.Request) {
	var sections map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&sections); err != nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object of sections: "+err.Error())
		return
	}
	if err := h.keepManaged(r, sections); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := h.opts.Config.SetSections(r.Context(), sections); err != nil {
		// Validation errors are for the user to fix; they say what is wrong.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	names := make([]string, 0, len(sections))
	for n := range sections {
		names = append(names, n)
	}
	h.log.Info("config changed in the web UI", "sections", names)
	h.getConfig(w, r)
}

func (h *Handler) resetSection(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("section")
	if h.managedByHA(r) && (slices.Contains(haSections, name) || name == "notifier") {
		writeError(w, http.StatusConflict, "section "+name+" is managed in Home Assistant")
		return
	}
	if err := h.opts.Config.ResetSection(r.Context(), name); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.log.Info("config section reset to the file", "section", name)
	h.getConfig(w, r)
}

// keepManaged refuses changes to sections Home Assistant manages, and keeps
// the notifier's morning trigger as Home Assistant set it.
func (h *Handler) keepManaged(r *http.Request, sections map[string]json.RawMessage) error {
	if !h.managedByHA(r) {
		return nil
	}
	for _, name := range haSections {
		if _, ok := sections[name]; ok {
			return fmt.Errorf("section %s is managed in Home Assistant", name)
		}
	}
	raw, ok := sections["notifier"]
	if !ok {
		return nil
	}
	var in, current map[string]json.RawMessage
	cur, _, err := h.opts.Config.Section("notifier")
	if err == nil {
		err = json.Unmarshal(cur, &current)
	}
	if err == nil {
		err = json.Unmarshal(raw, &in)
	}
	if err != nil {
		return err
	}
	for _, key := range []string{"morning_trigger", "room_mode", "indicators"} {
		in[key] = current[key]
	}
	sections["notifier"], err = json.Marshal(in)
	return err
}

func (h *Handler) entities(w http.ResponseWriter, _ *http.Request) {
	list := []Entity{}
	if h.opts.Entities != nil {
		if l := h.opts.Entities(); l != nil {
			list = l
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	writeJSON(w, http.StatusOK, list)
}

type noticeJSON struct {
	ID              string `json:"id"`
	State           string `json:"state"`
	Importance      int    `json:"importance"`
	Audience        string `json:"audience"`
	Confidentiality string `json:"confidentiality"`
	Spoken          string `json:"spoken"`
	Details         string `json:"details"`
	Source          string `json:"source"`
	CreatedAt       string `json:"created_at"`
	DeliveredVia    string `json:"delivered_via,omitempty"`
}

func (h *Handler) notices(w http.ResponseWriter, r *http.Request) {
	// The history: every state, newest first, a page at a time; with
	// open=1 only what still waits. A notice past its expiry that nobody
	// heard shows as expired.
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 0)
	f := store.NoticeFilter{IncludeExpired: true, NewestFirst: true, Limit: noticePage + 1, Offset: page * noticePage}
	if r.URL.Query().Get("open") == "1" {
		// Open is what nobody has heard yet.
		f.States, f.IncludeExpired = []string{store.NoticePending}, false
	}
	list, err := h.opts.Store.ListNotices(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]noticeJSON, 0, len(list))
	now := time.Now()
	for _, n := range list {
		state := n.State
		if state == store.NoticePending && !n.ExpiresAt.IsZero() && n.ExpiresAt.Before(now) {
			state = store.NoticeExpired
		}
		out = append(out, noticeJSON{ID: n.ID, State: state, Importance: n.Importance, Audience: n.Audience,
			Confidentiality: n.Confidentiality, Spoken: n.Spoken, Details: n.Details, Source: n.Source,
			CreatedAt: localTime(n.CreatedAt), DeliveredVia: n.DeliveredVia})
	}
	more := len(out) > noticePage
	if more {
		out = out[:noticePage]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "page": page, "more": more})
}

// noticePage is how many notices the UI shows per page.
const noticePage = 20

func (h *Handler) ackNotice(w http.ResponseWriter, r *http.Request) {
	err := h.opts.Store.SetNoticeState(r.Context(), r.PathValue("id"), store.NoticeAcked)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such notice")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"acked": true})
}

func (h *Handler) memory(w http.ResponseWriter, r *http.Request) {
	facts, err := h.opts.Store.AllFacts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile, updated, err := h.opts.Store.Profile(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type fact struct {
		ID      string `json:"id"`
		Text    string `json:"text"`
		Updated string `json:"updated"`
	}
	out := struct {
		Profile        string `json:"profile"`
		ProfileUpdated string `json:"profile_updated,omitempty"`
		Facts          []fact `json:"facts"`
	}{Profile: profile, Facts: []fact{}}
	if !updated.IsZero() {
		out.ProfileUpdated = updated.Format("2006-01-02 15:04")
	}
	for i := len(facts) - 1; i >= 0; i-- { // newest first
		out.Facts = append(out.Facts, fact{ID: facts[i].ID, Text: facts[i].Text, Updated: facts[i].UpdatedAt.Format("2006-01-02 15:04")})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) forget(w http.ResponseWriter, r *http.Request) {
	err := h.opts.Store.DeleteFact(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such fact")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) compact(w http.ResponseWriter, r *http.Request) {
	if h.opts.CompactProfile == nil {
		writeError(w, http.StatusNotImplemented, "profile compaction is off")
		return
	}
	changed, err := h.opts.CompactProfile(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"changed": changed})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
