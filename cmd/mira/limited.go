package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/clock"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/store"
)

// limitedSentence is a turn's answer while mira.yaml cannot be read.
var limitedSentence = map[i18n.Lang]string{
	i18n.DE: "Ich kann meine Einstellungen gerade nicht lesen. In Home Assistant unter Reparaturen steht, woran es liegt.",
	i18n.EN: "I can't read my settings right now. Repairs in Home Assistant say why.",
}

// configWatch is how often the limited start looks at mira.yaml again,
// as the hooks' folder is watched: a file has no signal of its own.
const configWatch = 5 * time.Second

// runLimited is Mira's start with a mira.yaml that cannot be read or does
// not fit (docs/spec/parsing.md, PA-07, PA-08): Home Assistant's integration
// reaches her, pairs and reads her status, which carries the problem for the
// repair; a turn gets the sentence that her settings cannot be read. Once the
// file reads, she starts anew. Only when not even her store opens does she
// end, with the file's error.
func runLimited(ctx context.Context, path string, cause error) error {
	log := slog.Default()
	file := config.ConfigPath(path)
	log.Error("mira.yaml cannot be read; Mira starts limited until it is corrected", "file", file, "error", cause)
	p, err := prepareFrom(ctx, path, config.Limited(path), false, false)
	if err != nil {
		log.Error("the limited start failed too; Mira ends", "error", err)
		return cause
	}
	st, err := store.Open(ctx, store.Options{Path: p.cfg.Store.Path})
	if err != nil {
		log.Error("the store cannot be opened for the limited start; Mira ends", "error", err)
		return cause
	}
	defer st.Close()
	id, err := st.InstanceID(ctx)
	if err != nil {
		return cause
	}
	mux := limitedMux(st, p, id, file, cause)
	ln, err := net.Listen("tcp", p.cfg.Server.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: serverRead, WriteTimeout: serverWrite, IdleTimeout: serverIdle}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	go watchConfig(ctx, clock.Real(), file, log, restart)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// limitedMux serves the pairing, the status with the problem and the turn's
// sentence.
func limitedMux(st *store.Store, p prepared, id, file string, cause error) http.Handler {
	mux := api.NewMux()
	(&setup.Handler{Store: st, Token: p.cfg.Server.Token, ID: id, Version: version, Pairing: p.pairing,
		SetupMode: true, Model: p.model(), Restart: restart, Log: slog.Default()}).Register(mux)
	problem := haconv.Problem{Source: "config", Name: "mira.yaml", Text: file + ": " + cause.Error()}
	mux.Handle(api.Route{Method: http.MethodGet, Path: "/v1/status", Tag: api.TagIntegration,
		Summary: "In the limited start: the problem of mira.yaml"}, func(w http.ResponseWriter, r *http.Request) {
		if !limitedAuthorized(r, p.cfg.Server.Token) {
			http.Error(w, "missing or wrong bearer token", http.StatusUnauthorized)
			return
		}
		writeLimitedJSON(w, haconv.Status{Version: version, InboxApps: []string{}, MailApps: []string{},
			Problems: []haconv.Problem{problem}})
	})
	mux.Handle(api.Route{Method: http.MethodPost, Path: "/v1/conversation", Tag: api.TagIntegration,
		Summary: "In the limited start: the sentence that Mira's settings cannot be read"}, func(w http.ResponseWriter, r *http.Request) {
		if !limitedAuthorized(r, p.cfg.Server.Token) {
			http.Error(w, "missing or wrong bearer token", http.StatusUnauthorized)
			return
		}
		var req struct {
			ConversationID string `json:"conversation_id"`
			Language       string `json:"language"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			slog.Warn("a turn in the limited start does not read; answered anyway", "error", err)
		}
		lang := i18n.DE
		if strings.HasPrefix(strings.ToLower(req.Language), "en") {
			lang = i18n.EN
		}
		writeLimitedJSON(w, map[string]any{"text": i18n.Pick(limitedSentence, lang),
			"conversation_id": req.ConversationID, "continue_conversation": false})
	})
	return mux
}

func limitedAuthorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func writeLimitedJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("could not write an answer of the limited start", "error", err)
	}
}

// watchConfig looks at mira.yaml every configWatch on the clock; once it
// changed and reads, start anew (PA-08). A change that still does not read
// is logged once.
func watchConfig(ctx context.Context, clk clock.Clock, file string, log *slog.Logger, startAnew func()) {
	stamp := func() string {
		info, err := os.Stat(file)
		if err != nil {
			return ""
		}
		return info.ModTime().String() + "|" + info.Name()
	}
	seen := stamp()
	tick := clk.NewTicker(configWatch)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C():
			now := stamp()
			if now == seen {
				continue
			}
			seen = now
			if _, _, err := config.LoadOptional(file); err != nil {
				log.Error("mira.yaml changed but still cannot be read; Mira stays limited", "file", file, "error", err)
				continue
			}
			log.Info("mira.yaml reads again; Mira starts anew", "file", file)
			startAnew()
			return
		}
	}
}
