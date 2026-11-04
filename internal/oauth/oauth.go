// Package oauth keeps the connections of skills with an oauth section
// (docs/spec/oauth.md): Home Assistant does the login and hands the tokens
// over once; from then on Mira keeps them, sealed like the secrets file, and
// renews them herself.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// The states of a connection, as the Connections page shows them.
const (
	StateNone      = "none"
	StateConnected = "connected"
	StateExpired   = "expired"
)

var (
	// ErrNotConnected: the skill has no connection (yet).
	ErrNotConnected = errors.New("not connected")
	// ErrExpired: the service refused the refresh token; the connection
	// must be made again.
	ErrExpired = errors.New("connection expired")
	// ErrUnknown: no enabled skill of that name has an oauth section.
	ErrUnknown = errors.New("no skill with a login of that name")
)

// margin renews a token this long before it runs out.
const margin = time.Minute

// Token is a connection as Mira keeps it.
type Token struct {
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
	// Expired marks a connection the service refused to renew; it stays
	// until the skill is connected again or disconnected.
	Expired bool `json:"expired,omitempty"`
}

// Handover is the token answer Home Assistant's OAuth helper got, as the
// integration puts it (PUT /v1/oauth/<skill>/token).
type Handover struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	TokenType    string  `json:"token_type"`
	Scope        string  `json:"scope"`
	ExpiresIn    float64 `json:"expires_in"`
	// ExpiresAt is the helper's own end in seconds since 1970; it wins
	// over ExpiresIn.
	ExpiresAt float64 `json:"expires_at"`
}

// Login is what the integration needs to start a login (GET
// /v1/oauth/<skill>).
type Login struct {
	Skill           string            `json:"skill"`
	Name            string            `json:"name"`
	AuthorizeURL    string            `json:"authorize_url"`
	TokenURL        string            `json:"token_url"`
	Scopes          []string          `json:"scopes"`
	PKCE            bool              `json:"pkce"`
	TokenAuth       string            `json:"token_auth"`
	AuthorizeParams map[string]string `json:"authorize_params"`
	ClientID        string            `json:"client_id"`
	ClientSecret    string            `json:"client_secret"`
}

// Options configure a Manager.
type Options struct {
	Store   *store.Store
	Secrets config.Secrets
	// Skills are the enabled skills; those with an oauth section count.
	Skills []skills.Skill
	// HTTP renews the tokens; http.DefaultClient if nil.
	HTTP *http.Client
	// Now is the clock; time.Now if nil.
	Now    func() time.Time
	Logger *slog.Logger
	// OnChange runs after a skill was connected, disconnected or its
	// connection expired; it must not block.
	OnChange func(skill string)
}

// Manager holds the connections of the enabled skills.
type Manager struct {
	st       *store.Store
	sec      config.Secrets
	http     *http.Client
	now      func() time.Time
	log      *slog.Logger
	onChange func(string)

	logins map[string]*skills.OAuth
	mcp    map[string]string // the skill's MCP server address, where the token may go

	mu     sync.Mutex
	tokens map[string]Token
}

