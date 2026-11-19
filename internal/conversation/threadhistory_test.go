package conversation_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/hooks"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/tools"
)

// The thread's history (docs/spec/thread-history.md, step 2), with Mira's
// own threads and store; only the model is scripted.

// scriptModel starts a cooking thread when asked to, and else answers.
type scriptModel struct {
	mu   sync.Mutex
	reqs [][]llm.Message
}

func (m *scriptModel) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req.Messages)
	m.mu.Unlock()
	last := req.Messages[len(req.Messages)-1]
	if last.Role == llm.RoleUser && strings.Contains(last.Content, "Fertig") {
		return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{ID: "c2", Type: "function", Function: llm.FunctionCall{Name: "fertig", Arguments: `{}`}}}}}}}, nil
	}
	if last.Role == llm.RoleUser && strings.Contains(last.Content, "Lass uns kochen") {
		return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: threads.ToolStart,
				Arguments: `{"target":"kochen","request":"Pfannkuchen"}`}}}}}}}, nil
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant, Content: "Gut."}}}}, nil
}

func (m *scriptModel) Warmup(context.Context, llm.ChatRequest) error { return nil }

// seen is everything the last request showed the model.
func (m *scriptModel) seen() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []string
	for _, msg := range m.reqs[len(m.reqs)-1] {
		all = append(all, msg.Content)
	}
	return strings.Join(all, "\n")
}

type historyHouse struct {
	engine  *conversation.Engine
	model   *scriptModel
	st      *store.Store
	threads *threads.Manager
	person  string
	mu      sync.Mutex
	now     time.Time
}

func (h *historyHouse) clock() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now
}

func newHistoryHouse(t *testing.T) *historyHouse {
	t.Helper()
	h := &historyHouse{model: &scriptModel{}, now: time.Now()}
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h.st = st
	kochen := skills.Skill{Name: "kochen", Description: "Kochen", Body: "Koch.", Target: &skills.Target{Plan: true, Bound: skills.BoundRoom}}
	vokabeln := skills.Skill{Name: "vokabeln", Description: "Vokabeln", Body: "Frag ab.", Target: &skills.Target{Plan: true}}
	h.threads = threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: []skills.Skill{kochen, vokabeln},
		Clock: h.clock, Logger: quietLogger(),
		Record: func(ctx context.Context, key, record, ack string) error {
			return h.engine.Record(ctx, key, record, ack)
		}})
	// fertig ends the turn's thread, as a skill's tool does (device_finish).
	fertig := tools.InternalTool{Name: "fertig", Description: "beendet den Vorgang", Schema: `{"type":"object","properties":{}}`,
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			h.threads.Finish(ctx, h.threads.TurnThread(ctx, inv))
			return "Ergebnis: fertig.", nil
		}}
	snap := structure.Build(structure.Registry{
		Areas:   []haws.Area{{AreaID: "wohnzimmer", Name: "Wohnzimmer"}, {AreaID: "kueche", Name: "Küche"}},
		Devices: []haws.Device{{ID: "sat-wohnzimmer", AreaID: "wohnzimmer"}, {ID: "sat-kueche", AreaID: "kueche"}},
		Exposed: map[string]bool{},
	})
	cfg := config.Default()
	h.engine = conversation.New(conversation.Options{StructurePrompt: func() string { return harnessHousePrompt },
		Config: cfg, LLM: h.model, Store: st, Tools: tools.NewRegistry(nil, append(h.threads.Tools(), fertig)...),
		Guard:     tools.NewGuard(i18n.DE, cfg.Security, st, houseSnapshot, hooks.Load("", i18n.DE, nil)),
		Structure: func() structure.Snapshot { return snap },
		Identify:  func(context.Context, conversation.Request) (string, string) { return h.person, "" },
		Thread:    h.threads.Context, JoinThread: h.threads.Join, SummarizeThread: h.threads.Summarize,
		Logger: quietLogger(), Location: time.UTC,
	})
	return h
}

