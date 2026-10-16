// Package language says which language Mira speaks when
// (docs/spec/sprache.md): in a conversation the one Home Assistant sends
// with the request, otherwise the one last spoken at that satellite's area,
// by that person, or at all; before anyone ever spoke, Home Assistant's
// server language. There is no setting for it.
//
// What was last spoken is kept in the store, apart from the sessions, so it
// outlives them.
package language

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/store"
)

const (
	keyLast   = "language.last"
	keyArea   = "language.area."
	keyPerson = "language.person."
)

type Options struct {
	Store *store.Store
	// Server returns Home Assistant's server language, for the very start.
	Server func(ctx context.Context) (string, error)
	Logger *slog.Logger
}

type Resolver struct {
	store  *store.Store
	server func(ctx context.Context) (string, error)
	log    *slog.Logger

	mu         sync.Mutex
	serverLang i18n.Lang
}

func New(opts Options) *Resolver {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Resolver{store: opts.Store, server: opts.Server, log: opts.Logger.With("component", "language")}
}

// Turn is the language of a conversation turn. A language Home Assistant
// sent (the Assist pipeline's) is used and remembered for the area, the
// person and overall; without one (Telegram) it is the person's last.
func (r *Resolver) Turn(ctx context.Context, sent, area, person string) i18n.Lang {
	if sent != "" {
		if l, err := i18n.Parse(sent); err == nil {
			r.remember(ctx, l, area, person)
			return l
		}
	}
	return r.ForPerson(ctx, person)
}

// ForArea is the language last spoken at the satellite of an area.
func (r *Resolver) ForArea(ctx context.Context, area string) i18n.Lang {
	if area != "" {
		if l, ok := r.get(ctx, keyArea+area); ok {
			return l
		}
	}
	return r.Last(ctx)
}

// ForPerson is the language a person last spoke, known only when Mira knew
// who spoke.
func (r *Resolver) ForPerson(ctx context.Context, person string) i18n.Lang {
	if person != "" {
		if l, ok := r.get(ctx, keyPerson+person); ok {
			return l
		}
	}
	return r.Last(ctx)
}

// Last is the language last spoken at all, for what has neither area nor
// person; before anyone spoke, Home Assistant's server language, and the
// default language while that cannot be read.
func (r *Resolver) Last(ctx context.Context) i18n.Lang {
	if l, ok := r.get(ctx, keyLast); ok {
		return l
	}
	return r.start(ctx)
}

// start is the language before anyone spoke.
func (r *Resolver) start(ctx context.Context) i18n.Lang {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serverLang != "" {
		return r.serverLang
	}
	code, err := r.server(ctx)
	if err != nil {
		r.log.Warn("could not read Home Assistant's language", "error", err)
		return i18n.Default
	}
	l, err := i18n.Parse(code)
	if err != nil {
		l = i18n.Default
	}
	r.serverLang = l
	return l
}

func (r *Resolver) get(ctx context.Context, key string) (i18n.Lang, bool) {
	v, err := r.store.GetKV(ctx, key)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			r.log.Warn("could not read the last language", "key", key, "error", err)
		}
		return "", false
	}
	l, err := i18n.Parse(v)
	return l, err == nil && v != ""
}

func (r *Resolver) remember(ctx context.Context, l i18n.Lang, area, person string) {
	keys := []string{keyLast}
	if area != "" {
		keys = append(keys, keyArea+area)
	}
	if person != "" {
		keys = append(keys, keyPerson+person)
	}
	for _, k := range keys {
		if cur, ok := r.get(ctx, k); ok && cur == l {
			continue
		}
		if err := r.store.SetKV(ctx, k, string(l)); err != nil {
			r.log.Warn("could not remember the language", "key", k, "error", err)
		}
	}
}
