package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/store/storetest"
	a2afake "github.com/miraassist/mira/testenv/a2a_fake"
)

// callHost is a host with the store and a channel of what it delivers.
func callHost(t *testing.T, url, a2aSkill string) (*Host, chan store.AgentRun, *syncBuffer) {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: storetest.Path(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log, buf := logger()
	got := make(chan store.AgentRun, 4)
	h := New(Options{Skills: load(t, skillFile(t, "          a2a_skill: "+a2aSkill+"\n", ""), url), Runs: st,
		Deliver: func(_ context.Context, rec store.AgentRun) { got <- rec }, CallRetry: 5 * time.Millisecond, Logger: log,
		Clock: clock.NewFake(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC))})
	h.Start(context.Background())
	t.Cleanup(h.Close)
	return h, got, buf
}

// delivered is the next run the host delivers; the waits before its
// attempts run out on the host's clock meanwhile.
func delivered(h *Host, got chan store.AgentRun) store.AgentRun {
	var rec store.AgentRun
	h.clock.(*clock.Fake).Drive(func() { rec = <-got })
	return rec
}

// proxy stands before the fake and answers the first n calls of the
// agent's endpoint with status.
func proxy(t *testing.T, target string, n int32, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	u, _ := url.Parse(target)
	rp := httputil.NewSingleHostReverseProxy(u)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/a2a") && calls.Add(1) <= n {
			http.Error(w, "busy", status)
			return
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// A send that did not reach the agent (503) is tried again with the same
// message id; one that may have reached it (504) is not (decision 26).
func TestRemoteSendRetriedOnlyWhenNotReached(t *testing.T) {
	f, srv := fake(t, a2afake.Options{})
	p, calls := proxy(t, srv.URL, 2, http.StatusServiceUnavailable)
	h, got, buf := callHost(t, p.URL+"/"+a2afake.CardStreamOnly, a2afake.SkillComplete)
	if err := h.Send(context.Background(), Request{CallID: "c1", Agent: "recherche", Input: json.RawMessage(`{"input":"Finde X"}`)}); err != nil {
		t.Fatal(err)
	}
	rec := delivered(h, got)
	if rec.Status != statusDone || rec.Output != a2afake.Result || calls.Load() != 3 {
		t.Errorf("rec %+v after %d calls", rec, calls.Load())
	}
	if !strings.Contains(buf.String(), "sending to the remote agent failed, trying again") {
		t.Errorf("log:\n%s", buf.String())
	}
	var ids []string
	for _, r := range f.Received() {
		if r.Kind == "SendStreamingMessage" {
			ids = append(ids, r.MessageID)
		}
	}
	if len(ids) != 1 || ids[0] != "c1" {
		t.Errorf("message ids %v", ids)
	}

	_, srv2 := fake(t, a2afake.Options{})
	p2, calls2 := proxy(t, srv2.URL, 1, http.StatusGatewayTimeout)
	h2, got2, _ := callHost(t, p2.URL+"/"+a2afake.CardStreamOnly, a2afake.SkillComplete)
	if err := h2.Send(context.Background(), Request{CallID: "c2", Agent: "recherche", Input: json.RawMessage(`{"input":"Finde X"}`)}); err != nil {
		t.Fatal(err)
	}
	rec = delivered(h2, got2)
	if rec.Status != statusFailed || calls2.Load() != 1 || !strings.Contains(rec.Error, "504") {
		t.Errorf("rec %+v after %d calls", rec, calls2.Load())
	}
}

// The input's text goes as a text part, its other fields as a data part
// beside it (decision 10); the card's skill in the metadata (decision 27).
func TestRemoteInputAsAMessage(t *testing.T) {
	m, text, err := firstMessage("c1", "research", json.RawMessage(`{"input":"Finde X","flaeche":140}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 2 || m.Parts[0].Text() != "Finde X" || m.ID != "c1" || m.Metadata["skill"] != "research" {
		t.Errorf("message %+v", m)
	}
	if d, ok := m.Parts[1].Data().(map[string]any); !ok || d["flaeche"] != float64(140) {
		t.Errorf("data part %+v", m.Parts[1])
	}
	if text != "Finde X\n{\"flaeche\":140}" {
		t.Errorf("text %q", text)
	}
	if _, _, err := firstMessage("c1", "", json.RawMessage(`{}`)); err == nil {
		t.Error("an empty input was sent")
	}
}

// A field due while the agent's card cannot be read fails at once, naming
// why.
func TestRemoteSendWhileTheCardCannotBeRead(t *testing.T) {
	h, _, _ := callHost(t, "http://127.0.0.1:1", a2afake.SkillComplete)
	err := h.Send(context.Background(), Request{CallID: "c1", Agent: "recherche", Input: json.RawMessage(`{"input":"x"}`)})
	if err == nil || !strings.Contains(err.Error(), "its card cannot be read") {
		t.Errorf("err = %v", err)
	}
}
