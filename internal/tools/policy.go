package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/ha/structure"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/prompt"
	"github.com/sker65/mira/internal/store"
)

// Outcome of a policy check.
type Outcome int

const (
	// Allow lets the call run.
	Allow Outcome = iota
	// NeedsConfirmation parks the call until the user approves it in the
	// next turn.
	NeedsConfirmation
	// Deny refuses the call for this turn.
	Deny
)

func (o Outcome) String() string {
	switch o {
	case Allow:
		return "allowed"
	case NeedsConfirmation:
		return "needs_confirmation"
	default:
		return "denied"
	}
}

// Verdict is the result of Guard.Check. Message is the tool result the model
// reads instead of a real result when the call does not run.
type Verdict struct {
	Outcome Outcome
	Reason  string
	Message string
	// Targets are the entities the call was resolved to, for logging.
	Targets []string
}

// ConfirmationStore persists pending confirmations.
type ConfirmationStore interface {
	PutConfirmation(ctx context.Context, c store.Confirmation, ttl time.Duration) error
}

// Guard enforces the security rules on every tool call.
type Guard struct {
	texts     texts
	domains   map[string]bool
	entities  map[string]bool
	tools     map[string]bool
	ttl       time.Duration
	store     ConfirmationStore
	structure func() structure.Snapshot
}

func NewGuard(l i18n.Lang, sec config.Security, st ConfirmationStore, snapshot func() structure.Snapshot) *Guard {
	g := &Guard{
		texts:     textsFor(l),
		domains:   map[string]bool{},
		entities:  map[string]bool{},
		tools:     map[string]bool{},
		ttl:       sec.ConfirmTTL.Duration(),
		store:     st,
		structure: snapshot,
	}
	if g.ttl == 0 {
		g.ttl = 5 * time.Minute
	}
	if g.structure == nil {
		g.structure = func() structure.Snapshot { return structure.Snapshot{} }
	}
	for _, d := range sec.ConfirmDomains {
		g.domains[strings.ToLower(d)] = true
	}
	for _, e := range sec.ConfirmEntities {
		g.entities[strings.ToLower(e)] = true
	}
	for _, t := range sec.ConfirmTools {
		g.tools[t] = true
	}
	return g
}

// TurnState is what the guard remembers within one turn.
type TurnState struct {
	SessionKey string
	Channel    config.Channel
	// Person is who speaks, when known; Area is the satellite's area.
	Person, Area string
	// UserText is the user's message of this turn. Only it can approve a
	// pending confirmation; tool results never reach this field.
	UserText string
	// Pending is the confirmation the previous turn asked for, already taken
	// out of the store. It approves at most one call.
	Pending *store.Confirmation

	webContentSeen        bool
	pendingUsed           bool
	confirmationRequested bool
}

// ConfirmationRequested reports whether this turn parked a critical call and
// therefore ends waiting for the user's yes or no.
func (t *TurnState) ConfirmationRequested() bool { return t.confirmationRequested }

// WebContentSeen reports whether the turn has read untrusted web content.
func (t *TurnState) WebContentSeen() bool { return t.webContentSeen }

// Check decides whether a call may run. A call that needs confirmation is
// stored as pending for the session before Check returns.
func (g *Guard) Check(ctx context.Context, ts *TurnState, spec Spec, args json.RawMessage) Verdict {
	name := spec.QualifiedName()

	if ts.Channel == config.ChannelBackground && !spec.ReadOnly {
		return Verdict{Outcome: Deny, Reason: "background_write",
			Message: g.texts.denyBackground}
	}

	critical, targets := g.critical(spec, args)
	if !critical {
		return Verdict{Outcome: Allow, Targets: targets}
	}

	if ts.webContentSeen {
		return Verdict{Outcome: Deny, Reason: "after_web_content", Targets: targets,
			Message: g.texts.denyAfterWeb}
	}

	if ts.Pending != nil && !ts.pendingUsed && Affirmative(ts.UserText) && g.matches(*ts.Pending, name, args, targets) {
		ts.pendingUsed = true
		return Verdict{Outcome: Allow, Reason: "confirmed", Targets: targets}
	}

	summary := describe(name, args, targets)
	if g.store != nil {
		err := g.store.PutConfirmation(ctx, store.Confirmation{
			ID: ulid.Make().String(), SessionKey: ts.SessionKey, ToolName: name,
			Arguments: canonicalArgs(args), Summary: summary,
		}, g.ttl)
		if err != nil {
			return Verdict{Outcome: Deny, Reason: "confirmation_store_failed", Targets: targets,
				Message: g.texts.denyStoreFailed}
		}
	}
	ts.confirmationRequested = true
	return Verdict{Outcome: NeedsConfirmation, Reason: "critical", Targets: targets,
		Message: fmt.Sprintf(g.texts.needsConfirmation, summary)}
}

