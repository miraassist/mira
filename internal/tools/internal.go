package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"

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
)

// TelegramSender delivers a handover message to the household's phones.
type TelegramSender interface {
	SendToAll(ctx context.Context, text string) error
}

// Internal returns the agent's own tools. The set is the same for every
// channel, so prompt layer 3 is too. A nil sender makes send_to_telegram
// report that Telegram is not configured instead of disappearing.
func Internal(st *store.Store, telegram TelegramSender) []InternalTool {
	return []InternalTool{
		{
			Name:        ToolMemorySave,
			Description: "Merkt sich dauerhaft eine Tatsache über den Haushalt oder den Nutzer, z.B. Vorlieben, Gewohnheiten, wo etwas steht. Nur speichern, wenn der Nutzer es möchte oder es eindeutig dauerhaft nützlich ist.",
			Schema:      `{"type":"object","properties":{"text":{"type":"string","description":"Die Tatsache als vollständiger Satz."},"tags":{"type":"array","items":{"type":"string"},"description":"Optionale Stichwörter."}},"required":["text"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					Text string   `json:"text"`
					Tags []string `json:"tags"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Text) == "" {
					return "", errors.New("text fehlt")
				}
				f, err := st.SaveFact(ctx, store.Fact{ID: ulid.Make().String(), Text: strings.TrimSpace(a.Text),
					Tags: a.Tags, Source: string(inv.Channel)})
				if err != nil {
					return "", err
				}
				return "Gespeichert (id " + f.ID + ").", nil
			},
		},
		{
			Name:        ToolMemorySearch,
			Description: "Sucht in den gespeicherten Tatsachen über den Haushalt und den Nutzer.",
			Schema:      `{"type":"object","properties":{"query":{"type":"string","description":"Suchbegriffe."}},"required":["query"]}`,
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
					// Said this bluntly on purpose: with a plain "nothing
					// found", local models fill the gap with a plausible guess.
					return "Nichts gefunden. Dazu ist nichts gespeichert, du weißt es also nicht. " +
						"Sag dem Nutzer genau das und rate nicht.", nil
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
			Description: "Löscht eine gespeicherte Tatsache anhand ihrer id (aus memory_search).",
			Schema:      `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if err := st.DeleteFact(ctx, a.ID); errors.Is(err, store.ErrNotFound) {
					return "", errors.New("keine Tatsache mit dieser id")
				} else if err != nil {
					return "", err
				}
				return "Gelöscht.", nil
			},
		},
		{
			Name:        ToolNoticesList,
			Description: "Listet offene Hinweise und Meldungen (z.B. Gerätestörungen, Nachrichten), die noch nicht erledigt sind.",
			Schema:      `{"type":"object","properties":{}}`,
			ReadOnly:    true,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				notices, err := st.ListNotices(ctx, store.NoticeFilter{
					States: []string{store.NoticePending, store.NoticeDelivered}, Limit: 20})
				if err != nil {
					return "", err
				}
				if len(notices) == 0 {
					return "Keine offenen Hinweise.", nil
				}
				var b strings.Builder
				for _, n := range notices {
					fmt.Fprintf(&b, "- [%s] Wichtigkeit %d, %s: %s", n.ID, n.Importance, n.State, n.Spoken)
					if n.Details != "" {
						fmt.Fprintf(&b, " Details: %s", n.Details)
					}
					b.WriteByte('\n')
				}
				return strings.TrimRight(b.String(), "\n"), nil
			},
		},
		{
			Name:        ToolNoticeAck,
			Description: "Markiert einen Hinweis als erledigt, wenn der Nutzer ihn zur Kenntnis genommen hat.",
			Schema:      `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				var a struct {
					ID string `json:"id"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if err := st.SetNoticeState(ctx, a.ID, store.NoticeAcked); errors.Is(err, store.ErrNotFound) {
					return "", errors.New("kein Hinweis mit dieser id")
				} else if err != nil {
					return "", err
				}
				return "Erledigt.", nil
			},
		},
		{
			Name:        ToolSendToTelegram,
			Description: "Schickt dem Nutzer eine Nachricht aufs Handy (Telegram), z.B. eine Liste, einen Link oder eine längere Antwort, die sich nicht gut vorlesen lässt.",
			Schema:      `{"type":"object","properties":{"text":{"type":"string","description":"Die Nachricht, Markdown erlaubt."}},"required":["text"]}`,
			Run: func(ctx context.Context, inv Invocation) (string, error) {
				if telegram == nil {
					return "", errors.New("Telegram ist nicht eingerichtet")
				}
				var a struct {
					Text string `json:"text"`
				}
				if err := decode(inv.Args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.Text) == "" {
					return "", errors.New("text fehlt")
				}
				if err := telegram.SendToAll(ctx, a.Text); err != nil {
					return "", err
				}
				return "Gesendet.", nil
			},
		},
	}
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("ungültige Argumente: %w", err)
	}
	return nil
}
