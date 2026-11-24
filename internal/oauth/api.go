package oauth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	apiTimeout  = 20 * time.Second
	apiMaxBytes = 2 << 20
)

// Request makes one request of a skill's Starlark tool
// (docs/spec/api-tools.md): only to an address below one of the skill's api
// prefixes that allows the method, with the connection's token (none for a
// skill without a login, decision 8) and Mira's name (decision 16); the
// service's status and body (at most 2 MB).
func (m *Manager) Request(ctx context.Context, skill, method, raw string, body []byte) (int, []byte, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" {
		return 0, nil, fmt.Errorf("%q is no https address", raw)
	}
	if !m.hasAPI(skill) {
		return 0, nil, fmt.Errorf("%s has no api section: its tools reach no address (%s %s)", skill, method, u.Redacted())
	}
	if !m.allows(skill, method, u) {
		return 0, nil, fmt.Errorf("%s %s is not allowed for %s: see the skill's api section", method, u.Redacted(), skill)
	}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	// real time: a bound of a call to the service's API; no test waits for it
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return 0, nil, err
	}
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.5")
	req.Header.Set("User-Agent", m.agent)
	hc := m.http
	if m.logins[skill] != nil {
		hc = &http.Client{Transport: m.Transport(skill, m.http.Transport)}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, apiMaxBytes))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading the answer of %s %s: %w", method, u.Redacted(), err)
	}
	return resp.StatusCode, data, nil
}

// hasAPI reports whether the skill names any api prefix.
func (m *Manager) hasAPI(skill string) bool {
	if o := m.logins[skill]; o != nil {
		return len(o.API) > 0
	}
	return len(m.plain[skill]) > 0
}

// allows reports whether an api prefix of the skill takes method at u: of
// its login, or its own for a skill without one.
func (m *Manager) allows(skill, method string, u *url.URL) bool {
	list := m.plain[skill]
	if o := m.logins[skill]; o != nil {
		list = o.API
	}
	for _, a := range list {
		if Below(u, a.URL) && slices.Contains(a.Methods, method) {
			return true
		}
	}
	return false
}
