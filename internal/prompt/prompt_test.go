package prompt

import (
	"encoding/json"
	"flag"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/llm"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestCanonicalJSONIsOrderIndependent(t *testing.T) {
	a := []byte(`{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"number"}},"required":["a","b"]}`)
	b := []byte("{\n  \"required\": [\"a\", \"b\"],\n  \"properties\": { \"a\": {\"type\": \"number\"}, \"b\": {\"type\":\"string\"} },\n  \"type\": \"object\"\n}")

	ca, err := CanonicalJSON(a)
	if err != nil {
		t.Fatalf("CanonicalJSON(a): %v", err)
	}
	cb, err := CanonicalJSON(b)
	if err != nil {
		t.Fatalf("CanonicalJSON(b): %v", err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("same schema serialised differently:\n  %s\n  %s", ca, cb)
	}
	if strings.ContainsAny(string(ca), " \n\t") {
		t.Errorf("canonical form contains whitespace: %s", ca)
	}
	want := `{"properties":{"a":{"type":"number"},"b":{"type":"string"}},"required":["a","b"],"type":"object"}`
	if string(ca) != want {
		t.Errorf("canonical form =\n  %s\nwant\n  %s", ca, want)
	}
}

func TestCanonicalJSONPreservesArrayOrder(t *testing.T) {
	// "required" and "enum" are ordered in practice even though JSON Schema
	// treats them as sets; reordering them would change the model's reading.
	got, err := CanonicalJSON([]byte(`{"enum":["z","a","m"]}`))
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if string(got) != `{"enum":["z","a","m"]}` {
		t.Errorf("array order changed: %s", got)
	}
}

func TestCanonicalJSONNormalisesNumbers(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"n":1.0}`, `{"n":1}`},
		{`{"n":1}`, `{"n":1}`},
		{`{"n":1e2}`, `{"n":100}`},
		{`{"n":1.5}`, `{"n":1.5}`},
	} {
		got, err := CanonicalJSON([]byte(tc.in))
		if err != nil {
			t.Fatalf("CanonicalJSON(%s): %v", tc.in, err)
		}
		if string(got) != tc.want {
			t.Errorf("CanonicalJSON(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalJSONHandlesEmptyAndInvalid(t *testing.T) {
	got, err := CanonicalJSON(nil)
	if err != nil || string(got) != "{}" {
		t.Errorf("empty schema = %s (%v), want {}", got, err)
	}
	if _, err := CanonicalJSON([]byte(`{"broken":`)); err == nil {
		t.Error("invalid JSON should be an error, not a silent empty schema")
	}
}

// sampleTools is deliberately given in a messy order with messy schemas.
func sampleTools() []ToolDef {
	return []ToolDef{
		{Server: "search", Name: "web_search", Description: "Durchsucht das Web.",
			Schema: json.RawMessage(`{ "type":"object", "properties": { "query": {"type":"string"}, "limit": {"type":"integer","default":5.0} }, "required":["query"] }`)},
		{Server: "ha", Name: "GetLiveContext", Description: "Liefert aktuelle Zustände.",
			Schema: json.RawMessage(`{"type":"object","properties":{}}`)},
		{Name: "memory_save", Description: "Merkt sich einen Fakt.",
			Schema: json.RawMessage(`{"properties":{"text":{"type":"string"},"tags":{"items":{"type":"string"},"type":"array"}},"type":"object","required":["text"]}`)},
		{Server: "ha", Name: "HassTurnOn", Description: "Schaltet etwas ein.",
			Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"area":{"type":"string"}}}`)},
	}
}

func TestBuildToolsSortsByServerThenName(t *testing.T) {
	tools, err := BuildTools(sampleTools())
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	want := []string{"memory_save", "ha__GetLiveContext", "ha__HassTurnOn", "search__web_search"}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(tools), len(want))
	}
	for i := range want {
		if tools[i].Function.Name != want[i] {
			t.Errorf("tool %d = %s, want %s", i, tools[i].Function.Name, want[i])
		}
	}
}

func TestBuildToolsIsOrderIndependent(t *testing.T) {
	base, err := BuildTools(sampleTools())
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	reference := renderTools(base)

	// Whatever order the MCP servers answer in, layer 3 must come out the same.
	rng := rand.New(rand.NewSource(7))
	for round := range 25 {
		shuffled := sampleTools()
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got, err := BuildTools(shuffled)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if renderTools(got) != reference {
			t.Fatalf("round %d produced a different layer 3", round)
		}
	}
}

