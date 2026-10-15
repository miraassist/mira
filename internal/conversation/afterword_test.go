package conversation_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/conversation"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/testsupport/fakellm"
	"github.com/sker65/mira/internal/tools"
)

func TestAfterwordFollowsAVoiceAnswer(t *testing.T) {
	model := fakellm.New()
	defer model.Close()
	cfg := config.Default()
	cfg.HA.Enabled, cfg.Telegram.Enabled = false, false
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var asked []string
	waiting := "Übrigens: Heute hast du einen Zahnarzttermin."
	internal := append(tools.Internal(i18n.DE, st, nil, nil),
		tools.NoticesRead(i18n.DE, func(context.Context, tools.Invocation) (string, error) { return "Nichts.", nil }))
	e := conversation.New(conversation.Options{
		Config: cfg, LLM: llm.New(llm.Options{BaseURL: model.BaseURL(), Model: "m"}), Store: st,
		Tools: tools.NewRegistry(nil, internal...), Guard: tools.NewGuard(i18n.DE, cfg.Security, st, houseSnapshot),
		Structure: houseSnapshot, Logger: quietLogger(), Location: time.UTC,
		Afterword: func(_ context.Context, area string) string {
			asked = append(asked, area)
			w := waiting
			waiting = ""
			return w
		},
	})
	say := func(conv, text string) conversation.Response {
		r, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice, ConversationID: conv, DeviceID: "sat-kueche", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// A question back gets nothing appended.
	model.Push(fakellm.Step{Content: "Welches Licht meinst du?"})
	if r := say("a", "Licht an"); strings.Contains(r.Text, "Übrigens") || len(asked) != 0 {
		t.Errorf("appended to a question: %q", r.Text)
	}
	// Asking for the notices themselves: nothing appended either.
	model.Push(fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("n", "notices_read", `{}`)}}, fakellm.Step{Content: "Nichts Neues."})
	if r := say("b", "Hast du eine Benachrichtigung?"); r.Text != "Nichts Neues." || len(asked) != 0 {
		t.Errorf("appended to a read-out: %q", r.Text)
	}
	// A plain answer gets the waiting notice, and the transcript has it, so
	// "Was genau?" knows what was said.
	model.Push(fakellm.Step{Content: "Gern geschehen."})
	r := say("c", "Danke dir")
	if r.Text != "Gern geschehen. Übrigens: Heute hast du einen Zahnarzttermin." || len(asked) != 1 || asked[0] != "kueche" {
		t.Fatalf("answer: %q, asked %v", r.Text, asked)
	}
	msgs, _ := st.Messages(context.Background(), conversation.SessionKey(config.ChannelVoice, "c"))
	if last := msgs[len(msgs)-1]; !strings.Contains(last.Content, "Zahnarzttermin") {
		t.Errorf("transcript lacks the afterword: %+v", last)
	}
	// Telegram never gets one.
	model.Push(fakellm.Step{Content: "Erledigt."})
	if _, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelTelegram, ConversationID: "1", Text: "Licht an"}); err != nil || len(asked) != 1 {
		t.Errorf("telegram asked for an afterword: %v", asked)
	}
}
