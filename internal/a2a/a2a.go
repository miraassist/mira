// Package a2a holds Mira's remote agents (docs/spec/remote-agents.md): the
// agents of skills that run elsewhere and speak A2A. When the skills are
// loaded it reads each agent's card (one GET), takes from it what Mira needs
// (description, input and output modes, name, transport, push and streaming,
// the login) and makes the SDK's client for it; a card that cannot be read is
// tried again like an MCP server that is down (decision 17). What the card
// asks that Mira cannot do is the skill's problem (decision 12).
//
// Mira is an A2A client only; the protocol is the official Go SDK's
// (github.com/a2aproject/a2a-go/v2).
package a2a

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
)

// CardPath is where an agent's card is, below its address.
const CardPath = "/.well-known/agent-card.json"

// How Mira logs in at a remote agent, as its card asks.
const (
	SchemeNone   = "none"
	SchemeBearer = "bearer"
	SchemeAPIKey = "api_key"
	SchemeOAuth2 = "oauth2"
)

// Connections are the skills' logins at services (docs/spec/oauth.md), for
// a card that asks for OAuth2 (decision 16).
type Connections interface {
	// Connected reports whether the skill has a connection to use.
	Connected(skill string) bool
	// Transport adds the skill's token to the requests it allows.
	Transport(skill string, next http.RoundTripper) http.RoundTripper
}

// Options configure the Host.
type Options struct {
	// Skills are the loaded skills; their remote agents count.
	Skills []skills.Skill
	// ServerURL is Mira's address for remote agents (server.url); without
	// it Mira takes no push.
	ServerURL string
	// Connections give an OAuth2 card the token of the skill's connection.
	Connections Connections
	// HTTP reads the cards; a client with a 30 s timeout when nil.
	HTTP *http.Client
	// Transport is under the calls to the agents; http.DefaultTransport
	// when nil.
	Transport http.RoundTripper
	// RetryInterval is the first wait before a card is read again; 30 s
	// when 0. The waits double up to 30 minutes.
	RetryInterval time.Duration
	// Runs keeps the calls; without it no remote agent can be called.
	Runs Runs
	// Deliver hands a call's end (done or failed) to its route: the
	// thread whose field or plan it fills.
	Deliver func(ctx context.Context, rec store.AgentRun)
	// Ask hands a remote agent's question on (an agent request, D);
	// without it a question fails the call.
	Ask func(ctx context.Context, q Question) error
	// CallRetry is the first wait before a send or a read of a call is
	// tried again; 1 s when 0, doubling, 3 attempts (Retries).
	CallRetry time.Duration
	Clock     func() time.Time
	Logger    *slog.Logger
}

// Host holds the remote agents of the loaded skills.
type Host struct {
	log       *slog.Logger
	http      *http.Client
	transport http.RoundTripper
	serverURL string
	conns     Connections
	retry     time.Duration
	agents    []*Agent

	runs      Runs
	deliver   func(ctx context.Context, rec store.AgentRun)
	ask       func(ctx context.Context, q Question) error
	callRetry time.Duration
	clock     func() time.Time
	// base ends every call's stream and send at shutdown; the calls stay
	// running in their rows and are taken up at the next start (G).
	base     context.Context
	stopBase context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	calls    map[string]*call
}

// Agent is one remote agent.
type Agent struct {
	// Skill is the skill that describes it, Name its name there.
	Skill, Name string
	def         config.AgentDef
	// oauth: the skill has an oauth section.
	oauth bool

	mu     sync.Mutex
	info   Info
	client *a2aclient.Client
	// read: the card was read; err is why it could not be, problem what
	// in it Mira cannot use.
	read    bool
	err     error
	problem *skills.Problem
	failed  int
	next    time.Time
}

// Info is what Mira took from an agent's card.
type Info struct {
	// Label is how Mira names it to the user (decision 22): the skill's
	// name for it, else the card's.
	Label       string
	Description string
	InputModes  []string
	OutputModes []string
	// Transport and Endpoint are the interface Mira talks to.
	Transport string
	Endpoint  string
	// Push: the card offers push and Mira has her address for it; Stream:
	// the card offers streaming.
	Push, Stream bool
	// Scheme is how Mira logs in (SchemeNone, SchemeBearer, …).
	Scheme string
}

