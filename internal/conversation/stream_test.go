package conversation_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/fakellm"
	"github.com/miraassist/mira/internal/tools"
)

// pieces collects what a turn hands on while it is written, with the time
// the first piece came.
type pieces struct {
	mu    sync.Mutex
	got   []string
	first time.Time
	// order is the text and the tool calls as they came: "call:name",
	// "done:name" for a tool.
	order []string
}

func (p *pieces) add(piece conversation.Piece) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if tool := piece.Tool; tool != nil {
		if tool.Done {
			p.order = append(p.order, "done:"+tool.Name)
		} else {
			p.order = append(p.order, "call:"+tool.Name)
		}
		return
	}
	if p.first.IsZero() {
		p.first = time.Now()
	}
	p.got = append(p.got, piece.Text)
	p.order = append(p.order, piece.Text)
}

// merged is the order with the text between two tool calls in one entry.
func (p *pieces) merged() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	text := false
	for _, o := range p.order {
		isText := !strings.HasPrefix(o, "call:") && !strings.HasPrefix(o, "done:")
		if isText && text {
			out[len(out)-1] += o
			continue
		}
		out = append(out, o)
		text = isText
	}
	return out
}

func (p *pieces) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.got, "")
}

// streamed runs a voice turn with a sink and says how long after the first
// piece the turn ended.
func streamed(t *testing.T, e *conversation.Engine, text string) (conversation.Response, *pieces, time.Duration) {
	t.Helper()
	p := &pieces{}
	resp, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice, ConversationID: "v1",
		DeviceID: "sat-kueche", Text: text, Language: "de", Stream: p.add})
	if err != nil {
		t.Fatal(err)
	}
	if p.first.IsZero() {
		t.Fatalf("nothing was handed on; answer %q", resp.Text)
	}
	return resp, p, time.Since(p.first)
}

const story = "Es war einmal ein Leuchtturmwärter, der jeden Abend die Treppe hinaufstieg. "

// ST-02: after a tool round the answer is handed on in pieces before the
// model has finished.
func TestST02_AfterAToolRoundTheAnswerIsHandedOnInPieces(t *testing.T) {
	h := newHarness(t,
		fakellm.Step{ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
		fakellm.Step{Content: "In der Küche sind es 21,5 Grad, und das Küchenlicht ist aus.", PieceDelay: 60 * time.Millisecond},
	)
	resp, p, after := streamed(t, h.engine, "Wie warm ist es in der Küche?")
	if after < 200*time.Millisecond || len(p.got) < 5 {
		t.Fatalf("first piece only %v before the end, %d pieces", after, len(p.got))
	}
	if p.text() != resp.Text {
		t.Fatalf("handed on %q, answer %q", p.text(), resp.Text)
	}
}

// ST-03: an answer without a tool call is handed on in pieces too.
func TestST03_AnAnswerWithoutAToolCallIsHandedOnInPiecesToo(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: story + story, PieceDelay: 30 * time.Millisecond})
	resp, p, after := streamed(t, h.engine, "Erzähl mir eine Geschichte.")
	if after < 200*time.Millisecond || strings.TrimSpace(p.text()) != resp.Text || resp.ToolRounds != 0 {
		t.Fatalf("after %v, handed on %q, answer %q", after, p.text(), resp.Text)
	}
}

// ST-04: thinking is never handed on, also when its tags are split.
func TestST04_ThinkingIsNeverSpoken(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: "<think>geheim</think>Hallo Welt.",
		Pieces: []string{"<thi", "nk>gehe", "im</th", "ink>Hallo ", "Welt."}})
	resp, p, _ := streamed(t, h.engine, "Hallo")
	if strings.Contains(p.text(), "geheim") || strings.Contains(p.text(), "think") || p.text() != "Hallo Welt." {
		t.Fatalf("handed on %q", p.text())
	}
	if resp.Text != "Hallo Welt." {
		t.Fatalf("answer %q", resp.Text)
	}
}

