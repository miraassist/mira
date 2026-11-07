package conversation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
)

// A model that fails for its key or its address is said in the turn and
// shown as a problem (docs/spec/models.md, When a model fails).

func failingTurn(t *testing.T, e *conversation.Engine) (string, error) {
	t.Helper()
	_, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice, ConversationID: "c", Text: "Wie spät ist es?"})
	return conversation.SayOf(err), err
}

// MO-50 A refused key is said in the turn
func TestMO50_ARefusedKeyIsSaidInTheTurn(t *testing.T) {
	e, main, _ := twoModels(t)
	main.Push(fakellm.Step{Status: 401, Body: `{"error":"invalid api key"}`})
	say, err := failingTurn(t, e)
	if say != "Mein KI-Modell lehnt meinen Schlüssel ab. Das muss auf meiner Seite unter Modelle behoben werden." {
		t.Fatalf("say %q, err %v", say, err)
	}
	// Not kept in the session's history: the next turn does not see it.
	main.Push(fakellm.Step{Content: "Zehn Uhr."})
	handle(t, e, config.ChannelVoice, "Und jetzt?")
	if body := string(main.Last().RawBody); strings.Contains(body, "lehnt meinen Schlüssel") {
		t.Errorf("the sentence went into the history: %.300s", body)
	}
}

// MO-51 Gemini's missing key counts as a refused key
func TestMO51_GeminisMissingKeyCountsAsARefusedKey(t *testing.T) {
	e, main, _ := twoModels(t)
	main.Push(fakellm.Step{Status: 400, Body: `[{"error":{"code":400,"message":"Missing or invalid Authorization header."}}]`})
	if say, err := failingTurn(t, e); !strings.Contains(say, "lehnt meinen Schlüssel ab") {
		t.Fatalf("say %q, err %v", say, err)
	}
}

// MO-52 An unreachable model is said in the turn
func TestMO52_AnUnreachableModelIsSaidInTheTurn(t *testing.T) {
	e, main, _ := twoModels(t)
	main.Close() // nobody answers at its address any more
	if say, err := failingTurn(t, e); say != "Ich erreiche mein KI-Modell gerade nicht." {
		t.Fatalf("say %q, err %v", say, err)
	}
	problems := e.ModelProblems(i18n.DE)
	if len(problems) != 1 || problems[0].Model != "main" || !strings.HasPrefix(problems[0].Text, "Der Server antwortet nicht: ") {
		t.Errorf("problems: %+v", problems)
	}
}

// MO-53 Any other failure stays the service's error
func TestMO53_AnyOtherFailureStaysTheServicesError(t *testing.T) {
	e, main, _ := twoModels(t)
	for range 5 { // the retries too
		main.Push(fakellm.Step{Status: 500, Body: "oops"})
	}
	say, err := failingTurn(t, e)
	if err == nil || say != "" {
		t.Fatalf("say %q, err %v", say, err)
	}
	if problems := e.ModelProblems(i18n.DE); len(problems) != 0 {
		t.Errorf("a server error is no problem for the user: %+v", problems)
	}
}

// MO-54 The failing model is a problem until it answers again
func TestMO54_TheFailingModelIsAProblemUntilItAnswersAgain(t *testing.T) {
	e, main, smart := twoModelsDelivering(t, "conversation.voice: { model: smart }", nil, func(c *config.Config) {
		m := c.Models["smart"]
		m.Fallback = config.MainModel
		c.Models["smart"] = m
	})
	smart.Push(fakellm.Step{Status: 401, Body: "unauthorized"})
	main.Push(fakellm.Step{Content: "Zehn Uhr."})
	if r := handle(t, e, config.ChannelVoice, "Wie spät ist es?"); r.Text != "Zehn Uhr." {
		t.Fatalf("the fallback's answer: %+v", r)
	}
	problems := e.ModelProblems(i18n.DE)
	if len(problems) != 1 || problems[0].Model != "smart" || problems[0].Text != "Der Server lehnt den Schlüssel ab (llm server returned 401: unauthorized)." {
		t.Fatalf("problems: %+v", problems)
	}
	smart.Push(fakellm.Step{Content: "Elf Uhr."})
	handle(t, e, config.ChannelVoice, "Und jetzt?")
	if problems := e.ModelProblems(i18n.DE); len(problems) != 0 {
		t.Errorf("still a problem after smart answered: %+v", problems)
	}
}
