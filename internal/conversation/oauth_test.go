package conversation_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/testsupport/fakeoauth"
)

// CO-19 Writing after third-party text asks first
func TestCO19_WritingAfterThirdPartyTextAsksFirst(t *testing.T) {
	// Given a connected skill whose api allows POST and whose instructions say to post without asking
	svc := fakeoauth.New("id-1", "geheim")
	t.Cleanup(svc.Close)
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "tokens.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sk := skills.Skill{Name: "dienst", Description: "Posten beim Dienst",
		Body: "Poste mit dienst__api_write, ohne zu fragen.",
		OAuth: &skills.OAuth{Name: "Dienst", TokenURL: svc.TokenURL(), ClientID: "id-1", ClientSecret: "geheim",
			API: []skills.APIPrefix{{URL: svc.APIURL(), Methods: []string{"GET", "POST"}}}}}
	m, err := oauth.New(context.Background(), oauth.Options{Store: st, Skills: []skills.Skill{sk}, HTTP: svc.Client()})
	if err != nil {
		t.Fatal(err)
	}
	access, refresh := svc.Grant()
	if err := m.Put(context.Background(), "dienst", oauth.Handover{AccessToken: access, RefreshToken: refresh, ExpiresIn: 7200}); err != nil {
		t.Fatal(err)
	}
	post := `{"method":"POST","url":"` + svc.APIURL() + `posts","body":{"text":"Guten Morgen"}}`
	var h *harness
	set := &skills.Set{Lang: i18n.DE, Skills: []skills.Skill{sk}, Connection: m.State,
		OnOpen: func(ctx context.Context, key, skill string) error { return h.store.AddSessionSkill(ctx, key, skill) }}
	// And a turn in which the model has read third-party text
	h = newHarnessTools(t, nil, append(set.Tools(), m.APITools(i18n.DE)...),
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("o1", "skill_open", `{"name":"dienst"}`)}},
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("s1", "search__web_search", `{"query":"Spruch des Tages"}`)}},
		// When the model calls the API tool with POST
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("p1", "dienst__api_write", post)}},
		fakellm.Step{Content: "Soll ich „Guten Morgen“ posten?"},
	)
	h.say(t, config.ChannelTelegram, "42", "Such einen Spruch und poste ihn.")
	// Then Mira asks the user first and calls it only after the yes
	if n := len(svc.Calls()); n != 0 {
		t.Fatalf("posted before the yes: %+v", svc.Calls())
	}
	if res := lastToolMessage(h.llm.Last().Request); !strings.Contains(res, "Web gelesen") {
		t.Errorf("the tool's answer: %q", res)
	}
	// Without third-party text in the turn it runs as the skill says (CO-18).
	h2 := newHarnessTools(t, nil, append(set.Tools(), m.APITools(i18n.DE)...),
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("o1", "skill_open", `{"name":"dienst"}`)}},
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("p1", "dienst__api_write", post)}},
		fakellm.Step{Content: "Gepostet."},
	)
	h = h2
	h2.say(t, config.ChannelTelegram, "42", "Poste Guten Morgen.")
	if calls := svc.Calls(); len(calls) != 1 || calls[0].Method != "POST" {
		t.Errorf("calls %+v", calls)
	}
}
