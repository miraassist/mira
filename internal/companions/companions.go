// Package companions keeps the companion services that registered themselves with Mira and passes their page's
// calls on to them (docs/spec/companions.md).
package companions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miraassist/mira/internal/access"
	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/store"
)

// Limits of decisions 6 and 7.
const (
	maxTitle = 32
	maxBody  = 1 << 20
	timeout  = 10 * time.Second
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Companion is a registration as the page sees it: no token.
type Companion struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// Registration is the body of PUT /v1/companions/{name}.
type Registration struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Errors is a 400's answer: what is wrong per field.
type Errors struct {
	Errors map[string]string `json:"errors"`
}

// Accepted is the answer of a reload.
type Accepted struct {
	Restarting bool `json:"restarting"`
}

// Handler keeps the companions.
type Handler struct {
	Store   *store.Store
	Log     *slog.Logger
	Restart func() // the service's restart, after the answer is written (decision 10)
	Client  *http.Client
}

func (h *Handler) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: timeout}
}

// Register adds the companions' own routes: registering and reloading (decisions 5, 6, 10).
func (h *Handler) Register(mux *api.Mux) {
	const tag = api.TagAccess
	mux.Handle(api.Route{Method: http.MethodPut, Path: "/v1/companions/{name}", Tag: tag,
		Summary: "A companion service registers itself (with its partner token)", Request: Registration{},
		Status: http.StatusNoContent, Errors: []int{400, 403, 409, 500}}, h.put)
	mux.Handle(api.Route{Method: http.MethodDelete, Path: "/v1/companions/{name}", Tag: tag,
		Summary: "A companion service removes its registration", Status: http.StatusNoContent,
		Errors: []int{403, 404, 500}}, h.delete)
	mux.Handle(api.Route{Method: http.MethodPost, Path: "/v1/skills/reload", Tag: api.TagSkills,
		Summary: "Read the skills again: the service restarts (docs/spec/companions.md)", Response: Accepted{},
		Status: http.StatusAccepted, Errors: []int{401}}, h.reload)
}

func partner(r *http.Request) (access.Caller, bool) {
	c, ok := access.CallerFrom(r.Context())
	return c, ok && c.Kind == access.KindPartner
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	c, ok := partner(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a partner token registers a companion"})
		return
	}
	name := r.PathValue("name")
	var reg Registration
	errs := map[string]string{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&reg); err != nil {
		errs["body"] = "not a registration: " + err.Error()
	}
	if !nameRE.MatchString(name) {
		errs["name"] = fmt.Sprintf("%q is not a name like pro (lower case, digits, -, at most 32)", name)
	}
	if reg.Title == "" || utf8.RuneCountInString(reg.Title) > maxTitle {
		errs["title"] = "a title of 1 to 32 characters"
	}
	if u, err := url.Parse(reg.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs["url"] = fmt.Sprintf("%q is not an address like http://host:8792", reg.URL)
	}
	if reg.Token == "" {
		errs["token"] = "a token is needed"
	}
	if len(errs) > 0 {
		h.Log.Warn("a companion's registration refused", "name", name, "token", c.Name, "errors", errs)
		writeJSON(w, http.StatusBadRequest, Errors{Errors: errs})
		return
	}
	err := h.Store.PutCompanion(r.Context(), store.Companion{Name: name, Title: reg.Title, URL: strings.TrimRight(reg.URL, "/"),
		Token: reg.Token, TokenID: c.TokenID})
	switch {
	case errors.Is(err, store.ErrCompanionTaken):
		h.Log.Warn("a companion's name is another token's", "name", name, "token", c.Name)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "companion " + name + " is registered by another token"})
	case err != nil:
		h.Log.Error("a companion could not be kept", "name", name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the registration could not be kept"})
	default:
		h.Log.Info("companion registered", "name", name, "url", reg.URL, "token", c.Name)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	c, ok := partner(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a partner token removes its companion"})
		return
	}
	name := r.PathValue("name")
	gone, err := h.Store.DeleteCompanion(r.Context(), name, c.TokenID)
	switch {
	case err != nil:
		h.Log.Error("a companion could not be removed", "name", name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the registration could not be removed"})
	case !gone:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no companion " + name + " of this token"})
	default:
		h.Log.Info("companion removed", "name", name, "token", c.Name)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) reload(w http.ResponseWriter, r *http.Request) {
	// A partner token or the integration's: the access check marks both; anyone else is no caller here.
	c, ok := access.CallerFrom(r.Context())
	if !ok || (c.Kind != access.KindPartner && c.Kind != access.KindIntegration) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "a partner token or the integration's is needed"})
		return
	}
	who := c.Name
	h.Log.Info("skills to be read again: restarting", "asked_by", who)
	// With its length and flushed, the whole answer is with the kernel before the restart ends the process: a
	// chunked answer would lose its end (signals, not a wait).
	b, _ := json.Marshal(Accepted{Restarting: true})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusAccepted)
	w.Write(b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	if h.Restart != nil {
		go h.Restart()
	}
}

// List is the companions as the page sees them.
func (h *Handler) List(ctx context.Context) ([]Companion, error) {
	all, err := h.Store.Companions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Companion, 0, len(all))
	for _, c := range all {
		out = append(out, Companion{Name: c.Name, Title: c.Title})
	}
	return out, nil
}

func (h *Handler) find(ctx context.Context, name string) (store.Companion, bool, error) {
	all, err := h.Store.Companions(ctx)
	if err != nil {
		return store.Companion{}, false, err
	}
	for _, c := range all {
		if c.Name == name {
			return c, true, nil
		}
	}
	return store.Companion{}, false, nil
}

// PassPage serves a companion's page file from <url>/ui/<path> (decision 7): no token, like Mira's own page.
func (h *Handler) PassPage(w http.ResponseWriter, r *http.Request) {
	h.pass(w, r, "/ui/", false)
}

// PassData passes a companion's data call on to <url>/api/<path> with its token (decision 7). The caller is an
// admin: the page's check comes before.
func (h *Handler) PassData(w http.ResponseWriter, r *http.Request) {
	h.pass(w, r, "/api/", true)
}

func (h *Handler) pass(w http.ResponseWriter, r *http.Request, base string, withToken bool) {
	name := r.PathValue("name")
	c, ok, err := h.find(r.Context(), name)
	switch {
	case err != nil:
		h.Log.Error("the companions could not be read", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the companions could not be read"})
		return
	case !ok:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no companion " + name})
		return
	}
	target := c.URL + base + r.PathValue("path")
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	// The 10 s per call are the client's (decision 7).
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a call to pass on: " + err.Error()})
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if withToken {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := h.client().Do(req)
	if err != nil {
		h.Log.Warn("a companion cannot be reached", "name", name, "url", c.URL, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "companion " + name + " cannot be reached"})
		return
	}
	defer resp.Body.Close()
	if withToken && resp.StatusCode == http.StatusUnauthorized {
		h.Log.Warn("a companion refuses Mira's token", "name", name, "url", c.URL)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "companion " + name + " refuses Mira's token"})
		return
	}
	for _, k := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, maxBody)); err != nil {
		h.Log.Warn("a companion's answer was cut", "name", name, "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
