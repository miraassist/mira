package startools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/startools"
)

// fakeRequester answers every request with its answer and records them.
type fakeRequester struct {
	mu     sync.Mutex
	urls   []string
	status int
	body   string
	// block holds a request until the call gives it up; entered, when set,
	// is closed as one comes in.
	block   bool
	entered chan struct{}
}

func (f *fakeRequester) Request(ctx context.Context, skill, method, raw string, body []byte) (int, []byte, error) {
	f.mu.Lock()
	f.urls = append(f.urls, method+" "+raw)
	if f.entered != nil {
		close(f.entered)
		f.entered = nil
	}
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return 0, nil, ctx.Err()
	}
	return f.status, []byte(f.body), nil
}

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func load(t *testing.T, src string) (*startools.File, *logBuf) {
	t.Helper()
	logs := &logBuf{}
	f, err := startools.Load("dienst", []byte(src), map[string]string{"land": "DE"}, slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return f, logs
}

const suche = `
def suche(thema, tage = 7):
    """Sucht Videos zu einem Thema.

    Kostet 100 Einheiten.

    Args:
      thema (str): Das Thema in wenigen Wörtern.
      tage (int): Wie viele Tage zurück,
        höchstens 30.
    """
    if tage > 30:
        fail("tage ist 1 bis 30, nicht %d" % tage)
    r = api.get("https://api.example/v3/search", params = {"q": thema, "tage": tage})
    return _kurz(r)

def _kurz(r):
    """Ein Helfer."""
    return {"status": r.status, "ok": r.ok, "titel": r.json["items"][0]["title"]}
`

// SL-01 A function with a docstring is a tool
func TestSL01_AFunctionWithADocstringIsATool(t *testing.T) {
	// Given a tools.star that defines suche(thema, tage = 7) with a docstring in Google's style
	// When the skill loads
	f, _ := load(t, suche)
	// Then it has the tool suche with the docstring's text as its description
	tl, ok := f.Tool("suche")
	if !ok || tl.Description != "Sucht Videos zu einem Thema.\n\nKostet 100 Einheiten." {
		t.Fatalf("tool %+v", tl)
	}
	// And thema is a required string and tage an optional integer with the default 7, each with its text
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
		Additional bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal([]byte(tl.Schema), &schema); err != nil {
		t.Fatal(err)
	}
	th, tg := schema.Properties["thema"], schema.Properties["tage"]
	if th["type"] != "string" || th["description"] != "Das Thema in wenigen Wörtern." || th["default"] != nil {
		t.Errorf("thema %v", th)
	}
	if tg["type"] != "integer" || tg["description"] != "Wie viele Tage zurück, höchstens 30." || tg["default"] != float64(7) {
		t.Errorf("tage %v", tg)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "thema" || schema.Additional {
		t.Errorf("schema %s", tl.Schema)
	}
}

// SL-02 A function beginning with _ is a helper
func TestSL02_AFunctionBeginningWithUnderscoreIsAHelper(t *testing.T) {
	// Given a tools.star with the functions suche and _kurz
	// When the skill loads
	f, _ := load(t, suche)
	// Then suche is a tool and _kurz is none
	if got := f.Names(); len(got) != 1 || got[0] != "suche" {
		t.Errorf("tools %v", got)
	}
}

func loadErr(src string) string {
	_, err := startools.Load("dienst", []byte(src), nil, nil)
	if err == nil {
		return ""
	}
	return err.Error()
}

// SL-03 A docstring that does not fit its function is a problem of the skill
func TestSL03_ADocstringThatDoesNotFitItsFunctionIsAProblem(t *testing.T) {
	// Given a tools.star whose docstring names a parameter the function does not have, or misses one it has
	for src, want := range map[string]string{
		"\n\ndef suche(thema):\n    \"\"\"Sucht.\n\n    Args:\n      thema (str): Das Thema.\n      tage (int): Tage.\n    \"\"\"\n    return 1\n": "tools.star:3: suche: the docstring names the parameter tage",
		"\ndef suche(thema, tage):\n    \"\"\"Sucht.\n\n    Args:\n      thema (str): Das Thema.\n    \"\"\"\n    return 1\n":                      "tools.star:2: suche: the parameter tage has no entry under Args:",
		"def suche(thema):\n    \"\"\"Sucht.\n\n    Args:\n      thema: Das Thema.\n    \"\"\"\n    return 1\n":                                    "tools.star:1: suche: the parameter thema in the docstring has no type",
	} {
		// When the skill loads
		// Then its problem names tools.star, the line and the parameter
		if got := loadErr(src); !strings.Contains(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// SL-04 A tools.star that is wrong in other ways is a problem of the skill
func TestSL04_AToolsStarThatIsWrongInOtherWaysIsAProblem(t *testing.T) {
	// Given a tools.star without a docstring, or with an unknown type, or with *args, or that does not parse
	for src, want := range map[string]string{
		"def suche(thema):\n    return 1\n": "tools.star:1: suche: the tool has no docstring",
		"def suche(thema):\n    \"\"\"Sucht.\n\n    Args:\n      thema (dict): Das Thema.\n    \"\"\"\n    return 1\n": "the parameter thema has the type \"dict\"",
		"def suche(*thema):\n    \"\"\"Sucht.\"\"\"\n    return 1\n":                                                   "tools.star:1: suche: a tool takes no *args",
		"def suche(thema)\n    return 1\n":                                                                             "tools.star:2:1: got newline, want ':'",
		"x = 1\n":                                                                                                      "has no tool",
		"def suche(thema):\n    \"\"\"Sucht.\n\n    Args:\n      thema (int): Das Thema.\n    \"\"\"\n    return 1\nfail('kaputt')\n": "tools.star:8",
	} {
		// When the skill loads
		// Then its problem says what is wrong, with the line
		if got := loadErr(src); !strings.Contains(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	// A default that does not fit its type.
	if got := loadErr("def suche(n = \"x\"):\n    \"\"\"Sucht.\n\n    Args:\n      n (int): Wie viele.\n    \"\"\"\n    return 1\n"); !strings.Contains(got, "the default of n") {
		t.Errorf("got %q", got)
	}
	// api is no call while the file loads.
	if got := loadErr("r = api.get(\"https://api.example/\")\n"); !strings.Contains(got, "only inside a tool") {
		t.Errorf("got %q", got)
	}
}

// SL-07 The arguments are checked before the function runs
func TestSL07_TheArgumentsAreCheckedBeforeTheFunctionRuns(t *testing.T) {
	// Given the tool suche(thema, tage = 7)
	f, _ := load(t, suche)
	req := &fakeRequester{status: 200, body: `{"items":[{"title":"A"}]}`}
	// When the model calls it without thema, with tage "sieben", or with a parameter it does not have
	for args, want := range map[string]string{
		`{}`:                            "suche needs the parameter thema",
		`{"thema":"x","tage":"sieben"}`: `the parameter tage of suche: "sieben" is no int`,
		`{"thema":"x","tage":2.5}`:      "the parameter tage of suche: 2.5 is no int",
		`{"thema":"x","farbe":"rot"}`:   "suche has no parameter farbe",
		`{"thema":3}`:                   "the parameter thema of suche: 3 is no str",
		`[1]`:                           "no JSON object",
	} {
		res := f.Call(context.Background(), "suche", json.RawMessage(args), startools.Env{Requester: req})
		// Then the call is the tool's error naming the parameter
		if res.Err == nil || !strings.Contains(res.Err.Error(), want) {
			t.Errorf("%s: %v, want %q", args, res.Err, want)
		}
	}
	// and the function does not run
	if len(req.urls) != 0 {
		t.Errorf("requests %v", req.urls)
	}
	// A whole number as a float is an int; a missing optional is its default.
	res := f.Call(context.Background(), "suche", json.RawMessage(`{"thema":"x","tage":3.0}`), startools.Env{Requester: req})
	if res.Err != nil || !strings.Contains(req.urls[0], "tage=3") {
		t.Errorf("%v %v", res.Err, req.urls)
	}
	f.Call(context.Background(), "suche", json.RawMessage(`{"thema":"x"}`), startools.Env{Requester: req})
	if !strings.Contains(req.urls[1], "tage=7") {
		t.Errorf("default: %v", req.urls)
	}
}

// SL-08 api.get builds the address
func TestSL08_ApiGetBuildsTheAddress(t *testing.T) {
	// Given a tool that calls api.get with params: a string with spaces, None, a list and a time
	f, _ := load(t, `
def liste(thema):
    """Liste.

    Args:
      thema (str): Thema.
    """
    seit = time.now() - 24 * time.hour
    r = api.get("https://api.example/v3/search?part=snippet", params = {"q": thema, "leer": None,
        "id": ["a", "b"], "seit": seit, "n": 5, "an": True, "land": settings["land"]})
    return r.text
`)
	req := &fakeRequester{status: 200, body: "fertig"}
	now := time.Date(2026, 11, 20, 14, 30, 0, 0, time.FixedZone("CET", 3600))
	// When it runs
	res := f.Call(context.Background(), "liste", json.RawMessage(`{"thema":"Home Assistant & Voice"}`),
		startools.Env{Requester: req, Now: func() time.Time { return now }})
	if res.Err != nil || res.Text != "fertig" {
		t.Fatalf("%v %q", res.Err, res.Text)
	}
	u, err := url.Parse(strings.TrimPrefix(req.urls[0], "GET "))
	if err != nil {
		t.Fatal(err)
	}
	// Then the query has every value encoded, no parameter for None, the list repeated and the time in RFC 3339 in UTC
	q := u.Query()
	if q.Get("q") != "Home Assistant & Voice" || q.Has("leer") || strings.Join(q["id"], ",") != "a,b" ||
		q.Get("seit") != "2026-11-19T13:30:00Z" || q.Get("n") != "5" || q.Get("an") != "true" || q.Get("part") != "snippet" ||
		q.Get("land") != "DE" {
		t.Errorf("query %v", q)
	}
	if !strings.Contains(u.RawQuery, "q=Home+Assistant+%26+Voice") {
		t.Errorf("not encoded: %s", u.RawQuery)
	}
}

// SL-10 What fails reaches the model and the log
func TestSL10_WhatFailsReachesTheModelAndTheLog(t *testing.T) {
	// Given a tool that calls fail("tage ist 1 bis 30"), and a tool that reads a key its answer lacks
	f, logs := load(t, suche)
	req := &fakeRequester{status: 200, body: `{"keine":[]}`}
	// When they run
	failed := f.Call(context.Background(), "suche", json.RawMessage(`{"thema":"x","tage":40}`), startools.Env{Requester: req})
	broken := f.Call(context.Background(), "suche", json.RawMessage(`{"thema":"x"}`), startools.Env{Requester: req})
	// Then the model reads the message of fail
	if failed.Err == nil || failed.Err.Error() != "tage ist 1 bis 30, nicht 40" {
		t.Errorf("fail: %v", failed.Err)
	}
	// and for the other the first line of the error
	if broken.Err == nil || !strings.Contains(broken.Err.Error(), `the tool suche failed: key "items" not in dict`) {
		t.Errorf("broken: %v", broken.Err)
	}
	// And the log has the backtrace with tools.star and the line
	if !strings.Contains(broken.Backtrace, "tools.star:19") || !strings.Contains(logs.String(), "tools.star:19") ||
		!strings.Contains(failed.Backtrace, "tools.star:13") {
		t.Errorf("backtraces %q / %q\nlog %s", failed.Backtrace, broken.Backtrace, logs)
	}
}

// SL-11 A tool has limits
func TestSL11_AToolHasLimits(t *testing.T) {
	// Given a tool that loops for ever, and a tool whose request does not answer
	f, logs := load(t, `
def ewig():
    """Rechnet ewig."""
    n = 0
    for i in range(100000000):
        n += i
    return n

def warten():
    """Wartet auf den Dienst."""
    return api.get("https://api.example/langsam").text
`)
	// When they run
	loop := f.Call(context.Background(), "ewig", nil, startools.Env{})
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	ctx, cancel := clock.WithTimeout(context.Background(), clk, 50*time.Millisecond)
	defer cancel()
	entered := make(chan struct{})
	waited := make(chan startools.Result, 1)
	go func() {
		waited <- f.Call(ctx, "warten", nil, startools.Env{Requester: &fakeRequester{block: true, entered: entered}, Clock: clk})
	}()
	<-entered                          // the request waits for an answer
	clk.Advance(50 * time.Millisecond) // the caller's time is up
	wait := <-waited
	// Then the first ends at the step limit and the second at the time limit, each as the tool's error and a log line
	if loop.Err == nil || !strings.Contains(loop.Err.Error(), "took too long") {
		t.Errorf("loop: %v", loop.Err)
	}
	if wait.Err == nil || !strings.Contains(wait.Err.Error(), "context deadline exceeded") {
		t.Errorf("wait: %v", wait.Err)
	}
	if !strings.Contains(logs.String(), "step limit reached") || !strings.Contains(logs.String(), "starlark tool request failed") {
		t.Errorf("log %s", logs)
	}
	if startools.Timeout != 60*time.Second {
		t.Errorf("timeout %s", startools.Timeout)
	}
}

// SL-12 Only the tools under writes write (the part of the file: a tool
// not under writes cannot write)
func TestSL12_OnlyTheToolsUnderWritesWrite(t *testing.T) {
	f, _ := load(t, `
def lies():
    """Liest und schreibt doch."""
    return api.post("https://api.example/v2/posts", body = {"text": "hallo"}).status
`)
	req := &fakeRequester{status: 200}
	// When the tool lies calls api.post
	res := f.Call(context.Background(), "lies", nil, startools.Env{Requester: req})
	// Then the call of lies fails before anything is sent
	if res.Err == nil || !strings.Contains(res.Err.Error(), "only the tools under writes in SKILL.md write") || len(req.urls) != 0 {
		t.Errorf("%v %v", res.Err, req.urls)
	}
	// Under writes it is sent.
	res = f.Call(context.Background(), "lies", nil, startools.Env{Requester: req, Writes: true})
	if res.Err != nil || res.Text != "200" || len(req.urls) != 1 || req.urls[0] != "POST https://api.example/v2/posts" {
		t.Errorf("%v %q %v", res.Err, res.Text, req.urls)
	}
}

// SL-13 Every request and print leaves a trace
func TestSL13_EveryRequestAndPrintLeavesATrace(t *testing.T) {
	// Given a tool that prints and makes a request
	f, logs := load(t, `
def zeigen(thema):
    """Zeigt.

    Args:
      thema (str): Thema.
    """
    print("suche", thema)
    return api.get("https://api.example/v3/search", params = {"q": thema}).json
`)
	// When it runs
	res := f.Call(context.Background(), "zeigen", json.RawMessage(`{"thema":"voice"}`),
		startools.Env{Requester: &fakeRequester{status: 200, body: `{"items":[1,2]}`}})
	if res.Err != nil || res.Text != `{"items":[1,2]}` {
		t.Fatalf("%v %q", res.Err, res.Text)
	}
	// Then the log has a line with the skill, the tool, the method, the address with its query, the status, the time
	// and the size, and a line with the print
	l := logs.String()
	for _, want := range []string{`msg="starlark tool request" skill=dienst tool=zeigen method=GET url="https://api.example/v3/search?q=voice" status=200 ms=`,
		"bytes=15", `msg="starlark tool print" skill=dienst tool=zeigen message="suche voice"`} {
		if !strings.Contains(l, want) {
			t.Errorf("log lacks %q:\n%s", want, l)
		}
	}
	// The events are in order, for the page.
	if len(res.Events) != 2 || res.Events[0].Kind != "print" || res.Events[1].Kind != "request" ||
		res.Events[1].Status != 200 || res.Events[1].Start != `{"items":[1,2]}` {
		t.Errorf("events %+v", res.Events)
	}
}

// SL-19 A tool that takes too much memory or answers too much is stopped
func TestSL19_AToolThatTakesTooMuchMemoryOrAnswersTooMuchIsStopped(t *testing.T) {
	old := startools.SetMemLimit(16 << 20)
	defer startools.SetMemLimit(old)
	// Given a tool that makes a string of 64 MB and keeps it through a request
	f, logs := load(t, `
def gross():
    """Macht einen großen Text."""
    x = "x" * 64000000
    api.get("https://api.example/langsam")
    return len(x)

def viel():
    """Antwortet zu viel."""
    return "y" * 2000000
`)
	// When it runs
	clk := clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))
	entered := make(chan struct{})
	called := make(chan startools.Result, 1)
	go func() {
		called <- f.Call(context.Background(), "gross", nil, startools.Env{Requester: &fakeRequester{block: true, entered: entered}, Clock: clk})
	}()
	<-entered                        // the 64 MB are made; the call waits in its request
	<-clk.AwaitWaiters(2)            // the call's time limit and the look at the memory
	clk.Advance(startools.MemSample) // the next look sees too much
	res := <-called
	// Then it is stopped as the tool's error and a log line
	if res.Err == nil || !strings.Contains(res.Err.Error(), "used too much memory (more than 16 MB)") ||
		!strings.Contains(logs.String(), "memory limit reached") {
		t.Errorf("%v\n%s", res.Err, logs)
	}
	// And an answer of more than 1 MB is the tool's error
	res = f.Call(context.Background(), "viel", nil, startools.Env{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "more than 1024 KB") || res.Text != "" {
		t.Errorf("%v %d", res.Err, len(res.Text))
	}
}

// SL-20 JSON that nests too deep is not decoded
func TestSL20_JSONThatNestsTooDeepIsNotDecoded(t *testing.T) {
	// Given a service that answers with two megabytes of "[", which would
	// overflow the stack of Starlark's decoder and end the whole process
	deep := strings.Repeat("[", 2<<20)
	f, logs := load(t, `
def hole():
    """Holt etwas."""
    r = api.get("https://api.example/tief")
    return {"json": r.json, "len": len(r.text)}

def selbst(text):
    """Dekodiert selbst.

    Args:
      text (str): Der Text.
    """
    return json.decode(text)
`)
	// When a tool gets it
	res := f.Call(context.Background(), "hole", nil, startools.Env{Requester: &fakeRequester{status: 200, body: deep}})
	// Then its json is None, its text stays, and the log says why
	if res.Err != nil || !strings.Contains(res.Text, `"json":null`) || !strings.Contains(res.Text, `"len":2097152`) {
		t.Fatalf("%v %.200s", res.Err, res.Text)
	}
	if !strings.Contains(logs.String(), "nests too deep") {
		t.Errorf("log:\n%s", logs)
	}
	// And json.decode refuses it as the tool's error
	arg, _ := json.Marshal(map[string]string{"text": deep})
	res = f.Call(context.Background(), "selbst", arg, startools.Env{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "deeper than 1000 levels") {
		t.Fatalf("decode: %v", res.Err)
	}
	// And a nesting within the bound, with brackets in strings, is decoded
	ok := strings.Repeat("[", 1000) + `"[[{{"` + strings.Repeat("]", 1000)
	arg, _ = json.Marshal(map[string]string{"text": ok})
	if res = f.Call(context.Background(), "selbst", arg, startools.Env{}); res.Err != nil {
		t.Fatalf("within the bound: %v", res.Err)
	}
	// And a tools.star that decodes it while it loads is a problem, not a crash
	if err := loadErr("x = json.decode(\"" + strings.Repeat("[", 2<<20) + "\")\n"); !strings.Contains(err, "deeper than 1000 levels") {
		t.Errorf("loading: %s", err)
	}
}
