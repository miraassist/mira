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
// prefixes that allows the method, with the connection's token; the
// service's status and body (at most 2 MB).
func (m *Manager) Request(ctx context.Context, skill, method, raw string, body []byte) (int, []byte, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" {
		return 0, nil, fmt.Errorf("%q is no https address", raw)
	}
	if !m.allows(skill, method, u) {
		return 0, nil, fmt.Errorf("%s %s is not allowed for %s: see the skill's api section", method, u.Redacted(), skill)
	}
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
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
	hc := &http.Client{Transport: m.Transport(skill, m.http.Transport)}
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

// allows reports whether an api prefix of the skill takes method at u.
func (m *Manager) allows(skill, method string, u *url.URL) bool {
	o := m.logins[skill]
	if o == nil {
		return false
	}
	for _, a := range o.API {
		if Below(u, a.URL) && slices.Contains(a.Methods, method) {
			return true
		}
	}
	return false
}
