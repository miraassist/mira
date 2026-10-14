package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sker65/lares/internal/channels/haconv"
	"github.com/sker65/lares/internal/channels/telegram"
	"github.com/sker65/lares/internal/config"
	"github.com/sker65/lares/internal/conversation"
	"github.com/sker65/lares/internal/ha/structure"
	haws "github.com/sker65/lares/internal/ha/ws"
	"github.com/sker65/lares/internal/llm"
	"github.com/sker65/lares/internal/mcp"
	"github.com/sker65/lares/internal/store"
	"github.com/sker65/lares/internal/telemetry"
	"github.com/sker65/lares/internal/tools"
)

// app is the wired agent. Fields are nil for disabled parts.
type app struct {
	cfg       config.Config
	log       *slog.Logger
	tel       *telemetry.Provider
	store     *store.Store
	llm       *llm.Client
	ha        *haws.Client
	structure *structure.Cache
	mcp       *mcp.Host
	telegram  *telegram.Bot
	engine    *conversation.Engine

	// background holds goroutines started by build, stopped through ctx.
	background sync.WaitGroup
	warmup     func(reason string)
}

type buildOptions struct {
	// withTelegram builds the bot. The chat command leaves it out so a test
	// turn never competes with the running agent for updates.
	withTelegram bool
}

// build wires everything that both run and chat need, and connects to Home
// Assistant and the MCP servers. Long-running loops it starts end with ctx.
func build(ctx context.Context, cfg config.Config, opts buildOptions) (*app, error) {
	tel, err := telemetry.Setup(ctx, telemetry.Options{
		ServiceName: cfg.Telemetry.ServiceName, Version: version,
		OTLPEndpoint: otlpEndpoint(cfg.Telemetry.OTLPEndpoint), LogLevel: cfg.Telemetry.LogLevel,
		CaptureContent: cfg.Telemetry.CaptureContent,
	})
	if err != nil {
		return nil, err
	}
	slog.SetDefault(tel.Logger)
	a := &app{cfg: cfg, log: tel.Logger, tel: tel, warmup: func(string) {}}

	a.store, err = store.Open(ctx, store.Options{Path: cfg.Store.Path})
	if err != nil {
		return nil, err
	}

	a.llm = llm.New(llm.Options{
		BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model,
		Timeout: cfg.LLM.Timeout.Duration(), MaxRetries: 2, Telemetry: tel,
	})

	if cfg.HA.Enabled {
		if err := a.startHA(ctx); err != nil {
			return nil, err
		}
	}

	a.mcp = mcp.New(mcp.Options{
		Servers: cfg.MCPServers, Logger: a.log, Version: version,
		ReadOnlyTools: cfg.Security.ReadOnlyTools,
		OnChange: func(tools []mcp.Tool) {
			a.log.Info("tool list changed", "tools", len(tools))
			a.warmup("tools_changed")
		},
	})
	if err := a.mcp.Start(ctx); err != nil {
		// Not fatal: the agent still talks, and Run keeps retrying.
		a.log.Error("no mcp server reachable; the agent has no house tools until one comes up", "error", err)
	}
	a.goRun(ctx, "mcp", a.mcp.Run)

	if opts.withTelegram && cfg.Telegram.Enabled {
		a.telegram = telegram.New(telegram.Options{
			Token: cfg.Telegram.Token, APIBase: cfg.Telegram.APIBaseURL,
			AllowedUserIDs: cfg.Telegram.AllowedUserIDs, PollTimeout: cfg.Telegram.PollTimeout.Duration(),
			Offsets: a.store, OffsetKey: store.KeyTelegramOffset, Logger: a.log,
			Handler: func(ctx context.Context, m telegram.Incoming) (string, error) {
				resp, err := a.engine.Handle(ctx, conversation.Request{
					Channel: config.ChannelTelegram, ConversationID: strconv.FormatInt(m.ChatID, 10),
					Text: m.Text, Language: cfg.Prompt.Language,
				})
				return resp.Text, err
			},
			Reset: func(ctx context.Context, chatID int64) error {
				return a.engine.ResetSession(ctx, config.ChannelTelegram, strconv.FormatInt(chatID, 10))
			},
		})
	}

	// The handover tool needs a sender even in the chat command, so it is
	// built from the config alone; it only sends, it does not poll.
	var sender tools.TelegramSender
	if a.telegram != nil {
		sender = a.telegram
	} else if cfg.Telegram.Enabled {
		sender = telegram.New(telegram.Options{Token: cfg.Telegram.Token, APIBase: cfg.Telegram.APIBaseURL,
			AllowedUserIDs: cfg.Telegram.AllowedUserIDs, Logger: a.log})
	}

	snapshot := func() structure.Snapshot { return structure.Snapshot{} }
	if a.structure != nil {
		snapshot = a.structure.Snapshot
	}
	registry := tools.NewRegistry(a.mcp, tools.Internal(a.store, sender)...)
	a.engine = conversation.New(conversation.Options{
		Config: cfg, LLM: a.llm, Store: a.store, Tools: registry,
		Guard:     tools.NewGuard(cfg.Security, a.store, snapshot),
		Structure: snapshot, Telemetry: tel, Logger: a.log,
	})
	if cfg.LLM.WarmupOnPrefixChange {
		a.warmup = a.engine.RequestWarmup
	}
	return a, nil
}

