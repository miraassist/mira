package conversation

import (
	"context"
	"strings"
	"sync"

	"github.com/miraassist/mira/internal/llm"
)

// Piece is a part of a voice answer, handed on while the answer is written
// (docs/spec/streaming.md): a piece of its text, or one of its tool calls.
type Piece struct {
	Text string
	Tool *ToolPiece
}

// ToolPiece is a tool call of the turn, for Home Assistant's chat log: when
// it starts (Done false) and when it ended, with its result.
type ToolPiece struct {
	ID, Name, Arguments string
	Done                bool
	Result              string
}

// toolResultChars is how much of a tool's result goes into Home Assistant's
// chat log, where it only shows in the debug view.
const toolResultChars = 300

// streamer is a model client that can stream; the engine streams a voice
// answer only through one.
type streamer interface {
	ChatStream(ctx context.Context, req llm.ChatRequest, onText func(string)) (*llm.ChatResponse, error)
}

// passer hands a voice answer on while it is written: the visible text of
// every round, thinking cut out, one space between the rounds, and the tool
// calls, which make Home Assistant speak a short text before them at once.
type passer struct {
	mu    sync.Mutex
	sink  func(Piece)
	think thinkFilter
	said  strings.Builder
	// fresh is set at the start of a round, until its first visible text.
	fresh bool
}

func newPasser(sink func(Piece)) *passer {
	if sink == nil {
		return nil
	}
	return &passer{sink: sink}
}

// tool hands on a tool call before it runs, and toolDone after it ran.
func (p *passer) tool(call llm.ToolCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sink(Piece{Tool: &ToolPiece{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}})
}

func (p *passer) toolDone(call llm.ToolCall, result string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r := []rune(result); len(r) > toolResultChars {
		result = string(r[:toolResultChars]) + "…"
	}
	p.sink(Piece{Tool: &ToolPiece{ID: call.ID, Name: call.Function.Name, Done: true, Result: result}})
}

// round starts the next model round.
func (p *passer) round() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.think = thinkFilter{}
	p.fresh = true
}

// text passes on what the model wrote in this round.
func (p *passer) text(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emit(p.think.feed(s))
}

// endRound passes on what the round held back at its end.
func (p *passer) endRound() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emit(p.think.flush())
}

// add passes on a text of Mira's own, as a sentence after what was said.
func (p *passer) add(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fresh = true
	p.emit(s)
}

// emit passes on visible text; the caller holds mu.
func (p *passer) emit(s string) {
	if p.fresh {
		s = strings.TrimLeft(s, " \t\n")
		if s == "" {
			return
		}
		if p.said.Len() > 0 && !strings.HasSuffix(p.said.String(), " ") {
			s = " " + s
		}
		p.fresh = false
	}
	if s == "" {
		return
	}
	p.said.WriteString(s)
	p.sink(Piece{Text: s})
}

// passed reports whether anything was handed on.
func (p *passer) passed() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.said.Len() > 0
}

// spoken is everything handed on.
func (p *passer) spoken() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.said.String()
}

// onText is the callback for the model client, nil without a passer.
func (p *passer) onText() func(string) {
	if p == nil {
		return nil
	}
	return p.text
}

// thinkFilter cuts <think>…</think> out of a text that arrives in pieces; a
// tag may be split between two pieces.
type thinkFilter struct {
	in   bool
	hold string
}

func (f *thinkFilter) feed(s string) string {
	s = f.hold + s
	f.hold = ""
	var out strings.Builder
	for s != "" {
		tag := "<think>"
		if f.in {
			tag = "</think>"
		}
		if i := strings.Index(s, tag); i >= 0 {
			if !f.in {
				out.WriteString(s[:i])
			}
			s = s[i+len(tag):]
			f.in = !f.in
			continue
		}
		keep := partialTag(s, tag)
		if !f.in {
			out.WriteString(s[:len(s)-keep])
		}
		f.hold = s[len(s)-keep:]
		break
	}
	return out.String()
}

// flush is what was held back for a tag that did not come.
func (f *thinkFilter) flush() string {
	if f.in {
		return ""
	}
	s := f.hold
	f.hold = ""
	return s
}

// partialTag is how many bytes at the end of s could start tag.
func partialTag(s, tag string) int {
	for k := min(len(tag)-1, len(s)); k > 0; k-- {
		if strings.HasSuffix(s, tag[:k]) {
			return k
		}
	}
	return 0
}
