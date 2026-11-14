package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Connections are the skills' logins at services, for the Connections page
// (docs/spec/oauth.md).
type Connections interface {
	List(ctx context.Context) []Connection
	// SaveClient writes client id and secret (empty keeps it) to mira.yaml
	// and the secrets file; they hold at once.
	SaveClient(ctx context.Context, skill, id, secret string) error
	Disconnect(ctx context.Context, skill string) error
	// RemoteAddress is Mira's address for remote agents (server.url) and
	// whether a skill that is on has a remote agent
	// (docs/spec/remote-agents.md, decision 13).
	RemoteAddress() (url string, used bool)
	// SaveRemoteAddress writes server.url to mira.yaml; it holds after a
	// restart.
	SaveRemoteAddress(ctx context.Context, url string) error
}

// Connection is one skill's login as the page shows it. The secret never
// comes back, only whether it is set.
type Connection struct {
	Skill     string `json:"skill"`
	Name      string `json:"name"`
	State     string `json:"state"`
	ClientID  string `json:"client_id"`
	SecretSet bool   `json:"secret_set"`
	// Redirect is the address to enter at the service.
	Redirect string `json:"redirect"`
}

// ClientEdit is the form of a connection.
type ClientEdit struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (h *Handler) connectionRoutes() {
	h.mux.HandleFunc("GET /v1/ui/connections", h.auth(h.connections))
	h.mux.HandleFunc("PUT /v1/ui/connections/{skill}", h.auth(h.saveClient))
	h.mux.HandleFunc("DELETE /v1/ui/connections/{skill}/token", h.auth(h.disconnect))
	h.mux.HandleFunc("PUT /v1/ui/remote_address", h.auth(h.saveRemoteAddress))
}

func (h *Handler) connections(w http.ResponseWriter, r *http.Request) {
	if h.opts.Connections == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connections": []Connection{}})
		return
	}
	url, used := h.opts.Connections.RemoteAddress()
	writeJSON(w, http.StatusOK, map[string]any{"connections": h.opts.Connections.List(r.Context()),
		"remote_address": url, "remote_agents": used})
}

// saveRemoteAddress writes Mira's address for remote agents.
func (h *Handler) saveRemoteAddress(w http.ResponseWriter, r *http.Request) {
	if h.opts.Connections == nil {
		writeError(w, http.StatusNotFound, "no connections")
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, errBadBody.Error())
		return
	}
	if err := h.opts.Connections.SaveRemoteAddress(r.Context(), strings.TrimSpace(in.URL)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.log.Info("Mira's address for remote agents changed on the page", "url", strings.TrimSpace(in.URL))
	h.connections(w, r)
}

func (h *Handler) saveClient(w http.ResponseWriter, r *http.Request) {
	if h.opts.Connections == nil {
		writeError(w, http.StatusNotFound, "no connections")
		return
	}
	var c ClientEdit
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, errBadBody.Error())
		return
	}
	skill := r.PathValue("skill")
	if err := h.opts.Connections.SaveClient(r.Context(), skill, strings.TrimSpace(c.ClientID), strings.TrimSpace(c.ClientSecret)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.log.Info("connection client changed on the page", "skill", skill)
	h.connections(w, r)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	if h.opts.Connections == nil {
		writeError(w, http.StatusNotFound, "no connections")
		return
	}
	skill := r.PathValue("skill")
	if err := h.opts.Connections.Disconnect(r.Context(), skill); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.connections(w, r)
}
