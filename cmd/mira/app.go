package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sker65/mira/internal/announce"
	"github.com/sker65/mira/internal/channels/haconv"
	"github.com/sker65/mira/internal/channels/telegram"
	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/conversation"
	"github.com/sker65/mira/internal/ha/assistprompt"
	"github.com/sker65/mira/internal/ha/structure"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/mcp"
	"github.com/sker65/mira/internal/notifier"
	"github.com/sker65/mira/internal/profile"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/telemetry"
	"github.com/sker65/mira/internal/tools"
	"github.com/sker65/mira/internal/ui"
	"github.com/sker65/mira/internal/watchers"
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
	assist    *assistprompt.Cache
	mcp       *mcp.Host
	telegram  *telegram.Bot
	engine    *conversation.Engine

	// live is the config the proactive parts run with: the YAML file plus
	// the sections changed in the web UI. fileCfg is the file alone, for
	// "back to mira.yaml".
	liveMu  sync.Mutex
	live    config.Config
	fileCfg config.Config
	// proactive runs the watchers and the notifier; the UI restarts it.
	proactiveMu     sync.Mutex
	proactiveCancel context.CancelFunc
	proactiveWG     sync.WaitGroup

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

	a.mcp = mcp.New(mcp.Options{
		Servers: cfg.MCPServers, Logger: a.log, Version: version,
		ReadOnlyTools: cfg.Security.ReadOnlyTools,
		OnChange: func(tools []mcp.Tool) {
			a.log.Info("tool list changed", "tools", len(tools))
			a.warmup("tools_changed")
			// Also after a reconnect: the prompt may have changed meanwhile.
			if a.assist != nil {
				a.assist.Trigger(ctx)
			}
		},
	})
	// Built before Home Assistant connects: the structure cache's change
	// handler refreshes it.
	if cfg.HA.Enabled && cfg.HA.AssistPromptServer != "" {
		a.assist = assistprompt.New(assistprompt.Options{
			Source: a.mcp, Server: cfg.HA.AssistPromptServer,
			Interval: cfg.HA.AssistPromptRefresh.Duration(), Logger: a.log,
			OnChange: func() { a.warmup("structure_changed") },
		})
	}
	if cfg.HA.Enabled {
		if err := a.startHA(ctx); err != nil {
			return nil, err
		}
	}

	if err := a.mcp.Start(ctx); err != nil {
		// Not fatal: the agent still talks, and Run keeps retrying.
		a.log.Error("no mcp server reachable; the agent has no house tools until one comes up", "error", err)
	}
	a.goRun(ctx, "mcp", a.mcp.Run)

	if a.assist != nil {
		if _, err := a.assist.Refresh(ctx); err != nil {
			a.log.Warn("no assist prompt from home assistant; using the own structure block", "error", err)
		}
		a.goRun(ctx, "assist-prompt", a.assist.Run)
	}

	if opts.withTelegram && cfg.Telegram.Enabled {
		a.telegram = telegram.New(telegram.Options{
			Lang:  i18n.Of(cfg.Prompt.Language),
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
	registry := tools.NewRegistry(a.mcp, tools.Internal(i18n.Of(cfg.Prompt.Language), a.store, sender)...)
	var assistText func() string
	if a.assist != nil {
		assistText = a.assist.Text
	}
	a.engine = conversation.New(conversation.Options{
		Config: cfg, LLM: a.llm, Store: a.store, Tools: registry,
		Guard:     tools.NewGuard(i18n.Of(cfg.Prompt.Language), cfg.Security, a.store, snapshot),
		Structure: snapshot, StructurePrompt: assistText, Telemetry: tel, Logger: a.log,
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
		Lang:   i18n.Of(a.cfg.Prompt.Language),
		Source: a.ha, Subscriber: a.ha,
		Debounce: a.cfg.HA.StructureDebounce.Duration(), Logger: a.log,
		OnChange: func(s structure.Snapshot) {
			if a.assist != nil {
				// The Assist prompt follows the registry; its own change
				// triggers the warmup.
				a.assist.Trigger(ctx)
				return
			}
			a.warmup("structure_changed")
		},
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
	a.proactiveWG.Wait()
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

	if cfg.Memory.ProfileSchedule != "" {
		compactor := a.profileCompactor()
		a.goRun(ctx, "profile", compactor.Run)
	}

	a.fileCfg = cfg
	a.live = a.withUIOverrides(ctx, cfg)
	if a.ha != nil {
		a.startProactive(ctx)
	} else if len(cfg.Watchers) > 0 || len(cfg.Notifier.MorningTrigger.Areas) > 0 {
		a.log.Warn("watchers and notifier need Home Assistant (ha.enabled); they are off")
	}

	errs := make(chan error, 2)
	if a.telegram != nil {
		go func() { errs <- fmt.Errorf("telegram: %w", a.telegram.Run(ctx)) }()
	}

	if cfg.Server.Announce {
		a.announce(ctx)
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           a.httpHandler(ctx),
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

	a.log.Info("mira is up", "version", version)
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

// instanceID is the agent's stable id, or "" if the store cannot give one.
func (a *app) instanceID() string {
	id, err := a.store.InstanceID(context.Background())
	if err != nil {
		a.log.Warn("no instance id", "error", err)
	}
	return id
}

// announce makes the agent discoverable by Home Assistant. A failure only
// costs discovery; the integration can still be set up by address.
func (a *app) announce(ctx context.Context) {
	if announce.Loopback(a.cfg.Server.Listen) {
		a.log.Info("not announcing: server.listen is loopback only", "listen", a.cfg.Server.Listen)
		return
	}
	id, err := a.store.InstanceID(ctx)
	if err != nil {
		a.log.Warn("no instance id, not announcing", "error", err)
		return
	}
	go func() {
		err := announce.Run(ctx, announce.Options{
			Instance: a.cfg.Server.Name, ID: id, Version: version, Listen: a.cfg.Server.Listen, Logger: a.log,
		})
		if err != nil && ctx.Err() == nil {
			a.log.Warn("mdns announcement failed", "error", err)
		}
	}()
}

// startProactive runs the watchers, which turn HA events into notices, and
// the notifier, which delivers them, with the live config. They get their own
// context so the web UI can restart them with a changed config.
func (a *app) startProactive(parent context.Context) {
	a.proactiveMu.Lock()
	defer a.proactiveMu.Unlock()
	cfg := a.liveConfig()
	ctx, cancel := context.WithCancel(parent)
	a.proactiveCancel = cancel

	w := watchers.New(watchers.Options{
		Lang:     i18n.Of(cfg.Prompt.Language),
		Watchers: cfg.Watchers, MinImportance: cfg.Notifier.MinImportance,
		Store: a.store, Assessor: a.engine, Logger: a.log,
	})
	a.goProactive(ctx, "watchers", func(ctx context.Context) error { return w.Run(ctx, a.ha) })

	var tg notifier.Telegram
	if a.telegram != nil {
		tg = a.telegram
	}
	n := notifier.New(notifier.Options{
		Config: cfg, Store: a.store, HA: a.ha, Telegram: tg, Logger: a.log, Telemetry: a.tel,
	})
	a.goProactive(ctx, "notifier", func(ctx context.Context) error { return n.Run(ctx, a.ha) })
}

func (a *app) goProactive(ctx context.Context, name string, fn func(context.Context) error) {
	a.proactiveWG.Add(1)
	go func() {
		defer a.proactiveWG.Done()
		if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("background task stopped", "task", name, "error", err)
		}
	}()
}

// restartProactive stops the watchers and the notifier, waits for them and
// starts them again with the live config.
func (a *app) restartProactive(parent context.Context) {
	if a.ha == nil {
		return
	}
	a.proactiveMu.Lock()
	cancel := a.proactiveCancel
	a.proactiveMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.proactiveWG.Wait()
	a.startProactive(parent)
	a.log.Info("watchers and notifier restarted with the changed config")
}

func (a *app) liveConfig() config.Config {
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	return a.live
}

// uiKey is where a section changed in the web UI is stored, as YAML.
func uiKey(section string) string { return "ui_config:" + section }

// withUIOverrides lays the sections changed in the web UI over the file.
// They are validated together, like when they were saved: one may depend on
// another (a presence sensor needs its area's satellite). If the file changed
// so that they no longer fit, each is tried alone and the misfits are skipped.
func (a *app) withUIOverrides(ctx context.Context, cfg config.Config) config.Config {
	stored := map[string][]byte{}
	for _, name := range config.UISections {
		if raw, err := a.store.GetKV(ctx, uiKey(name)); err == nil {
			stored[name] = []byte(raw)
		}
	}
	if len(stored) == 0 {
		return cfg
	}
	all := cfg
	ok := true
	for _, name := range config.UISections {
		if raw, found := stored[name]; found {
			if err := all.ApplySection(name, raw); err != nil {
				ok = false
			}
		}
	}
	if ok && all.Validate() == nil {
		for name := range stored {
			a.log.Info("config section from the web UI", "section", name)
		}
		return all
	}
	for _, name := range config.UISections {
		raw, found := stored[name]
		if !found {
			continue
		}
		next := cfg
		err := next.ApplySection(name, raw)
		if err == nil {
			err = next.Validate()
		}
		if err != nil {
			a.log.Error("ignoring a section changed in the web UI", "section", name, "error", err)
			continue
		}
		a.log.Info("config section from the web UI", "section", name)
		cfg = next
	}
	return cfg
}

// setSections applies sections from the UI (YAML by name), validates the
// whole config, stores them and restarts what depends on them. A nil value
// resets that section to the file's.
func (a *app) setSections(ctx, runCtx context.Context, sections map[string][]byte) error {
	a.liveMu.Lock()
	next := a.live
	for name, raw := range sections {
		if raw == nil {
			fileJSON, err := a.fileCfg.SectionJSON(name)
			if err == nil {
				raw, err = config.SectionYAML(name, fileJSON)
			}
			if err != nil {
				a.liveMu.Unlock()
				return err
			}
		}
		if err := next.ApplySection(name, raw); err != nil {
			a.liveMu.Unlock()
			return err
		}
	}
	if err := next.Validate(); err != nil {
		a.liveMu.Unlock()
		return err
	}
	for name, raw := range sections {
		var err error
		if raw == nil {
			err = a.store.DeleteKV(ctx, uiKey(name))
		} else {
			err = a.store.SetKV(ctx, uiKey(name), string(raw))
		}
		if err != nil {
			a.liveMu.Unlock()
			return err
		}
	}
	a.live = next
	a.liveMu.Unlock()
	a.restartProactive(runCtx)
	return nil
}

// uiConfig adapts the app to the web UI.
type uiConfig struct {
	a      *app
	runCtx context.Context
}

func (u uiConfig) Section(name string) (json.RawMessage, bool, error) {
	raw, err := u.a.liveConfig().SectionJSON(name)
	if err != nil {
		return nil, false, err
	}
	_, kvErr := u.a.store.GetKV(context.Background(), uiKey(name))
	return raw, kvErr == nil, nil
}

func (u uiConfig) SetSections(ctx context.Context, sections map[string]json.RawMessage) error {
	yamls := make(map[string][]byte, len(sections))
	for name, raw := range sections {
		y, err := config.SectionYAML(name, raw)
		if err != nil {
			return err
		}
		yamls[name] = y
	}
	return u.a.setSections(ctx, u.runCtx, yamls)
}

func (u uiConfig) ResetSection(ctx context.Context, name string) error {
	if !slices.Contains(config.UISections, name) {
		return fmt.Errorf("unknown or read-only config section %q", name)
	}
	return u.a.setSections(ctx, u.runCtx, map[string][]byte{name: nil})
}

// httpHandler serves the web UI and its API under /ui and /v1/ui/, and the
// HA integration's API everywhere else.
func (a *app) httpHandler(runCtx context.Context) http.Handler {
	cfg := a.cfg
	api := haconv.New(haconv.Options{
		Token: cfg.Server.Token, Conversation: a.engine, Store: a.store, Version: version, InstanceID: a.instanceID(),
		Logger: a.log, TurnTimeout: cfg.LLM.Timeout.Duration(), Checks: a.healthChecks(),
		Persons: func() map[string]config.Person { return a.liveConfig().Persons },
	})
	var compact func(context.Context) (bool, error)
	if cfg.Memory.ProfileSchedule != "" {
		compact = a.profileCompactor().Compact
	}
	page := ui.New(ui.Options{
		Token: cfg.Server.Token, Sections: config.UISections, Config: uiConfig{a: a, runCtx: runCtx},
		Store: a.store, Entities: a.uiEntities, CompactProfile: compact, Logger: a.log,
	})
	mux := http.NewServeMux()
	mux.Handle("/ui", page)
	mux.Handle("/ui/", page)
	mux.Handle("/v1/ui/", page)
	mux.Handle("/", api)
	return mux
}

// uiEntities lists the known entities for autocompletion.
func (a *app) uiEntities() []ui.Entity {
	if a.structure == nil {
		return nil
	}
	snap := a.structure.Snapshot()
	out := make([]ui.Entity, 0, len(snap.EntityNames))
	for id, names := range snap.EntityNames {
		e := ui.Entity{ID: id, Area: snap.AreaNames[snap.EntityArea[id]]}
		for _, n := range names {
			if n != id {
				e.Name = n
				break
			}
		}
		out = append(out, e)
	}
	return out
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

func (a *app) profileCompactor() *profile.Compactor {
	return profile.New(profile.Options{
		Lang:  i18n.Of(a.cfg.Prompt.Language),
		Store: a.store, Assessor: a.engine, Schedule: a.cfg.Memory.ProfileSchedule,
		MaxChars: a.cfg.Memory.MaxProfileChars, Logger: a.log,
		OnChange: func() { a.warmup("profile_changed") },
	})
}

// runProfile compacts the profile now and prints it.
func runProfile(parent context.Context, cfg config.Config) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := build(ctx, cfg, buildOptions{})
	if err != nil {
		return err
	}
	defer a.close()
	defer cancel()
	changed, err := a.profileCompactor().Compact(ctx)
	if err != nil {
		return err
	}
	text, _, err := a.store.Profile(ctx)
	if err != nil {
		return err
	}
	fmt.Println(text)
	state := "unchanged"
	if changed {
		state = "updated"
	}
	fmt.Printf("\n[profile %s]\n", state)
	return nil
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
