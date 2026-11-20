// Package ui is the service's small web interface: a single page that edits
// the config sections the service allows (notifier, persons,
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
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/miraassist/mira/docs"
	"github.com/miraassist/mira/internal/access"
	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/memfile"
	"github.com/miraassist/mira/internal/satellite"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/waiting"
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

// Config is what the service exposes to the UI.
type Config interface {
	// Section returns a section as JSON, as Mira runs with it.
	Section(name string) (json.RawMessage, error)
	// SetSections writes one or more sections into mira.yaml and applies
	// them at once; they are validated together, because some depend on
	// each other (a presence sensor needs its area's satellite).
	SetSections(ctx context.Context, sections map[string]json.RawMessage) error
	// SetHASections applies what Home Assistant sends, held in memory only
	// (docs/spec/editing.md, What Home Assistant sends).
	SetHASections(ctx context.Context, sections map[string]json.RawMessage) error
	// HAManaged says the integration manages persons and rooms.
	HAManaged() bool
	// ReleaseHA hands them back to the page.
	ReleaseHA()
}

type Options struct {
	// SetExposed keeps what Home Assistant exposes to Assist, as the
	// integration sends it.
	SetExposed func(ctx context.Context, ids []string) error
	// SetLogins keeps the persons' login names (person entity: login).
	SetLogins func(ctx context.Context, logins map[string]string) error
	Token     string
	Sections  []string
	Config    Config
	Store     *store.Store
	// Entities lists Home Assistant entities for autocompletion.
	Entities func() []Entity
	// CompactProfile rebuilds the profile now.
	CompactProfile func(ctx context.Context) (bool, error)
	// Tasks are Mira's tasks.
	Tasks Tasks
	// Plans are the running plans, for everyone to follow.
	Plans PlanSource
	// Threads confirms or cancels tasks still being planned, and ends
	// plans (docs/spec/plan-end.md).
	Threads Threads
	// Persons lists the configured person keys; each gets a memory card, if
	// empty.
	Persons func() []string
	// People are Home Assistant's persons, each a person for Mira.
	People func() []PersonInfo
	// Voices is the speaker-id service, for enrolling a voice on the page;
	// nil without speaker_id (docs/spec/speaker-id.md).
	Voices Voices
	// VoiceThreshold is the score a person's voice needs to count as
	// theirs: enrolling hides what sounds like someone else.
	VoiceThreshold func(person string) float64
	// PersonOfHAUser maps a Home Assistant user to a person key ("" for
	// none), for the page inside Home Assistant.
	PersonOfHAUser func(ctx context.Context, user string) string
	// AreaName names a Home Assistant area, for facts told at a satellite.
	AreaName func(id string) string
	// Location is the time zone times are shown in.
	Location *time.Location
	// Schedules are the skills' and Mira's own jobs at a time
	// (docs/spec/schedules.md).
	Schedules Schedules
	// Catalog lists the skills, agents and models (docs/spec/overview.md).
	Catalog CatalogSource
	// CheckModel asks a model whether it answers.
	CheckModel ModelCheck
	// Editor changes models, agents and skills (docs/spec/editing.md);
	// without it the pages only read.
	Editor Editor
	// Connections are the skills' logins at services (docs/spec/oauth.md).
	Connections Connections
	// Setup calls a skill's setup tools (docs/spec/skill-programs.md).
	Setup Setup
	// Try runs a skill's Starlark tool for an admin ("Ausprobieren",
	// docs/spec/api-tools.md, decision 11).
	Try Try
	// Tokens are the named access tokens (docs/spec/access-tokens.md); nil
	// without them.
	Tokens Tokens
	// Phone is what waits on the answering machine, for the start page and
	// the messages page (docs/spec/voicemail.md); nil without it.
	Phone func(ctx context.Context) (PhoneNews, error)
	// Version is the service's version, shown in the page header.
	Version string
	Logger  *slog.Logger
}

type Handler struct {
	opts   Options
	log    *slog.Logger
	mux    *api.Mux
	enroll enrolling
}

// pageRoute marks a route as the page's own (docs/spec/openapi.md, decision 4).
func pageRoute(r api.Route) api.Route {
	r.Tag = api.TagPage
	return r
}

