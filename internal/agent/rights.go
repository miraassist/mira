package agent

import (
	"strings"

	"github.com/miraassist/mira/internal/tools"
)

// Rights is the small rights model (docs/spec/agenten.md, Rights): only what
// Mira already tells apart. A run may do what both its call context and its
// agent's description allow.
type Rights uint8

const (
	// ReadHouse reads states, calendars, lists, memory.
	ReadHouse Rights = 1 << iota
	// ActOnHouse runs Home Assistant actions that change something.
	ActOnHouse
	// Remember writes to memory.
	Remember
	// Messages sends messages to persons or the house.
	Messages
	// Web searches the web and reads web pages.
	Web

	// NoRights is the empty set.
	NoRights Rights = 0
	// AllRights is every right.
	AllRights = ReadHouse | ActOnHouse | Remember | Messages | Web
)

// Has reports whether all rights in r are in s.
func (s Rights) Has(r Rights) bool { return s&r == r }

// And is what both allow.
func (s Rights) And(o Rights) Rights { return s & o }

var rightNames = []struct {
	r    Rights
	name string
}{{ReadHouse, "read_house"}, {ActOnHouse, "act_on_house"}, {Remember, "remember"}, {Messages, "messages"}, {Web, "web"}}

// String lists the rights by name, for the log.
func (s Rights) String() string {
	var out []string
	for _, n := range rightNames {
		if s.Has(n.r) {
			out = append(out, n.name)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ",")
}

// ParseRights reads rights by name ("read_house", "web"); unknown names are
// reported.
func ParseRights(names []string) (Rights, []string) {
	var r Rights
	var unknown []string
	for _, name := range names {
		found := false
		for _, n := range rightNames {
			if n.name == strings.TrimSpace(name) {
				r |= n.r
				found = true
			}
		}
		if !found {
			unknown = append(unknown, name)
		}
	}
	return r, unknown
}

// ownToolRights are the rights Mira's own tools need, where the flags they
// declare do not say it (docs/spec/agenten.md, Decided 16).
var ownToolRights = map[string]Rights{
	tools.ToolMemorySave:     Remember,
	tools.ToolMemoryForget:   Remember,
	tools.ToolMessageSend:    Messages,
	tools.ToolMessageAck:     Messages,
	tools.ToolReminderSet:    Messages,
	tools.ToolReminderCancel: Messages,
	"radio_play":             ActOnHouse,
	"sleep_timer":            ActOnHouse,
	"task_delete":            ActOnHouse,
	"task_enable":            ActOnHouse,
	// Threads: filling or confirming one may run its finish (an action, a
	// fact); taking back runs an action.
	"thread_fill":    ActOnHouse,
	"thread_confirm": ActOnHouse,
	"thread_undo":    ActOnHouse,
}

// RightFor is the right a tool needs: Mira's own tools as listed; otherwise a
// tool that returns web content needs Web, one that only reads ReadHouse, and
// every other one ActOnHouse.
func RightFor(spec tools.Spec) Rights {
	if spec.Internal {
		if r, ok := ownToolRights[spec.Name]; ok {
			return r
		}
	}
	switch {
	case spec.WebContent:
		return Web
	case spec.ReadOnly || spec.Internal:
		return ReadHouse
	}
	return ActOnHouse
}
