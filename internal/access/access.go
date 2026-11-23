// Package access keeps the named access tokens beside the integration's and
// checks every call of the HTTP interface against them
// (docs/spec/access-tokens.md). A partner token's call goes on to the
// handlers with the integration's token, so their own checks stay as they
// are; who called travels in the request's context.
package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/store"
)

// Kinds of callers.
const (
	KindIntegration = "integration"
	KindPartner     = "partner"
)

// HeaderPartnerUser names the partner user; Mira trusts it only with a
// partner token (decision 5).
const HeaderPartnerUser = "X-Partner-User"

// Headers of Home Assistant's proxy, ignored with a partner token
// (decision 10); the same names as internal/ui's.
const (
	headerHAUser  = "X-Mira-HA-User"
	headerHAAdmin = "X-Mira-HA-Admin"
)

// tokenPrefix marks a partner token wherever it turns up (a log, a file).
const tokenPrefix = "mira_p_"

// prefixLen is how much of a token is kept to recognise it on the page.
const prefixLen = 10

// recordTimeout bounds the write of one partner call.
const recordTimeout = 5 * time.Second

// ErrNameTaken: a token of that name exists.
var ErrNameTaken = store.ErrNameTaken

// ErrNoName: a token needs a name.
var ErrNoName = errors.New("a token needs a name")

// Caller is who made a call.
type Caller struct {
	TokenID     string
	Name        string
	Kind        string
	PartnerUser string
}

type ctxKey struct{}

// CallerFrom is the caller a check put into the context; ok is false for
// none (a call without a known token, or the setup before pairing).
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(ctxKey{}).(Caller)
	return c, ok
}

// Token is one stored token as the page shows it.
type Token struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Prefix  string    `json:"prefix"`
	Created time.Time `json:"created"`
}

// Keeper holds the tokens in memory, so a call costs no read of the
// database; every change writes the database first, then the memory.
type Keeper struct {
	store       *store.Store
	integration string
	log         *slog.Logger

	mu     sync.RWMutex
	byHash map[string]store.AccessToken
}

// New reads the stored tokens.
func New(ctx context.Context, st *store.Store, integrationToken string, log *slog.Logger) (*Keeper, error) {
	if log == nil {
		log = slog.Default()
	}
	k := &Keeper{store: st, integration: integrationToken, log: log}
	if err := k.load(ctx); err != nil {
		return nil, fmt.Errorf("read the access tokens: %w", err)
	}
	return k, nil
}

func (k *Keeper) load(ctx context.Context) error {
	list, err := k.store.AccessTokens(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]store.AccessToken, len(list))
	for _, t := range list {
		m[t.Hash] = t
	}
	k.mu.Lock()
	k.byHash = m
	k.mu.Unlock()
	return nil
}

func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Make makes a partner token and answers it once; only its hash is kept.
func (k *Keeper) Make(ctx context.Context, name string) (string, Token, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", Token{}, ErrNoName
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Token{}, fmt.Errorf("random bytes: %w", err)
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	t, err := k.store.AddAccessToken(ctx, name, KindPartner, hashOf(token), token[:prefixLen])
	if err != nil {
		return "", Token{}, err
	}
	k.mu.Lock()
	k.byHash[t.Hash] = t
	k.mu.Unlock()
	return token, tokenOf(t), nil
}

// Delete withdraws a token; store.ErrNotFound for an unknown id.
func (k *Keeper) Delete(ctx context.Context, id string) (Token, error) {
	t, err := k.store.DeleteAccessToken(ctx, id)
	if err != nil {
		return Token{}, err
	}
	k.mu.Lock()
	delete(k.byHash, t.Hash)
	k.mu.Unlock()
	return tokenOf(t), nil
}

// List is the stored tokens, oldest first.
func (k *Keeper) List(ctx context.Context) ([]Token, error) {
	list, err := k.store.AccessTokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Token, 0, len(list))
	for _, t := range list {
		out = append(out, tokenOf(t))
	}
	return out, nil
}

func tokenOf(t store.AccessToken) Token {
	return Token{ID: t.ID, Name: t.Name, Kind: t.Kind, Prefix: t.Prefix, Created: t.Created}
}

// partner finds the partner token a bearer is; ok is false for none.
func (k *Keeper) partner(bearer string) (store.AccessToken, bool) {
	if !strings.HasPrefix(bearer, tokenPrefix) {
		return store.AccessToken{}, false
	}
	k.mu.RLock()
	t, ok := k.byHash[hashOf(bearer)]
	k.mu.RUnlock()
	return t, ok
}

