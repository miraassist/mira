// Package fakellm is a scriptable OpenAI-compatible server for tests.
//
// Besides returning canned answers and tool calls, it imitates llama.cpp's
// prompt cache: per slot it remembers the last prompt it saw and reports the
// common prefix as cached tokens. That makes the agent's cache invariants
// testable without a GPU — a request whose stable layers changed shows up as a
// cache miss here exactly as it would on the real server.
package fakellm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/llm"
)

// Step is one scripted reply. Give it Content, ToolCalls, or both.
type Step struct {
	Content      string
	ToolCalls    []llm.ToolCall
	FinishReason string
	// Status, if non-zero, makes the server answer with that HTTP status
	// instead of a completion.
	Status int
	Body   string
	Delay  time.Duration
}

// Recorded is one request as the server saw it.
type Recorded struct {
	Request    llm.ChatRequest
	RawBody    json.RawMessage
	Slot       int
	HasSlot    bool
	CacheQuery bool
	// PromptTokens and CachedTokens are what the fake reported back.
	PromptTokens int
	CachedTokens int
}

type Server struct {
	*httptest.Server

	mu       sync.Mutex
	script   []Step
	next     int
	requests []Recorded
	// slotPrompts holds the last tokenised prompt per slot. -1 collects every
	// request that did not pin a slot.
	slotPrompts map[int][]string
	// Default is returned when the script is exhausted.
	Default Step
}

// New starts a server with the given script. When the script runs out, the
// server keeps answering with Default (an empty acknowledgement by default).
func New(script ...Step) *Server {
	s := &Server{
		script:      script,
		slotPrompts: map[int][]string{},
		Default:     Step{Content: "ok", FinishReason: "stop"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":[{"id":"fake","object":"model"}]}`))
	})
	s.Server = httptest.NewServer(mux)
	return s
}

// BaseURL is the value for llm.Options.BaseURL.
func (s *Server) BaseURL() string { return s.Server.URL + "/v1" }

// Push appends steps to the script at runtime.
func (s *Server) Push(steps ...Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script = append(s.script, steps...)
}

// Requests returns everything the server has seen so far.
func (s *Server) Requests() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recorded, len(s.requests))
	copy(out, s.requests)
	return out
}

// Last returns the most recent request, failing the caller's expectations
// loudly if there is none.
func (s *Server) Last() Recorded {
	reqs := s.Requests()
	if len(reqs) == 0 {
		return Recorded{}
	}
	return reqs[len(reqs)-1]
}

func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
	s.next = 0
	s.slotPrompts = map[int][]string{}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	raw, _ := readAll(r)
	var req llm.ChatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	step := s.Default
	if s.next < len(s.script) {
		step = s.script[s.next]
		s.next++
	}
	slot := -1
	if req.IDSlot != nil {
		slot = *req.IDSlot
	}
	prompt := Tokenize(&req)
	cached := commonPrefix(s.slotPrompts[slot], prompt)
	s.slotPrompts[slot] = prompt
	rec := Recorded{
		Request: req, RawBody: raw, Slot: slot, HasSlot: req.IDSlot != nil,
		PromptTokens: len(prompt), CachedTokens: cached,
	}
	s.requests = append(s.requests, rec)
	s.mu.Unlock()

	if step.Delay > 0 {
		time.Sleep(step.Delay)
	}
	if step.Status != 0 {
		body := step.Body
		if body == "" {
			body = fmt.Sprintf(`{"error":{"message":"scripted status %d"}}`, step.Status)
		}
		http.Error(w, body, step.Status)
		return
	}

	finish := step.FinishReason
	if finish == "" {
		if len(step.ToolCalls) > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	completion := len(strings.Fields(step.Content)) + 1
	cacheN := cached
	resp := llm.ChatResponse{
		ID:    "fake-completion",
		Model: req.Model,
		Choices: []llm.Choice{{
			Index: 0,
			Message: llm.Message{
				Role:      llm.RoleAssistant,
				Content:   step.Content,
				ToolCalls: step.ToolCalls,
			},
			FinishReason: finish,
		}},
		Usage: llm.Usage{
			PromptTokens:     len(prompt),
			CompletionTokens: completion,
			TotalTokens:      len(prompt) + completion,
		},
		Timings: &llm.Timings{
			PromptN:    len(prompt) - cached,
			PredictedN: completion,
			CacheN:     &cacheN,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Tokenize renders a request the way a chat template would and splits it into
// pseudo-tokens. The exact scheme does not matter; what matters is that two
// requests with the same stable prefix tokenise to the same leading slice.
func Tokenize(req *llm.ChatRequest) []string {
	var b strings.Builder
	for _, t := range req.Tools {
		b.WriteString("\x00tool ")
		b.WriteString(t.Function.Name)
		b.WriteString(" ")
		b.WriteString(t.Function.Description)
		b.WriteString(" ")
		b.Write(t.Function.Parameters)
		b.WriteString("\n")
	}
	for _, m := range req.Messages {
		b.WriteString("\x00" + m.Role + " ")
		b.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			b.WriteString(" " + tc.Function.Name + " " + tc.Function.Arguments)
		}
		b.WriteString("\n")
	}
	return strings.Fields(b.String())
}

func commonPrefix(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

// ToolCall is a convenience constructor for scripted tool calls.
func ToolCall(id, name, argsJSON string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: argsJSON}}
}
