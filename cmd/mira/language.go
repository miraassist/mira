package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/language"
	"github.com/miraassist/mira/internal/store"
)

// newLanguages builds the resolver of docs/spec/sprache.md. Before anyone
// spoke it takes Home Assistant's server language, read over REST so it is
// there before the WebSocket connects.
func newLanguages(cfg config.Config, st *store.Store, log *slog.Logger) *language.Resolver {
	return language.New(language.Options{
		Store: st, Logger: log,
		Server: func(ctx context.Context) (string, error) { return haServerLanguage(ctx, cfg.HA) },
	})
}

// haServerLanguage reads Home Assistant's own language (Settings → System →
// General); any user may.
func haServerLanguage(ctx context.Context, ha config.HA) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(ha.URL, "/")+"/api/config", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+ha.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /api/config: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Language string `json:"language"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Language, nil
}
