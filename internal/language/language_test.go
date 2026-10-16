package language

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/store"
)

func newResolver(t *testing.T, server func(context.Context) (string, error)) *Resolver {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "mira.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Options{Store: st, Server: server, Fallback: i18n.DE})
}

func TestLastSpokenPerAreaPersonAndOverall(t *testing.T) {
	ctx := context.Background()
	r := newResolver(t, func(context.Context) (string, error) { return "de-DE", nil })
	if got := r.Last(ctx); got != i18n.DE {
		t.Fatalf("before anyone spoke: %s, want the server language", got)
	}
	if got := r.Turn(ctx, "en", "kinderzimmer", "ben"); got != i18n.EN {
		t.Fatalf("turn = %s", got)
	}
	r.Turn(ctx, "de", "wohnzimmer", "")
	for name, got := range map[string]i18n.Lang{
		"kinderzimmer": r.ForArea(ctx, "kinderzimmer"),
		"wohnzimmer":   r.ForArea(ctx, "wohnzimmer"),
		"ben":          r.ForPerson(ctx, "ben"),
		"anna":         r.ForPerson(ctx, "anna"),
		"last":         r.Last(ctx),
		"unknown area": r.ForArea(ctx, "keller"),
	} {
		want := map[string]i18n.Lang{"kinderzimmer": i18n.EN, "wohnzimmer": i18n.DE, "ben": i18n.EN,
			"anna": i18n.DE, "last": i18n.DE, "unknown area": i18n.DE}[name]
		if got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	// Telegram sends no language: the person's last applies.
	if got := r.Turn(ctx, "", "", "ben"); got != i18n.EN {
		t.Errorf("telegram turn for ben = %s", got)
	}
}

func TestStartWithoutServerLanguage(t *testing.T) {
	r := newResolver(t, func(context.Context) (string, error) { return "", errors.New("down") })
	if got := r.Last(context.Background()); got != i18n.DE {
		t.Errorf("Home Assistant unreachable: %s, want the fallback", got)
	}
	r = newResolver(t, func(context.Context) (string, error) { return "en-GB", nil })
	if got := r.Last(context.Background()); got != i18n.EN {
		t.Errorf("server language en-GB: %s", got)
	}
}
