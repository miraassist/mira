package prompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/miraassist/mira/internal/llm"
)

// ServerSeparator joins an MCP server name and a tool name into the name the
// model sees. Server names may not contain it (config validates that), so the
// split back is unambiguous.
const ServerSeparator = "__"

// ToolDef is a tool as the agent knows it, before it is rendered for the
// model. Internal tools leave Server empty.
type ToolDef struct {
	Server      string
	Name        string
	Description string
	// Schema is the JSON Schema of the parameters, as received.
	Schema json.RawMessage
}

// QualifiedName is what the model calls: "<server>__<tool>", or just the tool
// name for internal tools.
func (t ToolDef) QualifiedName() string {
	if t.Server == "" {
		return t.Name
	}
	return t.Server + ServerSeparator + t.Name
}

// SplitQualifiedName reverses QualifiedName. An unqualified name is internal.
func SplitQualifiedName(qualified string) (server, name string) {
	if i := strings.Index(qualified, ServerSeparator); i >= 0 {
		return qualified[:i], qualified[i+len(ServerSeparator):]
	}
	return "", qualified
}

// BuildTools turns tool definitions into layer 3: sorted by (server, name) and
// with canonically serialised schemas, so the same set of tools always renders
// to the same bytes no matter what order the MCP servers answered in.
func BuildTools(defs []ToolDef) ([]llm.Tool, error) {
	sorted := make([]ToolDef, len(defs))
	copy(sorted, defs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Server != sorted[j].Server {
			return sorted[i].Server < sorted[j].Server
		}
		return sorted[i].Name < sorted[j].Name
	})

	out := make([]llm.Tool, 0, len(sorted))
	seen := make(map[string]bool, len(sorted))
	for _, d := range sorted {
		qn := d.QualifiedName()
		if seen[qn] {
			return nil, fmt.Errorf("duplicate tool %q", qn)
		}
		seen[qn] = true
		schema, err := CanonicalJSON(d.Schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", qn, err)
		}
		out = append(out, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        qn,
				Description: strings.TrimSpace(d.Description),
				Parameters:  schema,
			},
		})
	}
	return out, nil
}

// renderTools is the textual form of layer 3 used for hashing. The tools
// themselves travel in the request's tools field, where the server's chat
// template renders them; this is only the agent's own stable view of them.
func renderTools(tools []llm.Tool) string {
	var b strings.Builder
	for _, t := range tools {
		b.WriteString(t.Function.Name)
		b.WriteByte('\x1f')
		b.WriteString(t.Function.Description)
		b.WriteByte('\x1f')
		b.Write(t.Function.Parameters)
		b.WriteByte('\x1e')
	}
	return b.String()
}
