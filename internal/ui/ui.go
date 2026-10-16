// Package ui is the agent's small web interface: a single page that edits
// the config sections the agent allows (notifier, persons,
// satellites), shows open messages and the memory.
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
	"mime"
	"net/http"
	"path"
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
	// Tasks are Mira's tasks.
	Tasks Tasks
	// Threads confirms or cancels tasks still being planned.
	Threads Threads
	// Persons lists the configured person keys; each gets a memory card, if
	// empty.
	Persons func() []string
	// People are Home Assistant's persons, each a person for Mira.
	People func() []PersonInfo
	// PersonOfHAUser maps a Home Assistant user to a person key ("" for
	// none), for the page inside Home Assistant.
	PersonOfHAUser func(ctx context.Context, user string) string
	// AreaName names a Home Assistant area, for facts told at a satellite.
	AreaName func(id string) string
	// Location is the time zone times are shown in.
	Location *time.Location
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
	if opts.Location == nil {
		opts.Location = time.Local
	}
	h := &Handler{opts: opts, log: opts.Logger.With("component", "ui"), mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /ui", h.page)
	h.mux.HandleFunc("GET /ui/icon.png", h.icon)
	h.mux.HandleFunc("GET /ui/guide/{lang}", h.guide)
	h.mux.HandleFunc("GET /ui/guide/img/{name}", h.guideFile)
	h.mux.HandleFunc("GET /ui/", h.page)
	h.mux.HandleFunc("GET /v1/ui/config", h.auth(h.getConfig))
	h.mux.HandleFunc("PUT /v1/ui/config", h.auth(h.putSections))
	h.mux.HandleFunc("DELETE /v1/ui/config/{section}", h.auth(h.resetSection))
	h.mux.HandleFunc("GET /v1/ui/entities", h.auth(h.entities))
	h.mux.HandleFunc("GET /v1/ui/messages", h.auth(h.messages))
	h.mux.HandleFunc("POST /v1/ui/messages/{id}/ack", h.auth(h.ackMessage))
	// Everyone in Home Assistant may see and change their own memory;
	// the rest is for admins (and the operator with the token).
	h.mux.HandleFunc("GET /v1/ui/memory", h.authAnyone(h.memory))
	h.mux.HandleFunc("DELETE /v1/ui/memory/{id}", h.authAnyone(h.forget))
	h.mux.HandleFunc("PUT /v1/ui/memory/{id}", h.authAnyone(h.editFact))
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

// guideFile serves the guide's pictures and recordings; like the guide it
// needs no token. Recordings come in ranges: Safari plays a video only
// from a server that answers Range requests.
func (h *Handler) guideFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	data, err := docs.GuideFile(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		w.Header().Set("Content-Type", t)
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (h *Handler) icon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Write(icon)
}

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return h.authAnyone(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HeaderHAUser) != "" && r.Header.Get(HeaderHAAdmin) != "1" {
			writeError(w, http.StatusForbidden, "for Home Assistant admins only")
			return
		}
		next(w, r)
	})
}

// authAnyone checks the token only: for what every Home Assistant user may
// do with their own data.
func (h *Handler) authAnyone(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if h.opts.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(h.opts.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong bearer token")
			return
		}
		next(w, r)
	}
}

// PersonInfo is one of Home Assistant's persons.
type PersonInfo struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Entity string `json:"entity"`
}

func (h *Handler) people() []PersonInfo {
	if h.opts.People == nil {
		return nil
	}
	list := h.opts.People()
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// personName names a person key: Home Assistant's name, else the key.
func (h *Handler) personName(key string) string {
	for _, p := range h.people() {
		if p.Key == key && p.Name != "" {
			return p.Name
		}
	}
	return key
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	type section struct {
		Value      json.RawMessage `json:"value"`
		Overridden bool            `json:"overridden"`
		// People are Home Assistant's persons, with the persons section:
		// its value then holds only Mira's settings per person.
		People []PersonInfo `json:"people,omitempty"`
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
		if name == "persons" {
			s.People = h.people()
		}
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

type messageJSON struct {
	ID              string `json:"id"`
	State           string `json:"state"`
	Importance      int    `json:"importance"`
	Receiver        string `json:"receiver"`
	Confidentiality string `json:"confidentiality"`
	Spoken          string `json:"spoken"`
	Details         string `json:"details"`
	Source          string `json:"source"`
	CreatedAt       string `json:"created_at"`
	DeliveredVia    string `json:"delivered_via,omitempty"`
	// DeliverAt is when a scheduled message (a reminder) goes out.
	DeliverAt string `json:"deliver_at,omitempty"`
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	// The history: every state, newest first, a page at a time; with
	// open=1 only what still waits. A message past its expiry that nobody
	// heard shows as expired.
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 0)
	f := store.MessageFilter{IncludeExpired: true, NewestFirst: true, Limit: messagePage + 1, Offset: page * messagePage}
	if r.URL.Query().Get("open") == "1" {
		// Open is what nobody has heard yet.
		f.States, f.IncludeExpired = []string{store.MessagePending}, false
	}
	list, err := h.opts.Store.ListMessages(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]messageJSON, 0, len(list))
	now := time.Now()
	for _, n := range list {
		state := n.State
		if state == store.MessagePending && !n.ExpiresAt.IsZero() && n.ExpiresAt.Before(now) {
			state = store.MessageExpired
		}
		out = append(out, messageJSON{ID: n.ID, State: state, Importance: n.Importance, Receiver: n.Receiver,
			Confidentiality: n.Confidentiality, Spoken: n.Spoken, Details: n.Details, Source: n.Source,
			CreatedAt: localTime(n.CreatedAt), DeliveredVia: n.DeliveredVia})
		if !n.DeliverAt.IsZero() {
			out[len(out)-1].DeliverAt = localTime(n.DeliverAt)
		}
	}
	more := len(out) > messagePage
	if more {
		out = out[:messagePage]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "page": page, "more": more})
}