// Observe records that a call ran. Reading from a web-content tool locks
// critical actions for the rest of the turn.
func (g *Guard) Observe(ts *TurnState, spec Spec) {
	if spec.WebContent {
		ts.webContentSeen = true
	}
}

// critical decides whether a call touches something that needs approval, and
// which entities it was resolved to. Read-only tools never are.
func (g *Guard) critical(spec Spec, raw json.RawMessage) (bool, []string) {
	if g.tools[spec.QualifiedName()] {
		return true, nil
	}
	if spec.ReadOnly || spec.Internal {
		return false, nil
	}
	args := parseArgs(raw)
	snap := g.structure()

	var targets []string
	critical := false
	mark := func(ids ...string) {
		for _, id := range ids {
			targets = append(targets, id)
			if g.isCriticalEntity(id) {
				critical = true
			}
		}
	}

	domains := stringList(args["domain"])
	for _, d := range domains {
		if g.domains[strings.ToLower(d)] {
			critical = true
		}
	}
	mark(stringList(args["entity_id"])...)

	area := firstString(args["area"])
	for _, name := range stringList(args["name"]) {
		ids := snap.ResolveName(name, area)
		if len(ids) == 0 {
			// An unresolved name is judged by what it resembles: a model that
			// says "Haustürschloss" for an entity called "Haustür" must not
			// slip past the check.
			ids = g.similarCritical(snap, name)
		}
		mark(ids...)
	}
	if len(stringList(args["name"])) == 0 && len(domains) == 0 && area != "" {
		// "Turn everything in the garage off" reaches every entity there.
		mark(snap.EntitiesInArea(area)...)
	}
	sort.Strings(targets)
	return critical, slices.Compact(targets)
}

func (g *Guard) isCriticalEntity(id string) bool {
	id = strings.ToLower(id)
	return g.entities[id] || g.domains[structure.Domain(id)]
}

func (g *Guard) similarCritical(snap structure.Snapshot, name string) []string {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil
	}
	var out []string
	for id, names := range snap.EntityNames {
		if !g.isCriticalEntity(id) {
			continue
		}
		for _, n := range names {
			if strings.Contains(key, n) || strings.Contains(n, key) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// matches reports whether a call is the one the user was asked about: same
// tool and either the same arguments or the same resolved targets.
func (g *Guard) matches(c store.Confirmation, name string, args json.RawMessage, targets []string) bool {
	if c.ToolName != name {
		return false
	}
	if c.Arguments == canonicalArgs(args) {
		return true
	}
	if len(targets) == 0 {
		return false
	}
	_, pendingTargets := g.critical(Spec{ToolDef: specDef(name)}, json.RawMessage(c.Arguments))
	return slices.Equal(pendingTargets, targets)
}

func specDef(qualified string) prompt.ToolDef {
	server, name := prompt.SplitQualifiedName(qualified)
	return prompt.ToolDef{Server: server, Name: name}
}

func describe(name string, args json.RawMessage, targets []string) string {
	s := name + " " + canonicalArgs(args)
	if len(targets) > 0 {
		s += ", betrifft " + strings.Join(targets, ", ")
	}
	return s
}

func canonicalArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return "{}"
	}
	c, err := prompt.CanonicalJSON(args)
	if err != nil {
		return string(args)
	}
	return string(c)
}

func parseArgs(raw json.RawMessage) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// stringList accepts a string or a list of strings, as Home Assistant's
// intent tools do.
func stringList(v any) []string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstString(v any) string {
	if l := stringList(v); len(l) > 0 {
		return l[0]
	}
	return ""
}

// Describe is exported for the conversation loop's logs.
func Describe(name string, args json.RawMessage) string { return describe(name, args, nil) }