// New reads the stored connections of the skills with an oauth section.
// A stored value that cannot be read (encrypted with another key) counts as
// no connection, and the log says why.
func New(ctx context.Context, o Options) (*Manager, error) {
	m := &Manager{st: o.Store, sec: o.Secrets, http: o.HTTP, now: o.Now, log: o.Logger, onChange: o.OnChange,
		logins: map[string]*skills.OAuth{}, mcp: map[string]string{}, tokens: map[string]Token{}}
	if m.http == nil {
		m.http = http.DefaultClient
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	for _, sk := range o.Skills {
		if sk.OAuth == nil {
			continue
		}
		m.logins[sk.Name] = sk.OAuth
		if sk.MCP != nil && sk.MCP.OAuth {
			m.mcp[sk.Name] = sk.MCP.URL
		}
		raw, err := m.st.GetOAuthToken(ctx, sk.Name)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		plain, err := m.sec.Open(raw)
		if err != nil {
			m.log.Warn("oauth: the stored connection cannot be read", "skill", sk.Name, "error", err)
			continue
		}
		var t Token
		if err := json.Unmarshal([]byte(plain), &t); err != nil {
			m.log.Warn("oauth: the stored connection cannot be read", "skill", sk.Name, "error", err)
			continue
		}
		m.tokens[sk.Name] = t
	}
	return m, nil
}

// Skills are the names of the skills with a login, sorted.
func (m *Manager) Skills() []string {
	out := make([]string, 0, len(m.logins))
	for n := range m.logins {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// OAuth is a skill's oauth section; nil for a skill without one.
func (m *Manager) OAuth(skill string) *skills.OAuth { return m.logins[skill] }

// State is a skill's connection state.
func (m *Manager) State(skill string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[skill]
	switch {
	case !ok:
		return StateNone
	case t.Expired:
		return StateExpired
	}
	return StateConnected
}

// Connected reports whether the skill has a connection to use.
func (m *Manager) Connected(skill string) bool { return m.State(skill) == StateConnected }

// Login is what the integration starts a login with; ErrUnknown for a skill
// without a login, ErrNotConnected while client id or secret is missing.
func (m *Manager) Login(skill string) (Login, error) {
	o := m.logins[skill]
	if o == nil {
		return Login{}, ErrUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !o.Ready() {
		return Login{}, fmt.Errorf("%s: client_id and client_secret are not set", skill)
	}
	return Login{Skill: skill, Name: o.Name, AuthorizeURL: o.AuthorizeURL, TokenURL: o.TokenURL,
		Scopes: append([]string{}, o.Scopes...), PKCE: o.PKCE, TokenAuth: o.TokenAuth,
		AuthorizeParams: o.AuthorizeParams, ClientID: o.ClientID, ClientSecret: o.ClientSecret}, nil
}

// Client is a skill's client id and whether its secret is set.
func (m *Manager) Client(skill string) (id string, secretSet bool) {
	o := m.logins[skill]
	if o == nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return o.ClientID, o.ClientSecret != ""
}

// SetClient takes a client id and secret entered on the Connections page,
// once they are written to mira.yaml and the secrets file: they hold at
// once. An empty secret keeps the one there is.
func (m *Manager) SetClient(skill, id, secret string) error {
	o := m.logins[skill]
	if o == nil {
		return ErrUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	o.ClientID = id
	if secret != "" {
		o.ClientSecret = secret
	}
	return nil
}

// Put stores the tokens of a login Home Assistant has done.
func (m *Manager) Put(ctx context.Context, skill string, h Handover) error {
	if m.logins[skill] == nil {
		return ErrUnknown
	}
	if strings.TrimSpace(h.AccessToken) == "" {
		return errors.New("access_token is missing")
	}
	t := Token{AccessToken: h.AccessToken, RefreshToken: h.RefreshToken, TokenType: h.TokenType, Scope: h.Scope}
	switch {
	case h.ExpiresAt > 0:
		t.Expiry = time.Unix(0, int64(h.ExpiresAt*float64(time.Second)))
	case h.ExpiresIn > 0:
		t.Expiry = m.now().Add(time.Duration(h.ExpiresIn * float64(time.Second)))
	}
	m.mu.Lock()
	err := m.save(ctx, skill, t)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.log.Info("oauth: connected", "skill", skill)
	m.changed(skill)
	return nil
}

// Disconnect forgets a skill's connection.
func (m *Manager) Disconnect(ctx context.Context, skill string) error {
	if m.logins[skill] == nil {
		return ErrUnknown
	}
	m.mu.Lock()
	delete(m.tokens, skill)
	err := m.st.DeleteOAuthToken(ctx, skill)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.log.Info("oauth: disconnected", "skill", skill)
	m.changed(skill)
	return nil
}

// Token is the skill's current access token, renewed first when it has run
// out. ErrNotConnected or ErrExpired when there is none to give.
func (m *Manager) Token(ctx context.Context, skill string) (string, error) {
	return m.token(ctx, skill, "")
}

// Renew is for a token the service refused although it should be valid:
// it renews once, unless another request did so since stale was handed out.
func (m *Manager) Renew(ctx context.Context, skill, stale string) (string, error) {
	return m.token(ctx, skill, stale)
}

func (m *Manager) token(ctx context.Context, skill, stale string) (string, error) {
	o := m.logins[skill]
	if o == nil {
		return "", ErrUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[skill]
	switch {
	case !ok:
		return "", ErrNotConnected
	case t.Expired:
		return "", ErrExpired
	}
	due := !t.Expiry.IsZero() && !m.now().Add(margin).Before(t.Expiry)
	if !due && (stale == "" || stale != t.AccessToken) {
		return t.AccessToken, nil
	}
	if t.RefreshToken == "" {
		if stale != "" || due {
			return "", m.expire(ctx, skill, errors.New("no refresh token"))
		}
		return t.AccessToken, nil
	}
	nt, err := m.refresh(ctx, o, t)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.Response != nil && re.Response.StatusCode >= 400 && re.Response.StatusCode < 500 {
			return "", m.expire(ctx, skill, err)
		}
		return "", fmt.Errorf("renew the token of %s: %w", skill, err)
	}
	if err := m.save(ctx, skill, nt); err != nil {
		return "", err
	}
	m.log.Info("oauth: token renewed", "skill", skill)
	return nt.AccessToken, nil
}

// refresh renews t; m.mu is held.
func (m *Manager) refresh(ctx context.Context, o *skills.OAuth, t Token) (Token, error) {
	style := oauth2.AuthStyleInParams
	if o.TokenAuth == skills.TokenAuthBasic {
		style = oauth2.AuthStyleInHeader
	}
	conf := &oauth2.Config{ClientID: o.ClientID, ClientSecret: o.ClientSecret,
		Endpoint: oauth2.Endpoint{TokenURL: o.TokenURL, AuthStyle: style}}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, m.http)
	nt, err := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: t.RefreshToken}).Token()
	if err != nil {
		return Token{}, err
	}
	out := Token{AccessToken: nt.AccessToken, RefreshToken: nt.RefreshToken, TokenType: nt.TokenType, Scope: t.Scope}
	if s, ok := nt.Extra("scope").(string); ok && s != "" {
		out.Scope = s
	}
	if nt.ExpiresIn > 0 {
		out.Expiry = m.now().Add(time.Duration(nt.ExpiresIn) * time.Second)
	}
	return out, nil
}

// expire marks the connection as refused; m.mu is held.
func (m *Manager) expire(ctx context.Context, skill string, cause error) error {
	m.log.Warn("oauth: the service refused to renew the token", "skill", skill, "error", cause)
	if err := m.save(ctx, skill, Token{Expired: true}); err != nil {
		return err
	}
	go m.changed(skill)
	return ErrExpired
}

// save writes a connection sealed; m.mu is held.
func (m *Manager) save(ctx context.Context, skill string, t Token) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	sealed, err := m.sec.Seal(string(raw))
	if err != nil {
		return err
	}
	if err := m.st.SetOAuthToken(ctx, skill, sealed); err != nil {
		return err
	}
	m.tokens[skill] = t
	return nil
}

func (m *Manager) changed(skill string) {
	if m.onChange != nil {
		m.onChange(skill)
	}
}

// Transport adds the skill's token to the requests that go where the skill
// allows (its MCP server, its api prefixes) and to no other; a request the
// service answers with 401 is renewed and repeated once.
func (m *Manager) Transport(skill string, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &transport{m: m, skill: skill, next: next}
}

// Allowed reports whether the token of skill may go to u: below the
// skill's MCP server address or one of its api prefixes.
func (m *Manager) Allowed(skill string, u *url.URL) bool {
	if mcp := m.mcp[skill]; mcp != "" && below(u, mcp) {
		return true
	}
	if o := m.logins[skill]; o != nil {
		for _, a := range o.API {
			if below(u, a.URL) {
				return true
			}
		}
	}
	return false
}

// below reports whether u is the address prefix or under it: same scheme
// and host, and the path continues the prefix's at a "/".
func below(u *url.URL, prefix string) bool {
	p, err := url.Parse(prefix)
	if err != nil || u == nil || !strings.EqualFold(u.Scheme, p.Scheme) || !strings.EqualFold(u.Host, p.Host) {
		return false
	}
	pp := p.EscapedPath()
	up := u.EscapedPath()
	if pp == "" || pp == "/" || up == pp {
		return true
	}
	if !strings.HasSuffix(pp, "/") {
		pp += "/"
	}
	return strings.HasPrefix(up, pp)
}

type transport struct {
	m     *Manager
	skill string
	next  http.RoundTripper
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !t.m.Allowed(t.skill, r.URL) {
		return nil, fmt.Errorf("the token of %s does not go to %s", t.skill, r.URL.Redacted())
	}
	tok, err := t.m.Token(r.Context(), t.skill)
	if err != nil {
		return nil, err
	}
	resp, err := t.next.RoundTrip(withToken(r, tok))
	if err != nil || resp.StatusCode != http.StatusUnauthorized || (r.Body != nil && r.GetBody == nil) {
		return resp, err
	}
	fresh, rerr := t.m.Renew(r.Context(), t.skill, tok)
	if rerr != nil {
		return resp, nil
	}
	again := withToken(r, fresh)
	if r.GetBody != nil {
		body, err := r.GetBody()
		if err != nil {
			return resp, nil
		}
		again.Body = body
	}
	resp.Body.Close()
	return t.next.RoundTrip(again)
}

func withToken(r *http.Request, tok string) *http.Request {
	c := r.Clone(r.Context())
	c.Header.Set("Authorization", "Bearer "+tok)
	return c
}