// The text a model writes before its tools is handed on too, a space
// between it and the answer.
func TestStreamedTextBeforeToolsAndTheAnswerAreSentences(t *testing.T) {
	h := newHarness(t,
		fakellm.Step{Content: "Ich schaue nach.", ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
		fakellm.Step{Content: "Das Küchenlicht ist aus."},
	)
	_, p, _ := streamed(t, h.engine, "Ist das Küchenlicht an?")
	if p.text() != "Ich schaue nach. Das Küchenlicht ist aus." {
		t.Fatalf("handed on %q", p.text())
	}
}

// ST-05: a waiting message follows as the last piece.
func TestST05_UebrigensComesAsTheLastPiece(t *testing.T) {
	model := fakellm.New(fakellm.Step{Content: "Gern geschehen."})
	defer model.Close()
	cfg := config.Default()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := conversation.New(conversation.Options{StructurePrompt: func() string { return harnessHousePrompt },
		Config: cfg, LLM: llm.New(llm.Options{BaseURL: model.BaseURL(), Model: "m"}), Store: st,
		Tools: tools.NewRegistry(nil, tools.Internal(i18n.DE, st, nil)...), Guard: tools.NewGuard(i18n.DE, cfg.Security, st, houseSnapshot, hooks.Load("", i18n.DE, nil)),
		Structure: houseSnapshot, Logger: quietLogger(), Location: time.UTC,
		Afterword: func(context.Context, string, string) string { return "Übrigens: Heute hast du einen Zahnarzttermin." },
	})
	p := &pieces{}
	r, err := e.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice, ConversationID: "c",
		DeviceID: "sat-kueche", Text: "Danke dir", Stream: p.add})
	if err != nil {
		t.Fatal(err)
	}
	if p.text() != r.Text || p.got[len(p.got)-1] != " Übrigens: Heute hast du einen Zahnarzttermin." {
		t.Fatalf("pieces %q, answer %q", p.got, r.Text)
	}
}

// ST-06: a model that fails after the first piece gets no fallback; a fixed
// sentence ends the answer.
func TestST06_AModelThatFailsAfterTheFirstPieceGetsNoFallback(t *testing.T) {
	e, main, smart := twoModelsDelivering(t, "conversation.voice: { model: smart }", nil, func(c *config.Config) {
		s := c.Models["smart"]
		s.Fallback = config.MainModel
		c.Models["smart"] = s
	})
	smart.Push(fakellm.Step{Content: story, BreakAfter: 3})
	main.Push(fakellm.Step{Content: "Hallo von main."})
	r, p, _ := streamed(t, e, "Erzähl mir eine Geschichte.")
	if r.Text != "Es war einmal Da ist etwas schiefgegangen." || p.text() != r.Text || r.FallbackFrom != "" {
		t.Fatalf("answer %q, handed on %q, fallback from %q", r.Text, p.text(), r.FallbackFrom)
	}
	for _, req := range main.Requests() {
		if req.Request.MaxTokens != 1 {
			t.Fatalf("main was asked: %s", req.RawBody)
		}
	}
}

// ST-07: Telegram stays whole: nothing is handed on, the model is not asked
// to stream.
func TestST07_TelegramAndBackgroundRunsStayWhole(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: story})
	p := &pieces{}
	if _, err := h.engine.Handle(context.Background(), conversation.Request{Channel: config.ChannelTelegram,
		ConversationID: "1", Text: "Erzähl mir eine Geschichte.", Stream: p.add}); err != nil {
		t.Fatal(err)
	}
	if len(p.got) != 0 || h.llm.Last().Request.Stream {
		t.Fatalf("handed on %q, stream %v", p.got, h.llm.Last().Request.Stream)
	}
}

