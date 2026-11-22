package conversation_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	"github.com/miraassist/mira/internal/testsupport/logcap"
	"github.com/miraassist/mira/internal/tools"
)

// The conversation agents (docs/spec/agents.md, Several conversations at
// once, What a turn belongs to).

// slowModel answers, when it holds, once the test releases it, and notes
// how many requests ran at once.
type slowModel struct {
	hold     chan struct{} // nil: answers at once
	entered  chan struct{} // closed and made anew at each request
	mu       sync.Mutex
	reqs     []llm.ChatRequest
	running  atomic.Int32
	maxAtOne atomic.Int32
}

func (m *slowModel) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	n := m.running.Add(1)
	defer m.running.Add(-1)
	for {
		old := m.maxAtOne.Load()
		if n <= old || m.maxAtOne.CompareAndSwap(old, n) {
			break
		}
	}
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	if m.entered != nil {
		close(m.entered)
	}
	m.entered = make(chan struct{})
	hold := m.hold
	m.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{Role: llm.RoleAssistant, Content: "Hallo."}}}}, nil
}

// awaitRunning waits until n requests are in the model at once.
func (m *slowModel) awaitRunning(n int32) {
	for {
		m.mu.Lock()
		if m.entered == nil {
			m.entered = make(chan struct{})
		}
		entered := m.entered
		m.mu.Unlock()
		if m.running.Load() >= n {
			return
		}
		<-entered
	}
}

func (m *slowModel) Warmup(context.Context, llm.ChatRequest) error { return nil }

// last is the turn layer of the last request.
func (m *slowModel) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := m.reqs[len(m.reqs)-1].Messages
	var all []string
	for _, msg := range msgs {
		all = append(all, msg.Content)
	}
	return strings.Join(all, "\n")
}

// house has satellites in the kitchen, the living room and the guest room.
type house struct {
	engine  *conversation.Engine
	model   *slowModel
	logs    *logcap.Lines
	person  string
	private map[string]bool
	mu      sync.Mutex
	plans   map[string]string // place → plan, as the threads would show it
	places  []string
}

// newHouse is the house; a model that holds answers once the test closes
// its hold.
func newHouse(t *testing.T, privacy string, hold chan struct{}) *house {
	t.Helper()
	log, lines := logcap.Logger()
	h := &house{model: &slowModel{hold: hold}, logs: lines, private: map[string]bool{}, plans: map[string]string{}}
	cfg := config.Default()
	m := cfg.Main()
	m.Privacy, m.Parallel = privacy, 1
	if privacy == config.PrivacyCloud {
		m.Parallel = 4
	}
	cfg.Models[config.MainModel] = m
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	snapshot := func() structure.Snapshot {
		snap := structure.Build(structure.Registry{
			Areas: []haws.Area{{AreaID: "kueche", Name: "Küche"}, {AreaID: "wohnzimmer", Name: "Wohnzimmer"},
				{AreaID: "gaeste", Name: "Gästezimmer"}},
			Devices: []haws.Device{{ID: "sat-kueche", AreaID: "kueche"}, {ID: "sat-wohnzimmer", AreaID: "wohnzimmer"},
				{ID: "sat-gaeste", AreaID: "gaeste"}},
			Exposed: map[string]bool{},
		})
		for a := range h.private {
			snap.Private[a] = true
		}
		return snap
	}
	h.engine = conversation.New(conversation.Options{StructurePrompt: func() string { return harnessHousePrompt },
		Config: cfg, LLM: h.model, Store: st, Tools: tools.NewRegistry(nil), Structure: snapshot,
		Identify: func(context.Context, conversation.Request) (string, string) { return h.person, "" },
		Thread: func(_ context.Context, _ store.Session, place string, _ []string, _ string) (string, bool) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.places = append(h.places, place)
			return h.plans[place], false
		},
		Logger: log, Location: time.UTC,
	})
	return h
}

func (h *house) say(t *testing.T, device, conversationID, text string) {
	t.Helper()
	if _, err := h.engine.Handle(context.Background(), conversation.Request{Channel: config.ChannelVoice,
		DeviceID: device, ConversationID: conversationID, Text: text}); err != nil {
		t.Fatal(err)
	}
}

// lastPlace is the place of the last turn; a plan started in it lives there.
func (h *house) lastPlace() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.places[len(h.places)-1]
}

func (h *house) startPlan(place, plan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.plans[place] = plan
}

// AG-95 Two conversations in different rooms run at the same time on a cloud model
func TestAG95_TwoConversationsRunAtTheSameTimeOnACloudModel(t *testing.T) {
	// Given two satellites in different rooms and a cloud model for conversations
	hold := make(chan struct{})
	h := newHouse(t, config.PrivacyCloud, hold)
	// When two persons ask at the same moment
	var wg sync.WaitGroup
	for _, dev := range []string{"sat-kueche", "sat-wohnzimmer"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.say(t, dev, "c-"+dev, "Wie spät ist es?")
		}()
	}
	h.model.awaitRunning(2)
	close(hold)
	wg.Wait()
	// Then both turns run at the same time
	if n := h.model.maxAtOne.Load(); n != 2 {
		t.Errorf("%d requests at once, want 2", n)
	}
}

