package prompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/llm"
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
func Examples(l i18n.Lang, tools []llm.Tool) []llm.Message {
	t := textsFor(l)
	var out []llm.Message
	if name := findTool(tools, "__GetLiveContext"); name != "" {
		out = append(out, exchange(l, "ex1", t.exampleLookupQ,
			name, map[string]any{"area": t.exampleArea, "domain": "light"},
			fmt.Sprintf(`{"result": "Live Context: An overview of the areas and the devices in this smart home:\n- names: %s\n  domain: light\n  state: 'off'\n  areas: %s\n"}`, t.exampleLight, t.exampleArea),
			t.exampleLookupA)...)
	}
	if name := findTool(tools, "__HassTurnOn"); name != "" {
		out = append(out, exchange(l, "ex2", t.exampleTurnOnQ,
			name, map[string]any{"area": t.exampleArea, "domain": []string{"light"}},
			fmt.Sprintf(`{"speech": {}, "response_type": "action_done", "data": {"targets": [], "success": [{"name": %q, "type": "entity", "id": "light.hobbyraum_decke"}], "failed": []}}`, t.exampleLight),
			t.exampleTurnOnA)...)
	}
	if name := findTool(tools, "memory_save"); name != "" {
		out = append(out, exchange(l, "ex3", t.exampleSaveQ,
			name, map[string]any{"text": t.exampleSaveFact},
			t.exampleSaveResult,
			t.exampleSaveA)...)
	}
	return out
}

// exampleTime is the fixed clock of the sample turns. It is long past, so
// the model never reads it as the current time.
var exampleTime = time.Date(2026, time.January, 5, 18, 30, 0, 0, time.UTC)

func exchange(l i18n.Lang, id, question, tool string, args map[string]any, result, answer string) []llm.Message {
	raw, _ := json.Marshal(args)
	user := Turn{Lang: l, Now: exampleTime, ChannelContext: textsFor(l).exampleChannel, Text: question}
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