func TestBuildToolsRejectsDuplicates(t *testing.T) {
	_, err := BuildTools([]ToolDef{
		{Server: "ha", Name: "x", Schema: json.RawMessage(`{}`)},
		{Server: "ha", Name: "x", Schema: json.RawMessage(`{}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

func TestQualifiedNameRoundTrip(t *testing.T) {
	for _, tc := range []struct{ server, name string }{
		{"ha", "HassTurnOn"},
		{"", "memory_save"},
		{"search", "web_fetch"},
	} {
		d := ToolDef{Server: tc.server, Name: tc.name}
		server, name := SplitQualifiedName(d.QualifiedName())
		if server != tc.server || name != tc.name {
			t.Errorf("round trip of %+v gave (%q, %q)", tc, server, name)
		}
	}
}

const sampleStructure = `Bereich Küche (kueche)
  light.kueche_decke — Deckenlicht [Licht, Küchenlicht]
  sensor.kueche_temperatur — Temperatur
Bereich Bad (bad)
  light.bad_spiegel — Spiegellicht`

func samplePrefix(t *testing.T, ch config.Channel) Prefix {
	t.Helper()
	tools, err := BuildTools(sampleTools())
	if err != nil {
		t.Fatalf("BuildTools: %v", err)
	}
	return Prefix{
		Agent:     AgentLayer(i18n.DE, "Du duzt den Nutzer."),
		Style:     StyleFor(i18n.DE, ch),
		Tools:     tools,
		Structure: sampleStructure,
		Profile:   "Stefan trinkt morgens Kaffee. Die Photovoltaikanlage ist wichtig.",
	}
}

func TestPrefixHashIsStableAcrossBuilds(t *testing.T) {
	first := samplePrefix(t, config.ChannelVoice).Hash()
	for range 10 {
		if got := samplePrefix(t, config.ChannelVoice).Hash(); got != first {
			t.Fatalf("prefix hash changed between identical builds: %s vs %s", got, first)
		}
	}
}

func TestPrefixHashSeparatesChannels(t *testing.T) {
	voice := samplePrefix(t, config.ChannelVoice).Hash()
	telegram := samplePrefix(t, config.ChannelTelegram).Hash()
	if voice == telegram {
		t.Error("voice and telegram share a prefix hash; they have different style layers")
	}
}

func TestPrefixHashReactsToEveryStableLayer(t *testing.T) {
	base := samplePrefix(t, config.ChannelVoice)
	baseHash := base.Hash()

	mutations := map[string]func(p *Prefix){
		"layer 1 agent": func(p *Prefix) { p.Agent += " Noch eine Regel." },
		"layer 2 style": func(p *Prefix) { p.Style += " Kürzer." },
		"layer 3 tools": func(p *Prefix) {
			p.Tools = append(p.Tools, llm.Tool{Type: "function",
				Function: llm.ToolFunction{Name: "zz__new", Parameters: json.RawMessage(`{}`)}})
		},
		"layer 4 structure": func(p *Prefix) { p.Structure += "\n  light.neu — Neu" },
		"layer 5 profile":   func(p *Prefix) { p.Profile += " Mag Tee." },
	}
	for name, mutate := range mutations {
		p := base
		p.Tools = append([]llm.Tool(nil), base.Tools...)
		mutate(&p)
		if p.Hash() == baseHash {
			t.Errorf("a change to %s did not change the prefix hash", name)
		}
	}
}

func TestPrefixHashIgnoresNothingSilently(t *testing.T) {
	// Length prefixing: moving text across a layer boundary must not collide.
	a := Prefix{Agent: "ab", Style: "c"}
	b := Prefix{Agent: "a", Style: "bc"}
	if a.Hash() == b.Hash() {
		t.Error("layer boundaries are not part of the hash")
	}
}

func TestBuildPlacesTurnLast(t *testing.T) {
	p := samplePrefix(t, config.ChannelVoice)
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "alter turn"},
		{Role: llm.RoleAssistant, Content: "alte antwort"},
	}
	req := Build(p, history, Turn{
		Now:            time.Date(2026, 10, 14, 7, 12, 0, 0, time.UTC),
		ChannelContext: "Kanal: Sprache, Satellit Küche.",
		Text:           "Mach das Licht an",
	})
	if want := len(p.Messages()) + 3; len(req.Messages) != want {
		t.Fatalf("got %d messages, want the stable start + 2 history + turn = %d", len(req.Messages), want)
	}
	if req.Messages[0].Role != llm.RoleSystem {
		t.Errorf("first message is %s, want system", req.Messages[0].Role)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Content, "Mach das Licht an") {
		t.Errorf("last message is not the current turn: %+v", last)
	}
	if req.PrefixHash != p.Hash() {
		t.Errorf("request carries the wrong prefix hash")
	}
}

func TestVolatileDataNeverReachesTheStableLayers(t *testing.T) {
	p := samplePrefix(t, config.ChannelVoice)
	system := p.SystemText()

	turn := Turn{
		Now:                 time.Date(2026, 10, 14, 7, 12, 0, 0, time.UTC),
		ChannelContext:      "Kanal: Sprache, Satellit Küche.",
		Memories:            []string{"Der Wechselrichter steht im Keller."},
		NoticeContext:       "Wechselrichter meldet seit 03:14 Uhr einen Fehler.",
		PendingConfirmation: "Haustür abschließen",
		Extra:               "Vom Satelliten durchgereicht.",
		Text:                "Was ist los?",
	}
	rendered := turn.Render()

	for _, volatile := range []string{
		"7:12", "Satellit Küche", "Wechselrichter steht im Keller",
		"03:14", "Haustür abschließen", "durchgereicht", "Was ist los?",
	} {
		if strings.Contains(system, volatile) {
			t.Errorf("%q leaked from layer 7 into the cached system text", volatile)
		}
		if !strings.Contains(rendered, volatile) {
			t.Errorf("%q is missing from the rendered turn", volatile)
		}
	}

	// Building twice with different turns must not move the prefix hash.
	h1 := Build(p, nil, turn).PrefixHash
	turn.Now = turn.Now.Add(time.Hour)
	turn.Text = "etwas ganz anderes"
	h2 := Build(p, nil, turn).PrefixHash
	if h1 != h2 {
		t.Error("the prefix hash depends on the current turn")
	}
}

func TestFormatTimeIsGermanAndDeterministic(t *testing.T) {
	got := FormatTime(i18n.DE, time.Date(2026, 10, 14, 7, 5, 0, 0, time.UTC))
	want := "Mittwoch, 14. Oktober 2026, 07:05 Uhr"
	if got != want {
		t.Errorf("FormatTime = %q, want %q", got, want)
	}
}

// Golden files pin the exact bytes of the stable layers. A diff here means the
// prompt changed; that is allowed, but it must be a deliberate release change,
// not an accident, so the test forces someone to look.
func TestGoldenPrefix(t *testing.T) {
	for _, ch := range []config.Channel{config.ChannelVoice, config.ChannelTelegram, config.ChannelBackground} {
		t.Run(string(ch), func(t *testing.T) {
			p := samplePrefix(t, ch)
			var b strings.Builder
			b.WriteString("=== layers 1,4,5 (system message) ===\n")
			b.WriteString(p.SystemText())
			b.WriteString("\n\n=== layer 2 (channel style, after tools and examples) ===\n")
			for _, m := range p.StyleMessages() {
				b.WriteString(string(m.Role) + ": " + m.Content + "\n")
			}
			b.WriteString("\n\n=== layer 3 (tools) ===\n")
			for _, tool := range p.Tools {
				b.WriteString(tool.Function.Name)
				b.WriteString("\n  ")
				b.WriteString(tool.Function.Description)
				b.WriteString("\n  ")
				b.Write(tool.Function.Parameters)
				b.WriteString("\n")
			}
			b.WriteString("\n=== prefix hash ===\n")
			b.WriteString(p.Hash())
			b.WriteString("\n")
			compareGolden(t, "prefix_"+string(ch)+".txt", b.String())
		})
	}
}

func TestGoldenTurn(t *testing.T) {
	turn := Turn{
		Now:            time.Date(2026, 10, 14, 7, 12, 0, 0, time.UTC),
		ChannelContext: "Kanal: Sprache, Satellit Küche. Ohne Raumangabe ist die Küche gemeint.",
		Memories:       []string{"Der Wechselrichter steht im Keller.", "Stefan steht um 6:30 auf."},
		NoticeContext:  "Wechselrichter: Fehlerzustand seit 03:14 Uhr, Code E-042.",
		Text:           "Guten Morgen, was gibt's?",
	}
	compareGolden(t, "turn_voice.txt", turn.Render())
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run: go test ./internal/prompt -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// TestGoldenEnglish pins the English prompt: layers 1 and 2, the sample
// exchange and a turn. Reviewing testdata/*_en.txt is how the English
// wording is checked; it has not been tested against a model yet.
func TestGoldenEnglish(t *testing.T) {
	tools, err := BuildTools(sampleTools())
	if err != nil {
		t.Fatal(err)
	}
	p := Prefix{
		Lang:      i18n.EN,
		Agent:     AgentLayer(i18n.EN, ""),
		Style:     StyleFor(i18n.EN, config.ChannelVoice),
		Tools:     tools,
		Structure: "Area Kitchen (kitchen)\n  light.kitchen — Kitchen light",
		Profile:   "The household drinks coffee in the morning.",
		Examples:  Examples(i18n.EN, tools),
	}
	var b strings.Builder
	b.WriteString("=== system ===\n" + p.SystemText() + "\n\n=== examples ===\n")
	for _, m := range p.Examples {
		b.WriteString(string(m.Role) + ": " + m.Content)
		for _, c := range m.ToolCalls {
			b.WriteString(c.Function.Name + " " + c.Function.Arguments)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n=== style ===\n")
	for _, m := range p.StyleMessages() {
		b.WriteString(string(m.Role) + ": " + m.Content + "\n")
	}
	turn := Turn{Lang: i18n.EN, Now: time.Date(2026, 10, 14, 7, 12, 0, 0, time.UTC),
		ChannelContext:      "Channel: voice, satellite in area Kitchen. Without a room given, area Kitchen is meant.",
		Memories:            []string{"The inverter is in the basement."},
		PendingConfirmation: "lock.front_door: lock",
		Text:                "Good morning, what's up?"}
	b.WriteString("\n=== turn ===\n" + turn.Render() + "\n")
	compareGolden(t, "prefix_voice_en.txt", b.String())
}