func (h *historyHouse) say(t *testing.T, person, conversationID, text string) {
	t.Helper()
	h.person = person
	if _, err := h.engine.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice,
		DeviceID: "sat-wohnzimmer", ConversationID: conversationID, Text: text}); err != nil {
		t.Fatal(err)
	}
}

// startAt starts a thread of target as a turn at place would, outside the
// engine (as code does).
func (h *historyHouse) startAt(t *testing.T, target, place string) store.Thread {
	t.Helper()
	ctx := context.Background()
	if _, err := h.st.UpsertSession(ctx, store.Session{Key: "voice:starter", Channel: "voice", AreaID: "wohnzimmer"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	inv := tools.Invocation{SessionKey: "voice:starter", Channel: config.ChannelVoice, Area: "wohnzimmer", Place: place}
	if _, err := h.threads.Start(ctx, inv, target, "Los"); err != nil {
		t.Fatal(err)
	}
	open, _ := h.st.OpenThreads(ctx, "")
	for _, th := range open {
		if th.Target == target {
			return th
		}
	}
	t.Fatalf("no %s thread", target)
	return store.Thread{}
}

// announce says a question of the thread at the living room's satellite.
func (h *historyHouse) announce(t *testing.T, th store.Thread, question string) {
	t.Helper()
	ctx := context.Background()
	n := store.Message{ID: "m-" + th.ID, Kind: store.KindReply, Source: store.SourceTask, DedupeKey: "q-" + th.ID,
		Importance: 2, Spoken: question, ThreadID: th.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if _, _, err := h.st.PutMessage(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := h.st.MarkDelivered(ctx, n.ID, "announce"); err != nil {
		t.Fatal(err)
	}
	if err := h.st.RecordAnnouncement(ctx, store.Announcement{ID: "a-" + th.ID, DeviceID: "sat-wohnzimmer",
		AreaID: "wohnzimmer", Channel: "voice", MessageIDs: []string{n.ID}}); err != nil {
		t.Fatal(err)
	}
}

func (h *historyHouse) history(t *testing.T, key string) string {
	t.Helper()
	msgs, err := h.st.ChatMessages(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, m := range msgs {
		all = append(all, m.Content)
	}
	return strings.Join(all, "\n")
}

func (h *historyHouse) participants(t *testing.T, id string) []string {
	t.Helper()
	th, err := h.st.GetThread(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return th.Participants
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TV-03 The answer to a thread's announcement runs in the thread's history
// (and TV-08: the announced question is in the thread's history with it).
func TestTV03_TheAnswerToAThreadsAnnouncementRunsInTheThreadsHistory(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "In welchen Raum kommt er?")
	h.say(t, "anna", "c2", "ins Wohnzimmer")
	key := "voice:thread:" + th.ID
	if got := h.history(t, key); !strings.Contains(got, "ins Wohnzimmer") {
		t.Fatalf("thread's history: %q", got)
	}
	if got := h.history(t, "voice:person:anna"); got != "" {
		t.Errorf("Anna's own history: %q", got)
	}
	if p := h.participants(t, th.ID); !has(p, "voice:person:anna") {
		t.Errorf("participants: %v", p)
	}
}

// TV-08 Mira's announcement of a thread is in the thread's history
func TestTV08_MirasAnnouncementOfAThreadIsInTheThreadsHistory(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "In welchen Raum kommt er?")
	h.say(t, "", "c2", "ins Wohnzimmer")
	if got := h.history(t, "voice:thread:"+th.ID); !strings.Contains(got, "In welchen Raum kommt er?") {
		t.Fatalf("thread's history: %q", got)
	}
}

// TV-04 A change of voice in the same conversation stays in the thread's history
func TestTV04_AChangeOfVoiceInTheSameConversationStaysInTheThreadsHistory(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "In welchen Raum kommt er?")
	h.say(t, "anna", "c2", "ins Wohnzimmer")
	h.say(t, "martin", "c2", "Ja, das passt.")
	if seen := h.model.seen(); !strings.Contains(seen, "ins Wohnzimmer") || !strings.Contains(seen, "In welchen Raum kommt er?") {
		t.Errorf("Martin's turn did not see the turn before: %q", seen)
	}
	if got := h.history(t, "voice:person:martin"); got != "" {
		t.Errorf("Martin's own history: %q", got)
	}
	if p := h.participants(t, th.ID); !has(p, "voice:person:anna") || !has(p, "voice:person:martin") {
		t.Errorf("participants: %v", p)
	}
}

// TV-05 A person's thread is continued in the running dialog by another voice
func TestTV05_APersonsThreadIsContinuedInTheRunningDialogByAnotherVoice(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "vokabeln", "person:anna")
	h.announce(t, th, "Was heißt Hund?")
	h.say(t, "martin", "c3", "dog")
	if got := h.history(t, "voice:thread:"+th.ID); !strings.Contains(got, "dog") {
		t.Fatalf("thread's history: %q", got)
	}
	if seen := h.model.seen(); !strings.Contains(seen, "Vokabeln") {
		t.Errorf("Martin's turn did not see Anna's thread: %q", seen)
	}
}

// TV-06 A person's waiting thread is not shown to another person
func TestTV06_APersonsWaitingThreadIsNotShownToAnotherPerson(t *testing.T) {
	h := newHistoryHouse(t)
	h.startAt(t, "vokabeln", "person:anna")
	h.say(t, "martin", "c4", "Wie spät ist es?")
	if seen := h.model.seen(); strings.Contains(seen, "Vokabeln") {
		t.Errorf("Martin saw Anna's waiting thread: %q", seen)
	}
	if got := h.history(t, "voice:person:martin"); !strings.Contains(got, "Wie spät ist es?") {
		t.Errorf("Martin's turn is not in his own history: %q", got)
	}
}

// TV-07 A turn that starts a thread moves into the thread's history
func TestTV07_ATurnThatStartsAThreadMovesIntoTheThreadsHistory(t *testing.T) {
	h := newHistoryHouse(t)
	h.say(t, "anna", "c5", "Wie spät ist es?")
	h.say(t, "anna", "c5", "Lass uns kochen")
	open, _ := h.st.OpenThreads(context.Background(), "")
	if len(open) != 1 {
		t.Fatalf("open threads: %+v", open)
	}
	id := open[0].ID
	key := "voice:thread:" + id
	if got := h.history(t, key); !strings.Contains(got, "Lass uns kochen") || !strings.Contains(got, "Gut.") {
		t.Fatalf("thread's history: %q", got)
	}
	own := h.history(t, "voice:person:anna")
	if strings.Contains(own, "Lass uns kochen") || !strings.Contains(own, "Wie spät ist es?") {
		t.Errorf("Anna's own history: %q", own)
	}
	// The conversation is linked: its next turn runs in the thread's history.
	h.say(t, "martin", "c5", "Und jetzt?")
	if got := h.history(t, key); !strings.Contains(got, "Und jetzt?") {
		t.Errorf("next turn not in the thread's history: %q", got)
	}
	if p := h.participants(t, id); !has(p, "voice:person:anna") || !has(p, "voice:person:martin") {
		t.Errorf("participants: %v", p)
	}
	if sess, err := h.st.GetSession(context.Background(), "voice:person:anna"); err != nil || sess.ThreadID != "" {
		t.Errorf("Anna's own history still linked: %q %v", sess.ThreadID, err)
	}
}

// TV-09 A done thread's summary goes into every participant's own history
func TestTV09_ADoneThreadsSummaryGoesIntoEveryParticipantsOwnHistory(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "Wie viele Eier?")
	h.say(t, "anna", "c6", "Drei")
	h.say(t, "martin", "c6", "Fertig")
	if got, _ := h.st.GetThread(context.Background(), th.ID); got.State != store.ThreadDone {
		t.Fatalf("state %s", got.State)
	}
	for _, own := range []string{"voice:person:anna", "voice:person:martin"} {
		got := h.history(t, own)
		for _, want := range []string{"Ergebnis eines Vorgangs", "„kochen“ erledigt", "Anliegen: Los", "Zuletzt gesagt: Gut."} {
			if !strings.Contains(got, want) {
				t.Errorf("%s misses %q: %q", own, want, got)
			}
		}
	}
	// The thread's history went with the thread.
	if _, err := h.st.GetSession(context.Background(), "voice:thread:"+th.ID); err == nil {
		t.Error("the thread's history is still there")
	}
}

// TV-10 An expired or cancelled thread gets its summary at once
func TestTV10_AnExpiredOrCancelledThreadGetsItsSummaryAtOnce(t *testing.T) {
	h := newHistoryHouse(t)
	ctx := context.Background()
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "Wie viele Eier?")
	h.say(t, "anna", "c7", "Drei")
	if err := h.threads.CancelThread(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.history(t, "voice:person:anna"); !strings.Contains(got, "„kochen“ abgebrochen") {
		t.Errorf("cancelled from the page: %q", got)
	}

	other := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, other, "Welche Pfanne?")
	h.say(t, "martin", "c8", "Die große")
	h.mu.Lock()
	h.now = h.now.Add(48 * time.Hour)
	h.mu.Unlock()
	run, stop := context.WithTimeout(ctx, 300*time.Millisecond)
	defer stop()
	h.threads.Run(run)
	if got := h.history(t, "voice:person:martin"); !strings.Contains(got, "„kochen“ abgelaufen") {
		t.Errorf("expired: %q", got)
	}
}

// TV-11 A summary that cannot be written leaves a trace and spares the others
func TestTV11_ASummaryThatCannotBeWrittenLeavesATraceAndSparesTheOthers(t *testing.T) {
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var log syncBuffer
	written := map[string]string{}
	var mu sync.Mutex
	kochen := skills.Skill{Name: "kochen", Description: "Kochen", Body: "Koch.", Target: &skills.Target{Plan: true, Bound: skills.BoundRoom}}
	m := threads.New(threads.Options{Lang: i18n.DE, Store: st, Targets: []skills.Skill{kochen},
		Logger: slog.New(slog.NewTextHandler(&log, nil)),
		Record: func(_ context.Context, key, record, _ string) error {
			if key == "voice:person:anna" {
				return errors.New("disk full")
			}
			mu.Lock()
			defer mu.Unlock()
			written[key] = record
			return nil
		}})
	ctx := context.Background()
	if _, err := st.UpsertSession(ctx, store.Session{Key: "voice:x", Channel: "voice", AreaID: "wohnzimmer"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(ctx, tools.Invocation{SessionKey: "voice:x", Channel: config.ChannelVoice, Area: "wohnzimmer", Place: "area:wohnzimmer"}, "kochen", "Los"); err != nil {
		t.Fatal(err)
	}
	open, _ := st.OpenThreads(ctx, "")
	id := open[0].ID
	for _, p := range []string{"voice:person:anna", "voice:person:martin"} {
		if err := st.AddThreadParticipant(ctx, id, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.CancelThread(ctx, id); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"could not write the thread's summary", id, "voice:person:anna", "disk full"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log misses %q: %s", want, log.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(written["voice:person:martin"], "abgebrochen") {
		t.Errorf("Martin got: %q", written["voice:person:martin"])
	}
}

// TV-12 The model is told what a summary in a history is
func TestTV12_TheModelIsToldWhatASummaryInAHistoryIs(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "Wie viele Eier?")
	h.say(t, "anna", "c9", "Drei")
	// A turn in a thread's history is told that several people may speak in it.
	if seen := h.model.seen(); !strings.Contains(seen, "verschiedene Personen sprachen") {
		t.Errorf("not told about several speakers: %q", seen)
	}
	h.say(t, "anna", "c9", "Fertig")
	// The record explains itself where the model reads it.
	h.say(t, "anna", "c10", "Wie viele Eier waren es?")
	if seen := h.model.seen(); !strings.Contains(seen, "nicht gesagt. Darauf kannst du dich beziehen") {
		t.Errorf("the record is not explained: %q", seen)
	}
}

// The chat's history (docs/spec/telegram-history.md, step 2): a Telegram
// chat is one history, also in a thread.

const annasChat = "notify.telegram_anna"

// chat is a turn of Anna's Telegram chat, as the adapter sends it.
func (h *historyHouse) chat(t *testing.T, text string) {
	t.Helper()
	h.person = "anna"
	if _, err := h.engine.Handle(context.Background(), conversation.Request{Channel: config.ChannelTelegram,
		ConversationID: annasChat, Text: text}); err != nil {
		t.Fatal(err)
	}
}

// openThread is the one open thread.
func (h *historyHouse) openThread(t *testing.T) store.Thread {
	t.Helper()
	open, err := h.st.OpenThreads(context.Background(), "")
	if err != nil || len(open) != 1 {
		t.Fatalf("open threads: %+v %v", open, err)
	}
	return open[0]
}

// TC-03 A turn of a thread in a chat sees the chat before the thread
func TestTC03_ATurnOfAThreadInAChatSeesTheChatBeforeTheThread(t *testing.T) {
	h := newHistoryHouse(t)
	h.chat(t, "Meine Lieblingszahl ist 37.")
	h.chat(t, "Lass uns kochen")
	th := h.openThread(t)
	h.chat(t, "Welche Lieblingszahl habe ich dir genannt?")
	seen := h.model.seen()
	if !strings.Contains(seen, "Meine Lieblingszahl ist 37.") {
		t.Errorf("the turn did not see the chat before the thread: %q", seen)
	}
	if !strings.Contains(seen, "Laufender Vorgang") || !strings.Contains(seen, "kochen") {
		t.Errorf("the turn does not belong to the thread: %q", seen)
	}
	if got := h.history(t, "telegram:"+annasChat); !strings.Contains(got, "Welche Lieblingszahl") {
		t.Errorf("the turn is not in the chat's history: %q", got)
	}
	if _, err := h.st.GetSession(context.Background(), "telegram:thread:"+th.ID); err == nil {
		t.Error("a thread's history was made for the chat")
	}
}

// TC-04 A turn that starts a thread in a chat is not moved
func TestTC04_ATurnThatStartsAThreadInAChatIsNotMoved(t *testing.T) {
	h := newHistoryHouse(t)
	ctx := context.Background()
	h.chat(t, "Lass uns kochen")
	th := h.openThread(t)
	key := "telegram:" + annasChat
	if got := h.history(t, key); !strings.Contains(got, "Lass uns kochen") || !strings.Contains(got, "Gut.") {
		t.Fatalf("the chat's history: %q", got)
	}
	if _, err := h.st.GetSession(ctx, "telegram:thread:"+th.ID); err == nil {
		t.Error("a thread's history was made for the chat")
	}
	if p := h.participants(t, th.ID); !has(p, key) {
		t.Errorf("participants: %v", p)
	}
	if linked, err := h.st.ConversationThread(ctx, "telegram", annasChat); err != nil || linked != th.ID {
		t.Errorf("chat linked to %q (%v), want %s", linked, err, th.ID)
	}
	if sess, err := h.st.GetSession(ctx, key); err != nil || sess.ThreadID != th.ID {
		t.Errorf("the chat's history is linked to %q (%v), want %s", sess.ThreadID, err, th.ID)
	}
}

// TC-05 A thread's turns stay in the chat after it ends
func TestTC05_AThreadsTurnsStayInTheChatAfterItEnds(t *testing.T) {
	h := newHistoryHouse(t)
	h.chat(t, "Lass uns kochen")
	th := h.openThread(t)
	h.chat(t, "Drei Eier")
	h.chat(t, "Fertig")
	if got, _ := h.st.GetThread(context.Background(), th.ID); got.State != store.ThreadDone {
		t.Fatalf("state %s", got.State)
	}
	got := h.history(t, "telegram:"+annasChat)
	for _, want := range []string{"Lass uns kochen", "Drei Eier", "Fertig", "Ergebnis eines Vorgangs", "„kochen“ erledigt"} {
		if !strings.Contains(got, want) {
			t.Errorf("the chat's history misses %q: %q", want, got)
		}
	}
	if strings.Index(got, "Ergebnis eines Vorgangs") < strings.Index(got, "Drei Eier") {
		t.Errorf("the summary does not follow the thread's turns: %q", got)
	}
}

// TC-06 The thread's heading on Telegram names the chat's history
func TestTC06_TheThreadsHeadingOnTelegramNamesTheChatsHistory(t *testing.T) {
	h := newHistoryHouse(t)
	h.chat(t, "Lass uns kochen")
	h.chat(t, "Und jetzt?")
	seen := h.model.seen()
	if !strings.Contains(seen, "der Verlauf oben ist dieser Chat, der Vorgang ist ein Teil davon") {
		t.Errorf("heading: %q", seen)
	}
	if strings.Contains(seen, "der Verlauf oben ist der dieses Vorgangs") {
		t.Errorf("the voice heading on Telegram: %q", seen)
	}
}

// TC-07 A voice turn in a thread keeps the thread's history
func TestTC07_AVoiceTurnInAThreadKeepsTheThreadsHistory(t *testing.T) {
	h := newHistoryHouse(t)
	th := h.startAt(t, "kochen", "area:wohnzimmer")
	h.announce(t, th, "Wie viele Eier?")
	h.say(t, "anna", "c1", "Drei")
	if got := h.history(t, "voice:thread:"+th.ID); !strings.Contains(got, "Drei") {
		t.Fatalf("thread's history: %q", got)
	}
	if seen := h.model.seen(); !strings.Contains(seen, "der Verlauf oben ist der dieses Vorgangs") {
		t.Errorf("heading: %q", seen)
	}
}

// TC-12 A new conversation keeps the chat known for deliveries
func TestTC12_ANewConversationKeepsTheChatKnownForDeliveries(t *testing.T) {
	h := newHistoryHouse(t)
	ctx := context.Background()
	key := "telegram:" + annasChat
	h.chat(t, "Lass uns kochen")
	if err := h.engine.ResetSession(ctx, config.ChannelTelegram, annasChat); err != nil {
		t.Fatal(err)
	}
	sess, err := h.st.GetSession(ctx, key)
	if err != nil {
		t.Fatalf("the chat is not known after /neu: %v", err)
	}
	if sess.ThreadID != "" {
		t.Errorf("the chat is still linked to %q", sess.ThreadID)
	}
	if got := h.history(t, key); got != "" {
		t.Errorf("the chat's history after /neu: %q", got)
	}
	// The next delivery is recorded in it (the notifier writes it with Record).
	if err := h.engine.Record(ctx, key, "Von Mira selbst in diesen Chat geschickt", "Paket kommt heute."); err != nil {
		t.Fatal(err)
	}
	if got := h.history(t, key); !strings.Contains(got, "Paket kommt heute.") {
		t.Errorf("the record is missing: %q", got)
	}
	// A voice conversation is still dropped whole.
	h.say(t, "", "c9", "Hallo")
	if err := h.engine.ResetSession(ctx, config.ChannelVoice, "c9"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.GetSession(ctx, "voice:c9"); err == nil {
		t.Error("a voice conversation was kept after a reset")
	}
}