// Status is a remote agent's state, for the skill's problems.
type Status struct {
	Skill, Agent, Label string
	// Ready: the card was read and fits; with an OAuth2 login also the
	// skill's connection is made.
	Ready bool
	// Waiting: it waits for the skill's connection (docs/spec/oauth.md);
	// the skill's page says "Verbinden", which is no problem.
	Waiting bool
	// Problem is why it is not ready, if it is a problem.
	Problem *skills.Problem
}

// New collects the remote agents of the skills; Start reads their cards.
func New(o Options) *Host {
	h := &Host{log: o.Logger, http: o.HTTP, transport: o.Transport, serverURL: o.ServerURL, conns: o.Connections,
		retry: o.RetryInterval, runs: o.Runs, deliver: o.Deliver, ask: o.Ask, callRetry: o.CallRetry, clock: o.Clock,
		calls: map[string]*call{}}
	h.base, h.stopBase = context.WithCancel(context.Background())
	if h.callRetry == 0 {
		h.callRetry = time.Second
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	h.log = h.log.With("component", "a2a")
	if h.http == nil {
		h.http = &http.Client{Timeout: 30 * time.Second}
	}
	if h.transport == nil {
		h.transport = http.DefaultTransport
	}
	if h.retry == 0 {
		h.retry = 30 * time.Second
	}
	for _, sk := range o.Skills {
		for _, name := range sortedKeys(sk.Agents) {
			d := sk.Agents[name]
			if d.Remote == nil {
				continue
			}
			h.agents = append(h.agents, &Agent{Skill: sk.Name, Name: name, def: d, oauth: sk.OAuth != nil})
		}
	}
	return h
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Agents are the remote agents, by skill and name.
func (h *Host) Agents() []*Agent { return slices.Clone(h.agents) }

// Agent is the remote agent of that name.
func (h *Host) Agent(name string) (*Agent, bool) {
	for _, a := range h.agents {
		if a.Name == name {
			return a, true
		}
	}
	return nil, false
}

// Start reads every card once, at the same time.
func (h *Host) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for _, a := range h.agents {
		wg.Go(func() { h.load(ctx, a) })
	}
	wg.Wait()
}

// Run reads every card, then reads again those that could not be read,
// each with waits doubling from the retry interval up to 30 minutes; one
// whose card was refused (a 4xx) is not read again (decision 17;
// docs/spec/retry.md, row 6, as an MCP server).
func (h *Host) Run(ctx context.Context) error {
	if len(h.agents) == 0 {
		return nil
	}
	defer h.Close()
	h.Start(ctx)
	t := time.NewTicker(h.retry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			now := time.Now()
			for _, a := range h.agents {
				a.mu.Lock()
				due := !a.read && a.err != nil && retry.Retryable(a.err) && !now.Before(a.next)
				a.mu.Unlock()
				if due {
					h.load(ctx, a)
				}
			}
		}
	}
}

// Close ends the calls' streams and sends without ending the calls: their
// rows stay running and the next start takes them up (G). It waits up to
// 5 seconds for them.
func (h *Host) Close() {
	h.stopBase()
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		h.log.Warn("remote agents' calls did not stop within 5 seconds")
	}
}

func (h *Host) policy() retry.Policy {
	return retry.Policy{First: h.retry, Max: 30 * time.Minute, Growth: retry.Exponential}
}

// load reads an agent's card and takes it, or keeps why not.
func (h *Host) load(ctx context.Context, a *Agent) {
	card, err := h.readCard(ctx, a.def.Remote.URL)
	if err != nil {
		h.failedRead(a, err)
		return
	}
	info, client, p := h.inspect(ctx, a, card)
	a.mu.Lock()
	again := a.failed > 0
	a.read, a.err, a.failed, a.info, a.client, a.problem = true, nil, 0, info, client, p
	a.mu.Unlock()
	if p != nil {
		h.log.Warn("remote agent not usable", "skill", a.Skill, "agent", a.Name, "url", a.def.Remote.URL,
			"problem", p.Text(i18n.EN))
		return
	}
	msg := "remote agent ready"
	if again {
		msg = "remote agent ready after its card could be read again"
	}
	h.log.Info(msg, "skill", a.Skill, "agent", a.Name, "name", info.Label, "endpoint", info.Endpoint,
		"transport", info.Transport, "push", info.Push, "stream", info.Stream, "scheme", info.Scheme)
}

