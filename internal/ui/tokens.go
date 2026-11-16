package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/miraassist/mira/internal/access"
	"github.com/miraassist/mira/internal/store"
)

// Tokens are the named access tokens beside the integration's, made and
// deleted by admins on the page (docs/spec/access-tokens.md).
type Tokens interface {
	List(ctx context.Context) ([]access.Token, error)
	Make(ctx context.Context, name string) (string, access.Token, error)
	Delete(ctx context.Context, id string) (access.Token, error)
	Activity(ctx context.Context, id string, limit, offset int) ([]store.AccessCall, error)
}

// activityPage is how many calls the page shows at once.
const activityPage = 50

func (h *Handler) tokenRoutes() {
	h.mux.HandleFunc("GET /v1/ui/tokens", h.auth(h.listTokens))
	h.mux.HandleFunc("POST /v1/ui/tokens", h.auth(h.makeToken))
	h.mux.HandleFunc("DELETE /v1/ui/tokens/{id}", h.auth(h.deleteToken))
	h.mux.HandleFunc("GET /v1/ui/tokens/{id}/activity", h.auth(h.tokenActivity))
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	if h.opts.Tokens == nil {
		writeJSON(w, http.StatusOK, map[string]any{"tokens": []access.Token{}})
		return
	}
	list, err := h.opts.Tokens.List(r.Context())
	if err != nil {
		h.log.Error("could not read the access tokens for the page", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": list})
}

func (h *Handler) makeToken(w http.ResponseWriter, r *http.Request) {
	if h.opts.Tokens == nil {
		writeError(w, http.StatusNotFound, "no access tokens")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, errBadBody.Error())
		return
	}
	token, t, err := h.opts.Tokens.Make(r.Context(), in.Name)
	switch {
	case errors.Is(err, access.ErrNoName):
		writeError(w, http.StatusBadRequest, "a token needs a name")
		return
	case errors.Is(err, access.ErrNameTaken):
		writeError(w, http.StatusConflict, "name taken")
		return
	case err != nil:
		h.log.Error("could not make an access token; nothing was stored", "name", in.Name, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log.Info("access token made on the page", "name", t.Name, "kind", t.Kind, "prefix", t.Prefix)
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "info": t})
}

func (h *Handler) deleteToken(w http.ResponseWriter, r *http.Request) {
	if h.opts.Tokens == nil {
		writeError(w, http.StatusNotFound, "no such token")
		return
	}
	id := r.PathValue("id")
	t, err := h.opts.Tokens.Delete(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such token")
		return
	case err != nil:
		h.log.Error("could not delete an access token; it still holds", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.log.Info("access token deleted on the page", "name", t.Name, "kind", t.Kind)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) tokenActivity(w http.ResponseWriter, r *http.Request) {
	if h.opts.Tokens == nil {
		writeJSON(w, http.StatusOK, map[string]any{"calls": []store.AccessCall{}})
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	calls, err := h.opts.Tokens.Activity(r.Context(), r.PathValue("id"), activityPage, offset)
	if err != nil {
		h.log.Error("could not read a token's partner activity for the page", "id", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if calls == nil {
		calls = []store.AccessCall{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"calls": calls, "page": activityPage})
}
