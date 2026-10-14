// Package telegram is the Telegram channel: long polling, an allowlist of
// user ids, private chats only, and formatting of the model's Markdown for
// Telegram. There is no webhook and therefore no inbound port.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Update is the subset of Telegram's Update object the bot reads.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// APIError is a non-OK answer from the Bot API.
type APIError struct {
	Method      string
	StatusCode  int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.StatusCode, e.Description)
}

type api struct {
	base string // https://api.telegram.org/bot<token>
	http *http.Client
}

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

func (a *api) call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		// The URL contains the bot token; never let it reach a log line.
		return fmt.Errorf("telegram %s: %s", method, redact(err.Error(), a.base))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: read body: %w", method, err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{Method: method, StatusCode: resp.StatusCode, Description: "unparseable response"}
	}
	if !env.OK {
		return &APIError{Method: method, StatusCode: resp.StatusCode, Description: env.Description}
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func redact(s, base string) string {
	if i := strings.LastIndex(base, "/bot"); i >= 0 {
		return strings.ReplaceAll(s, base[i+4:], "<token>")
	}
	return s
}

func (a *api) getUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var out []Update
	err := a.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": []string{"message"},
	}, &out)
	return out, err
}

func (a *api) sendMessage(ctx context.Context, chatID int64, text, parseMode string) error {
	params := map[string]any{"chat_id": chatID, "text": text, "link_preview_options": map[string]any{"is_disabled": true}}
	if parseMode != "" {
		params["parse_mode"] = parseMode
	}
	return a.call(ctx, "sendMessage", params, nil)
}

func (a *api) sendTyping(ctx context.Context, chatID int64) error {
	return a.call(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": "typing"}, nil)
}