func (k *Keeper) isIntegration(bearer string) bool {
	return k.integration != "" && subtle.ConstantTimeCompare([]byte(bearer), []byte(k.integration)) == 1
}

// partnerAllowed is all a partner token may call: who it is (answered by
// the check itself), the state the Partner service asks at an interval
// (decision 23), and since docs/spec/companions.md its registration as a
// companion service and a reload of the skills. Since Mira works with the owner's Home Assistant token, the
// rest of the interface would be an administrator's access without a sign-in;
// the partner works in Home Assistant itself instead.
func partnerAllowed(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/v1/whoami" || r.URL.Path == "/v1/status"):
		return true
	case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.HasPrefix(r.URL.Path, "/v1/companions/"):
		// Its own registration as a companion service (docs/spec/companions.md, decision 5).
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/v1/skills/reload":
		// Reading the skills again, when the skills it brings changed (companions.md, decision 10).
		return true
	}
	return false
}

// refusedText is the 403's error for a partner token's call elsewhere.
const refusedText = "a partner token may only ask who it is and the state"

// Check wraps the HTTP interface: it answers GET /v1/whoami, passes a
// partner token's call on with the integration's token, and keeps it in
// the partner activity.
func (k *Keeper) Check(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		t, isPartner := k.partner(bearer)
		if !isPartner {
			// Only a partner token carries a partner user (decision 5).
			r.Header.Del(HeaderPartnerUser)
			if r.Method == http.MethodGet && r.URL.Path == "/v1/whoami" {
				k.whoami(w, k.isIntegration(bearer), Caller{})
				return
			}
			if k.isIntegration(bearer) {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{},
					Caller{Name: KindIntegration, Kind: KindIntegration}))
			}
			next.ServeHTTP(w, r)
			return
		}
		c := Caller{TokenID: t.ID, Name: t.Name, Kind: KindPartner, PartnerUser: r.Header.Get(HeaderPartnerUser)}
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		if !unrecorded(r) {
			defer func() { k.record(r, c, rec.status) }()
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/whoami" {
			k.whoami(rec, false, c)
			return
		}
		if !partnerAllowed(r) {
			writeJSON(rec, http.StatusForbidden, map[string]string{"error": refusedText})
			return
		}
		r = r.Clone(context.WithValue(r.Context(), ctxKey{}, c))
		// Without a Home Assistant user (decision 10).
		r.Header.Del(headerHAUser)
		r.Header.Del(headerHAAdmin)
		if k.integration != "" {
			r.Header.Set("Authorization", "Bearer "+k.integration)
		} else {
			r.Header.Del("Authorization")
		}
		next.ServeHTTP(rec, r)
	})
}

// unrecorded is the call a partner token makes all the time and that only
// reads: the state the Partner service asks at a fixed interval (decision
// 22). It gets no row and no log line.
func unrecorded(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/v1/status"
}

// Who is GET /v1/whoami's answer: the calling token's kind and name.
type Who struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (k *Keeper) whoami(w http.ResponseWriter, integration bool, c Caller) {
	switch {
	case c.Kind == KindPartner:
		writeJSON(w, http.StatusOK, Who{Name: c.Name, Kind: KindPartner})
	case integration:
		writeJSON(w, http.StatusOK, Who{Name: KindIntegration, Kind: KindIntegration})
	default:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or wrong bearer token"})
	}
}

// record keeps one partner call; a failed write does not stop the call
// (it has run already) and leaves a log line.
func (k *Keeper) record(r *http.Request, c Caller, status int) {
	k.log.Info("partner call", "token", c.Name, "partner_user", c.PartnerUser, "method", r.Method,
		"path", r.URL.Path, "status", status)
	// real time: a bound of writing the access log; no test waits for it
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), recordTimeout)
	defer cancel()
	err := k.store.RecordAccessCall(ctx, store.AccessCall{TokenID: c.TokenID, TokenName: c.Name,
		PartnerUser: c.PartnerUser, Method: r.Method, Path: r.URL.Path, Status: status})
	if err != nil {
		k.log.Warn("could not record a partner call; the call was answered, its record is missing",
			"token", c.Name, "partner_user", c.PartnerUser, "method", r.Method, "path", r.URL.Path, "error", err)
	}
}

// statusWriter remembers the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status, s.wrote = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}

// Flush passes on, for the answers that stream.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Activity is a token's calls, newest first, a page of limit from offset.
func (k *Keeper) Activity(ctx context.Context, id string, limit, offset int) ([]store.AccessCall, error) {
	return k.store.AccessCalls(ctx, id, limit, offset)
}
