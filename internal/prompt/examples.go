package prompt

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/sker65/mira/internal/llm"
)

// Examples is a short, fixed sample conversation that follows the system
// message: a lookup and an action, each done through a tool. Local models
// decide by example far more than by instruction. Without one, the model in
// a fresh conversation answers "21 Grad" or "Licht an." from imagination;
// with one, it calls the tool (measured: 0 of 15 house questions without,
// 15 of 15 with). Home Assistant gets the same effect by accident: its
// failed local intent match lands in the chat log as a tool call.
//
// The exchange is part of the cached prefix, so it is fixed text built only
// from the tool list. The house in it is fictional ("Hobbyraum") so the model
// does not take its states for real ones. An example is left out when its
// tool is missing, so the model never sees a call to a tool it lacks.
func Examples(tools []llm.Tool) []llm.Message {
	var out []llm.Message
	if name := findTool(tools, "__GetLiveContext"); name != "" {
		out = append(out, exchange("ex1", "Ist im Hobbyraum noch Licht an?",
			name, map[string]any{"area": "Hobbyraum", "domain": "light"},
			`{"result": "Live Context: An overview of the areas and the devices in this smart home:\n- names: Hobbyraum Decke\n  domain: light\n  state: 'off'\n  areas: Hobbyraum\n"}`,
			"Nein, im Hobbyraum ist alles aus.")...)
	}
	if name := findTool(tools, "__HassTurnOn"); name != "" {
		out = append(out, exchange("ex2", "Mach im Hobbyraum das Licht an.",
			name, map[string]any{"area": "Hobbyraum", "domain": []string{"light"}},
			`{"speech": {}, "response_type": "action_done", "data": {"targets": [], "success": [{"name": "Hobbyraum Decke", "type": "entity", "id": "light.hobbyraum_decke"}], "failed": []}}`,
			"Licht im Hobbyraum ist an.")...)
	}
	if name := findTool(tools, "memory_save"); name != "" {
		out = append(out, exchange("ex3", "Merk dir, dass der Schornsteinfeger im März kommt.",
			name, map[string]any{"text": "Der Schornsteinfeger kommt im März."},
			"Gespeichert (id ex3).",
			"Gemerkt.")...)
	}
	return out
}

// exampleTime is the fixed clock of the sample turns. It is long past, so
// the model never reads it as the current time.
var exampleTime = time.Date(2026, time.January, 5, 18, 30, 0, 0, time.UTC)

func exchange(id, question, tool string, args map[string]any, result, answer string) []llm.Message {
	raw, _ := json.Marshal(args)
	user := Turn{Now: exampleTime, ChannelContext: "Kanal: Beispiel.", Text: question}
	return []llm.Message{
		{Role: llm.RoleUser, Content: user.Render()},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: id, Type: "function",
			Function: llm.FunctionCall{Name: tool, Arguments: string(raw)},
		}}},
		{Role: llm.RoleTool, ToolCallID: id, Name: tool, Content: result},
		{Role: llm.RoleAssistant, Content: answer},
	}
}

// findTool returns the first tool, in name order, whose name ends in suffix.
func findTool(tools []llm.Tool, suffix string) string {
	var names []string
	for _, t := range tools {
		if strings.HasSuffix(t.Function.Name, suffix) || t.Function.Name == suffix {
			names = append(names, t.Function.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
