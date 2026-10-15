package telegram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Incoming is a message from an allowed user in a private chat.
type Incoming struct {
	ChatID   int64
	UserID   int64
	Username string
	Text     string
}

// Handler answers a message. The returned text is sent back as Markdown.
type Handler func(ctx context.Context, msg Incoming) (string, error)

// OffsetStore persists the update offset, so a restart neither replays nor
// loses messages.
type OffsetStore interface {
	GetInt64(ctx context.Context, key string, def int64) (int64, error)
	SetInt64(ctx context.Context, key string, value int64) error
}

type Options struct {
	Token   string
	APIBase string
	// AllowedUserIDs is the allowlist. Everyone else is ignored and logged.
	AllowedUserIDs []int64
	PollTimeout    time.Duration
	Offsets        OffsetStore
	OffsetKey      string
	Handler        Handler
	// Reset is called for the /neu command to drop the chat's session.
	Reset      func(ctx context.Context, chatID int64) error
	Logger     *slog.Logger
	HTTPClient *http.Client
}

type Bot struct {
	api     *api
	allowed map[int64]bool
	users   []int64
	timeout time.Duration
	offsets OffsetStore
	key     string
	handle  Handler
	reset   func(context.Context, int64) error
	log     *slog.Logger
}

func New(opts Options) *Bot {
	if opts.APIBase == "" {
		opts.APIBase = "https://api.telegram.org"
	}
	if opts.PollTimeout == 0 {
		opts.PollTimeout = 50 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.OffsetKey == "" {
		opts.OffsetKey = "telegram.offset"
	}
	if opts.HTTPClient == nil {
		// The long poll holds the request open for PollTimeout; the client
		// timeout must outlast it.
		opts.HTTPClient = &http.Client{Timeout: opts.PollTimeout + 15*time.Second}
	}
	b := &Bot{
		api:     &api{base: strings.TrimRight(opts.APIBase, "/") + "/bot" + opts.Token, http: opts.HTTPClient},
		allowed: map[int64]bool{},
		users:   opts.AllowedUserIDs,
		timeout: opts.PollTimeout,
		offsets: opts.Offsets,
		key:     opts.OffsetKey,
		handle:  opts.Handler,
		reset:   opts.Reset,
		log:     opts.Logger.With("component", "telegram"),
	}
	for _, id := range opts.AllowedUserIDs {
		b.allowed[id] = true
	}
	return b
}

// Run polls until ctx ends. An invalid token ends it with an error; network
// trouble is retried with backoff.
func (b *Bot) Run(ctx context.Context) error {
	offset, err := b.offsets.GetInt64(ctx, b.key, 0)
	if err != nil {
		return err
	}
	b.log.Info("telegram polling started", "allowed_users", len(b.allowed))
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		updates, err := b.api.getUpdates(ctx, offset, int(b.timeout/time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusNotFound) {
				return err
			}
			b.log.Warn("telegram polling failed", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second

		for _, u := range updates {
			// The offset moves past the update before it is handled: a crash
			// mid-turn must not replay "unlock the door" after the restart.
			offset = u.UpdateID + 1
			if err := b.offsets.SetInt64(ctx, b.key, offset); err != nil {
				b.log.Error("could not persist telegram offset", "error", err)
			}
			b.dispatch(ctx, u)
		}
	}
}

func (b *Bot) dispatch(ctx context.Context, u Update) {
	m := u.Message
	if m == nil || m.From == nil {
		return
	}
	if m.Chat.Type != "private" {
		b.log.Warn("ignoring message outside a private chat", "chat_id", m.Chat.ID, "chat_type", m.Chat.Type, "user_id", m.From.ID)
		return
	}
	if !b.allowed[m.From.ID] || m.From.IsBot {
		b.log.Warn("ignoring message from a user not on the allowlist", "user_id", m.From.ID, "username", m.From.Username)
		return
	}
	text := strings.TrimSpace(m.Text)
	switch {
	case text == "":
		b.reply(ctx, m.Chat.ID, "Ich verstehe bisher nur Textnachrichten.")
		return
	case text == "/start":
		b.reply(ctx, m.Chat.ID, "Hallo, ich bin Mira. Schreib mir einfach, was ich tun soll. Mit /neu beginnst du ein neues Gespräch.")
		return
	case text == "/neu" || text == "/reset":
		if b.reset != nil {
			if err := b.reset(ctx, m.Chat.ID); err != nil {
				b.log.Error("session reset failed", "chat_id", m.Chat.ID, "error", err)
			}
		}
		b.reply(ctx, m.Chat.ID, "Neues Gespräch.")
		return
	}

	typingCtx, stopTyping := context.WithCancel(ctx)
	go b.keepTyping(typingCtx, m.Chat.ID)
	answer, err := b.handle(ctx, Incoming{ChatID: m.Chat.ID, UserID: m.From.ID, Username: m.From.Username, Text: text})
	stopTyping()
	if err != nil {
		b.log.Error("turn failed", "chat_id", m.Chat.ID, "error", err)
		answer = "Da ist gerade etwas schiefgegangen. Bitte versuch es gleich noch einmal."
	}
	b.reply(ctx, m.Chat.ID, answer)
}

// keepTyping shows "typing…" until the answer is ready. Telegram clears the
// indicator after five seconds, so it is refreshed.
func (b *Bot) keepTyping(ctx context.Context, chatID int64) {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		_ = b.api.sendTyping(ctx, chatID)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if err := b.Send(ctx, chatID, text); err != nil {
		b.log.Error("could not send telegram reply", "chat_id", chatID, "error", err)
	}
}

// Send delivers a Markdown text, split as needed. A chunk Telegram refuses to
// parse is resent as plain text rather than dropped.
func (b *Bot) Send(ctx context.Context, chatID int64, text string) error {
	for _, chunk := range Split(text, MaxMessageLen) {
		err := b.api.sendMessage(ctx, chatID, ToHTML(chunk), "HTML")
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
			b.log.Warn("telegram rejected formatting, sending plain text", "error", apiErr.Description)
			err = b.api.sendMessage(ctx, chatID, chunk, "")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// SendToAll delivers a message to every allowed user, which in a private chat
// is the same id as the chat. It implements the handover tool and escalation.
func (b *Bot) SendToAll(ctx context.Context, text string) error {
	var errs []error
	for _, id := range b.users {
		if err := b.Send(ctx, id, text); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