// failedRead keeps a card that could not be read, logs it and sets when it
// is read again: a warning at the first failure and when the error changes,
// an error once for a refused card (not read again).
func (h *Host) failedRead(a *Agent, err error) {
	a.mu.Lock()
	before := a.err
	a.read, a.err = false, err
	a.failed++
	failed := a.failed
	next, stop := h.policy().Next(time.Now(), failed, err)
	var tooLong *retry.WaitTooLong
	if errors.As(stop, &tooLong) {
		// A wait the agent names, longer than the longest of the policy:
		// kept (docs/spec/retry.md, decision 5).
		next, stop = time.Now().Add(tooLong.Wait), nil
	}
	a.next = next
	a.mu.Unlock()
	changed := before == nil || before.Error() != err.Error()
	switch {
	case stop != nil:
		if changed {
			h.log.Error("remote agent's card refused; not read again until the skill is loaded again",
				"skill", a.Skill, "agent", a.Name, "url", a.def.Remote.URL, "error", err)
		}
	case changed:
		h.log.Warn("remote agent's card cannot be read, trying again", "skill", a.Skill, "agent", a.Name,
			"url", a.def.Remote.URL, "attempt", failed, "wait", time.Until(next).Round(time.Second).String(), "error", err)
	default:
		h.log.Debug("remote agent's card still cannot be read", "skill", a.Skill, "agent", a.Name,
			"url", a.def.Remote.URL, "attempt", failed, "error", err)
	}
}

// CardError is a card answered with another status than 200.
type CardError struct {
	URL    string
	Status int
	// Body is the start of the answer.
	Body string
}

func (e *CardError) Error() string {
	return fmt.Sprintf("GET %s: %d %s, the answer begins %q", e.URL, e.Status, http.StatusText(e.Status), e.Body)
}

