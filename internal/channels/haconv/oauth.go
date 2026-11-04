package haconv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/miraassist/mira/internal/oauth"
)

// Logins are the skills' connections as the integration needs them for a
// login through Home Assistant (docs/spec/oauth.md, The login).
type Logins interface {
	Login(skill string) (oauth.Login, error)
	Put(ctx context.Context, skill string, h oauth.Handover) error
}

// GET /v1/oauth/{skill}: what the integration starts the login with,
// client secret included (only over the bearer-protected interface).
func (s *Server) oauthLogin(w http.ResponseWriter, r *http.Request) {
	l, err := s.opts.Logins.Login(r.PathValue("skill"))
	switch {
	case errors.Is(err, oauth.ErrUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeJSON(w, http.StatusOK, l)
	}
}

// PUT /v1/oauth/{skill}/token: the tokens of a login Home Assistant has
// done, once.
func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	var h oauth.Handover
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&h); err != nil {
		writeError(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	err := s.opts.Logins.Put(r.Context(), r.PathValue("skill"), h)
	switch {
	case errors.Is(err, oauth.ErrUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
	}
}
