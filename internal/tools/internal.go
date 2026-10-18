package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

// Names of the internal tools. They carry no server prefix.
const (
	ToolMemorySave   = "memory_save"
	ToolMemorySearch = "memory_search"
	ToolMemoryForget = "memory_forget"
	ToolMessagesList = "messages_list"
	ToolMessageAck   = "message_ack"
	ToolDelegate     = "delegate"
)

// Delegator hands a task to a stronger model and returns its answer.
type Delegator func(ctx context.Context, task string) (string, error)

// Internal returns the agent's own tools. The set is the same for every
// channel, so prompt layer 3 is too.
func Internal(l i18n.Lang, st *store.Store, delegate Delegator) []InternalTool {
	t := textsFor(l)
	decode := func(raw json.RawMessage, v any) error { return decodeArgs(t, raw, v) }
	list := []InternalTool{
		{
			Name:        ToolMemorySave,
			AskAfterWeb: true,
			Description: t.memorySaveDesc,
			Schema:      schema(`{"type":"object","properties":{"text":{"type":"string","description":%q},"tags":{"type":"array","items":{"type":"string"},"description":%q},"house":{"type":"boolean","description":%q}},"required":["text"]}`, t.memorySaveText, t.memorySaveTags, t.memorySaveHouse),
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					Text  string   `json:"text"`
					Tags  []string `json:"tags"`
					House bool     `json:"house"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Text) == "" {
					return "", errors.New(t.textMissing)
				}
				// Nobody writes into someone else's memory: the owner is the
				// speaker, or the house.
				owner := store.OwnerHouse
				if inv.Person != "" && !a.House {
					owner = inv.Person
				}
				f, err := st.SaveFact(ctx, store.Fact{ID: ulid.Make().String(), Text: strings.TrimSpace(a.Text),
					Tags: a.Tags, Owner: owner, Author: inv.Author(), Source: string(inv.Channel)})
				if err != nil {
					return "", err
				}
				if owner != store.OwnerHouse {
					return fmt.Sprintf(t.memorySavedPerson, owner, f.ID), nil
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
				facts, err := st.SearchFacts(ctx, a.Query, store.Owners(inv.Person), 10)
				if err != nil {
					return "", err
				}
				if len(facts) == 0 {
					return t.memoryNothing, nil
				}
				var b strings.Builder
				for _, f := range facts {
					mark := ""
					if f.Owner != store.OwnerHouse {
						mark = t.personalMark
					}
					fmt.Fprintf(&b, "- [%s] %s%s\n", f.ID, f.Text, mark)
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
				// Only facts this speaker may read can be forgotten.
				if f, err := st.GetFact(ctx, a.ID); err == nil && !slices.Contains(store.Owners(inv.Person), f.Owner) {
					return "", errors.New(t.noSuchFact)
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
			Name:        ToolMessagesList,
			Description: t.messagesListDesc,
			Schema:      `{"type":"object","properties":{}}`,
			ReadOnly:    true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				messages, err := st.ListMessages(ctx, store.MessageFilter{
					States: []string{store.MessagePending, store.MessageDelivered}, Limit: 20})
				if err != nil {
					return "", err
				}
				if len(messages) == 0 {
					return t.noMessages, nil
				}
				var b strings.Builder
				for _, n := range messages {
					fmt.Fprintf(&b, t.messageLine, n.ID, n.Importance, n.State, n.Spoken)
					if n.Details != "" {
						fmt.Fprintf(&b, t.messageDetails, n.Details)
					}
					b.WriteByte('\n')
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        ToolMessageAck,
			Description: t.messageAckDesc,
			Schema:      `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if err := st.SetMessageState(ctx, a.ID, store.MessageAcked); errors.Is(err, store.ErrNotFound) {
					return "", errors.New(t.noSuchMessage)
				} else if err != nil {
					return "", err
				}
				return t.messageAcked, nil
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