// Mux is the page's routes, for the service's mux and its description.
func (h *Handler) Mux() *api.Mux { return h.mux }

func New(opts Options) *Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	h := &Handler{opts: opts, log: opts.Logger.With("component", "ui"), mux: api.NewMux()}
	h.mux.Hidden("GET /ui", "the page's files, not part of the interface", http.HandlerFunc(h.page))
	h.mux.Hidden("GET /ui/icon.png", "the page's files, not part of the interface", http.HandlerFunc(h.icon))
	h.mux.Hidden("GET /ui/guide/{lang}", "the page's files, not part of the interface", http.HandlerFunc(h.guide))
	h.mux.Hidden("GET /ui/guide/img/{name...}", "the page's files, not part of the interface", http.HandlerFunc(h.guideFile))
	h.mux.Hidden("GET /ui/js/{name}", "the page's files, not part of the interface", http.HandlerFunc(h.script))
	h.mux.Hidden("GET /ui/sounds/{name}", "the page's files, not part of the interface", http.HandlerFunc(h.sound))
	h.mux.Hidden("GET /ui/videos/{name}", "the page's files, not part of the interface", http.HandlerFunc(h.video))
	h.mux.Hidden("GET /ui/", "the page's files, not part of the interface", http.HandlerFunc(h.page))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/config", Summary: "The settings' sections as the page edits them", Description: "Each section's value is free-form: it is the section as mira.yaml holds it.", Response: ConfigSections{}, Errors: []int{403, 500}}), h.auth(h.getConfig))
	h.mux.Handle(pageRoute(api.Route{Method: "PUT", Path: "/v1/ui/config", Summary: "Save settings' sections", Description: "The body maps a section's name to its new value; a value is free-form, the section as mira.yaml holds it.", Request: ConfigSectionsEdit{}, Response: ConfigSections{}, Errors: []int{400, 403, 409, 422, 500}}), h.auth(h.putSections))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/entities", Summary: "Home Assistant's entities, for completion", Response: []Entity{}, Errors: []int{403}}), h.auth(h.entities))
	// Everyone sees the house's messages and, with mine=1, their own;
	// admins see all.
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/messages", Summary: "Messages: the house's, the caller's own, all for admins", Response: MessagePage{}, Query: []api.Param{{Name: "page"}, {Name: "mine", Description: "1: only the caller's own"}, {Name: "open", Description: "1: only the unread"}}, Errors: []int{500}}), h.authAnyone(h.messages))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/messages/{id}/ack", Summary: "Mark a message as read", Response: Acked{}, Errors: []int{404, 500}}), h.authAnyone(h.ackMessage))
	// Everyone in Home Assistant may see and change their own memory;
	// the rest is for admins (and the operator with the token).
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/memory", Summary: "The memories the caller may see", Response: Memories{}, Errors: []int{500}}), h.authAnyone(h.memory))
	h.mux.Handle(pageRoute(api.Route{Method: "DELETE", Path: "/v1/ui/memory/{id}", Summary: "Forget a fact", Response: Deleted{}, Errors: []int{404, 500}}), h.authAnyone(h.forget))
	h.mux.Handle(pageRoute(api.Route{Method: "PUT", Path: "/v1/ui/memory/{id}", Summary: "Change a fact", Request: FactEdit{}, Response: Saved{}, Errors: []int{400, 404, 500}}), h.authAnyone(h.editFact))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/memory/compact", Summary: "Compact the persons' profiles now", Response: Compacted{}, Errors: []int{403, 501, 502}}), h.auth(h.compact))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/memory/export", Summary: "The memory as text", Response: MemoryExport{}, Errors: []int{500}}), h.authAnyone(h.exportMemory))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/memory/import", Summary: "Import facts from text (preview, then apply)", Description: "Answers either a problem, or the changes and whether they were applied.", Request: MemoryImport{}, Response: MemoryImportResult{}, Errors: []int{400, 500}}), h.authAnyone(h.importMemory))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/plans", Summary: "The open plans", Response: PlanList{}, Errors: []int{500}}), h.authAnyone(h.plans))
	h.mux.Handle(pageRoute(api.Route{Method: "DELETE", Path: "/v1/ui/plans/{id}", Summary: "End a plan", Response: Ended{}, Errors: []int{404, 500}}), h.authAnyone(h.endPlan))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/me", Summary: "Who looks at the page", Response: Me{}}), h.authAnyone(h.me))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/voice", Summary: "The voices known to the speaker id", Response: VoiceList{}, Errors: []int{403, 502}}), h.auth(h.voices))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/voice/{person}", Summary: "A person's voice", Response: VoiceOf{}, Errors: []int{403, 404, 502}}), h.authAnyone(h.voice))
	h.mux.Handle(pageRoute(api.Route{Method: "DELETE", Path: "/v1/ui/voice/{person}", Summary: "Forget a person's voice", Response: OK{}, Errors: []int{403, 404, 502}}), h.authAnyone(h.forgetVoice))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/voice/{person}/enroll", Summary: "Start enrolling a voice", Response: EnrollStarted{}, Query: []api.Param{{Name: "lang", Description: "The sentences' language"}}, Errors: []int{403, 404, 502}}), h.authAnyone(h.enrollStart))
	h.mux.Handle(pageRoute(api.Route{Method: "DELETE", Path: "/v1/ui/voice/{person}/enroll", Summary: "Stop enrolling a voice", Response: OK{}, Errors: []int{403, 404}}), h.authAnyone(h.enrollStop))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/voice/{person}/heard", Summary: "What the speaker id heard while enrolling", Response: HeardList{}, Errors: []int{403, 404, 409, 502}}), h.authAnyone(h.heard))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/voice/{person}/assign", Summary: "Assign a heard sample to a person", Request: VoiceAssign{}, Response: OK{}, Errors: []int{400, 403, 404, 409, 502}}), h.authAnyone(h.assign))
	// The stored agent runs, for debugging (agents.go).
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/catalog", Summary: "The skills, agents and models", Response: Catalog{}, Errors: []int{403}}), h.auth(h.catalog))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/skills/{skill}/tools/{tool}/try", Summary: "Try a skill's Starlark tool and see what went over the wire (admins only)", Request: TryCall{}, Response: TryResult{}, Errors: []int{400, 403, 404, 503}}), h.auth(h.tryTool))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/skills/{skill}/setup/{tool}", Summary: "Call a skill's setup tool (admins only, never a model)", Request: SetupCall{}, Response: SetupResult{}, Errors: []int{400, 403, 404, 502, 503}}), h.auth(h.callSetup))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/schedules", Summary: "The skills' and Mira's jobs at a time", Response: ScheduleList{}, Errors: []int{403, 500, 503}}), h.auth(h.schedules))
	h.mux.Handle(pageRoute(api.Route{Method: "PUT", Path: "/v1/ui/schedules/{skill}/{name}", Summary: "Change a job's time", Request: ScheduleEdit{}, Response: Saved{}, Errors: []int{400, 403, 503}}), h.auth(h.changeSchedule))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/schedules/{skill}/{name}/run", Summary: "Run a job now", Response: Started{}, Status: http.StatusAccepted, Errors: []int{400, 403, 409, 503}}), h.auth(h.runSchedule))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/models/{name}/check", Summary: "Ask a model whether it answers", Response: CheckResult{}, Errors: []int{403, 404}}), h.auth(h.checkModel))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/agents", Summary: "The stored agent runs", Response: RunList{}, Errors: []int{403, 500}}), h.auth(h.runs))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/agents/{id}", Summary: "One agent run", Response: RunDetail{}, Errors: []int{403, 404, 500}}), h.auth(h.run))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/tasks", Summary: "The tasks and the ones being planned", Response: TaskList{}, Errors: []int{403, 503}}), h.auth(h.tasks))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/tasks/{id}/activate", Summary: "Turn a task on", Response: TaskOn{}, Errors: []int{403, 404, 502, 503}}), h.auth(h.enableTask(true)))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/tasks/{id}/pause", Summary: "Pause a task", Response: TaskOn{}, Errors: []int{403, 404, 502, 503}}), h.auth(h.enableTask(false)))
	h.mux.Handle(pageRoute(api.Route{Method: "POST", Path: "/v1/ui/tasks/{id}/try", Summary: "Try a task now", Response: TaskTried{}, Errors: []int{403, 404, 502, 503}}), h.auth(h.tryTask))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/tasks/{id}/log", Summary: "A task's assessments", Response: []assessmentJSON{}, Errors: []int{403, 500}}), h.auth(h.taskLog))
	h.mux.Handle(pageRoute(api.Route{Method: "DELETE", Path: "/v1/ui/tasks/{id}", Summary: "Delete a task", Response: Deleted{}, Errors: []int{403, 404, 502, 503}}), h.auth(h.deleteTask))
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/ha", Summary: "Mira's connection to Home Assistant as the page shows it", Response: haConfig{}, Errors: []int{403, 500}}), h.auth(h.getHA))
	h.mux.Handle(pageRoute(api.Route{Method: "PUT", Path: "/v1/ui/ha", Summary: "Change the connection to Home Assistant", Request: haConfig{}, Response: haConfig{}, Errors: []int{400, 403, 422, 500}}), h.auth(h.putHA))
	h.mux.Handle(pageRoute(api.Route{Method: "DELETE", Path: "/v1/ui/ha", Summary: "Hand the sections Home Assistant set back to the page", Response: HAManaged{}, Errors: []int{403}}), h.auth(h.deleteHA))
	h.editRoutes()
	h.connectionRoutes()
	h.tokenRoutes()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Recordings of the guide come from their own server over https, and a
	// plan from a recipe shows the recipe's picture from its site.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'unsafe-inline'; media-src 'self' https:; img-src 'self' https:")
	// Inside Home Assistant the integration says its version, for the start
	// page's footer; the page is open, so only a version's characters pass.
	integration := r.Header.Get(HeaderIntegration)
	if !versionText.MatchString(integration) {
		integration = ""
	}
	w.Write(bytes.ReplaceAll(bytes.ReplaceAll(page, []byte("%%VERSION%%"), []byte(html.EscapeString(h.opts.Version))),
		[]byte("%%INTEGRATION%%"), []byte(integration)))
}

