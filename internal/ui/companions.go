package ui

import (
	"context"
	"net/http"

	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/companions"
)

// Companions are the companion services registered with Mira (docs/spec/companions.md).
type Companions interface {
	List(ctx context.Context) ([]companions.Companion, error)
	PassPage(w http.ResponseWriter, r *http.Request)
	PassData(w http.ResponseWriter, r *http.Request)
}

// CompanionList is the answer of GET /v1/ui/companions.
type CompanionList struct {
	Companions []companions.Companion `json:"companions"`
}

// companionRoutes are a companion's tab and the passing on (decisions 7, 8).
func (h *Handler) companionRoutes() {
	h.mux.Handle(pageRoute(api.Route{Method: "GET", Path: "/v1/ui/companions", Summary: "The companion services registered (their tabs)",
		Response: CompanionList{}, Errors: []int{403, 500}}), h.auth(h.companions))
	// A companion's page files: no token, like the page's own (decision 7).
	h.mux.Hidden("GET /ui/companion/{name}/{path...}", "a companion's page files, passed on (docs/spec/companions.md)",
		http.HandlerFunc(h.companionPage))
	for _, m := range []string{"GET", "PUT", "POST", "DELETE"} {
		h.mux.Handle(pageRoute(api.Route{Method: m, Path: "/v1/ui/companion/{name}/{path...}",
			Summary:     "A companion's data, passed on to it for admins (docs/spec/companions.md)",
			Description: "The body and the answer are the companion's own.", Errors: []int{403, 404, 502}}),
			h.auth(h.companionData))
	}
}

func (h *Handler) companions(w http.ResponseWriter, r *http.Request) {
	if h.opts.Companions == nil {
		writeJSON(w, http.StatusOK, CompanionList{Companions: []companions.Companion{}})
		return
	}
	list, err := h.opts.Companions.List(r.Context())
	if err != nil {
		h.log.Error("could not read the companions for the page", "error", err)
		writeError(w, http.StatusInternalServerError, "the companions could not be read")
		return
	}
	writeJSON(w, http.StatusOK, CompanionList{Companions: list})
}

func (h *Handler) companionPage(w http.ResponseWriter, r *http.Request) {
	if h.opts.Companions == nil {
		http.NotFound(w, r)
		return
	}
	h.opts.Companions.PassPage(w, r)
}

func (h *Handler) companionData(w http.ResponseWriter, r *http.Request) {
	if h.opts.Companions == nil {
		writeError(w, http.StatusNotFound, "no companion "+r.PathValue("name"))
		return
	}
	h.opts.Companions.PassData(w, r)
}