// Retryable: a timeout, a 429 and a 5xx may heal by themselves; another 4xx
// needs a change of the address or the agent.
func (e *CardError) Retryable() bool {
	return e.Status == http.StatusRequestTimeout || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// bodyStart is the start of an answer, for an error that names it.
func bodyStart(b []byte) string {
	s := strings.ToValidUTF8(string(b), "?")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// readCard is one GET of the card below an agent's address.
func (h *Host) readCard(ctx context.Context, base string) (*sdk.AgentCard, error) {
	u := strings.TrimRight(base, "/") + CardPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, retry.Fatal(fmt.Errorf("GET %s: %w", u, err))
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("GET %s: the answer broke off: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		cerr := &CardError{URL: u, Status: resp.StatusCode, Body: bodyStart(body)}
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			return nil, retry.After(cerr, time.Duration(secs)*time.Second)
		}
		return nil, cerr
	}
	card, err := agentcard.DefaultCardParser(body)
	if err != nil {
		return nil, fmt.Errorf("the answer of %s is no agent card (%v); it begins %q", u, err, bodyStart(body))
	}
	return card, nil
}

// inspect takes from a card what Mira needs and makes the client; a problem
// for what Mira cannot use.
func (h *Host) inspect(ctx context.Context, a *Agent, card *sdk.AgentCard) (Info, *a2aclient.Client, *skills.Problem) {
	info := Info{Label: a.def.Name}
	if info.Label == "" {
		info.Label = strings.TrimSpace(card.Name)
	}
	if info.Label == "" {
		info.Label = a.Name
	}
	fail := func(kind string, args ...any) (Info, *a2aclient.Client, *skills.Problem) {
		p := skills.NewProblem(a.Skill, kind, append([]any{info.Label}, args...)...)
		return info, nil, &p
	}
	info.Description, info.InputModes, info.OutputModes = card.Description, card.DefaultInputModes, card.DefaultOutputModes
	reqs := card.SecurityRequirements
	if want := a.def.Remote.A2ASkill; want != "" {
		i := slices.IndexFunc(card.Skills, func(s sdk.AgentSkill) bool { return s.ID == want })
		if i < 0 {
			ids := make([]string, 0, len(card.Skills))
			for _, s := range card.Skills {
				ids = append(ids, s.ID)
			}
			return fail(skills.KindRemoteSkill, want, strings.Join(ids, ", "))
		}
		s := card.Skills[i]
		if s.Description != "" {
			info.Description = s.Description
		}
		if len(s.InputModes) > 0 {
			info.InputModes = s.InputModes
		}
		if len(s.OutputModes) > 0 {
			info.OutputModes = s.OutputModes
		}
		if len(s.SecurityRequirements) > 0 {
			reqs = s.SecurityRequirements
		}
	}
	if a.def.Description != "" {
		info.Description = a.def.Description
	}
	iface := pickInterface(card.SupportedInterfaces)
	if iface == nil {
		var offered []string
		for _, i := range card.SupportedInterfaces {
			offered = append(offered, strings.TrimSpace(string(i.ProtocolBinding)+" "+string(i.ProtocolVersion)))
		}
		if len(offered) == 0 {
			offered = []string{"none"}
		}
		return fail(skills.KindRemoteTransport, strings.Join(offered, ", "))
	}
	info.Transport, info.Endpoint = string(iface.ProtocolBinding), iface.URL
	info.Push = card.Capabilities.PushNotifications && h.serverURL != ""
	info.Stream = card.Capabilities.Streaming
	if !info.Push && !info.Stream {
		if card.Capabilities.PushNotifications {
			return fail(skills.KindRemoteNoAddress)
		}
		return fail(skills.KindRemoteNoWay)
	}
	scheme, named, unsupported := pickScheme(card.SecuritySchemes, reqs)
	if scheme == nil && unsupported != "" {
		return fail(skills.KindRemoteScheme, unsupported)
	}
	cred := credentials{base: a.def.Remote.URL, token: a.def.Remote.Token, next: h.transport}
	info.Scheme = SchemeNone
	switch s := scheme.(type) {
	case sdk.HTTPAuthSecurityScheme:
		info.Scheme, cred.header, cred.prefix = SchemeBearer, "Authorization", "Bearer "
	case sdk.APIKeySecurityScheme:
		info.Scheme = SchemeAPIKey
		switch s.Location {
		case sdk.APIKeySecuritySchemeLocationQuery:
			cred.query = s.Name
		case sdk.APIKeySecuritySchemeLocationCookie:
			cred.cookie = s.Name
		default:
			cred.header = s.Name
		}
	case sdk.OAuth2SecurityScheme:
		info.Scheme = SchemeOAuth2
		if !a.oauth || h.conns == nil {
			return fail(skills.KindRemoteOAuth, named)
		}
		cred.next = h.conns.Transport(a.Skill, h.transport)
		cred.token = ""
	}
	if (info.Scheme == SchemeBearer || info.Scheme == SchemeAPIKey) && cred.token == "" {
		return fail(skills.KindRemoteToken, named)
	}
	if info.Scheme != SchemeNone {
		if u, err := url.Parse(info.Endpoint); err != nil || !oauth.Below(u, a.def.Remote.URL) {
			return fail(skills.KindRemoteElsewhere, info.Endpoint, a.def.Remote.URL)
		}
	}
	// The SDK gets only the interface Mira chose, so it talks to that one.
	only := *card
	only.SupportedInterfaces = []*sdk.AgentInterface{iface}
	hc := &http.Client{Transport: statusTransport{next: cred}}
	client, err := a2aclient.NewFromCard(ctx, &only, a2aclient.WithJSONRPCTransport(hc), a2aclient.WithRESTTransport(hc))
	if err != nil {
		return fail(skills.KindRemoteTransport, fmt.Sprintf("%s %s: %v", iface.ProtocolBinding, iface.URL, err))
	}
	return info, client, nil
}

// pickInterface is the first interface of the card Mira speaks: A2A 1.x
// over JSON-RPC or HTTP+JSON (gRPC would bring gRPC into the service).
func pickInterface(ifaces []*sdk.AgentInterface) *sdk.AgentInterface {
	for _, i := range ifaces {
		if i == nil || (i.ProtocolBinding != sdk.TransportProtocolJSONRPC && i.ProtocolBinding != sdk.TransportProtocolHTTPJSON) {
			continue
		}
		if v := strings.TrimPrefix(string(i.ProtocolVersion), "v"); v != "" && v != "1" && !strings.HasPrefix(v, "1.") {
			continue
		}
		return i
	}
	return nil
}

// pickScheme is the login the card asks for: the first of its requirements
// that names one scheme Mira can do (a bearer token, an API key, OAuth2), or
// none when the card asks for no login or allows going without. Without one,
// unsupported names what the card asks for.
func pickScheme(schemes sdk.NamedSecuritySchemes, reqs sdk.SecurityRequirementsOptions) (scheme sdk.SecurityScheme, named, unsupported string) {
	if len(reqs) == 0 {
		return nil, "", ""
	}
	var asked []string
	for _, req := range reqs {
		if len(req) == 0 {
			return nil, "", ""
		}
		names := sortedKeys(req)
		var parts []string
		for _, n := range names {
			parts = append(parts, schemeName(n, schemes[n]))
		}
		asked = append(asked, strings.Join(parts, " + "))
		if len(req) != 1 {
			continue
		}
		s := schemes[names[0]]
		switch v := s.(type) {
		case sdk.HTTPAuthSecurityScheme:
			if v.Scheme == "" || strings.EqualFold(v.Scheme, "bearer") {
				return s, parts[0], ""
			}
		case sdk.APIKeySecurityScheme:
			return s, parts[0], ""
		case sdk.OAuth2SecurityScheme:
			return s, parts[0], ""
		}
	}
	return nil, "", strings.Join(asked, "; ")
}

// schemeName names a scheme for the skill's problem.
func schemeName(name sdk.SecuritySchemeName, s sdk.SecurityScheme) string {
	switch v := s.(type) {
	case sdk.HTTPAuthSecurityScheme:
		return fmt.Sprintf("%s: HTTP %s", name, v.Scheme)
	case sdk.APIKeySecurityScheme:
		return fmt.Sprintf("%s: API key", name)
	case sdk.OAuth2SecurityScheme:
		return fmt.Sprintf("%s: OAuth2", name)
	case sdk.OpenIDConnectSecurityScheme:
		return fmt.Sprintf("%s: OpenID Connect", name)
	case sdk.MutualTLSSecurityScheme:
		return fmt.Sprintf("%s: mTLS", name)
	case nil:
		return fmt.Sprintf("%s: not described in the card", name)
	}
	return fmt.Sprintf("%s: %T", name, s)
}

// credentials add the skill's token to the requests below the agent's
// address, as its card asks, and refuse every other address: the token goes
// only where the skill says (decision 28).
type credentials struct {
	base           string
	token          string
	header, prefix string
	query, cookie  string
	next           http.RoundTripper
}

func (c credentials) RoundTrip(r *http.Request) (*http.Response, error) {
	if !oauth.Below(r.URL, c.base) {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, retry.Fatal(fmt.Errorf("the token of the remote agent at %s does not go to %s", c.base, r.URL.Redacted()))
	}
	if c.token == "" {
		return c.next.RoundTrip(r)
	}
	out := r.Clone(r.Context())
	switch {
	case c.header != "":
		out.Header.Set(c.header, c.prefix+c.token)
	case c.query != "":
		q := out.URL.Query()
		q.Set(c.query, c.token)
		out.URL.RawQuery = q.Encode()
	case c.cookie != "":
		out.AddCookie(&http.Cookie{Name: c.cookie, Value: c.token})
	}
	return c.next.RoundTrip(out)
}

// Info is what Mira took from the card; ok once it was read and fits.
func (a *Agent) Info() (Info, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.info, a.read && a.problem == nil
}

// Client is the SDK's client for the agent, with its login; nil until its
// card was read and fits.
func (a *Agent) Client() *a2aclient.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

// Status is each remote agent's state.
func (h *Host) Status() []Status {
	out := make([]Status, 0, len(h.agents))
	for _, a := range h.agents {
		a.mu.Lock()
		st := Status{Skill: a.Skill, Agent: a.Name, Label: a.info.Label}
		if st.Label == "" {
			st.Label = a.def.Name
		}
		if st.Label == "" {
			st.Label = a.Name
		}
		switch {
		case a.problem != nil:
			st.Problem = a.problem
		case !a.read && a.err != nil:
			kind := skills.KindRemoteDown
			if !retry.Retryable(a.err) {
				kind = skills.KindRemoteRefused
			}
			p := skills.NewProblem(a.Skill, kind, st.Label, a.err.Error())
			st.Problem = &p
		case !a.read:
		case a.info.Scheme == SchemeOAuth2 && !h.conns.Connected(a.Skill):
			st.Waiting = true
		default:
			st.Ready = true
		}
		a.mu.Unlock()
		out = append(out, st)
	}
	return out
}
