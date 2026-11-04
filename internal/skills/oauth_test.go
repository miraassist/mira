package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/i18n"
)

// writeOAuthSkill writes a skill "dienst" whose metadata.mira is mira.
func writeOAuthSkill(t *testing.T, dir, mira string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "dienst"), 0o755)
	text := "---\nname: dienst\ndescription: Ein Dienst.\nmetadata:\n  mira:\n" + mira + "---\nNimm dienst__suche.\n"
	if err := os.WriteFile(filepath.Join(dir, "dienst", "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testOAuth = `    oauth:
      name: Dienst
      authorize_url: https://dienst.example/authorize
      token_url: "{{ server }}/token"
      scopes: [read, offline.access]
      pkce: true
      token_auth: basic
      api:
        - url: https://dienst.example/api/
          methods: [get, post]
    settings:
      server: { description: Adresse des Dienstes }
    mcp:
      url: https://dienst.example/mcp
      token: oauth
`

// CO-01 A skill's oauth section brings client id and secret as settings
func TestCO01_AnOAuthSectionBringsClientIDAndSecretAsSettings(t *testing.T) {
	dir := t.TempDir()
	// Given a skill with an oauth section and no settings for them
	writeOAuthSkill(t, dir, testOAuth)
	// When mira.yaml sets client_id and client_secret
	loaded, problems := Load([]string{dir}, map[string]map[string]any{"dienst": {
		"server": "https://login.example", "client_id": "id-1", "client_secret": "geheim"}})
	if len(problems) != 0 || len(loaded) != 1 {
		t.Fatalf("problems %+v", problems)
	}
	// Then they are the login's, not settings the skill's page shows
	sk := loaded[0]
	o := sk.OAuth
	if o == nil || o.ClientID != "id-1" || o.ClientSecret != "geheim" || o.Name != "Dienst" || o.TokenURL != "https://login.example/token" ||
		!o.PKCE || o.TokenAuth != TokenAuthBasic || len(o.API) != 1 || strings.Join(o.API[0].Methods, ",") != "GET,POST" {
		t.Errorf("oauth %+v", o)
	}
	if _, ok := sk.Settings[SettingClientID]; ok {
		t.Error("client_id is among the skill's settings")
	}
	// And the mcp section takes the connection's token, never a setting's
	if sk.MCP == nil || !sk.MCP.OAuth || sk.MCP.Token != "" {
		t.Errorf("mcp %+v", sk.MCP)
	}
	if strings.Contains(sk.Body, "geheim") {
		t.Error("the secret reached the skill's text")
	}
}

// CO-02 A mistake in the oauth section is a problem of the skill
func TestCO02_AMistakeInTheOAuthSectionIsAProblemOfTheSkill(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ from, to, want string }{
		// Given a skill whose oauth section has a token_url that is not https
		{`token_url: "{{ server }}/token"`, `token_url: http://login.example/token`, "https-Adresse"},
		{"token_auth: basic", "token_auth: header", "„header“"},
		{"methods: [get, post]", "methods: [fetch]", "„FETCH“"},
	} {
		writeOAuthSkill(t, dir, strings.Replace(testOAuth, c.from, c.to, 1))
		// When Mira starts
		_, problems := Load([]string{dir}, map[string]map[string]any{"dienst": {"server": "https://login.example"}})
		// Then the skill does not start and the problem names the field
		if len(problems) != 1 || !strings.Contains(problems[0].Text(i18n.DE), c.want) {
			t.Errorf("%s: problems %+v", c.want, problems)
		}
	}
	// token: oauth without an oauth section
	writeOAuthSkill(t, dir, "    mcp:\n      url: https://dienst.example/mcp\n      token: oauth\n")
	if _, problems := Load([]string{dir}, map[string]map[string]any{"dienst": nil}); len(problems) != 1 ||
		!strings.Contains(problems[0].Text(i18n.DE), "keinen Abschnitt oauth") {
		t.Errorf("token oauth without a section: %+v", problems)
	}
}
