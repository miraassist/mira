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

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
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
// reads instead of a real result when the call does not run. Reason is the
// hook that decided ("system/web.star"), or "confirmed" for the call the user
// said yes to.
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

// Policy decides on a call: the hooks (docs/spec/hooks.md, package hooks).
type Policy interface {
	Decide(ctx context.Context, c Call) Decision
}

// Decision is what the hooks decided: the outcome, the hook that made it so
// and its reason (what the model reads after a deny, inside the question
// after a confirm).
type Decision struct {
	Outcome    Outcome
	By, Reason string
}

// Call is a tool call as the hooks see it (docs/spec/hooks.md, What a hook
// sees): the tool, its arguments, the targets resolved from them and the
// facts of the turn.
type Call struct {
	Spec Spec
	// Right is the right the tool needs in an agent's run.
	Right   string
	Args    map[string]any
	Domains []string
	// Targets are the entities the call surely reaches, Candidates those it
	// could reach and Mira cannot narrow down.
	Targets, Candidates []Entity
	Summary             string

	Person, PersonName, PersonBy string
	Channel                      config.Channel
	Area, DeviceID               string
	// Agent: the call comes from an agent's run with these rights.
	Agent  bool
	Rights []string
	// WebRead: this turn read web content; Foreign: it has third-party text
	// before it.
	WebRead, Foreign bool
	Text             string
	Time             time.Time
}

// Entity is an entity as a hook sees it.
type Entity struct {
	ID, Domain, Name, Area, AreaName string
	Labels                           []string
}

// Guard is the mechanism around the hooks: it resolves what a call reaches,
// asks the hooks, keeps the question for the user's yes and matches the yes
// to it. What is allowed is the hooks' decision, not the guard's.
type Guard struct {
	texts     texts
	ttl       time.Duration
	store     ConfirmationStore
	structure func() structure.Snapshot
	policy    Policy
	rightOf   func(Spec) string
	clock     func() time.Time
}

// GuardOption sets what the guard tells the hooks beyond the turn.
type GuardOption func(*Guard)

// WithRightOf names the right a tool needs in an agent's run (agent.RightFor).
func WithRightOf(f func(Spec) string) GuardOption { return func(g *Guard) { g.rightOf = f } }

// WithClock sets the clock the hooks' time comes from.
func WithClock(f func() time.Time) GuardOption { return func(g *Guard) { g.clock = f } }