// versionText is what a version may look like (0.11.0, 0.12.0b1).
var versionText = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,32}$`)

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

// sound serves Mira's sounds to the satellites (docs/spec/satellite.md),
// through the integration's open page; like the page they need no token.
func (h *Handler) sound(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutSuffix(r.PathValue("name"), ".mp3")
	data, found := satellite.File(name)
	if !ok || !found {
		writeError(w, http.StatusNotFound, "no such sound")
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "max-age=86400")
	http.ServeContent(w, r, name+".mp3", time.Time{}, bytes.NewReader(data))
}

// video serves the waiting videos to the satellites
// (docs/spec/waiting-indicator.md), as the sounds.
func (h *Handler) video(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutSuffix(r.PathValue("name"), ".mp4")
	data, found := waiting.Video(name)
	if !ok || !found {
		writeError(w, http.StatusNotFound, "no such video")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "max-age=86400")
	http.ServeContent(w, r, name+".mp4", time.Time{}, bytes.NewReader(data))
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

// ConfigSection is one section of the settings as the page edits it.
type ConfigSection struct {
	// Value is the section as mira.yaml holds it: free-form.
	Value json.RawMessage `json:"value"`
	// People are Home Assistant's persons, with the persons section:
	// its value then holds only Mira's settings per person.
	People []PersonInfo `json:"people,omitempty"`
	// ManagedBy is "home_assistant" while the integration manages the
	// section; the page shows it read-only then. For the notifier it
	// means only its morning_trigger.
	ManagedBy string `json:"managed_by,omitempty"`
}

// ConfigSections are the settings' sections by name.
type ConfigSections map[string]ConfigSection

// ConfigSectionsEdit are the sections the page saves, by name; each value
// is free-form, the section as mira.yaml holds it.
type ConfigSectionsEdit map[string]json.RawMessage

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	managed := h.opts.Config.HAManaged()
	out := ConfigSections{}
	for _, name := range h.opts.Sections {
		v, err := h.opts.Config.Section(name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s := ConfigSection{Value: v}
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
	var sections ConfigSectionsEdit
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

// keepManaged refuses changes to sections Home Assistant manages, and keeps
// the notifier's morning trigger as Home Assistant set it.
func (h *Handler) keepManaged(r *http.Request, sections map[string]json.RawMessage) error {
	if !h.opts.Config.HAManaged() {
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
	cur, err := h.opts.Config.Section("notifier")
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

// MessagePage is a page of messages; Phone is what waits on the answering
// machine, on the first page while something waits there.
type MessagePage struct {
	Items []messageJSON `json:"items"`
	More  bool          `json:"more"`
	Page  int           `json:"page"`
	Phone *PhoneNews    `json:"phone,omitempty"`
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	// The history: every state, newest first, a page at a time; with
	// open=1 only what still waits. A message past its expiry that nobody
	// heard shows as expired.
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(page, 0)
	f := store.MessageFilter{IncludeExpired: true, NewestFirst: true, Limit: messagePage + 1, Offset: page * messagePage}
	// The house's and a person's own are apart, as their memories are:
	// mine=1 is the viewer's own, for their card; without it someone who is
	// no admin sees the house's.
	v := h.viewer(r)
	switch {
	case r.URL.Query().Get("mine") == "1":
		if v.person == "" {
			writeJSON(w, http.StatusOK, MessagePage{Items: []messageJSON{}, Page: page, More: false})
			return
		}
		f.Receiver = v.person
	case v.limited && !v.admin:
		f.Receiver = store.ReceiverHouse
	}
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
	resp := MessagePage{Items: out, Page: page, More: more}
	// The answering machine's beside the house's, while something waits there.
	if h.opts.Phone != nil && page == 0 {
		if news, err := h.opts.Phone(r.Context()); err == nil && news.Messages+news.Missed > 0 {
			// A link the page opens: only to a web interface.
			if !strings.HasPrefix(news.URL, "http://") && !strings.HasPrefix(news.URL, "https://") {
				news.URL = ""
			}
			resp.Phone = &news
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// PhoneNews is what waits on the answering machine and where to play it.
type PhoneNews struct {
	Messages int `json:"messages"`
	Missed   int `json:"missed"`
	// URL is the FRITZ!Box's web interface; empty when Home Assistant does
	// not know it.
	URL string `json:"url,omitempty"`
}

// messagePage is how many messages the UI shows per page.
const messagePage = 20

func (h *Handler) ackMessage(w http.ResponseWriter, r *http.Request) {
	// Someone who is no admin marks only their own messages done.
	if v := h.viewer(r); v.limited && !v.admin {
		n, err := h.opts.Store.GetMessage(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) || (err == nil && (v.person == "" || n.Receiver != v.person)) {
			writeError(w, http.StatusNotFound, "no such message")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	err := h.opts.Store.SetMessageState(r.Context(), r.PathValue("id"), store.MessageAcked)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such message")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Acked{Acked: true})
}

// Headers the Home Assistant integration's proxy sets on the page's calls:
// the signed-in HA user and whether they are an admin. They come with the
// service's token, so only the integration can set them.
const (
	HeaderHAUser  = "X-Mira-HA-User"
	HeaderHAAdmin = "X-Mira-HA-Admin"
)

// HeaderIntegration is the integration's version, on everything its proxy
// passes on, the page too (docs/spec/versions.md). Older integrations do
// not send it.
const HeaderIntegration = "X-Mira-Integration"

// viewer is who looks at the page. Inside Home Assistant everyone, admins
// too, sees the house's memory and their own only; with the service's token
// alone (the page on its own) the operator sees all.
type viewer struct {
	limited bool
	person  string
	admin   bool
	// partner is a partner token's call: an admin who sees only the house's
	// memory (docs/spec/access-tokens.md, decision 17).
	partner bool
}

func (h *Handler) viewer(r *http.Request) viewer {
	if c, ok := access.CallerFrom(r.Context()); ok && c.Kind == access.KindPartner {
		return viewer{limited: true, admin: true, partner: true}
	}
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

// MemoryFact is a fact as the page shows it.
type MemoryFact struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Updated string `json:"updated"`
	// Author is a person key or an area's name; Told is set when the
	// fact was edited after it was told.
	Author string `json:"author,omitempty"`
	Told   string `json:"told,omitempty"`
}

// MemoryOf is one owner's memory: the house's or a person's.
type MemoryOf struct {
	Owner          string       `json:"owner"`
	Name           string       `json:"name,omitempty"`
	Profile        string       `json:"profile"`
	ProfileUpdated string       `json:"profile_updated,omitempty"`
	Facts          []MemoryFact `json:"facts"`
}

// Memories are the memories the viewer may see.
type Memories struct {
	Memories   []MemoryOf `json:"memories"`
	CanCompact bool       `json:"can_compact"`
	// Unknown is set inside Home Assistant when the HA user belongs to
	// no person, so only the house's memory shows.
	Unknown bool `json:"unknown_person,omitempty"`
	// You is the viewer's own person inside Home Assistant.
	You string `json:"you,omitempty"`
}

// memory lists the memories by owner: the house first, then each person
// with facts or a profile.
func (h *Handler) memory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := h.viewer(r)
	owners, err := h.memoryOwners(ctx, v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := Memories{Memories: []MemoryOf{}, CanCompact: h.opts.CompactProfile != nil && v.admin, Unknown: v.limited && v.person == "" && !v.partner, You: v.person}
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
		m := MemoryOf{Owner: owner, Profile: profile, Facts: []MemoryFact{}}
		if owner != store.OwnerHouse {
			m.Name = h.personName(owner)
		}
		if !updated.IsZero() {
			m.ProfileUpdated = updated.In(h.opts.Location).Format(stamp)
		}
		for i := len(facts) - 1; i >= 0; i-- { // newest first
			f := facts[i]
			item := MemoryFact{ID: f.ID, Text: f.Text, Updated: f.UpdatedAt.In(h.opts.Location).Format(stamp), Author: h.author(f.Author)}
			if f.UpdatedAt.Sub(f.CreatedAt) > time.Minute {
				item.Told = f.CreatedAt.In(h.opts.Location).Format(stamp)
			}
			m.Facts = append(m.Facts, item)
		}
		out.Memories = append(out.Memories, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// memoryOwners are the memories the viewer sees on the page: the house
// first, then each person with facts or configured.
func (h *Handler) memoryOwners(ctx context.Context, v viewer) ([]string, error) {
	if v.limited {
		return v.owners(), nil
	}
	owners, err := h.opts.Store.FactOwners(ctx)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(owners, store.OwnerHouse) {
		owners = append([]string{store.OwnerHouse}, owners...)
	}
	if h.opts.Persons != nil {
		persons := h.opts.Persons()
		slices.Sort(persons)
		for _, p := range persons {
			if !slices.Contains(owners, p) {
				owners = append(owners, p)
			}
		}
	}
	return owners, nil
}

// fileOwners names owners for the memory file.
func (h *Handler) fileOwners(keys []string) []memfile.Owner {
	out := make([]memfile.Owner, 0, len(keys))
	for _, k := range keys {
		o := memfile.Owner{Key: k}
		if k != store.OwnerHouse {
			o.Name = h.personName(k)
		}
		out = append(out, o)
	}
	return out
}

// MemoryExport is the memory as a file's text, and the file's name.
type MemoryExport struct {
	File string `json:"file"`
	Text string `json:"text"`
}

// exportMemory is the memories the viewer sees, as a file
// (docs/spec/memory-file.md).
func (h *Handler) exportMemory(w http.ResponseWriter, r *http.Request) {
	owners, err := h.memoryOwners(r.Context(), h.viewer(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	text, err := memfile.Write(r.Context(), h.opts.Store, h.fileOwners(owners))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, MemoryExport{Text: text, File: "mira-gedaechtnis.md"})
}

// MemoryImport is a memory file's text, and whether to apply it.
type MemoryImport struct {
	Text  string `json:"text"`
	Apply bool   `json:"apply"`
}

// MemoryImportResult is either a problem with the file, or the changes it
// makes and whether they were applied.
type MemoryImportResult struct {
	Changes *[]ImportChange `json:"changes,omitempty"`
	Applied *bool           `json:"applied,omitempty"`
	Problem *ImportProblem  `json:"problem,omitempty"`
}

// ImportProblem is why a file cannot be imported.
type ImportProblem struct {
	Kind    string `json:"kind"` // line, unknown, not_allowed
	Heading string `json:"heading,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// ImportChange is what a file changes in one owner's memory.
