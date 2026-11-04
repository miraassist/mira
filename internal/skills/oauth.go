package skills

import (
	"net/url"
	"strings"
)

// The settings an oauth section brings without being declared
// (docs/spec/oauth.md, decision 3). They are entered on the Connections
// page, not among the skill's settings.
const (
	SettingClientID     = "client_id"
	SettingClientSecret = "client_secret"
)

// TokenOAuth as an mcp section's token is the connection's token, renewed
// by Mira (docs/spec/oauth.md, In the skill).
const TokenOAuth = "oauth"

// How the client secret goes to the token address.
const (
	TokenAuthPost  = "post"
	TokenAuthBasic = "basic"
)

// OAuth is a skill's login at a service (metadata.mira.oauth), with the
// skill's settings filled in.
type OAuth struct {
	// Name is the service's name for "Connect to …"; the skill's name if
	// not said.
	Name            string            `yaml:"name"`
	AuthorizeURL    string            `yaml:"authorize_url"`
	TokenURL        string            `yaml:"token_url"`
	Scopes          []string          `yaml:"scopes"`
	PKCE            bool              `yaml:"pkce"`
	TokenAuth       string            `yaml:"token_auth"`
	AuthorizeParams map[string]string `yaml:"authorize_params"`
	// API names where the token may go besides the MCP server, for the
	// skill's web API tool.
	API []APIPrefix `yaml:"api"`

	// From the skill's settings client_id and client_secret.
	ClientID     string `yaml:"-"`
	ClientSecret string `yaml:"-"`
}

// APIPrefix is an address the web API tool may call with the token, and
// the methods allowed there.
type APIPrefix struct {
	URL     string   `yaml:"url"`
	Methods []string `yaml:"methods"`
}

// Ready reports whether client id and secret are set, so a login can start.
func (o *OAuth) Ready() bool { return o != nil && o.ClientID != "" && o.ClientSecret != "" }

var apiMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// oauthSection fills the settings into an oauth section and checks it.
func oauthSection(name string, o OAuth, vals map[string]string) (OAuth, *Problem) {
	all := func(s string) string {
		return strings.TrimSpace(placeholder.ReplaceAllStringFunc(s, func(m string) string {
			return vals[placeholder.FindStringSubmatch(m)[1]]
		}))
	}
	if o.Name = strings.TrimSpace(o.Name); o.Name == "" {
		o.Name = name
	}
	o.AuthorizeURL, o.TokenURL = all(o.AuthorizeURL), all(o.TokenURL)
	for _, u := range []string{o.AuthorizeURL, o.TokenURL} {
		if !isHTTPS(u) {
			return o, problem("oauthurl")
		}
	}
	switch o.TokenAuth {
	case "":
		o.TokenAuth = TokenAuthPost
	case TokenAuthPost, TokenAuthBasic:
	default:
		return o, problem("oauthauth", o.TokenAuth)
	}
	params := map[string]string{}
	for k, v := range o.AuthorizeParams {
		params[k] = all(v)
	}
	o.AuthorizeParams = params
	for i, a := range o.API {
		a.URL = all(a.URL)
		if !isHTTPS(a.URL) {
			return o, problem("oauthapi", a.URL)
		}
		if len(a.Methods) == 0 {
			a.Methods = []string{"GET"}
		}
		for j, m := range a.Methods {
			m = strings.ToUpper(strings.TrimSpace(m))
			if !apiMethods[m] {
				return o, problem("oauthmethod", m, a.URL)
			}
			a.Methods[j] = m
		}
		o.API[i] = a
	}
	o.ClientID, o.ClientSecret = vals[SettingClientID], vals[SettingClientSecret]
	return o, nil
}

func isHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