// startHA connects the WebSocket client and builds the structure block once
// before the first turn. HA being down is not fatal: the agent starts with an
// empty layer 4 and fills it when the connection comes up.
func (a *app) startHA(ctx context.Context) error {
	var err error
	a.ha, err = haws.New(haws.Options{
		URL: a.cfg.HA.URL, Token: a.cfg.HA.Token, Logger: a.log,
		// Registry changes may have happened while the connection was down.
		OnConnect: func(context.Context) { a.structure.Invalidate() },
	})
	if err != nil {
		return err
	}
	a.structure = structure.NewCache(structure.Options{
		Source: a.ha, Subscriber: a.ha,
		Debounce: a.cfg.HA.StructureDebounce.Duration(), Logger: a.log,
		OnChange: func(s structure.Snapshot) { a.warmup("structure_changed") },
	})

	authFailed := make(chan error, 1)
	a.background.Add(1)
	go func() {
		defer a.background.Done()
		if err := a.ha.Run(ctx); err != nil && ctx.Err() == nil {
			a.log.Error("home assistant connection stopped", "error", err)
			authFailed <- err
		}
	}()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ready := make(chan error, 1)
	go func() { ready <- a.ha.WaitReady(waitCtx) }()
	select {
	case err := <-authFailed:
		return fmt.Errorf("home assistant: %w", err)
	case <-ready:
	}
	if a.ha.Connected() {
		if _, err := a.structure.Rebuild(ctx); err != nil {
			a.log.Error("initial structure build failed", "error", err)
		}
	} else {
		a.log.Warn("home assistant not reachable yet; starting without the structure block")
	}
	a.goRun(ctx, "ha-structure", a.structure.Run)
	return nil
}

func (a *app) goRun(ctx context.Context, name string, fn func(context.Context) error) {
	a.background.Add(1)
	go func() {
		defer a.background.Done()
		if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("background task stopped", "task", name, "error", err)
		}
	}()
}

// close waits for the background loops (ctx must be cancelled first) and
// flushes telemetry.
func (a *app) close() {
	if a.mcp != nil {
		a.mcp.Close()
	}
	a.background.Wait()
	if a.store != nil {
		a.store.Close()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.tel.Shutdown(shutdownCtx)
}

func runAgent(parent context.Context, cfg config.Config) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := build(ctx, cfg, buildOptions{withTelegram: true})
	if err != nil {
		return err
	}
	defer a.close()
	defer cancel() // runs before close: stop the loops, then wait for them

	if err := a.llm.Health(ctx); err != nil {
		a.log.Warn("llm server is not answering yet", "error", err)
	}
	a.goRun(ctx, "warmup", a.engine.RunWarmups)
	if cfg.LLM.WarmupOnStart {
		a.engine.RequestWarmup("startup")
	}
	a.goRun(ctx, "session-purge", func(ctx context.Context) error {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				if n, err := a.store.PurgeExpiredSessions(ctx); err != nil {
					a.log.Warn("session purge failed", "error", err)
				} else if n > 0 {
					a.log.Info("expired sessions purged", "count", n)
				}
			}
		}
	})

	errs := make(chan error, 2)
	if a.telegram != nil {
		go func() { errs <- fmt.Errorf("telegram: %w", a.telegram.Run(ctx)) }()
	}

	httpServer := &http.Server{
		Addr: cfg.Server.Listen,
		Handler: haconv.New(haconv.Options{
			Token: cfg.Server.Token, Conversation: a.engine, Store: a.store, Version: version,
			Logger: a.log, TurnTimeout: cfg.LLM.Timeout.Duration(), Checks: a.healthChecks(),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if cfg.Server.Token == "" {
		a.log.Warn("server.token is empty: the HTTP API accepts unauthenticated requests")
	}
	go func() {
		a.log.Info("http api listening", "addr", cfg.Server.Listen)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- fmt.Errorf("http: %w", err)
		}
	}()

	a.log.Info("lares is up", "version", version)
	var runErr error
	select {
	case <-ctx.Done():
		a.log.Info("shutting down")
	case runErr = <-errs:
		if errors.Is(runErr, context.Canceled) {
			runErr = nil
		}
	}
	cancel()
	// A running turn gets a few seconds to finish; after that, and for
	// clients that hold a connection without sending anything, close hard.
	shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
	}
	return runErr
}

func (a *app) healthChecks() map[string]haconv.HealthCheck {
	checks := map[string]haconv.HealthCheck{
		"llm": a.llm.Health,
		"mcp": func(context.Context) error {
			var down []string
			for _, s := range a.mcp.Status() {
				if !s.Connected {
					down = append(down, s.Name)
				}
			}
			if len(down) > 0 {
				return fmt.Errorf("not connected: %s", strings.Join(down, ", "))
			}
			return nil
		},
	}
	if a.ha != nil {
		checks["home_assistant"] = func(context.Context) error {
			if !a.ha.Connected() {
				return errors.New("not connected")
			}
			return nil
		}
	}
	return checks
}

func runChat(parent context.Context, cfg config.Config, ch config.Channel, session, device, text string) error {
	if ch != config.ChannelVoice && ch != config.ChannelTelegram {
		return fmt.Errorf("-channel must be voice or telegram, not %q", ch)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := build(ctx, cfg, buildOptions{})
	if err != nil {
		return err
	}
	defer a.close()
	defer cancel()

	resp, err := a.engine.Handle(ctx, conversation.Request{
		Channel: ch, ConversationID: session, DeviceID: device, Text: text, Language: cfg.Prompt.Language,
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Text)
	fmt.Printf("\n[session %s:%s · %d tool rounds · prompt %d tokens, %d from cache · prefix %s]\n",
		ch, resp.ConversationID, resp.ToolRounds, resp.PromptTokens, resp.CachedTokens, resp.PrefixHash)
	return nil
}

// otlpEndpoint accepts the URL form from the config example and returns the
// host:port the gRPC exporter wants.
func otlpEndpoint(s string) string {
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	return strings.TrimRight(s, "/")
}