type ImportChange struct {
	Owner  string   `json:"owner"`
	Name   string   `json:"name,omitempty"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
	Keep   int      `json:"keep"`
}

// importMemory says what a file would change and, with apply, changes it:
// each owner with a section then has exactly its facts. A file that cannot
// be imported comes back as a problem the page puts into words.
func (h *Handler) importMemory(w http.ResponseWriter, r *http.Request) {
	var req MemoryImport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be {"text": "...", "apply": false}`)
		return
	}
	ctx := r.Context()
	v := h.viewer(r)
	allowed, err := h.memoryOwners(ctx, v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	known := []string{store.OwnerHouse}
	for _, p := range h.people() {
		known = append(known, p.Key)
	}
	if more, err := h.memoryOwners(ctx, viewer{admin: true}); err == nil {
		for _, k := range more {
			if !slices.Contains(known, k) {
				known = append(known, k)
			}
		}
	}
	if !v.limited {
		allowed = known
	}
	sections, err := memfile.Parse(req.Text)
	var le memfile.LineError
	if errors.As(err, &le) {
		writeJSON(w, http.StatusOK, MemoryImportResult{Problem: &ImportProblem{Kind: "line", Line: le.Line}})
		return
	}
	changes, err := memfile.Plan(ctx, h.opts.Store, sections, h.fileOwners(known), h.fileOwners(allowed))
	var he memfile.HeadingError
	if errors.As(err, &he) {
		kind := "not_allowed"
		if he.Unknown {
			kind = "unknown"
		}
		writeJSON(w, http.StatusOK, MemoryImportResult{Problem: &ImportProblem{Kind: kind, Heading: he.Heading}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, applied := []ImportChange{}, false
	for _, c := range changes {
		ch := ImportChange{Owner: c.Owner.Key, Name: c.Owner.Name, Add: []string{}, Remove: []string{}, Keep: len(c.Keep)}
		for _, l := range c.Add {
			ch.Add = append(ch.Add, l.Text)
		}
		for _, f := range c.Remove {
			ch.Remove = append(ch.Remove, f.Text)
		}
		list = append(list, ch)
	}
	if req.Apply {
		if err := memfile.Apply(ctx, h.opts.Store, changes, v.person); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		applied = true
	}
	writeJSON(w, http.StatusOK, MemoryImportResult{Changes: &list, Applied: &applied})
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
	writeJSON(w, http.StatusOK, Deleted{Deleted: true})
}

// FactEdit is a fact's new text.
type FactEdit struct {
	Text string `json:"text"`
}

// editFact changes a fact's text; tags and source stay.
func (h *Handler) editFact(w http.ResponseWriter, r *http.Request) {
	var req FactEdit
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
	writeJSON(w, http.StatusOK, Saved{Saved: true})
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
	writeJSON(w, http.StatusOK, Compacted{Changed: changed})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Acknowledgements: an answer of one key that says what was done.
type (
	// Acked: a message was marked as read.
	Acked struct {
		Acked bool `json:"acked"`
	}
	// Deleted: a fact, a task or a token is gone.
	Deleted struct {
		Deleted bool `json:"deleted"`
	}
	// Saved: a change was saved.
	Saved struct {
		Saved bool `json:"saved"`
	}
	// Compacted says whether compacting changed a profile.
	Compacted struct {
		Changed bool `json:"changed"`
	}
	// Ended: a plan was ended.
	Ended struct {
		Ended bool `json:"ended"`
	}
	// OK: a voice's change was done.
	OK struct {
		OK bool `json:"ok"`
	}
	// Started: a job runs.
	Started struct {
		Started bool `json:"started"`
	}
	// TaskOn says whether a task is on now.
	TaskOn struct {
		On bool `json:"on"`
	}
	// HAManaged says whether Home Assistant still manages the sections.
	HAManaged struct {
		Managed bool `json:"managed"`
	}
	// Restarting: the service starts anew.
	Restarting struct {
		Restarting bool `json:"restarting"`
	}
)

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readJSON decodes a request body of at most 64 KB into v.
func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v)
}
