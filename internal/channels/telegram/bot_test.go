package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI is a minimal Bot API: it hands out queued updates once and records
// sent messages.
type fakeAPI struct {
	*httptest.Server
	mu        sync.Mutex
	updates   []Update
	sent      []map[string]any
	offsets   []int64
	rejectFmt bool
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/botTOKEN/") {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"ok":false,"error_code":404,"description":"Not Found"}`)
			return
		}
		var params map[string]any
		json.NewDecoder(r.Body).Decode(&params)
		method := strings.TrimPrefix(r.URL.Path, "/botTOKEN/")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch method {
		case "getUpdates":
			off := int64(params["offset"].(float64))
			f.offsets = append(f.offsets, off)
			var out []Update
			for _, u := range f.updates {
				if u.UpdateID >= off {
					out = append(out, u)
				}
			}
			if len(out) == 0 {
				f.mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				f.mu.Lock()
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": out})
		case "sendMessage":
			if f.rejectFmt && params["parse_mode"] != nil {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`)
				return
			}
			f.sent = append(f.sent, params)
			io.WriteString(w, `{"ok":true,"result":{}}`)
		default:
			io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) push(u Update) {
	f.mu.Lock()
	f.updates = append(f.updates, u)
	f.mu.Unlock()
}

func (f *fakeAPI) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.sent {
		out = append(out, p["text"].(string))
	}
	return out
}

type memOffsets struct {
	mu sync.Mutex
	v  int64
}

func (m *memOffsets) GetInt64(context.Context, string, int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v, nil
}
func (m *memOffsets) SetInt64(_ context.Context, _ string, v int64) error {
	m.mu.Lock()
	m.v = v
	m.mu.Unlock()
	return nil
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func msg(id, from int64, chatType, text string) Update {
	chat := from
	if chatType != "private" {
		chat = -100
	}
	return Update{UpdateID: id, Message: &Message{MessageID: id, From: &User{ID: from}, Chat: Chat{ID: chat, Type: chatType}, Text: text}}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOnlyAllowedUsersInPrivateChatsReachTheHandler(t *testing.T) {
	api := newFakeAPI(t)
	offsets := &memOffsets{}
	var mu sync.Mutex
	var seen []Incoming
	bot := New(Options{
		Token: "TOKEN", APIBase: api.URL, AllowedUserIDs: []int64{111}, PollTimeout: time.Second,
		Offsets: offsets, Logger: quiet(),
		Handler: func(_ context.Context, m Incoming) (string, error) {
			mu.Lock()
			seen = append(seen, m)
			mu.Unlock()
			return "**Licht** ist an: `light.kueche_decke`", nil
		},
	})
	api.push(msg(10, 999, "private", "Mach alles auf")) // stranger
	api.push(msg(11, 111, "group", "Licht an"))         // group chat
	api.push(msg(12, 111, "private", "Mach Licht an"))  // allowed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Run(ctx)

	waitFor(t, func() bool { return len(api.sentTexts()) == 1 })
	mu.Lock()
	if len(seen) != 1 || seen[0].Text != "Mach Licht an" || seen[0].ChatID != 111 {
		t.Fatalf("handler saw %+v", seen)
	}
	mu.Unlock()
	if got := api.sentTexts()[0]; got != "<b>Licht</b> ist an: <code>light.kueche_decke</code>" {
		t.Errorf("sent %q", got)
	}
	waitFor(t, func() bool { v, _ := offsets.GetInt64(ctx, "", 0); return v == 13 })
}

func TestOffsetIsResumedAfterRestart(t *testing.T) {
	api := newFakeAPI(t)
	offsets := &memOffsets{v: 21}
	var calls sync.WaitGroup
	calls.Add(1)
	bot := New(Options{Token: "TOKEN", APIBase: api.URL, AllowedUserIDs: []int64{1}, PollTimeout: time.Second,
		Offsets: offsets, Logger: quiet(),
		Handler: func(_ context.Context, m Incoming) (string, error) {
			if m.Text != "neu" {
				t.Errorf("replayed old message %q", m.Text)
			}
			calls.Done()
			return "ok", nil
		}})
	api.push(msg(20, 1, "private", "alt"))
	api.push(msg(21, 1, "private", "neu"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Run(ctx)
	calls.Wait()
}

func TestUnparseableFormattingFallsBackToPlainText(t *testing.T) {
	api := newFakeAPI(t)
	api.rejectFmt = true
	bot := New(Options{Token: "TOKEN", APIBase: api.URL, Offsets: &memOffsets{}, Logger: quiet()})
	if err := bot.Send(context.Background(), 1, "*kaputt"); err != nil {
		t.Fatal(err)
	}
	if got := api.sentTexts(); len(got) != 1 || got[0] != "*kaputt" {
		t.Fatalf("sent %q", got)
	}
}

func TestBadTokenStopsPolling(t *testing.T) {
	api := newFakeAPI(t)
	bot := New(Options{Token: "WRONG", APIBase: api.URL, Offsets: &memOffsets{}, Logger: quiet(), PollTimeout: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := bot.Run(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("Run = %v (ctx %v)", err, ctx.Err())
	}
}

func TestToHTML(t *testing.T) {
	cases := map[string]string{
		"# Titel":                       "<b>Titel</b>",
		"- eins\n- zwei":                "• eins\n• zwei",
		"a < b & c":                     "a &lt; b &amp; c",
		"*kursiv* und **fett**":         "<i>kursiv</i> und <b>fett</b>",
		"sensor.bad_temp_1 ist 2*3":     "sensor.bad_temp_1 ist 2*3",
		"[HA](https://example.org/a_b)": `<a href="https://example.org/a_b">HA</a>`,
		"```\nx <y>\n```":               "<pre>x &lt;y&gt;\n</pre>",
		"`a*b*c`":                       "<code>a*b*c</code>",
	}
	for in, want := range cases {
		if got := ToHTML(in); got != want {
			t.Errorf("ToHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	para := strings.Repeat("Wort ", 300) // ~1500 chars
	text := para + "\n\n" + para + "\n\n" + para
	chunks := Split(text, 2000)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 2000 {
			t.Errorf("chunk of %d chars", len(c))
		}
	}
	if got := Split(strings.Repeat("ä", 3000), 1001); strings.Join(got, "") != strings.Repeat("ä", 3000) {
		t.Error("splitting without spaces lost or broke runes")
	}
}
