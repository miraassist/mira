package prompt

import (
	"testing"

	"github.com/sker65/mira/internal/llm"
)

func tool(name string) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.ToolFunction{Name: name}}
}

func TestExamplesFollowTheToolList(t *testing.T) {
	all := []llm.Tool{tool("memory_save"), tool("ha__homeassistant__GetLiveContext"), tool("ha__intent__HassTurnOn")}
	ex := Examples(all)
	if len(ex) != 12 {
		t.Fatalf("got %d messages, want three exchanges of four", len(ex))
	}
	if got := ex[1].ToolCalls[0].Function.Name; got != "ha__homeassistant__GetLiveContext" {
		t.Errorf("lookup example calls %q", got)
	}
	if ex[2].Role != llm.RoleTool || ex[2].ToolCallID != ex[1].ToolCalls[0].ID {
		t.Error("tool result does not answer the call")
	}

	readOnly := Examples([]llm.Tool{tool("ha__homeassistant__GetLiveContext")})
	if len(readOnly) != 4 {
		t.Fatalf("background jobs must only see the lookup example, got %d messages", len(readOnly))
	}
	if len(Examples(nil)) != 0 {
		t.Error("examples without tools")
	}
}

func TestExamplesAreStableAndHashed(t *testing.T) {
	tools := []llm.Tool{tool("ha__homeassistant__GetLiveContext")}
	a := Prefix{Agent: "a", Examples: Examples(tools)}
	b := Prefix{Agent: "a", Examples: Examples(tools)}
	if a.Hash() != b.Hash() {
		t.Error("the same examples hash differently")
	}
	if a.Hash() == (Prefix{Agent: "a"}).Hash() {
		t.Error("examples are not part of the prefix hash")
	}
	if msgs := a.Messages(); len(msgs) != 5 || msgs[0].Role != llm.RoleSystem {
		t.Errorf("messages = %d", len(msgs))
	}
}
