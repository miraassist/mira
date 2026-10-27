package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/announce"
	"github.com/miraassist/mira/internal/calendar"
	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	"github.com/miraassist/mira/internal/ha/assistprompt"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/language"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/mailcal"
	"github.com/miraassist/mira/internal/mcp"
	"github.com/miraassist/mira/internal/notifier"
	"github.com/miraassist/mira/internal/phone"
	"github.com/miraassist/mira/internal/profile"
	"github.com/miraassist/mira/internal/radio"
	"github.com/miraassist/mira/internal/satellite"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/speakerid"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tasks"
	"github.com/miraassist/mira/internal/telemetry"
	"github.com/miraassist/mira/internal/threads"
	"github.com/miraassist/mira/internal/timer"
	"github.com/miraassist/mira/internal/tools"
	"github.com/miraassist/mira/internal/ui"
	"github.com/miraassist/mira/internal/websearch"
)

// app is the wired agent. Fields are nil for disabled parts.
type app struct {
	cfg config.Config
	// configPath is the mira.yaml the page edits (edit.go); empty for the
	// commands that do not serve the page.
	configPath string
	edit       *editor
	// phonebook is the FRITZ!Box's, while the skill telefon is on.
	phonebook *phone.Book
	log       *slog.Logger
	tel       *telemetry.Provider
	store     *store.Store
	llm       *llm.Client
	// langs says which language applies when; lang is the one at the start,
	// for what only the model reads.
	langs *language.Resolver
	lang  i18n.Lang
	// agents is the runtime of Mira's agents (docs/spec/agenten.md).
	agents *agent.Runtime
	// calendar reads and changes Home Assistant's calendars.
	calendar *calendar.Calendar
	// mail keeps the mail calendars (mail.go); nil without a mail skill.
	mail *mailcal.Service
	// pairing: Home Assistant pairs with a code (the token is the agent's
	// own).
	pairing bool
	// models are the further models by name (config.Models).
	models map[string]*llm.Client
	skills *skills.Set
	// homeCache keeps the house's location for skills (home.go).
	homeCache homeCache
	// targetSkills are the skills with a target (they run as threads, not
	// in skills); their agents count as every skill's.
	targetSkills []skills.Skill
	// problems are the skills that did not start, as the user can read them.
	problems []haconv.Problem
	// satellites are the voice satellites' device ids last seen in the structure.
	satellites []string
	tasks      *tasks.Manager
	// threads runs threads ("Vorgänge"): concerns over several turns.
	threads *threads.Manager
	// people are Home Assistant's persons (people.go).
	people people
	// speaker asks who said a satellite turn's text; nil without
	// speaker_id (docs/spec/sprecher.md).
	speaker *speakerid.Client
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
	// pairing lets Home Assistant pair with a code; prepared by prepare.
	pairing bool
	// configPath is the mira.yaml the page edits; empty: no editing.
	configPath string
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
	a := &app{cfg: cfg, log: tel.Logger, tel: tel, warmup: func(string) {}, pairing: opts.pairing, configPath: opts.configPath,
		speaker: speakerid.New(cfg.SpeakerID.URL, cfg.SpeakerID.Token)}

	a.store, err = store.Open(ctx, store.Options{Path: cfg.Store.Path})
	if err != nil {
		return nil, err
	}
	// The config as it runs: the file with what the web UI or Home
	// Assistant changed. Read from the start, by the web search switch too.
	a.fileCfg = cfg
	a.live = a.withUIOverrides(ctx, cfg)
	// What only the model reads (tool descriptions, the structure block) is
	// built once, in the language last spoken; what is said follows each
	// turn, area or person (docs/spec/sprache.md).
	a.langs = newLanguages(cfg, a.store, a.log)
	a.lang = a.langs.Last(ctx)

	a.models = map[string]*llm.Client{}
	for name, m := range cfg.Models {
		a.models[name] = llm.New(llm.Options{
			BaseURL: m.BaseURL, APIKey: m.APIKey, Model: m.Model,
			Timeout: m.Timeout.Duration(), MaxRetries: 2, Telemetry: tel,
		})
	}
	a.llm = a.models[config.MainModel]

	// The goals Mira works towards in threads come as skills too and are
	// on when their folder is there (builtinTargets), unless mira.yaml
	// switches one off (enabled: false), for a replacement of one's own.
	enabled := map[string]map[string]any{}
	for name, settings := range cfg.Skills.Enabled {
		enabled[name] = settings
	}
	for _, name := range append(builtinTargets(), builtinMail()...) {
		if _, ok := enabled[name]; !ok && !cfg.Skills.Off[name] && skills.Exists(cfg.Skills.Dirs, name) {
			enabled[name] = nil
		}
	}
	// A skill that cannot start is left out and reported (log, and the
	// integration shows it as a repair); the agent starts all the same.
	all, problems := skills.Load(cfg.Skills.Dirs, enabled)
	// A skill may bring its MCP server (docs/spec/skill-mcp.md): it is
	// connected as the others, its tools come only with the skill.
	servers := append([]config.MCPServer(nil), cfg.MCPServers...)
	for _, sk := range all {
		if sk.MCP != nil {
			servers = append(servers, *sk.MCP)
		}
	}
	a.mcp = mcp.New(mcp.Options{
		Servers: servers, Logger: a.log, Version: version,
		ReadOnlyTools: cfg.Security.ReadOnlyTools,
		// Home Assistant's tools as each voice satellite would get them
		// (the timers exist only for those that have them).
		Devices: func() []string {
			if a.structure == nil {
				return nil
			}
			return a.structure.Snapshot().SatelliteDevices
		},
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
	if cfg.HA.AssistPromptServer != "" {
		a.assist = assistprompt.New(assistprompt.Options{
			Source: a.mcp, Server: cfg.HA.AssistPromptServer,
			Devices: func() []string {
				if a.structure == nil {
					return nil
				}
				return a.structure.Snapshot().SatelliteDevices
			},
			Interval: cfg.HA.AssistPromptRefresh.Duration(), Logger: a.log,
			OnChange: func() { a.warmup("structure_changed") },
		})
	}
	if err := a.startHA(ctx); err != nil {
		return nil, err
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

	snapshot := a.structure.Snapshot
	// The delegate tool is bound late: the engine it calls is built from the
	// registry.
	delegate := func(ctx context.Context, task string) (string, error) { return a.engine.Delegate(ctx, task) }
	internal := tools.Internal(a.lang, a.store, delegate)
	internal = append(internal, tools.Reminders(tools.ReminderOptions{
		Lang: a.lang, Store: a.store,
		Persons: a.persons,
		AreaName: func(id string) string {
			if name := a.structure.Snapshot().AreaNames[id]; name != "" {
				return name
			}
			return id
		},
	})...)
	var loaded, targets []skills.Skill
	for _, sk := range all {
		// A skill that must run locally does not start on a cloud model
		// (docs/spec/modelle.md, Privacy as a rule).
		if name, model, privacy := notLocal(cfg, sk); name != "" {
			problems = append(problems, skills.NewProblem(sk.Name, skills.KindNotLocal, name, model, privacy))
			continue
		}
		if sk.Target != nil {
			targets = append(targets, sk)
		} else {
			loaded = append(loaded, sk)
		}
	}
	a.skills = &skills.Set{Lang: a.lang, Skills: loaded, OnOpen: a.store.AddSessionSkill, Home: a.home}
	internal = append(internal, a.skills.Tools()...)
	for _, sk := range loaded {
		a.log.Info("skill enabled", "skill", sk.Name, "schedules", len(sk.Schedules))
	}
	// Home Assistant's calendars, read the way Mira decides
	// (docs/spec/kalender.md).
	a.calendar = calendar.New(calendar.Options{HAURL: cfg.HA.URL, Token: cfg.HA.Token, Lang: a.lang,
		Calendars: a.calendarList, Delete: a.deleteEvent, Logger: a.log})
	internal = append(internal, a.calendar.Tool(a.lang))
	// Mails that announce something with a time, kept in a calendar
	// (docs/spec/mail-kalender.md); the mail skills say what to read.
	if apps := mailApps(loaded); len(apps) > 0 {
		a.mail = mailcal.New(mailcal.Options{Calendar: mailCalendar{a}, Model: a.mailRead(), Matcher: a.mailMatch(),
			Apps: func() map[string]mailcal.App { return apps }, Logger: a.log})
		a.log.Info("mail calendar on", "apps", a.mail.AppNames())
	}
	// Mira's own web search (docs/spec/websuche.md); switched in the
	// integration's options, so it is asked at every turn.
	internal = append(internal, websearch.New(websearch.Options{Lang: a.lang, Logger: a.log,
		Enabled: func() bool { return a.liveConfig().WebSearch.Enabled }}).Tools()...)
	internal = append(internal, tools.MessagesRead(a.lang, a.readMessages))
	// Sleep timer: one-time automations in Home Assistant (docs/spec/geplante-aktionen.md).
	internal = append(internal, timer.Tools(timer.Options{
		Lang: a.lang, HA: a.ha,
		Players: func(area string) []string { return exposedIn(snapshot(), area, "media_player.") },
		Lights:  func(area string) []string { return exposedIn(snapshot(), area, "light.") },
		Area:    func(name string) string { return snapshot().AreaKeys[name] },
		AreaName: func(id string) string {
			if name := snapshot().AreaNames[id]; name != "" {
				return name
			}
			return id
		},
		Forbidden: func() map[string]any {
			return map[string]any{"domains": nonEmpty(cfg.Security.ConfirmDomains), "entities": nonEmpty(cfg.Security.ConfirmEntities)}
		},
	})...)
	// Radio on the room's speaker (docs/spec/radio.md).
	internal = append(internal, radio.Tool(radio.Options{
		Lang: a.lang, Search: radio.Search(a.ha), Play: radio.Play(a.ha),
		Players: func(area string) []radio.Player {
			snap := snapshot()
			var out []radio.Player
			for id := range snap.ExposedEntities {
				if strings.HasPrefix(id, "media_player.") && snap.EntityArea[id] == area {
					out = append(out, radio.Player{Entity: id, Name: snap.EntityLabel[id], Names: snap.EntityNames[id],
						Device: snap.EntityDevice[id]})
				}
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Entity < out[j].Entity })
			return out
		},
		Area: func(name string) string { return snapshot().AreaKeys[name] },
		AreaName: func(id string) string {
			if name := snapshot().AreaNames[id]; name != "" {
				return name
			}
			return id
		},
	}))
	// Phone calls from the satellite of the turn (docs/spec/telefon.md).
	a.phonebook = &phone.Book{Source: a.phoneBookSource}
	internal = append(internal, phone.Tool(phone.Options{Lang: a.lang, Book: a.phonebook,
		Line: a.phoneLine, Device: a.intercomDevice, Call: a.callESPHome}))
	// Sounds and cards on the satellite of the turn (docs/spec/satellit.md).
	internal = append(internal, satellite.Tools(satellite.Options{
		Lang: a.lang, Speaker: a.speakerOf, CardAction: a.cardActionOf, BaseURL: a.haAddress,
		Play: func(ctx context.Context, entity, url string) error {
			return a.ha.CallService(ctx, "media_player", "play_media", map[string]any{
				"media_content_id": url, "media_content_type": "music", "announce": true,
			}, &haws.Target{EntityID: []string{entity}})
		},
		Show: func(ctx context.Context, action string, c satellite.Card) error {
			domain, service, _ := strings.Cut(action, ".")
			return a.ha.CallService(ctx, domain, service, satellite.CardData(c), nil)
		},
	})...)
	// Judging, deciding and drafting are isolated agents (agents.go), bound
	// late: the agent runtime is built after this registry.
	a.tasks = tasks.New(tasks.Options{
		Lang: a.lang, Store: a.store, Source: a.ha, HA: a.ha, Snapshot: snapshot,
		Model:      a.tasksAssess(),
		Decider:    a.tasksDecide(),
		Planner:    a.tasksPlan(),
		QuietHours: cfg.Notifier.QuietHours, Logger: a.log, HAURL: cfg.HA.URL,
		ConfirmDomains: cfg.Security.ConfirmDomains, ConfirmEntities: cfg.Security.ConfirmEntities,
		Persons:    a.persons,
		Satellites: func() map[string]string { return a.liveConfig().Satellites },
		Upcoming: func(ctx context.Context, entity string, from, to time.Time) (string, error) {
			return a.calendar.Render(ctx, calendar.Query{From: from, To: to, Calendar: entity})
		},
	})
	internal = append(internal, a.tasks.Tools()...)
	checks := map[string]threads.Check{}
	finishers := map[string]threads.Finisher{"memory.save": threads.MemorySave(a.store, a.lang)}
	checks["entities"] = a.tasks.CheckEntities
	checks["calendar_writable"] = a.calendar.CheckWritable
	checks["calendar_event"] = a.calendar.CheckEvent
	checks["todo_list"] = a.checkTodoList
	finishers["tasks.automation"] = a.tasks.Finisher()
	finishers["calendar.delete"] = a.calendar.Delete()
	var known []string
	for name := range finishers {
		known = append(known, name)
	}
	sort.Strings(known)
	a.targetSkills = targets
	agentNames := a.agentNames()
	var startable []skills.Skill
	for _, sk := range targets {
		if fin := sk.Target.Finish; !sk.Target.Plan && fin.Action == nil && finishers[fin.Name] == nil {
			problems = append(problems, skills.NewProblem(sk.Name, skills.KindUnknownFinish, fin.Name, strings.Join(known, ", ")))
			continue
		}
		if name := unknownAgent(sk.Target, agentNames); name != "" {
			problems = append(problems, skills.NewProblem(sk.Name, skills.KindUnknownAgent, name))
			continue
		}
		startable = append(startable, sk)
	}
	targets = startable
	a.targetSkills = targets
	a.setProblems(problems)
	a.threads = threads.New(threads.Options{
		Lang: a.lang, Store: a.store, Targets: targets, Checks: checks, Finishers: finishers, Actions: haActions{a},
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
		// Bound late: the runtime is built after the threads.
		Run: func(ctx context.Context, id, name string, work func(context.Context) error) error {
			return a.agents.Job(ctx, agent.Spec{Name: name, Profile: agent.Isolated, Rights: agent.AllRights},
				agent.CallContext{CallID: id, Origin: agent.FromCode, Rights: agent.AllRights}, work)
		},
		Cancel:   func(id string) bool { return a.agents.Cancel(id) },
		Dispatch: a.dispatchForThread,
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
	var deviceHints func(string) string
	if a.assist != nil {
		assistText = a.assist.Text
		deviceHints = a.assist.Tail
	}
	guard := tools.NewGuard(a.lang, cfg.Security, a.store, snapshot)
	a.agents = a.newAgents(registry, guard)
	a.engine = conversation.New(conversation.Options{
		Config: cfg, LLM: a.llm, Models: models, Store: a.store, Tools: registry,
		Guard: guard, Agents: a.agents,
		Structure: snapshot, StructurePrompt: assistText, DeviceHints: deviceHints, Telemetry: tel, Logger: a.log,
		Thread: a.threads.Context, Identify: a.identify, PersonName: func(p string) string { return a.persons()[p].Name },
		Persona:   func() string { return a.liveConfig().Prompt.Persona },
		Languages: a.langs,
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
	a.registerAgents(registry)
	if anyModel(cfg, func(m config.LLM) bool { return m.WarmupOnPrefixChange }) {
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
		Lang:   a.lang,
		Source: a.ha, Subscriber: a.ha, Exposed: a.exposed,
		Debounce: a.cfg.HA.StructureDebounce.Duration(), Logger: a.log,
		OnChange: func(s structure.Snapshot) {
			// A satellite that came or went changes the tools it is offered.
			if !slices.Equal(a.satellites, s.SatelliteDevices) {
				a.satellites = s.SatelliteDevices
				if a.mcp != nil {
					go a.mcp.Refresh(context.WithoutCancel(ctx))
				}
			}
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
	if a.agents != nil {
		// The asynchronous runs stop between rounds and go on after the
		// next start.
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		a.agents.Close(stopCtx)
		stop()
	}
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

func runAgent(parent context.Context, cfg config.Config, p prepared) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := build(ctx, cfg, buildOptions{pairing: p.pairing, configPath: p.path})
	if err != nil {
		return err
	}
	defer a.close()
	defer cancel() // runs before close: stop the loops, then wait for them
	logPairing(a.log, p)

	if err := a.llm.Health(ctx); err != nil {
		a.log.Warn("llm server is not answering yet", "error", err)
	}
	a.goRun(ctx, "warmup", a.engine.RunWarmups)
	// The phone book once soon after the start, so Mira's page shows whether
	// the access works before the first call (docs/spec/telefon.md).
	if a.phonebook.On() {
		a.goRun(ctx, "phonebook", func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			if _, err := a.phonebook.Contacts(ctx); err != nil {
				a.log.Warn("phone book not read", "error", err)
			} else {
				a.log.Info("phone book read", "books", a.phonebook.Status().Books)
			}
			return nil
		})
	}
	if anyModel(cfg, func(m config.LLM) bool { return m.WarmupOnStart }) {
		a.engine.RequestWarmup(conversation.WarmupStartup)
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

	if a.mail != nil {
		a.goRun(ctx, "mail", a.mail.Run)
	}

	if a.skills != nil && len(a.skills.Skills) > 0 {
		runner := &skills.Runner{Set: a.skills, Tasker: a.engine, Store: a.store, Logger: a.log, People: a.briefingPeople}
		a.goRun(ctx, "skills", runner.Run)
	}

	if cfg.Memory.ProfileSchedule != "" {
		compactor := a.profileCompactor()
		a.goRun(ctx, "profile", compactor.Run)
	}

	// The persons come from Home Assistant; read them before the notifier
	// starts with them.
	if a.ha.Connected() {
		if _, err := a.people.refresh(ctx, a.ha); err != nil {
			a.log.Warn("could not read the persons", "error", err)
		} else {
			a.logPeople()
		}
	}
	a.runCtx = ctx
	a.goRun(ctx, "people", a.watchPeople)
	// The eval runner's agent announces nothing: the test Mira next to it
	// does that, and an eval must not speak on a real device.
	if !cfg.Server.Eval {
		a.startProactive(ctx)
	}
	// Asynchronous runs that were running when the agent stopped go on;
	// threads that were working start their work anew.
	if err := a.agents.Resume(ctx); err != nil {
		a.log.Error("could not take up the asynchronous runs", "error", err)
	}
	if a.threads != nil && a.threads.HasTargets() {
		a.threads.Resume(ctx)
	}

	errs := make(chan error, 2)

	if cfg.Server.Announce {
		a.announce(ctx)
	}

	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           a.httpHandler(ctx),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       serverRead,
		WriteTimeout:      serverWrite,
		IdleTimeout:       serverIdle,
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

	n := notifier.New(notifier.Options{
		Config: cfg, Store: a.store, HA: a.ha, Logger: a.log, Telemetry: a.tel,
		Composer: a.notifyCompose, Screens: a.screens(ctx, cfg), Languages: a.langs,
	})
	a.notifier = n
	// A new message shows on the room's indicator at once, not at the
	// next minute.
	a.store.OnMessage(func() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		n.SyncIndicators(ctx)
	})
	a.goProactive(ctx, "notifier", func(ctx context.Context) error { return n.Run(ctx, a.ha) })
}

// haUser is the agent's Home Assistant user id, asked once: the integration
// lets only this user manage Mira's automations.
func (a *app) haUser(ctx context.Context) string {
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

// builtinTargets are the goal skills that are always on.
func builtinTargets() []string {
	return []string{"aufgabe", "merken", "wecker", "termin", "termin-absagen", "todo", "kochen"}
}

// readMessages answers "Hast du eine Benachrichtigung?": by voice what waits
// for the satellite's room, on Telegram what waits for that person.
func (a *app) readMessages(ctx context.Context, inv tools.Invocation) (string, error) {
	a.proactiveMu.Lock()
	n := a.notifier
	if n == nil {
		// Not running (the chat command): reading out needs no loop.
		n = notifier.New(notifier.Options{Config: a.cfg, Store: a.store, HA: a.ha, Logger: a.log, Telemetry: a.tel,
			Composer: a.notifyCompose, Languages: a.langs})
		a.notifier = n
	}
	a.proactiveMu.Unlock()
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
		Eval: cfg.Server.Eval, Token: cfg.Server.Token, Conversation: a.engine, Store: a.store, Version: version, InstanceID: a.instanceID(),
		Logger: a.log, TurnTimeout: longestTimeout(cfg), Checks: a.healthChecks(),
		Persons: a.persons, HAUser: a.haUser, Tasks: a.tasks,
		Problems: a.allProblems, ExposedHash: a.exposedHash, Agents: a.agents,
	}
	if a.mail != nil {
		opts.Mail = a.mail
	}
	api := haconv.New(opts)
	var compact func(context.Context) (bool, error)
	if cfg.Memory.ProfileSchedule != "" {
		compact = a.profileCompactor().Compact
	}
	uiOpts := ui.Options{
		Token: cfg.Server.Token, Sections: config.UISections, Config: uiConfig{a: a, runCtx: runCtx},
		Store: a.store, Entities: a.uiEntities, CompactProfile: compact, Logger: a.log, Version: version,
		SetExposed: a.setExposed, SetLogins: a.setLogins,
		Persons: func() []string {
			keys := make([]string, 0, len(a.persons()))
			for k := range a.persons() {
				keys = append(keys, k)
			}
			return keys
		},
		PersonOfHAUser: a.personOfHAUser,
		People: func() []ui.PersonInfo {
			var out []ui.PersonInfo
			for key, p := range a.persons() {
				out = append(out, ui.PersonInfo{Key: key, Name: p.Name, Entity: p.Entity})
			}
			return out
		},
		AreaName: func(id string) string { return a.structure.Snapshot().AreaNames[id] },
		Tasks:    a.tasks, Threads: a.threads, Catalog: a.catalog, CheckModel: a.checkModel, Editor: a.editor(),
		Plans: func(ctx context.Context) ([]ui.Plan, error) {
			list, err := a.threads.Plans(ctx)
			out := make([]ui.Plan, 0, len(list))
			for _, p := range list {
				out = append(out, ui.Plan{ID: p.ID, Target: p.Target, Request: p.Request, Place: p.Place, Plan: p.Plan,
					Done: p.Done, Steps: p.Steps, Finished: p.Finished,
					Started: p.Started.In(time.Local).Format("15:04"), Updated: p.Updated.In(time.Local).Format("15:04")})
			}
			return out, err
		},
	}
	if a.speaker != nil { // a nil *Client would be a non-nil Voices
		uiOpts.Voices = a.speaker
		uiOpts.VoiceThreshold = func(person string) float64 { return a.voiceThreshold(a.persons(), person) }
	}

	page := ui.New(uiOpts)
	mux := http.NewServeMux()
	mux.Handle("/ui", page)
	mux.Handle("/ui/", page)
	mux.Handle("/v1/ui/", page)
	// Pairing anew and changing the model from Home Assistant
	// (docs/spec/einrichtung.md).
	(&setup.Handler{Store: a.store, Token: cfg.Server.Token, ID: a.instanceID(), Version: version,
		Pairing: a.pairing, Model: cfg.Main().Model, Restart: restart, Log: a.log}).Register(mux)
	mux.Handle("/", api)
	return mux
}

// uiEntities lists the known entities for autocompletion.
func (a *app) uiEntities() []ui.Entity {
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
	checks["home_assistant"] = func(context.Context) error {
		if !a.ha.Connected() {
			return errors.New("not connected")
		}
		return nil
	}
	return checks
}

func (a *app) profileCompactor() *profile.Compactor {
	return profile.New(profile.Options{
		Lang:  a.lang,
		Store: a.store, Assessor: a.profileCompact(), Schedule: a.cfg.Memory.ProfileSchedule,
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
		return fmt.Errorf("skill %s is not on (skills.%s in mira.yaml)", name, name)
	}
	// Home Assistant's persons, as the running agent has them, for
	// schedules that run per person.
	if _, err := a.people.refresh(ctx, a.ha); err != nil {
		a.log.Warn("could not read the persons from home assistant", "error", err)
	}
	runner := &skills.Runner{Set: a.skills, Tasker: a.engine, Store: a.store, Logger: a.log, People: a.briefingPeople}
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
		Channel: ch, ConversationID: session, DeviceID: device, Text: text,
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

// setProblems logs each skill that did not start in plain words and keeps
// them for /v1/status.
func (a *app) setProblems(list []skills.Problem) {
	a.problems = nil
	for _, p := range list {
		text := p.Text(a.lang)
		a.log.Warn("skill not started", "skill", p.Skill, "problem", text)
		a.problems = append(a.problems, haconv.Problem{Source: "skill", Name: p.Skill, Text: text})
	}
}

// exposedIn lists the entities of a domain prefix ("light.") that are exposed
// to Assist and in an area, sorted.
func exposedIn(snap structure.Snapshot, area, prefix string) []string {
	var out []string
	for id := range snap.ExposedEntities {
		if strings.HasPrefix(id, prefix) && snap.EntityArea[id] == area {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func nonEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// anyModel reports whether one of the models has a setting.
func anyModel(cfg config.Config, has func(config.LLM) bool) bool {
	for _, m := range cfg.Models {
		if has(m) {
			return true
		}
	}
	return false
}

// longestTimeout bounds a conversation request: the longest a model may
// take, whichever the conversation agents name.
// The HTTP server's limits: a request's body is small and read at once; an
// answer may wait for a whole turn with several model rounds (or an agent run
// from Home Assistant), so writing gets a generous bound that still ends a
// connection that hangs; an idle keep-alive connection is closed.
const (
	serverRead  = time.Minute
	serverWrite = 10 * time.Minute
	serverIdle  = 2 * time.Minute
)

func longestTimeout(cfg config.Config) time.Duration {
	var longest time.Duration
	for _, m := range cfg.Models {
		longest = max(longest, m.Timeout.Duration())
	}
	return longest
}

// allProblems are what did not start, and what of the skills' MCP servers
// does not work right now (docs/spec/skill-mcp.md): one that cannot be
// reached, tools a skill names that its server lacks. These come and go
// with the server.
func (a *app) allProblems() []haconv.Problem {
	out := append([]haconv.Problem(nil), a.problems...)
	if a.mcp == nil {
		return out
	}
	for _, st := range a.mcp.Status() {
		if st.Skill == "" {
			continue
		}
		var p skills.Problem
		switch {
		case !st.Connected:
			p = skills.NewProblem(st.Skill, skills.KindMCPDown, st.Error)
		case len(st.Missing) > 0:
			p = skills.NewProblem(st.Skill, skills.KindMCPTools, strings.Join(st.Missing, ", "))
		default:
			continue
		}
		out = append(out, haconv.Problem{Source: "skill", Name: st.Skill, Text: p.Text(a.lang)})
	}
	return out
}