// messagePage is how many messages the UI shows per page.
const messagePage = 20

func (h *Handler) ackMessage(w http.ResponseWriter, r *http.Request) {
	err := h.opts.Store.SetMessageState(r.Context(), r.PathValue("id"), store.MessageAcked)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such message")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"acked": true})
}

// Headers the Home Assistant integration's proxy sets on the page's calls:
// the signed-in HA user and whether they are an admin. They come with the
// agent's token, so only the integration can set them.
const (
	HeaderHAUser  = "X-Mira-HA-User"
	HeaderHAAdmin = "X-Mira-HA-Admin"
)

// viewer is who looks at the page. Inside Home Assistant everyone, admins
// too, sees the house's memory and their own only; with the agent's token
// alone (the page on its own) the operator sees all.
type viewer struct {
	limited bool
	person  string
	admin   bool
}

func (h *Handler) viewer(r *http.Request) viewer {
	user := r.Header.Get(HeaderHAUser)
	if user == "" {
		return viewer{admin: true}
	}
	v := viewer{limited: true, admin: r.Header.Get(HeaderHAAdmin) == "1"}
	if h.opts.PersonOfHAUser != nil {
		v.person = h.opts.PersonOfHAUser(r.Context(), user)
	}
	return v
}

// owners are the memories the viewer may see and change.
func (v viewer) owners() []string {
	if v.person == "" {
		return []string{store.OwnerHouse}
	}
	return []string{store.OwnerHouse, v.person}
}

// mayChange says whether the viewer may edit or forget the fact.
func (h *Handler) mayChange(w http.ResponseWriter, r *http.Request) bool {
	v := h.viewer(r)
	if !v.limited {
		return true
	}
	f, err := h.opts.Store.GetFact(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !slices.Contains(v.owners(), f.Owner)) {
		writeError(w, http.StatusNotFound, "no such fact")
		return false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

// memory lists the memories by owner: the house first, then each person
// with facts or a profile.
func (h *Handler) memory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := h.viewer(r)
	owners, err := h.opts.Store.FactOwners(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !slices.Contains(owners, store.OwnerHouse) {
		owners = append([]string{store.OwnerHouse}, owners...)
	}
	if v.limited {
		owners = v.owners()
	} else if h.opts.Persons != nil {
		persons := h.opts.Persons()
		slices.Sort(persons)
		for _, p := range persons {
			if !slices.Contains(owners, p) {
				owners = append(owners, p)
			}
		}
	}
	type fact struct {
		ID      string `json:"id"`
		Text    string `json:"text"`
		Updated string `json:"updated"`
		// Author is a person key or an area's name; Told is set when the
		// fact was edited after it was told.
		Author string `json:"author,omitempty"`
		Told   string `json:"told,omitempty"`
	}
	type memory struct {
		Owner          string `json:"owner"`
		Name           string `json:"name,omitempty"`
		Profile        string `json:"profile"`
		ProfileUpdated string `json:"profile_updated,omitempty"`
		Facts          []fact `json:"facts"`
	}
	out := struct {
		Memories   []memory `json:"memories"`
		CanCompact bool     `json:"can_compact"`
		// Unknown is set inside Home Assistant when the HA user belongs to
		// no person, so only the house's memory shows.
		Unknown bool `json:"unknown_person,omitempty"`
		// You is the viewer's own person inside Home Assistant.
		You string `json:"you,omitempty"`
	}{Memories: []memory{}, CanCompact: h.opts.CompactProfile != nil && v.admin, Unknown: v.limited && v.person == "", You: v.person}
	const stamp = "2006-01-02 15:04"
	for _, owner := range owners {
		facts, err := h.opts.Store.AllFacts(ctx, owner)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		profile, updated, err := h.opts.Store.Profile(ctx, owner)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		m := memory{Owner: owner, Profile: profile, Facts: []fact{}}
		if owner != store.OwnerHouse {
			m.Name = h.personName(owner)
		}
		if !updated.IsZero() {
			m.ProfileUpdated = updated.In(h.opts.Location).Format(stamp)
		}
		for i := len(facts) - 1; i >= 0; i-- { // newest first
			f := facts[i]
			item := fact{ID: f.ID, Text: f.Text, Updated: f.UpdatedAt.In(h.opts.Location).Format(stamp), Author: h.author(f.Author)}
			if f.UpdatedAt.Sub(f.CreatedAt) > time.Minute {
				item.Told = f.CreatedAt.In(h.opts.Location).Format(stamp)
			}
			m.Facts = append(m.Facts, item)
		}
		out.Memories = append(out.Memories, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// author renders who told a fact: the person's name, or the area's name.
func (h *Handler) author(a string) string {
	if id, ok := strings.CutPrefix(a, "area:"); ok {
		if h.opts.AreaName != nil {
			if name := h.opts.AreaName(id); name != "" {
				return name
			}
		}
		return id
	}
	if a == "" {
		return ""
	}
	return h.personName(a)
}

func (h *Handler) forget(w http.ResponseWriter, r *http.Request) {
	if !h.mayChange(w, r) {
		return
	}
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

// editFact changes a fact's text; tags and source stay.
func (h *Handler) editFact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, `body must be {"text": "..."}`)
		return
	}
	if !h.mayChange(w, r) {
		return
	}
	f, err := h.opts.Store.GetFact(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such fact")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	f.Text = strings.TrimSpace(req.Text)
	if _, err := h.opts.Store.SaveFact(r.Context(), f); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
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