// ST-13: a model that speaks before its tools: its sentence goes on at
// once, before the tool call that makes Home Assistant speak it, and no
// sentence of Mira's own comes.
func TestST13_AModelThatSpeaksBeforeItsToolsIsHandedOnFirst(t *testing.T) {
	h := newHarness(t,
		fakellm.Step{Content: "Ich schaue im Kalender nach. ", ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
		fakellm.Step{Content: "Morgen hast du keine Termine."},
	)
	_, p, _ := streamed(t, h.engine, "Was steht morgen an?")
	want := []string{"Ich schaue im Kalender nach. ", "call:ha__GetLiveContext", "done:ha__GetLiveContext", "Morgen hast du keine Termine."}
	if got := p.merged(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("order %q", got)
	}
}

// ST-17: Home Assistant ends the turn while the model writes (the wake
// word, "Stopp"): the model stops, no fixed sentence is added, the transcript
// keeps what was said, and the log says how far it got
// (docs/spec/streaming.md, ways to interrupt).
func TestST17_AnAnswerEndedWhileSpokenIsLoggedWithWhatWasSaid(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: story + story + story, PieceDelay: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &pieces{}
	_, err := h.engine.Handle(ctx, conversation.Request{Channel: config.ChannelVoice, ConversationID: "v1",
		Text: "Erzähl mir eine Geschichte.", Language: "de", Stream: func(piece conversation.Piece) {
			p.add(piece)
			if len(p.got) == 3 {
				cancel()
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	said := p.text()
	if strings.Contains(said, "schiefgegangen") || len(p.got) > 4 {
		t.Fatalf("handed on %q", p.got)
	}
	msgs, err := h.store.ChatMessages(context.Background(), "voice:v1")
	if err != nil || len(msgs) == 0 {
		t.Fatalf("transcript %v, %v", msgs, err)
	}
	if last := msgs[len(msgs)-1]; last.Role != llm.RoleAssistant || last.Content != said {
		t.Fatalf("transcript ends with %+v, said %q", last, said)
	}
	// The log says how long the round ran and how much was said by then.
	line := ""
	for _, l := range strings.Split(h.logs.String(), "\n") {
		if strings.Contains(l, `msg="answer ended by the caller"`) {
			line = l
		}
	}
	if !strings.Contains(line, "ms=") || !strings.Contains(line, fmt.Sprintf("spoken_chars=%d", len([]rune(said)))) {
		t.Errorf("log line %q, said %d chars", line, len([]rune(said)))
	}
	if logs := h.logs.String(); !strings.Contains(logs, "status=cancelled") || strings.Contains(logs, "status=failed") {
		t.Errorf("the run is not cancelled:\n%s", logs)
	}
}

// ST-18: a turn ended before its first piece is no error: one info line
// with nothing spoken, and the run cancelled.
func TestST18_ATurnEndedBeforeAnythingWasSaidIsNoError(t *testing.T) {
	h := newHarness(t, fakellm.Step{Content: story, Delay: 2 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	_, err := h.engine.Handle(ctx, conversation.Request{Channel: config.ChannelVoice, ConversationID: "v1",
		Text: "Erzähl mir eine Geschichte.", Language: "de", Stream: func(conversation.Piece) {}})
	if err == nil {
		t.Fatal("a turn ended by its caller returned no error")
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `msg="answer ended by the caller"`) || !strings.Contains(logs, "spoken_chars=0") {
		t.Errorf("no line for the ended turn:\n%s", logs)
	}
	for _, bad := range []string{`msg="model round failed"`, `msg="turn failed"`, "status=failed"} {
		if strings.Contains(logs, bad) {
			t.Errorf("logged %s:\n%s", bad, logs)
		}
	}
	if !strings.Contains(logs, "status=cancelled") {
		t.Errorf("the run is not cancelled:\n%s", logs)
	}
}

// ST-14: the text of a round with a tool was spoken; the tool's result says
// so, so the model does not say it again. A turn that is not streamed gets
// no note.
func TestST14_TextSpokenBeforeATool_IsNotedInItsResult(t *testing.T) {
	steps := func() []fakellm.Step {
		return []fakellm.Step{
			{Content: "Richtig!", ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
			{Content: "Nächste Frage: Wofür steht RAM?"},
		}
	}
	note := "(Schon gesprochen: „Richtig!“."
	toolResult := func(h *harness) string {
		reqs := h.llm.Requests()
		msgs := reqs[len(reqs)-1].Request.Messages
		return msgs[len(msgs)-1].Content
	}
	h := newHarness(t, steps()...)
	streamed(t, h.engine, "Central Processing Unit")
	if got := toolResult(h); !strings.Contains(got, note) {
		t.Errorf("streamed: the tool result lacks the note: %q", got)
	}
	h = newHarness(t, steps()...)
	h.say(t, config.ChannelVoice, "v1", "Central Processing Unit")
	if got := toolResult(h); strings.Contains(got, "Schon gesprochen") {
		t.Errorf("not streamed: a note although nothing was spoken: %q", got)
	}
}

// ST-15: a round with text and a tool is an answer: when the last round
// writes nothing, Mira does not add that she did not finish.
func TestST15_ARoundWithTextAndAToolIsAnAnswer(t *testing.T) {
	h := newHarness(t,
		fakellm.Step{Content: "Richtig! Nächste Frage: Wofür steht RAM?", ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
		fakellm.Step{Content: ""},
	)
	_, p, _ := streamed(t, h.engine, "Central Processing Unit")
	if strings.Contains(p.text(), "nicht ganz fertig") {
		t.Errorf("handed on %q", p.text())
	}
}

// ST-16: the question stands in a round with a tool, and the last round adds
// a filler: the satellite listens all the same.
func TestST16_AQuestionBeforeAToolOrAFillerKeepsTheSatelliteListening(t *testing.T) {
	h := newHarness(t,
		fakellm.Step{Content: "Richtig! Wie viele Beine hat eine Spinne?", ToolCalls: []llm.ToolCall{fakellm.ToolCall("c1", "ha__GetLiveContext", `{}`)}},
		fakellm.Step{Content: "Ich warte auf deine Antwort."},
	)
	resp, _, _ := streamed(t, h.engine, "Ich denke der Blauwal.")
	if !resp.ContinueConversation {
		t.Error("the satellite does not listen after the question")
	}
}
