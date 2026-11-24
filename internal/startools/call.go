package startools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miraassist/mira/internal/clock"
	"math"
	"net/http"
	"net/url"
	"runtime/metrics"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	starjson "go.starlark.net/lib/json"
	starmath "go.starlark.net/lib/math"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Timeout is the most a whole tool call may take (decision 14).
const Timeout = 60 * time.Second

// memLimit is how much Mira may allocate during a call before the call is
// stopped; a rough brake, not a hard limit: Starlark counts steps, not
// allocations, and one step may allocate much; what the rest of Mira
// allocates meanwhile counts too (docs/spec/api-tools.md, Limits).
// answerLimit is the most a tool may answer.
var memLimit uint64 = 256 << 20

const answerLimit = 1 << 20

// memSample is how often the heap is looked at during a call.
const memSample = 20 * time.Millisecond

// heapBytes is what Mira allocated since its start, as Go's runtime counts
// it (freed memory is not subtracted).
func heapBytes() uint64 {
	s := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

// Requester makes one request of a skill's tool with the connection's token
// (oauth.Manager.Request): only below the skill's api prefixes, for the
// methods allowed there; the answer's status and body.
type Requester interface {
	Request(ctx context.Context, skill, method, rawURL string, body []byte) (int, []byte, error)
}

// Reader calls one of Mira's tools that counts as read-only, by its full
// name (docs/spec/api-tools.md, decision 19): its text and whether it is an
// error. ErrNoTool when there is no such tool now, ErrNotReadOnly when it may
// change something.
type Reader interface {
	ReadTool(ctx context.Context, name string, args json.RawMessage) (text string, isError bool, err error)
}

var (
	// ErrNoTool: Mira has no tool of that name now (its skill or server is
	// off or not reached).
	ErrNoTool = errors.New("no such tool")
	// ErrNotReadOnly: the tool may change something.
	ErrNotReadOnly = errors.New("the tool is not read-only")
	// ErrWebContent: the tool returns third-party text, which a tool that
	// computes must not pass on as its own.
	ErrWebContent = errors.New("the tool returns third-party text")
)

// Env is what a call needs besides its arguments.
type Env struct {
	Requester Requester
	// Reader and Reads: mira.tool calls the tools under tools.reads in
	// SKILL.md through Reader (decision 19).
	Reader Reader
	Reads  []string
	// Writes: the tool is under writes in SKILL.md and may use a method
	// other than GET (decision 15).
	Writes bool
	// Now is the clock of time.now(); nil is the real one.
	Now func() time.Time
	// Clock is what the call's Timeout and its looks at the memory wait
	// on; nil is the real one.
	Clock clock.Clock
}

// Event is one thing a call did, in order: a request or a print
// (docs/spec/api-tools.md, Seeing what went over the wire).
type Event struct {
	Kind    string `json:"kind"` // "request", "tool" or "print"
	Tool    string `json:"tool,omitempty"`
	Method  string `json:"method,omitempty"`
	URL     string `json:"url,omitempty"`
	Status  int    `json:"status,omitempty"`
	MS      int64  `json:"ms,omitempty"`
	Size    int    `json:"size,omitempty"`
	Start   string `json:"start,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// Result is a call's outcome. Err is what the model reads when the call
// failed; Backtrace is the error's place in the file, for the log and the
// page.
type Result struct {
	Text      string
	Events    []Event
	Err       error
	Backtrace string
}

// startLen is how much of an answer an event keeps.
const startLen = 500

// call is the state of one tool call, as the thread's local "call".
type call struct {
	ctx    context.Context
	skill  string
	tool   string
	env    Env
	events []Event
	f      *File
}

// Call runs the tool with the model's arguments: checked against the
// tool's schema first (decision 6), then the function, with its limits.
func (f *File) Call(ctx context.Context, name string, args json.RawMessage, env Env) Result {
	t, ok := f.Tool(name)
	if !ok {
		return Result{Err: fmt.Errorf("%s has no tool %s", f.Skill, name)}
	}
	kwargs, err := t.arguments(args)
	if err != nil {
		f.log.Warn("starlark tool: arguments refused", "skill", f.Skill, "tool", name, "error", err)
		return Result{Err: err}
	}
	ctx, cancel := clock.WithTimeout(ctx, env.Clock, Timeout)
	defer cancel()
	c := &call{ctx: ctx, skill: f.Skill, tool: name, env: env, f: f}
	thread := &starlark.Thread{Name: f.Skill + "/" + name, Print: func(_ *starlark.Thread, msg string) {
		c.events = append(c.events, Event{Kind: "print", Message: msg})
		f.log.Info("starlark tool print", "skill", f.Skill, "tool", name, "message", msg)
	}}
	thread.SetMaxExecutionSteps(maxSteps)
	thread.SetLocal("call", c)
	if env.Now != nil {
		now := env.Now
		starlarktime.SetNow(thread, func() (time.Time, error) { return now(), nil })
	}
	done := make(chan struct{})
	var tooMuch atomic.Bool
	var watching sync.WaitGroup
	limit, base := memLimit, heapBytes()
	watching.Add(1)
	go func() {
		defer watching.Done()
		tick := clock.Or(env.Clock).NewTicker(memSample)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				// Its own limit, or its caller ended (the turn): each says
				// so (docs/spec/endings.md, EN-07).
				thread.Cancel(cancelReason(ctx.Err()))
				return
			case <-tick.C():
				if heapBytes() > base+limit {
					tooMuch.Store(true)
					thread.Cancel("the tool used too much memory")
					cancel() // a request it waits in ends now, too
					return
				}
			case <-done:
				return
			}
		}
	}()
	v, err := starlark.Call(thread, t.fn, nil, kwargs)
	close(done)
	watching.Wait()
	res := Result{Events: c.events}
	if tooMuch.Load() {
		f.log.Warn("starlark tool: memory limit reached", "skill", f.Skill, "tool", name, "limit_mb", limit>>20)
		res.Err = fmt.Errorf("the tool %s used too much memory (more than %d MB)", name, limit>>20)
		return res
	}
	if err != nil {
		res.Backtrace, res.Err = f.failure(name, err)
		return res
	}
	res.Text, err = answer(thread, v)
	switch {
	case err != nil:
		res.Err = fmt.Errorf("%s gave an answer that is no JSON: %v", name, err)
		f.log.Warn("starlark tool: answer is no JSON", "skill", f.Skill, "tool", name, "error", err)
	case len(res.Text) > answerLimit:
		f.log.Warn("starlark tool: answer too large", "skill", f.Skill, "tool", name, "bytes", len(res.Text))
		res.Err = fmt.Errorf("the answer of %s has %d KB, more than %d KB", name, len(res.Text)>>10, answerLimit>>10)
		res.Text = ""
	}
	return res
}

// failure is what the model reads of a failed call, and the log line with
// the backtrace (docs/spec/api-tools.md, Calling).
func (f *File) failure(tool string, err error) (string, error) {
	trace := ""
	var ev *starlark.EvalError
	if errors.As(err, &ev) {
		trace = strings.TrimSpace(ev.Backtrace())
	}
	msg := err.Error()
	if ev != nil {
		msg = ev.Msg
	}
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		f.log.Warn("starlark tool: request failed", "skill", f.Skill, "tool", tool, "error", ae.err, "backtrace", trace)
		return trace, ae
	case strings.HasPrefix(msg, "fail: "):
		text := strings.TrimPrefix(msg, "fail: ")
		f.log.Info("starlark tool failed", "skill", f.Skill, "tool", tool, "message", text, "backtrace", trace)
		return trace, errors.New(text)
	case strings.Contains(msg, "too many steps"):
		f.log.Warn("starlark tool: step limit reached", "skill", f.Skill, "tool", tool, "limit", maxSteps, "backtrace", trace)
		return trace, fmt.Errorf("the tool %s took too long (more than %d steps)", tool, maxSteps)
	case strings.Contains(msg, "took longer than"):
		f.log.Warn("starlark tool: time limit reached", "skill", f.Skill, "tool", tool, "limit", Timeout.String(), "backtrace", trace)
		return trace, fmt.Errorf("the tool %s took longer than %s", tool, Timeout)
	}
	first, _, _ := strings.Cut(msg, "\n")
	f.log.Error("starlark tool failed", "skill", f.Skill, "tool", tool, "error", msg, "backtrace", trace)
	return trace, fmt.Errorf("the tool %s failed: %s", tool, first)
}

// answer is the function's return value for the model: a string as it is,
// anything else as JSON.
func answer(thread *starlark.Thread, v starlark.Value) (string, error) {
	switch x := v.(type) {
	case starlark.String:
		return string(x), nil
	case starlark.NoneType:
		return "", nil
	}
	enc, err := starlark.Call(thread, starjson.Module.Members["encode"], starlark.Tuple{v}, nil)
	if err != nil {
		return "", err
	}
	return string(enc.(starlark.String)), nil
}

// arguments checks the model's arguments against the tool's parameters and
// makes them Starlark values.
func (t *Tool) arguments(raw json.RawMessage) ([]starlark.Tuple, error) {
	in := map[string]any{}
	if s := strings.TrimSpace(string(raw)); s != "" && s != "null" {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if err := dec.Decode(&in); err != nil {
			return nil, fmt.Errorf("the arguments of %s are no JSON object: %.200q", t.Name, s)
		}
	}
	known := map[string]Param{}
	for _, p := range t.Params {
		known[p.Name] = p
	}
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := known[n]; !ok {
			return nil, fmt.Errorf("%s has no parameter %s", t.Name, n)
		}
	}
	var out []starlark.Tuple
	for _, p := range t.Params {
		v, given := in[p.Name]
		if !given || v == nil {
			if p.Required {
				return nil, fmt.Errorf("%s needs the parameter %s", t.Name, p.Name)
			}
			continue
		}
		sv, err := value(v, p.Type)
		if err != nil {
			return nil, fmt.Errorf("the parameter %s of %s: %v", p.Name, t.Name, err)
		}
		out = append(out, starlark.Tuple{starlark.String(p.Name), sv})
	}
	return out, nil
}

// value is one argument as the parameter's type.
func value(v any, typ string) (starlark.Value, error) {
	bad := func() error { return fmt.Errorf("%s is no %s", short(v), typ) }
	switch typ {
	case "str":
		if s, ok := v.(string); ok {
			return starlark.String(s), nil
		}
	case "int":
		if n, ok := v.(json.Number); ok {
			if i, err := n.Int64(); err == nil {
				return starlark.MakeInt64(i), nil
			}
			if f, err := n.Float64(); err == nil && f == math.Trunc(f) && math.Abs(f) < 1<<53 {
				return starlark.MakeInt64(int64(f)), nil
			}
		}
	case "float":
		if n, ok := v.(json.Number); ok {
			if f, err := n.Float64(); err == nil {
				return starlark.Float(f), nil
			}
		}
	case "bool":
		if b, ok := v.(bool); ok {
			return starlark.Bool(b), nil
		}
	case "list":
		if xs, ok := v.([]any); ok {
			l := make([]starlark.Value, 0, len(xs))
			for _, x := range xs {
				s, ok := x.(string)
				if !ok {
					return nil, fmt.Errorf("%s is no list of strings", short(v))
				}
				l = append(l, starlark.String(s))
			}
			return starlark.NewList(l), nil
		}
	}
	return nil, bad()
}

func short(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// predeclared is what a tools.star sees (docs/spec/api-tools.md, What the
// file may do).
func (f *File) predeclared() starlark.StringDict {
	methods := starlark.StringDict{}
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		methods[strings.ToLower(m)] = starlark.NewBuiltin("api."+strings.ToLower(m), request(m))
	}
	return starlark.StringDict{
		"api": &starlarkstruct.Module{Name: "api", Members: methods},
		"mira": &starlarkstruct.Module{Name: "mira", Members: starlark.StringDict{
			"tool": starlark.NewBuiltin("mira.tool", miraTool),
		}},
		"json":     jsonModule,
		"math":     starmath.Module,
		"time":     starlarktime.Module,
		"settings": f.settings,
	}
}

// miraTool is mira.tool(name, args): one of Mira's read-only tools under
// tools.reads (decision 19), and none that returns third-party text. Its answer: ok, text, json (the text decoded
// when it is JSON) and error. A tool that is not there or fails is an
// answer with ok False, so the function can go on (another source); a
// name not under tools.reads, or a tool that writes, fails the call.
func miraTool(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var params starlark.Value = starlark.None
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name, "args?", &params); err != nil {
		return nil, err
	}
	c, _ := thread.Local("call").(*call)
	if c == nil {
		return nil, fmt.Errorf("%s works only inside a tool, not while %s loads", b.Name(), FileName)
	}
	if !slices.Contains(c.env.Reads, name) {
		c.f.log.Warn("starlark tool: reading a tool refused", "skill", c.skill, "tool", c.tool, "read", name)
		return nil, &apiError{fmt.Errorf("%s may not call %s: only the tools under tools.reads in SKILL.md", c.tool, name)}
	}
	if c.env.Reader == nil {
		return nil, &apiError{fmt.Errorf("%s: Mira's tools cannot be read here", b.Name())}
	}
	payload := []byte("{}")
	if params != starlark.None {
		enc, err := starlark.Call(thread, starjson.Module.Members["encode"], starlark.Tuple{params}, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: the args are no JSON: %v", b.Name(), err)
		}
		payload = []byte(enc.(starlark.String))
	}
	start := clock.Or(c.env.Clock).Now()
	text, isError, err := c.env.Reader.ReadTool(c.ctx, name, payload)
	ev := Event{Kind: "tool", Tool: name, MS: clock.Or(c.env.Clock).Since(start).Milliseconds(), Size: len(text)}
	if errors.Is(err, ErrNotReadOnly) || errors.Is(err, ErrWebContent) {
		ev.Error = err.Error()
		c.events = append(c.events, ev)
		c.f.log.Warn("starlark tool: reading a tool refused", "skill", c.skill, "tool", c.tool, "read", name, "error", err)
		return nil, &apiError{fmt.Errorf("%s may not call %s: %v", c.tool, name, err)}
	}
	failure := ""
	switch {
	case err != nil:
		failure = err.Error()
	case isError:
		failure = text
	}
	ev.Start = text
	if len(ev.Start) > startLen {
		ev.Start = ev.Start[:startLen] + "…"
	}
	ev.Error = failure
	c.events = append(c.events, ev)
	if failure != "" {
		c.f.log.Info("starlark tool read a tool: it failed", "skill", c.skill, "tool", c.tool, "read", name, "ms", ev.MS,
			"error", failure[:min(len(failure), 200)])
	} else {
		c.f.log.Info("starlark tool read a tool", "skill", c.skill, "tool", c.tool, "read", name, "ms", ev.MS, "bytes", len(text))
	}
	resp, deep := response(thread, 200, []byte(text))
	if deep {
		c.f.log.Warn("starlark tool: the answer nests too deep to decode; its json is None, its text stays",
			"skill", c.skill, "tool", c.tool, "read", name, "limit", maxJSONDepth)
	}
	d := resp.(*starlarkstruct.Struct)
	return starlarkstruct.FromStringDict(starlark.String("tool_result"), starlark.StringDict{
		"ok":    starlark.Bool(failure == ""),
		"text":  starlark.String(text),
		"json":  must(d.Attr("json")),
		"error": starlark.String(failure),
	}), nil
}

func must(v starlark.Value, _ error) starlark.Value { return v }

// apiError is a request that could not be made or got no answer; the tool
// fails with it (Starlark has no try).
type apiError struct{ err error }

func (e *apiError) Error() string { return e.err.Error() }

// request is api.get and the others: one request with the connection's
// token, logged and kept as an event.
func request(method string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var raw string
		var params, body starlark.Value = starlark.None, starlark.None
		if method == http.MethodGet || method == http.MethodDelete {
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &raw, "params?", &params); err != nil {
				return nil, err
			}
		} else if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &raw, "params?", &params, "body?", &body); err != nil {
			return nil, err
		}
		c, _ := thread.Local("call").(*call)
		if c == nil {
			return nil, fmt.Errorf("%s works only inside a tool, not while %s loads", b.Name(), FileName)
		}
		if method != http.MethodGet && !c.env.Writes {
			c.f.log.Warn("starlark tool: writing refused", "skill", c.skill, "tool", c.tool, "method", method)
			return nil, &apiError{fmt.Errorf("%s may not use %s: only the tools under writes in SKILL.md write", c.tool, b.Name())}
		}
		u, err := withQuery(raw, params)
		if err != nil {
			return nil, &apiError{fmt.Errorf("%s: %v", b.Name(), err)}
		}
		var payload []byte
		if body != starlark.None {
			enc, err := starlark.Call(thread, starjson.Module.Members["encode"], starlark.Tuple{body}, nil)
			if err != nil {
				return nil, fmt.Errorf("%s: the body is no JSON: %v", b.Name(), err)
			}
			payload = []byte(enc.(starlark.String))
		}
		start := clock.Or(c.env.Clock).Now()
		status, data, err := c.env.Requester.Request(c.ctx, c.skill, method, u, payload)
		ev := Event{Kind: "request", Method: method, URL: u, Status: status, MS: clock.Or(c.env.Clock).Since(start).Milliseconds(), Size: len(data)}
		if err != nil {
			ev.Error = err.Error()
			c.events = append(c.events, ev)
			c.f.log.Warn("starlark tool request failed", "skill", c.skill, "tool", c.tool, "method", method, "url", u,
				"ms", ev.MS, "error", err)
			return nil, &apiError{fmt.Errorf("%s %s: %v", method, u, err)}
		}
		ev.Start = string(data)
		if len(ev.Start) > startLen {
			ev.Start = ev.Start[:startLen] + "…"
		}
		c.events = append(c.events, ev)
		c.f.log.Info("starlark tool request", "skill", c.skill, "tool", c.tool, "method", method, "url", u,
			"status", status, "ms", ev.MS, "bytes", len(data))
		resp, deep := response(thread, status, data)
		if deep {
			c.f.log.Warn("starlark tool: the answer nests too deep to decode; its json is None, its text stays",
				"skill", c.skill, "tool", c.tool, "method", method, "url", u, "limit", maxJSONDepth)
		}
		return resp, nil
	}
}

// response is the answer as the function sees it: status, ok, text, json.
// deep says that the answer nested deeper than maxJSONDepth and was not
// decoded (its json is None).
func response(thread *starlark.Thread, status int, data []byte) (resp starlark.Value, deep bool) {
	var decoded starlark.Value = starlark.None
	if s := strings.TrimSpace(string(data)); strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		if deep = jsonDepth(s, maxJSONDepth) > maxJSONDepth; !deep {
			if v, err := starlark.Call(thread, starjson.Module.Members["decode"], starlark.Tuple{starlark.String(s)}, nil); err == nil {
				decoded = v
			}
		}
	}
	return starlarkstruct.FromStringDict(starlark.String("response"), starlark.StringDict{
		"status": starlark.MakeInt(status),
		"ok":     starlark.Bool(status >= 200 && status < 300),
		"text":   starlark.String(data),
		"json":   decoded,
	}), deep
}

// withQuery is the address with the query from params: every value encoded,
// None left out, a list as repeated parameters, a time in RFC 3339 (UTC).
func withQuery(raw string, params starlark.Value) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("%q is no https address", raw)
	}
	if params == starlark.None {
		return u.String(), nil
	}
	d, ok := params.(*starlark.Dict)
	if !ok {
		return "", fmt.Errorf("params is a %s, not a dict", params.Type())
	}
	q := u.Query()
	for _, item := range d.Items() {
		k, ok := item[0].(starlark.String)
		if !ok {
			return "", fmt.Errorf("a key of params is a %s, not a string", item[0].Type())
		}
		var vals []starlark.Value
		switch x := item[1].(type) {
		case *starlark.List:
			for i := 0; i < x.Len(); i++ {
				vals = append(vals, x.Index(i))
			}
		case starlark.Tuple:
			vals = x
		default:
			vals = []starlark.Value{x}
		}
		for _, v := range vals {
			s, skip, err := queryValue(v)
			if err != nil {
				return "", fmt.Errorf("params[%q]: %v", string(k), err)
			}
			if !skip {
				q.Add(string(k), s)
			}
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func queryValue(v starlark.Value) (string, bool, error) {
	switch x := v.(type) {
	case starlark.NoneType:
		return "", true, nil
	case starlark.String:
		return string(x), false, nil
	case starlark.Int:
		return x.String(), false, nil
	case starlark.Float:
		return x.String(), false, nil
	case starlark.Bool:
		if x {
			return "true", false, nil
		}
		return "false", false, nil
	case starlarktime.Time:
		return time.Time(x).UTC().Format(time.RFC3339), false, nil
	}
	return "", false, fmt.Errorf("a %s cannot go into an address", v.Type())
}

// cancelReason is why a tool's run is cancelled when its context ends: its
// own time limit, or its caller's end (docs/spec/endings.md, EN-07).
func cancelReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "the tool took longer than " + Timeout.String()
	}
	return "the tool's caller ended"
}
