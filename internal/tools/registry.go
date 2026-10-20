// Package tools is everything between the model asking for a tool and the
// tool running: the registry of MCP and internal tools, and the policy that
// decides whether a call may run at all.
//
// The policy is code, not prompt text. A web page that tells the model to
// unlock the front door can make the model ask for it, but it cannot make
// this package execute it.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/prompt"
)

// Spec describes a tool to the policy and the prompt builder.
type Spec struct {
	prompt.ToolDef
	ReadOnly   bool
	WebContent bool
	Internal   bool
	// AskAfterWeb: the tool keeps or passes on what the model says (memory,
	// messages to others, ending a thread). After web content in the same
	// turn it runs only after the user's yes.
	AskAfterWeb bool
}

// Invocation is a call as the conversation loop hands it over.
type Invocation struct {
	Name       string
	Args       json.RawMessage
	SessionKey string
	Channel    config.Channel
	// Person is who is speaking, when known (Telegram, the HA app, a
	// satellite under the "alone at home" rule); empty means anonymous.
	Person string
	// Area is the satellite's area of a voice turn.
	Area string
	// DeviceID is the satellite's device id of a voice turn. Home Assistant's
	// tools get it, so they act as for its own conversation agents.
	DeviceID string
	// Foreign: the turn has third-party text before it (TurnState.Foreign).
	Foreign bool
}

// Author is how a fact or message records who it came from: the person, else
// the satellite's area.
func (inv Invocation) Author() string {
	switch {
	case inv.Person != "":
		return inv.Person
	case inv.Area != "":
		return "area:" + inv.Area
	}
	return ""
}

// InternalTool is a tool implemented inside the agent.
type InternalTool struct {
	Name        string
	Description string
	Schema      string
	ReadOnly    bool
	// WebContent: the result is untrusted third-party text (a web page);
	// the guard locks critical actions for the rest of the turn.
	WebContent bool
	// AskAfterWeb: see Spec.
	AskAfterWeb bool
	// Enabled, when set, decides at each turn whether the tool is offered;
	// a switched-off tool is unknown to the model.
	Enabled func() bool
	Run     func(ctx context.Context, inv Invocation) (string, error)
}

func (t InternalTool) on() bool { return t.Enabled == nil || t.Enabled() }

// MCPSource is the part of the MCP host the registry uses.
type MCPSource interface {
	Tools() []mcp.Tool
	Lookup(name string) (mcp.Tool, bool)
	Call(ctx context.Context, name string, args json.RawMessage) (mcp.Result, error)
}

// MaxResultChars caps what a single tool result may put into the context.
// GetLiveContext of a large house or a long web page would otherwise eat the
// slot's context in one call.
const MaxResultChars = 8000

// Registry is the complete tool set: MCP tools plus internal ones.
type Registry struct {
	mcp      MCPSource
	internal map[string]InternalTool
}

func NewRegistry(src MCPSource, internal ...InternalTool) *Registry {
	r := &Registry{mcp: src, internal: map[string]InternalTool{}}
	for _, t := range internal {
		r.internal[t.Name] = t
	}
	return r
}

// Specs lists every tool, sorted by (server, name) with internal tools first.
// With readOnly set, only tools safe for background jobs are listed.
func (r *Registry) Specs(readOnly bool) []Spec {
	var out []Spec
	for _, t := range r.internal {
		if (readOnly && !t.ReadOnly) || !t.on() {
			continue
		}
		out = append(out, internalSpec(t))
	}
	if r.mcp != nil {
		for _, t := range r.mcp.Tools() {
			if readOnly && !t.ReadOnly {
				continue
			}
			out = append(out, Spec{ToolDef: t.ToolDef, ReadOnly: t.ReadOnly, WebContent: t.WebContent})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Server != out[j].Server {
			return out[i].Server < out[j].Server
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Defs is Specs reduced to what the prompt builder needs.
func (r *Registry) Defs(readOnly bool) []prompt.ToolDef {
	specs := r.Specs(readOnly)
	out := make([]prompt.ToolDef, len(specs))
	for i, s := range specs {
		out[i] = s.ToolDef
	}
	return out
}

func internalSpec(t InternalTool) Spec {
	return Spec{
		ToolDef:     prompt.ToolDef{Name: t.Name, Description: t.Description, Schema: json.RawMessage(t.Schema)},
		ReadOnly:    t.ReadOnly,
		WebContent:  t.WebContent,
		Internal:    true,
		AskAfterWeb: t.AskAfterWeb,
	}
}

// Lookup finds a tool by the name the model used.
func (r *Registry) Lookup(name string) (Spec, bool) {
	if t, ok := r.internal[name]; ok && t.on() {
		return internalSpec(t), true
	}
	if r.mcp != nil {
		if t, ok := r.mcp.Lookup(name); ok {
			return Spec{ToolDef: t.ToolDef, ReadOnly: t.ReadOnly, WebContent: t.WebContent}, true
		}
	}
	return Spec{}, false
}

// ErrUnknownTool is returned for names that are neither internal nor MCP.
var ErrUnknownTool = errors.New("unknown tool")

// Execute runs a tool without any policy check; callers go through the
// Guard first. The returned text is what the model reads; isError marks a
// failed call so the model does not claim success.
func (r *Registry) Execute(ctx context.Context, inv Invocation) (text string, isError bool, err error) {
	if t, ok := r.internal[inv.Name]; ok && t.on() {
		out, err := t.Run(ctx, inv)
		if err != nil {
			return "Fehler: " + err.Error(), true, nil
		}
		return clip(out), false, nil
	}
	if r.mcp == nil {
		return "", true, fmt.Errorf("%w: %s", ErrUnknownTool, inv.Name)
	}
	res, err := r.mcp.Call(mcp.WithDevice(ctx, inv.DeviceID), inv.Name, inv.Args)
	if errors.Is(err, mcp.ErrUnknownTool) {
		return "", true, fmt.Errorf("%w: %s", ErrUnknownTool, inv.Name)
	}
	if err != nil {
		return "Fehler: " + err.Error(), true, nil
	}
	return clip(res.Text), res.IsError, nil
}

func clip(s string) string {
	if len(s) <= MaxResultChars {
		return s
	}
	cut := MaxResultChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[…]"
}
