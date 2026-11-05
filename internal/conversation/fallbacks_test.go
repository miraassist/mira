package conversation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
)

// The skills a conversation opened cannot be read: the turn runs without
// their tools, and the log names the session.
func TestFB19_open_skills_that_cannot_be_read_are_in_the_log(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: "Eins."}, fakellm.Step{Content: "Zwei."})
	h.say(t, config.ChannelVoice, "v1", "Hallo")
	if _, err := h.store.DB().Exec(`UPDATE sessions SET skills = '{broken'`); err != nil {
		t.Fatal(err)
	}
	if resp := h.say(t, config.ChannelVoice, "v1", "Und jetzt?"); resp.Text != "Zwei." {
		t.Errorf("answer %q", resp.Text)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "could not read the skills the conversation opened") || !strings.Contains(logs, "session=voice:") {
		t.Errorf("no line for the open skills:\n%s", logs)
	}
}

// What was just announced at the satellite cannot be read: the turn runs
// without it, and the log names the device. No announcement at all stays
// without a line.
func TestFB20_an_announcement_that_cannot_be_read_is_in_the_log(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: "Eins."}, fakellm.Step{Content: "Zwei."})
	turn := func(text string) {
		t.Helper()
		if _, err := h.engine.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice,
			ConversationID: "v1", DeviceID: "dev-kueche", Text: text, Language: "de"}); err != nil {
			t.Fatal(err)
		}
	}
	turn("Hallo")
	if strings.Contains(h.logs.String(), "just announced") {
		t.Fatalf("a line without an announcement:\n%s", h.logs)
	}
	if _, err := h.store.DB().Exec(`DROP TABLE announcements`); err != nil {
		t.Fatal(err)
	}
	turn("Und jetzt?")
	logs := h.logs.String()
	if !strings.Contains(logs, "could not read what was just announced at the satellite") || !strings.Contains(logs, "device=dev-kueche") {
		t.Errorf("no line for the announcement:\n%s", logs)
	}
}