func NewGuard(l i18n.Lang, sec config.Security, st ConfirmationStore, snapshot func() structure.Snapshot, policy Policy, opts ...GuardOption) *Guard {
	g := &Guard{
		texts:     textsFor(l),
		ttl:       sec.ConfirmTTL.Duration(),
		store:     st,
		structure: snapshot,
		policy:    policy,
		clock:     time.Now,
	}
	if g.ttl == 0 {
		g.ttl = 5 * time.Minute
	}
	if g.structure == nil {
		g.structure = func() structure.Snapshot { return structure.Snapshot{} }
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

// TurnState is what the guard remembers within one turn.
type TurnState struct {
	SessionKey string
	Channel    config.Channel
	// Person is who speaks, when known; Area is the satellite's area.
	Person, Area string
	// PersonName is how Mira names the person.
	PersonName string
	// PersonBy is how Person is known (prompt.Turn.PersonBy).
	PersonBy string
	// DeviceID is the satellite's device id of a voice turn.
	DeviceID string
	// Place is where the turn's threads and plans live (Invocation.Place).
	Place string
	// UserText is the user's message of this turn. Only it can approve a
	// pending confirmation; tool results never reach this field.
	UserText string
	// Pending is the confirmation the previous turn asked for, already taken
	// out of the store. It approves at most one call.
	Pending *store.Confirmation
	// ForeignContext: the turn's context holds third-party text that Mira
	// wrote down earlier, such as a plan made from a web page. What keeps or
	// passes on asks for a yes, as after web content; critical actions are
	// not locked (docs/spec/vorgaenge.md, Pläne).
	ForeignContext bool
	// ByRights: an agent's run (docs/spec/agenten.md, Rights). Rights are its
	// effective rights by name; the hook system/rights.star holds its calls
	// to them, and what needs a yes is refused, as nobody can give one.
	ByRights bool
	Rights   []string

	webContentSeen        bool
	pendingUsed           bool
	confirmationRequested bool
}

// ConfirmationRequested reports whether this turn parked a critical call and
// therefore ends waiting for the user's yes or no.
func (t *TurnState) ConfirmationRequested() bool { return t.confirmationRequested }

// WebContentSeen reports whether the turn has read untrusted web content.
func (t *TurnState) WebContentSeen() bool { return t.webContentSeen }

// RestoreWebContentSeen sets what a run had read before it was stopped,
// when it is continued after a restart: what held before holds again.
func (t *TurnState) RestoreWebContentSeen() { t.webContentSeen = true }

// Foreign reports whether the turn has third-party text before it: web
// content it read, or a foreign plan in its context. What Mira writes down
// now (a plan) counts as foreign then.
func (t *TurnState) Foreign() bool { return t.webContentSeen || t.ForeignContext }

// Check asks the hooks whether a call may run and carries out their verdict.
// A call that needs confirmation is stored as pending for the session before
// Check returns.
func (g *Guard) Check(ctx context.Context, ts *TurnState, spec Spec, args json.RawMessage) Verdict {
	name := spec.QualifiedName()
	call := g.call(ts, spec, args)
	// All ids for matching a yes; the verdict carries a short list for the
	// log.
	all := targetIDs(call)
	targets := shortList(all)
	d := Decision{Outcome: Allow}
	if g.policy != nil {
		d = g.policy.Decide(ctx, call)
	}
	switch d.Outcome {
	case Allow:
		return Verdict{Outcome: Allow, Targets: targets}
	case Deny:
		return Verdict{Outcome: Deny, Reason: d.By, Targets: targets, Message: d.Reason}
	}

	if ts.Pending != nil && !ts.pendingUsed && Affirmative(ts.UserText) && g.matches(*ts.Pending, name, args, all) {
		ts.pendingUsed = true
		return Verdict{Outcome: Allow, Reason: "confirmed", Targets: targets}
	}

	if g.store != nil {
		err := g.store.PutConfirmation(ctx, store.Confirmation{
			ID: ulid.Make().String(), SessionKey: ts.SessionKey, ToolName: name,
			Arguments: canonicalArgs(args), Summary: call.Summary,
		}, g.ttl)
		if err != nil {
			return Verdict{Outcome: Deny, Reason: "confirmation_store_failed", Targets: targets,
				Message: g.texts.denyStoreFailed}
		}
	}
	ts.confirmationRequested = true
	reason := strings.TrimSpace(d.Reason)
	if reason != "" {
		reason += " "
	}
	return Verdict{Outcome: NeedsConfirmation, Reason: d.By, Targets: targets,
		Message: fmt.Sprintf(g.texts.needsConfirmation, call.Summary, reason)}
}

// Decide asks the hooks about a call without the question mechanism: for
// what is no tool call of the model but should pass the same hooks, a
// skill's action (docs/spec/hooks.md, Skill actions). The caller carries out
// a confirm itself (a thread proposes the action).
func (g *Guard) Decide(ctx context.Context, ts *TurnState, spec Spec, args json.RawMessage) Decision {
	if g.policy == nil {
		return Decision{Outcome: Allow}
	}
	return g.policy.Decide(ctx, g.call(ts, spec, args))
}

type turnKey struct{}

// WithTurn hands the turn's state to the tools a call runs, so that what a
// tool does in turn (a thread's action) passes the hooks with the same facts.
func WithTurn(ctx context.Context, ts *TurnState) context.Context {
	return context.WithValue(ctx, turnKey{}, ts)
}

// TurnOf is the state of the turn a tool runs in; nil outside a turn (the
// web UI, a thread an agent finishes later).
func TurnOf(ctx context.Context) *TurnState {
	ts, _ := ctx.Value(turnKey{}).(*TurnState)
	return ts
}

// Observe records that a call ran. Reading from a web-content tool locks
// critical actions for the rest of the turn.
func (g *Guard) Observe(ts *TurnState, spec Spec) {
	if spec.WebContent {
		ts.webContentSeen = true
	}
}

// call builds what the hooks see of a call.
func (g *Guard) call(ts *TurnState, spec Spec, raw json.RawMessage) Call {
	snap := g.structure()
	args := parseArgs(raw)
	c := Call{
		Spec: spec, Args: args, Person: ts.Person, PersonName: ts.PersonName, PersonBy: ts.PersonBy,
		Channel: ts.Channel, Area: ts.Area, DeviceID: ts.DeviceID, Agent: ts.ByRights, Rights: ts.Rights,
		WebRead: ts.webContentSeen, Foreign: ts.Foreign(), Text: ts.UserText, Time: g.clock(),
	}
	if g.rightOf != nil {
		c.Right = g.rightOf(spec)
	}
	for _, d := range stringList(args["domain"]) {
		c.Domains = append(c.Domains, strings.ToLower(d))
	}
	targets, candidates := resolve(snap, args, c.Domains)
	c.Targets = entities(snap, targets)
	c.Candidates = entities(snap, candidates)
	c.Summary = describe(spec.QualifiedName(), raw, targetIDs(c))
	return c
}

// resolve finds the entities a call surely reaches and those it could reach.
func resolve(snap structure.Snapshot, args map[string]any, domains []string) (targets, candidates []string) {
	targets = append(targets, stringList(args["entity_id"])...)

	area := firstString(args["area"])
	names := stringList(args["name"])
	for _, name := range names {
		if ids := snap.ResolveName(name, area); len(ids) > 0 {
			targets = append(targets, ids...)
			continue
		}
		// An unresolved name is judged by what it resembles: a model that
		// says "Haustürschloss" for an entity called "Haustür" must not
		// slip past the hooks.
		candidates = append(candidates, similar(snap, name)...)
	}
	if len(names) == 0 {
		floor := firstString(args["floor"])
		classes := stringList(args["device_class"])
		switch {
		case area != "" && floor == "" && len(classes) == 0:
			// "Turn everything in the garage off" reaches every entity
			// there, of the given domains if any.
			targets = append(targets, inDomains(snap.EntitiesInArea(area), domains)...)
		case area != "" || floor != "" || len(classes) > 0 || len(domains) > 0:
			// A floor, a device class or only a domain: Home Assistant
			// narrows by what the snapshot does not know, so every entity
			// that could be meant counts.
			var all []string
			if area != "" {
				all = snap.EntitiesInArea(area)
			} else {
				for id := range snap.EntityNames {
					all = append(all, id)
				}
			}
			candidates = append(candidates, inDomains(all, domains)...)
		}
	}
	return compact(targets), compact(candidates)
}

func compact(ids []string) []string {
	for i := range ids {
		ids[i] = strings.ToLower(ids[i])
	}
	sort.Strings(ids)
	return slices.Compact(ids)
}

// entities turns entity ids into what a hook sees of them.
func entities(snap structure.Snapshot, ids []string) []Entity {
	out := make([]Entity, 0, len(ids))
	for _, id := range ids {
		out = append(out, EntityOf(snap, id))
	}
	return out
}

// EntityOf is what a hook sees of an entity; one Mira does not know has only
// its id and domain.
func EntityOf(snap structure.Snapshot, id string) Entity {
	e := Entity{ID: id, Domain: structure.Domain(id), Name: snap.EntityLabel[id], Area: snap.EntityArea[id]}
	if e.Area != "" {
		e.AreaName = snap.AreaNames[e.Area]
	}
	if snap.Confirm[id] {
		// The label's id, as Home Assistant has it: its name may be
		// anything, in any language.
		e.Labels = []string{structure.LabelConfirm}
	}
	return e
}

// targetIDs are the ids a call is matched and logged by: its targets, or,
// without any, the entities it could reach.
func targetIDs(c Call) []string {
	list := c.Targets
	if len(list) == 0 {
		list = c.Candidates
	}
	ids := make([]string, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	return ids
}

// inDomains keeps the entities of the given domains, or all without any.
func inDomains(ids, domains []string) []string {
	if len(domains) == 0 {
		return ids
	}
	var out []string
	for _, id := range ids {
		for _, d := range domains {
			if structure.Domain(id) == strings.ToLower(d) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// similar are the entities whose names resemble an unresolved name.
func similar(snap structure.Snapshot, name string) []string {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil
	}
	var out []string
	for id, names := range snap.EntityNames {
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
	pending := g.call(&TurnState{}, Spec{ToolDef: specDef(name)}, json.RawMessage(c.Arguments))
	return slices.Equal(targetIDs(pending), targets)
}

func specDef(qualified string) prompt.ToolDef {
	server, name := prompt.SplitQualifiedName(qualified)
	return prompt.ToolDef{Server: server, Name: name}
}

func describe(name string, args json.RawMessage, targets []string) string {
	s := name + " " + canonicalArgs(args)
	if len(targets) > 0 {
		s += ", betrifft " + strings.Join(shortList(targets), ", ")
	}
	return s
}

// shortList keeps a list of entities readable in a question or a log line: a
// floor or a domain alone can reach the whole house.
func shortList(ids []string) []string {
	const most = 10
	if len(ids) <= most {
		return ids
	}
	return append(slices.Clone(ids[:most]), "…")
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
