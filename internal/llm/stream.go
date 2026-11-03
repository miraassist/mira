package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// streamChunk is one server-sent event of a streamed completion. llama.cpp
// splits a tool call's arguments over many chunks and numbers the calls with
// index; Gemini sends each call whole, without index, and the usage so far in
// every chunk (tried 2026-11-03).
type streamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index        *int            `json:"index"`
				ID           string          `json:"id"`
				Type         string          `json:"type"`
				Function     FunctionCall    `json:"function"`
				ExtraContent json.RawMessage `json:"extra_content"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage   *Usage    `json:"usage"`
	Timings *Timings  `json:"timings"`
	Error   *struct{} `json:"error"`
}

// readStream reads a streamed completion, passing each piece of text to
// onText, and returns it assembled as one response.
func readStream(body io.Reader, onText func(string)) (*ChatResponse, error) {
	out := &ChatResponse{Choices: []Choice{{Message: Message{Role: RoleAssistant}}}}
	choice := &out.Choices[0]
	var content strings.Builder
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	done := false
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			done = true
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return nil, fmt.Errorf("decode llm stream: %w (event: %s)", err, truncate(data, 300))
		}
		if ch.Error != nil {
			return nil, &Error{StatusCode: 500, Body: data}
		}
		if ch.ID != "" {
			out.ID = ch.ID
		}
		if ch.Model != "" {
			out.Model = ch.Model
		}
		if ch.Usage != nil {
			out.Usage = *ch.Usage
		}
		if ch.Timings != nil {
			out.Timings = ch.Timings
		}
		for _, c := range ch.Choices {
			if c.Delta.Content != "" {
				content.WriteString(c.Delta.Content)
				onText(c.Delta.Content)
			}
			for _, tc := range c.Delta.ToolCalls {
				calls := choice.Message.ToolCalls
				// The call's index says which one a piece belongs to; without
				// one, a new id starts the next call and a piece without an id
				// continues the last.
				n := len(calls) - 1
				switch {
				case tc.Index != nil:
					n = *tc.Index
				case tc.ID != "" && (n < 0 || calls[n].ID != tc.ID):
					n = len(calls)
				case n < 0:
					n = 0
				}
				for len(choice.Message.ToolCalls) <= n {
					choice.Message.ToolCalls = append(choice.Message.ToolCalls, ToolCall{})
				}
				call := &choice.Message.ToolCalls[n]
				if tc.ID != "" {
					call.ID = tc.ID
				}
				if tc.Type != "" {
					call.Type = tc.Type
				}
				call.Function.Name += tc.Function.Name
				call.Function.Arguments += tc.Function.Arguments
				if len(tc.ExtraContent) > 0 {
					call.ExtraContent = tc.ExtraContent
				}
			}
			if c.FinishReason != "" {
				choice.FinishReason = c.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read llm stream: %w", err)
	}
	if !done && choice.FinishReason == "" {
		return nil, fmt.Errorf("llm stream ended early")
	}
	choice.Message.Content = content.String()
	return out, nil
}
