package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

// Names of the internal tools. They carry no server prefix.
const (
	ToolMemorySave     = "memory_save"
	ToolMemorySearch   = "memory_search"
	ToolMemoryForget   = "memory_forget"
	ToolNoticesList    = "notices_list"
	ToolNoticeAck      = "notice_ack"
	ToolSendToTelegram = "send_to_telegram"
	ToolDelegate       = "delegate"
)

// Delegator hands a task to a stronger model and returns its answer.
type Delegator func(ctx context.Context, task string) (string, error)

// TelegramSender delivers a handover message to the household's phones.
type TelegramSender interface {
	SendToAll(ctx context.Context, text string) error
}

// Internal returns the agent's own tools. The set is the same for every
// channel, so prompt layer 3 is too. A nil sender makes send_to_telegram
// report that Telegram is not configured instead of disappearing.
func Internal(l i18n.Lang, st *store.Store, telegram TelegramSender, delegate Delegator) []InternalTool {
	t := textsFor(l)
	decode := func(raw json.RawMessage, v any) error { return decodeArgs(t, raw, v) }
	list := []InternalTool{
		{
			Name:        ToolMemorySave,
			Description: t.memorySaveDesc,
			Schema:      schema(`{"type":"object","properties":{"text":{"type":"string","description":%q},"tags":{"type":"array","items":{"type":"string"},"description":%q}},"required":["text"]}`, t.memorySaveText, t.memorySaveTags),
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					Text string   `json:"text"`
					Tags []string `json:"tags"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Text) == "" {
					return "", errors.New(t.textMissing)
				}
				f, err := st.SaveFact(ctx, store.Fact{ID: ulid.Make().String(), Text: strings.TrimSpace(a.Text),
					Tags: a.Tags, Source: string(inv.Channel)})
				if err != nil {
					return "", err
				}
				return fmt.Sprintf(t.memorySaved, f.ID), nil
			},
		},
		{
			Name:        ToolMemorySearch,
			Description: t.memorySearchDesc,
			Schema:      schema(`{"type":"object","properties":{"query":{"type":"string","description":%q}},"required":["query"]}`, t.memorySearchQuery),
			ReadOnly:    true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					Query string `json:"query"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				facts, err := st.SearchFacts(ctx, a.Query, 10)
				if err != nil {
					return "", err
				}
				if len(facts) == 0 {
					return t.memoryNothing, nil
				}
				var b strings.Builder
				for _, f := range facts {
					fmt.Fprintf(&b, "- [%s] %s\n", f.ID, f.Text)
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        ToolMemoryForget,
			Description: t.memoryForgetDesc,
			Schema:      `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if err := st.DeleteFact(ctx, a.ID); errors.Is(err, store.ErrNotFound) {
					return "", errors.New(t.noSuchFact)
				} else if err != nil {
					return "", err
				}
				return t.memoryForgotten, nil
			},
		},
		{
			Name:        ToolNoticesList,
			Description: t.noticesListDesc,
			Schema:      `{"type":"object","properties":{}}`,
			ReadOnly:    true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				notices, err := st.ListNotices(ctx, store.NoticeFilter{
					States: []string{store.NoticePending, store.NoticeDelivered}, Limit: 20})
				if err != nil {
					return "", err
				}
				if len(notices) == 0 {
					return t.noNotices, nil
				}
				var b strings.Builder
				for _, n := range notices {
					fmt.Fprintf(&b, t.noticeLine, n.ID, n.Importance, n.State, n.Spoken)
					if n.Details != "" {
						fmt.Fprintf(&b, t.noticeDetails, n.Details)
					}
					b.WriteByte('\n')
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        ToolNoticeAck,
			Description: t.noticeAckDesc,
			Schema:      `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if err := st.SetNoticeState(ctx, a.ID, store.NoticeAcked); errors.Is(err, store.ErrNotFound) {
					return "", errors.New(t.noSuchNotice)
				} else if err != nil {
					return "", err
				}
				return t.noticeAcked, nil
			},
		},
		{
			Name:        ToolSendToTelegram,
			Description: t.telegramDesc,
			Schema:      schema(`{"type":"object","properties":{"text":{"type":"string","description":%q}},"required":["text"]}`, t.telegramText),
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				if telegram == nil {
					return "", errors.New(t.noTelegram)
				}
				var a struct {
					Text string `json:"text"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Text) == "" {
					return "", errors.New(t.textMissing)
				}
				if err := telegram.SendToAll(ctx, a.Text); err != nil {
					return "", err
				}
				return t.telegramSent, nil
			},
		},
	}
	if delegate != nil {
		list = append(list, InternalTool{
			Name:        ToolDelegate,
			Description: t.delegateDesc,
			Schema:      schema(`{"type":"object","properties":{"task":{"type":"string","description":%q}},"required":["task"]}`, t.delegateTask),
			// It changes nothing: the delegated model only reads.
			ReadOnly: true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					Task string `json:"task"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				return delegate(ctx, a.Task)
			},
		})
	}
	return list
}

func decodeArgs(t texts, raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf(t.invalidArgs, err)
	}
	return nil
}

// schema fills JSON-quoted descriptions into a schema template.
func schema(format string, descriptions ...any) string {
	return fmt.Sprintf(format, descriptions...)
}
