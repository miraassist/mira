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
	"github.com/sker65/mira/internal/ical"
	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/mcp"
	"github.com/sker65/mira/internal/notifier"
	"github.com/sker65/mira/internal/profile"
	"github.com/sker65/mira/internal/skills"
	"github.com/sker65/mira/internal/store"
	"github.com/sker65/mira/internal/tasks"
	"github.com/sker65/mira/internal/telemetry"
	"github.com/sker65/mira/internal/threads"
	"github.com/sker65/mira/internal/tools"
	"github.com/sker65/mira/internal/ui"
)

// app is the wired agent. Fields are nil for disabled parts.
type app struct {
	cfg   config.Config
	log   *slog.Logger
	tel   *telemetry.Provider
	store *store.Store
	llm   *llm.Client
	// models are the further models by name (config.Models).
	models map[string]*llm.Client
	skills *skills.Set
	tasks  *tasks.Manager
	// threads runs threads ("Vorgänge"): concerns over several turns.
	threads *threads.Manager
	// people are Home Assistant's persons (people.go).
	people people
	// runCtx is run's context, for restarts from background work.
	runCtx context.Context
	// haUserID is the agent's Home Assistant user, read once (haUser).
	haUserMu sync.Mutex
	haUserID string
	// notifier is the running notifier, replaced when the UI restarts it;
	// guarded by proactiveMu.
	notifier  *notifier.Notifier
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
	// proactive runs the notifier; the UI restarts it.
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
	a.models = map[string]*llm.Client{}
	for name, m := range cfg.Models {
		a.models[name] = llm.New(llm.Options{
			BaseURL: m.BaseURL, APIKey: m.APIKey, Model: m.Model,
			Timeout: m.Timeout.Duration(), MaxRetries: 2, Telemetry: tel,
		})
	}

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
	// The delegate tool exists only with a delegate model. It is bound late:
	// the engine it calls is built from the registry.
	var delegate tools.Delegator
	if cfg.Routing.Delegate != "" {
		delegate = func(ctx context.Context, task string) (string, error) { return a.engine.Delegate(ctx, task) }
	}
	internal := tools.Internal(i18n.Of(cfg.Prompt.Language), a.store, sender, delegate)
	internal = append(internal, tools.Reminders(tools.ReminderOptions{
		Lang: i18n.Of(cfg.Prompt.Language), Store: a.store,
		Persons: a.persons,
	})...)
	// The goals Mira works towards in threads come as skills too and are
	// always on when their folder is there: "aufgabe" needs Home
	// Assistant, "merken" does not.
	enabled := map[string]map[string]any{}
	for name, settings := range cfg.Skills.Enabled {
		enabled[name] = settings
	}
	for _, name := range builtinTargets(a.ha != nil) {
		if _, ok := enabled[name]; !ok && skills.Exists(cfg.Skills.Dirs, name) {
			enabled[name] = nil
		}
	}
	all, err := skills.Load(cfg.Skills.Dirs, enabled)
	if err != nil {
		return nil, err
	}
	var loaded, targets []skills.Skill
	for _, sk := range all {
		if sk.Target != nil {
			targets = append(targets, sk)
		} else {
			loaded = append(loaded, sk)
		}
	}
	a.skills = &skills.Set{Lang: i18n.Of(cfg.Prompt.Language), Skills: loaded, Calendar: &ical.Cache{}}
	internal = append(internal, a.skills.Tools()...)
	for _, sk := range loaded {
		a.log.Info("skill enabled", "skill", sk.Name, "schedules", len(sk.Schedules), "calendars", len(sk.Calendars))
	}
	internal = append(internal, tools.MessagesRead(i18n.Of(cfg.Prompt.Language), a.readMessages))
	if a.ha != nil {
		// Drafting and judging run on the engine, which is built from this
		// registry: bound late through isolatedModel.
		a.tasks = tasks.New(tasks.Options{
			Lang: i18n.Of(cfg.Prompt.Language), Store: a.store, Source: a.ha, HA: a.ha, Snapshot: snapshot,
			Model:      isolatedModel{a, cfg.RouteFor(config.ChannelBackground)},
			Planner:    isolatedModel{a, cfg.Routing.Tasks},
			QuietHours: cfg.Notifier.QuietHours, Logger: a.log, HAURL: cfg.HA.URL,
			ConfirmDomains: cfg.Security.ConfirmDomains, ConfirmEntities: cfg.Security.ConfirmEntities,
			Persons: a.persons,
		})
		internal = append(internal, a.tasks.Tools()...)
	}
	checks := map[string]threads.Check{}
	finishers := map[string]threads.Finisher{"memory.save": threads.MemorySave(a.store, i18n.Of(cfg.Prompt.Language))}
	if a.tasks != nil {
		checks["entities"] = a.tasks.CheckEntities
		finishers["tasks.automation"] = a.tasks.Finisher()
	}
	a.threads = threads.New(threads.Options{
		Lang: i18n.Of(cfg.Prompt.Language), Store: a.store, Targets: targets, Checks: checks, Finishers: finishers,
		Persons: a.persons, Logger: a.log,
		Deliver: func(ctx context.Context, n store.Message, area string) error {
			a.proactiveMu.Lock()
			nf := a.notifier
			a.proactiveMu.Unlock()
			if nf == nil {
				return errors.New("no notifier is running")
			}
			return nf.Tell(ctx, n, area)
		},
	})
	for _, sk := range targets {
		a.log.Info("target enabled", "target", sk.Name, "finish", sk.Target.Finish)
	}
	internal = append(internal, a.threads.Tools()...)
	registry := tools.NewRegistry(a.mcp, internal...)
	models := map[string]conversation.Chatter{}
	for name, c := range a.models {
		models[name] = c
	}
	var assistText func() string
	if a.assist != nil {
		assistText = a.assist.Text
	}
	a.engine = conversation.New(conversation.Options{
		Config: cfg, LLM: a.llm, Models: models, Store: a.store, Tools: registry,
		Guard:     tools.NewGuard(i18n.Of(cfg.Prompt.Language), cfg.Security, a.store, snapshot),
		Structure: snapshot, StructurePrompt: assistText, Telemetry: tel, Logger: a.log,
		Thread: a.threads.Context, Identify: a.identify,
		Afterword: func(ctx context.Context, area string) string {
			a.proactiveMu.Lock()
			n := a.notifier
			a.proactiveMu.Unlock()
			if n == nil {
				return ""
			}
			return n.Afterword(ctx, area)
		},
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

	if a.threads != nil && a.threads.HasTargets() {
		a.goRun(ctx, "threads", a.threads.Run)
	}

	if a.skills != nil && len(a.skills.Skills) > 0 {
		runner := &skills.Runner{Set: a.skills, Tasker: a.engine, Store: a.store, Logger: a.log}
		a.goRun(ctx, "skills", runner.Run)
	}

	if cfg.Memory.ProfileSchedule != "" {
		compactor := a.profileCompactor()
		a.goRun(ctx, "profile", compactor.Run)
	}

	a.fileCfg = cfg
	a.live = a.withUIOverrides(ctx, cfg)
	if a.ha != nil {
		// The persons come from Home Assistant; read them before the
		// notifier starts with them.
		if a.ha.Connected() {
			if _, err := a.people.refresh(ctx, a.ha); err != nil {
				a.log.Warn("could not read the persons", "error", err)
			} else {
				a.logPeople()
			}
		}
		a.runCtx = ctx
		a.goRun(ctx, "people", a.watchPeople)
		a.startProactive(ctx)
	} else if len(cfg.Notifier.MorningTrigger.Areas) > 0 {
		a.log.Warn("the notifier needs Home Assistant (ha.enabled); it is off")
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

// startProactive runs the notifier, which delivers messages, with the live
// config. It gets its own context so the web UI can restart it with a
// changed config. What raises messages are tasks: Home Assistant
// automations calling Mira's actions.
func (a *app) startProactive(parent context.Context) {
	a.proactiveMu.Lock()
	defer a.proactiveMu.Unlock()
	cfg := a.liveConfig()
	cfg.Persons = a.persons()
	ctx, cancel := context.WithCancel(parent)
	a.proactiveCancel = cancel

	var tg notifier.Telegram
	if a.telegram != nil {
		tg = a.telegram
	}
	n := notifier.New(notifier.Options{
		Config: cfg, Store: a.store, HA: a.ha, Telegram: tg, Logger: a.log, Telemetry: a.tel,
	})
	a.notifier = n
	a.goProactive(ctx, "notifier", func(ctx context.Context) error { return n.Run(ctx, a.ha) })
}

// haUser is the agent's Home Assistant user id, asked once: the integration
// lets only this user manage Mira's automations.
func (a *app) haUser(ctx context.Context) string {
	if a.ha == nil {
		return ""
	}
	a.haUserMu.Lock()
	defer a.haUserMu.Unlock()
	if a.haUserID == "" {
		id, err := a.ha.CurrentUser(ctx)
		if err != nil {
			a.log.Warn("could not read the agent's Home Assistant user", "error", err)
			return ""
		}
		a.haUserID = id
	}
	return a.haUserID
}

// builtinTargets are the goal skills that are always on: "merken", and
// "aufgabe" with Home Assistant.
func builtinTargets(withHA bool) []string {
	if withHA {
		return []string{"aufgabe", "merken"}
	}
	return []string{"merken"}
}

// isolatedModel runs the tasks' judgements and drafts on the named model,
// without the house prompt.
type isolatedModel struct {
	a     *app
	model string
}

func (m isolatedModel) Ask(ctx context.Context, system, instruction string, schema json.RawMessage) (string, error) {
	return m.a.engine.AssessIsolatedOn(ctx, m.model, system, instruction, schema)
}

// persons are the configured persons from the live config.
// engineAssessor lets the task manager use the engine before it exists.
type engineAssessor struct{ a *app }

func (e engineAssessor) Assess(ctx context.Context, instruction string, schema json.RawMessage) (string, error) {
	return e.a.engine.Assess(ctx, instruction, schema)
}

// readMessages answers "Hast du eine Benachrichtigung?": by voice what waits
// for the satellite's room, on Telegram what waits for that person.
func (a *app) readMessages(ctx context.Context, inv tools.Invocation) (string, error) {
	a.proactiveMu.Lock()
	n := a.notifier
	if n == nil && a.ha != nil {
		// Not running (the chat command): reading out needs no loop.
		n = notifier.New(notifier.Options{Config: a.cfg, Store: a.store, HA: a.ha, Logger: a.log, Telemetry: a.tel})
		a.notifier = n
	}
	a.proactiveMu.Unlock()
	if n == nil {
		return "", errors.New("notifications are off (no Home Assistant)")
	}
	area := ""
	if inv.Channel == config.ChannelVoice {
		sess, err := a.store.GetSession(ctx, inv.SessionKey)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		area = sess.AreaID
	}
	return n.ReadOut(ctx, area, tools.ReceiverFor("", inv, a.persons()))
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

// restartProactive stops the notifier, waits for it and
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
	a.log.Info("notifier restarted with the changed config")
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
	opts := haconv.Options{
		Token: cfg.Server.Token, Conversation: a.engine, Store: a.store, Version: version, InstanceID: a.instanceID(),
		Logger: a.log, TurnTimeout: cfg.LLM.Timeout.Duration(), Checks: a.healthChecks(),
		Persons: a.persons, HAUser: a.haUser,
	}
	if a.tasks != nil {
		opts.Tasks = a.tasks
	}
	api := haconv.New(opts)
	var compact func(context.Context) (bool, error)
	if cfg.Memory.ProfileSchedule != "" {
		compact = a.profileCompactor().Compact
	}
	uiOpts := ui.Options{
		Token: cfg.Server.Token, Sections: config.UISections, Config: uiConfig{a: a, runCtx: runCtx},
		Store: a.store, Entities: a.uiEntities, CompactProfile: compact, Logger: a.log, Version: version,
		Persons: func() []string {
			keys := make([]string, 0, len(a.persons()))
			for k := range a.persons() {
				keys = append(keys, k)
			}
			return keys
		},
		PersonOfHAUser: a.personOfHAUser,
		People: func() []ui.PersonInfo {
			if a.ha == nil {
				return nil
			}
			var out []ui.PersonInfo
			for key, p := range a.persons() {
				out = append(out, ui.PersonInfo{Key: key, Name: p.Name, Entity: p.Entity})
			}
			return out
		},
		AreaName: func(id string) string {
			if a.structure == nil {
				return ""
			}
			return a.structure.Snapshot().AreaNames[id]
		},
	}
	if a.tasks != nil {
		uiOpts.Tasks = a.tasks
	}
	if a.threads != nil {
		uiOpts.Threads = a.threads
	}
	page := ui.New(uiOpts)
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
		// further models, as "llm:smart"; for a cloud model this lists its
		// models, which costs nothing
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
	for name, c := range a.models {
		checks["llm:"+name] = c.Health
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
	text, _, err := a.store.Profile(ctx, store.OwnerHouse)
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

// runSkill runs a skill's schedule now, as at its time, and prints the
// message it queued, if any.
func runSkill(parent context.Context, cfg config.Config, name string, index int) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := build(ctx, cfg, buildOptions{})
	if err != nil {
		return err
	}
	defer a.close()
	defer cancel()
	sk, ok := a.skills.Get(name)
	if !ok {
		return fmt.Errorf("skill %s is not enabled (skills.enabled)", name)
	}
	runner := &skills.Runner{Set: a.skills, Tasker: a.engine, Store: a.store, Logger: a.log}
	queued, err := runner.RunJob(ctx, sk, index)
	if err != nil {
		return err
	}
	if !queued {
		fmt.Println("[nothing to report]")
		return nil
	}
	list, err := a.store.ListMessages(ctx, store.MessageFilter{States: []string{store.MessagePending}, Limit: 1})
	if err != nil || len(list) == 0 {
		return err
	}
	fmt.Printf("%s\n\n[message queued, importance %d]\n", list[0].Spoken, list[0].Importance)
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