// AG-96 Two turns of the same session run one after the other
func TestAG96_TwoTurnsOfTheSameSessionRunOneAfterTheOther(t *testing.T) {
	hold := make(chan struct{})
	h := newHouse(t, config.PrivacyCloud, hold)
	// Given a session with a turn still running
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.say(t, "sat-kueche", "same", "Erstens")
	}()
	h.model.awaitRunning(1)
	// When a second turn of the same session arrives
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.say(t, "sat-kueche", "same", "Zweitens")
	}()
	<-h.logs.Await("a turn waits for the turn before it in its session")
	close(hold)
	wg.Wait()
	// Then it starts after the first one has ended
	if n := h.model.maxAtOne.Load(); n != 1 {
		t.Errorf("%d requests at once, want 1", n)
	}
	if !strings.Contains(h.model.last(), "Erstens") || !strings.Contains(h.model.last(), "Zweitens") {
		t.Error("the second turn does not see the first")
	}
}

// AG-B1 A known person's conversation follows them to another satellite
func TestAGB1_AKnownPersonsConversationFollowsThem(t *testing.T) {
	h := newHouse(t, config.PrivacyLocal, nil)
	// Given a person known to Mira (alone at home) talking at the satellite in the kitchen
	h.person = "anna"
	h.say(t, "sat-kueche", "k1", "Merk dir das Wort Erdbeere.")
	kitchen := h.lastPlace()
	h.startPlan(kitchen, "Plan: Kuchen backen")
	// When the same person goes on at the satellite in the living room
	h.say(t, "sat-wohnzimmer", "w1", "Welches Wort war das?")
	// Then the turn belongs to the same conversation, with its threads and plans
	if !strings.Contains(h.model.last(), "Erdbeere") {
		t.Error("the conversation did not follow the person")
	}
	if h.lastPlace() != kitchen || !strings.Contains(h.model.last(), "Kuchen backen") {
		t.Errorf("place %q, kitchen's %q", h.lastPlace(), kitchen)
	}
}

// AG-B2 Without a known person the conversation stays in its room
func TestAGB2_WithoutAKnownPersonTheConversationStaysInItsRoom(t *testing.T) {
	h := newHouse(t, config.PrivacyLocal, nil)
	// Given several persons at home and nobody recognized
	// And a conversation at the satellite in the kitchen
	h.say(t, "sat-kueche", "k1", "Merk dir das Wort Erdbeere.")
	h.startPlan(h.lastPlace(), "Plan: Kuchen backen")
	// When someone talks at the satellite in the living room
	h.say(t, "sat-wohnzimmer", "w1", "Welches Wort war das?")
	// Then that turn belongs to a conversation of its own
	// And it sees nothing of the kitchen's conversation, threads or plans
	if last := h.model.last(); strings.Contains(last, "Erdbeere") || strings.Contains(last, "Kuchen backen") {
		t.Errorf("the living room sees the kitchen: %q", last)
	}
}

// AG-B3 A private area is a hard border
func TestAGB3_APrivateAreaIsAHardBorder(t *testing.T) {
	h := newHouse(t, config.PrivacyLocal, nil)
	// Given the area "Gästezimmer" has the label "Mira: privater Bereich"
	h.private["gaeste"] = true
	// And a known person starts a plan there
	h.person = "anna"
	h.say(t, "sat-gaeste", "g1", "Merk dir das Wort Erdbeere.")
	h.startPlan(h.lastPlace(), "Plan: Kuchen backen")
	// When the same person talks at a satellite in another room
	h.say(t, "sat-kueche", "k1", "Welches Wort war das?")
	// Then the plan of the private area is not there
	if last := h.model.last(); strings.Contains(last, "Kuchen backen") || strings.Contains(last, "Erdbeere") {
		t.Errorf("the kitchen sees the guest room: %q", last)
	}
}

// AG-B4 Nothing comes into a private area from outside
func TestAGB4_NothingComesIntoAPrivateAreaFromOutside(t *testing.T) {
	h := newHouse(t, config.PrivacyLocal, nil)
	// Given a known person with a running plan from the kitchen
	h.person = "anna"
	h.say(t, "sat-kueche", "k1", "Merk dir das Wort Erdbeere.")
	h.startPlan(h.lastPlace(), "Plan: Kuchen backen")
	// And the area "Gästezimmer" has the label "Mira: privater Bereich"
	h.private["gaeste"] = true
	// When the person talks at the satellite in the Gästezimmer
	h.say(t, "sat-gaeste", "g1", "Welches Wort war das?")
	// Then the kitchen's plan is not there
	if last := h.model.last(); strings.Contains(last, "Kuchen backen") || strings.Contains(last, "Erdbeere") {
		t.Errorf("the guest room sees the kitchen: %q", last)
	}
}
