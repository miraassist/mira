// Package fakeoauth is a small OAuth 2.0 service for tests
// (docs/spec/fake-devices.md, Fake OAuth service): a login page that answers
// at once, a token address that exchanges codes (with PKCE) and renews with
// a new refresh token each time, as X does, and a web API that wants a
// current access token. A test can let tokens run out, revoke the refresh
// tokens, or ask for the client secret as HTTP Basic only.
package fakeoauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Service struct {
	ClientID, ClientSecret string
	// Basic: the secret is taken only as HTTP Basic (X), else only in the
	// body.
	Basic bool
	// ExpiresIn is the life of an access token; 2 hours if 0.
	ExpiresIn time.Duration

	mu       sync.Mutex
	codes    map[string]grant
	access   map[string]bool
	refresh  map[string]bool
	renewals int
	calls    []APICall
	srv      *httptest.Server
}

type grant struct{ redirect, challenge string }

// APICall is one request to the web API.
type APICall struct {
	Method, Path, Body, Token string
}

// New starts a service over TLS (an oauth section takes https only); its
// Client trusts it.
func New(clientID, secret string) *Service {
	s := &Service{ClientID: clientID, ClientSecret: secret, codes: map[string]grant{},
		access: map[string]bool{}, refresh: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("/api/", s.api)
	s.srv = httptest.NewTLSServer(mux)
	return s
}

func (s *Service) Close()                { s.srv.Close() }
func (s *Service) URL() string           { return s.srv.URL }
func (s *Service) AuthorizeURL() string  { return s.srv.URL + "/authorize" }
func (s *Service) TokenURL() string      { return s.srv.URL + "/token" }
func (s *Service) APIURL() string        { return s.srv.URL + "/api/" }
func (s *Service) Client() *http.Client  { return s.srv.Client() }
func (s *Service) Handler() http.Handler { return s.srv.Config.Handler }

// Grant issues a token pair as a finished login would: access token,
// refresh token.
func (s *Service) Grant() (access, refresh string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issue()
}

func (s *Service) issue() (string, string) {
	a, r := "at-"+random(), "rt-"+random()
	s.access[a], s.refresh[r] = true, true
	return a, r
}

// Valid reports whether an access token is current.
func (s *Service) Valid(access string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access[access]
}

// Expire lets every access token run out (the service refuses them).
func (s *Service) Expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = map[string]bool{}
}

// Revoke refuses every refresh token, as when the user withdrew the app.
func (s *Service) Revoke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh = map[string]bool{}
	s.access = map[string]bool{}
}

// Renewals counts the refresh requests answered.
func (s *Service) Renewals() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renewals
}

// Calls are the web API's requests.
func (s *Service) Calls() []APICall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]APICall(nil), s.calls...)
}

// authorize answers the login at once with a code, as if the user had
// signed in and allowed the app.
func (s *Service) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.ClientID || q.Get("redirect_uri") == "" {
		http.Error(w, "unknown client or no redirect_uri", http.StatusBadRequest)
		return
	}
	code := "code-" + random()
	s.mu.Lock()
	s.codes[code] = grant{redirect: q.Get("redirect_uri"), challenge: q.Get("code_challenge")}
	s.mu.Unlock()
	to, _ := url.Parse(q.Get("redirect_uri"))
	back := to.Query()
	back.Set("code", code)
	back.Set("state", q.Get("state"))
	to.RawQuery = back.Encode()
	http.Redirect(w, r, to.String(), http.StatusFound)
}

func (s *Service) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id, secret, basic := r.BasicAuth()
	switch {
	case s.Basic && !basic:
		oauthError(w, http.StatusUnauthorized, "unauthorized_client", "Missing valid authorization header")
		return
	case !s.Basic && !basic:
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	case !s.Basic && basic:
		oauthError(w, http.StatusUnauthorized, "invalid_client", "the secret goes in the body")
		return
	}
	if id != s.ClientID || secret != s.ClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "wrong client id or secret")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		g, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		if !ok || g.redirect != r.PostForm.Get("redirect_uri") {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "unknown code")
			return
		}
		if g.challenge != "" {
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
				oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match")
				return
			}
		}
	case "refresh_token":
		rt := r.PostForm.Get("refresh_token")
		if !s.refresh[rt] {
			oauthError(w, http.StatusBadRequest, "invalid_request", "Value passed for the token was invalid.")
			return
		}
		delete(s.refresh, rt)
		s.renewals++
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
		return
	}
	a, rt := s.issue()
	life := s.ExpiresIn
	if life == 0 {
		life = 2 * time.Hour
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"access_token": a, "refresh_token": rt, "token_type": "bearer",
		"expires_in": int(life / time.Second), "scope": "read write offline.access"})
}

// api answers any request with a current access token with what it got.
func (s *Service) api(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.calls = append(s.calls, APICall{Method: r.Method, Path: r.URL.Path, Body: string(body), Token: tok})
	ok := s.access[tok]
	s.mu.Unlock()
	if !ok {
		oauthError(w, http.StatusUnauthorized, "invalid_token", "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"method": r.Method, "path": r.URL.Path, "body": string(body)})
}

func oauthError(w http.ResponseWriter, code int, kind, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": kind, "error_description": desc})
}

func random() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
