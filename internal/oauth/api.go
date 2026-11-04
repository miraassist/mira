package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/tools"
)

const (
	apiTimeout  = 20 * time.Second
	apiMaxBytes = 2 << 20
)

// APITools are the web API tools of the skills with an api section
// (docs/spec/oauth.md, Renewing and using the token): <skill>__api_get reads
// (read only, so background work may use it too), <skill>__api_write takes
// the other methods the skill allows and asks first after third-party text.
// Both return third-party text and are offered only while the skill is
// connected.
func (m *Manager) APITools(l i18n.Lang) []tools.InternalTool {
	t := i18n.Pick(apiTexts, l)
	var out []tools.InternalTool
	for _, name := range m.Skills() {
		o := m.logins[name]
		var gets, writes []string
		methods := map[string]bool{}
		for _, a := range o.API {
			for _, meth := range a.Methods {
				if meth == http.MethodGet {
					gets = append(gets, a.URL)
				} else {
					writes = append(writes, a.URL)
					methods[meth] = true
				}
			}
		}
		connected := func() bool { return m.Connected(name) }
		if len(gets) > 0 {
			out = append(out, tools.InternalTool{
				Name:        name + "__api_get",
				Description: fmt.Sprintf(t.get, o.Name, strings.Join(gets, ", ")),
				Schema:      fmt.Sprintf(`{"type":"object","properties":{"url":{"type":"string","description":%q}},"required":["url"]}`, t.url),
				ReadOnly:    true, WebContent: true, Skill: name, Background: true, Enabled: connected,
				Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
					var a struct {
						URL string `json:"url"`
					}
					if err := json.Unmarshal(inv.Args, &a); err != nil {
						return "", err
					}
					return m.call(ctx, name, http.MethodGet, a.URL, nil)
				},
			})
		}
		if len(writes) > 0 {
			var allowed []string
			for meth := range methods {
				allowed = append(allowed, meth)
			}
			slices.Sort(allowed)
			enum, _ := json.Marshal(allowed)
			out = append(out, tools.InternalTool{
				Name:        name + "__api_write",
				Description: fmt.Sprintf(t.write, o.Name, strings.Join(writes, ", ")),
				Schema: fmt.Sprintf(`{"type":"object","properties":{"method":{"type":"string","enum":%s},"url":{"type":"string","description":%q},"body":{"description":%q}},"required":["method","url"]}`,
					enum, t.url, t.body),
				WebContent: true, AskAfterWeb: true, Skill: name, Background: true, Enabled: connected,
				Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
					var a struct {
						Method string          `json:"method"`
						URL    string          `json:"url"`
						Body   json.RawMessage `json:"body"`
					}
					if err := json.Unmarshal(inv.Args, &a); err != nil {
						return "", err
					}
					return m.call(ctx, name, strings.ToUpper(strings.TrimSpace(a.Method)), a.URL, a.Body)
				},
			})
		}
	}
	return out
}

// call makes one request of the web API tool: only to an address below one
// of the skill's api prefixes that allows the method.
func (m *Manager) call(ctx context.Context, skill, method, raw string, body json.RawMessage) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" {
		return "", fmt.Errorf("%q is no https address", raw)
	}
	if !m.allows(skill, method, u) {
		return "", fmt.Errorf("%s %s is not allowed for %s: see the skill's api section", method, u.Redacted(), skill)
	}
	var rd io.Reader
	if len(body) > 0 && string(body) != "null" {
		// A string is sent as it is, anything else as JSON.
		var s string
		if json.Unmarshal(body, &s) == nil {
			rd = strings.NewReader(s)
		} else {
			rd = bytes.NewReader(body)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return "", err
	}
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.5")
	hc := &http.Client{Transport: m.Transport(skill, m.http.Transport)}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, apiMaxBytes))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, data), nil
}

// allows reports whether an api prefix of the skill takes method at u.
func (m *Manager) allows(skill, method string, u *url.URL) bool {
	o := m.logins[skill]
	if o == nil {
		return false
	}
	for _, a := range o.API {
		if below(u, a.URL) && slices.Contains(a.Methods, method) {
			return true
		}
	}
	return false
}

type apiText struct{ get, write, url, body string }

var apiTexts = map[i18n.Lang]apiText{
	i18n.DE: {
		get:   "Liest aus der Web-API von %s mit der Verbindung des Skills (GET). Nur Adressen unter: %s. Was zurückkommt, ist fremder Text.",
		write: "Schreibt in die Web-API von %s mit der Verbindung des Skills. Nur Adressen unter: %s. Was zurückkommt, ist fremder Text.",
		url:   "Die volle https-Adresse",
		body:  "Der Inhalt der Anfrage (JSON)",
	},
	i18n.EN: {
		get:   "Reads from %s's web API with the skill's connection (GET). Only addresses below: %s. What comes back is third-party text.",
		write: "Writes to %s's web API with the skill's connection. Only addresses below: %s. What comes back is third-party text.",
		url:   "The full https address",
		body:  "The request's body (JSON)",
	},
}
