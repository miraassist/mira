package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sker65/mira/internal/config"
	"github.com/sker65/mira/internal/ha/structure"
	haws "github.com/sker65/mira/internal/ha/ws"
	"github.com/sker65/mira/internal/i18n"
	"github.com/sker65/mira/internal/llm"
	"github.com/sker65/mira/internal/mcp"
	"github.com/sker65/mira/internal/telemetry"
)

// runCheck validates the config (Load already did) and probes every
// dependency, printing what it finds. It is the first thing to run against a
// new installation, and it answers the plan's open questions: which tools the
// HA MCP server exposes and whether the exposure list can be read.
func runCheck(ctx context.Context, cfg config.Config) error {
	tel, err := telemetry.Setup(ctx, telemetry.Options{LogLevel: "error"})
	if err != nil {
		return err
	}
	failed := 0
	report := func(name string, err error, detail string) {
		if err != nil {
			failed++
			fmt.Printf("✗ %-16s %v\n", name, err)
			return
		}
		fmt.Printf("✓ %-16s %s\n", name, detail)
	}
	fmt.Println("config ok")

	client := llm.New(llm.Options{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model, Telemetry: tel})
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	report("llm", client.Health(hctx), cfg.LLM.BaseURL)
	cancel()

	if cfg.HA.Enabled {
		checkHA(ctx, cfg, tel, report)
	}

	host := mcp.New(mcp.Options{Servers: cfg.MCPServers, ReadOnlyTools: cfg.Security.ReadOnlyTools,
		Logger: tel.Logger, CallTimeout: 15 * time.Second})
	_ = host.Start(ctx)
	defer host.Close()
	for _, st := range host.Status() {
		if !st.Connected {
			report("mcp "+st.Name, fmt.Errorf("%s", st.Error), "")
			continue
		}
		report("mcp "+st.Name, nil, fmt.Sprintf("%d tools", st.Tools))
	}
	for _, t := range host.Tools() {
		flags := ""
		if t.ReadOnly {
			flags += " [read-only]"
		}
		if t.WebContent {
			flags += " [web]"
		}
		fmt.Printf("    %s%s\n", t.QualifiedName(), flags)
	}

	if failed > 0 {
		return fmt.Errorf("%d checks failed", failed)
	}
	return nil
}

func checkHA(ctx context.Context, cfg config.Config, tel *telemetry.Provider, report func(string, error, string)) {
	c, err := haws.New(haws.Options{URL: cfg.HA.URL, Token: cfg.HA.Token, Logger: tel.Logger})
	if err != nil {
		report("home assistant", err, "")
		return
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	runErr := make(chan error, 1)
	go func() { runErr <- c.Run(runCtx) }()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ready := make(chan error, 1)
	go func() { ready <- c.WaitReady(waitCtx) }()
	select {
	case err := <-runErr:
		report("home assistant", err, "")
		return
	case err := <-ready:
		if err != nil {
			report("home assistant", fmt.Errorf("not reachable at %s: %w", c.Endpoint(), err), "")
			return
		}
	}
	report("home assistant", nil, c.Endpoint())

	_, exposeErr := c.ExposedEntities(ctx)
	if exposeErr != nil {
		fmt.Printf("  ! exposure list not readable (%v); falling back to entity options\n", exposeErr)
	}
	cache := structure.NewCache(structure.Options{Lang: i18n.Of(cfg.Prompt.Language), Source: c, Logger: tel.Logger})
	if _, err := cache.Rebuild(ctx); err != nil {
		report("structure", err, "")
		return
	}
	snap := cache.Snapshot()
	report("structure", nil, fmt.Sprintf("%d areas, %d exposed entities, %d bytes", snap.AreaCount, snap.EntityCount, len(snap.Block)))
	for area, sat := range cfg.Satellites {
		if _, ok := snap.AreaNames[area]; !ok {
			fmt.Printf("  ! satellites: area %q (for %s) does not exist in Home Assistant\n", area, sat)
		}
	}
	checkEntities(ctx, c, cfg)
}

// checkEntities looks up every entity the notifier relies on and
// reports how each satellite would speak.
func checkEntities(ctx context.Context, c *haws.Client, cfg config.Config) {
	states, err := c.States(ctx)
	if err != nil {
		fmt.Printf("  ! could not read states: %v\n", err)
		return
	}
	byID := map[string]haws.State{}
	for _, s := range states {
		byID[s.EntityID] = s
	}
	areas := make([]string, 0, len(cfg.Satellites))
	for a := range cfg.Satellites {
		areas = append(areas, a)
	}
	sort.Strings(areas)
	for _, area := range areas {
		sat := cfg.Satellites[area]
		st, ok := byID[sat]
		switch {
		case !ok:
			fmt.Printf("  ! satellite %s (%s) does not exist\n", sat, area)
		case strings.HasPrefix(sat, "media_player."):
			fmt.Printf("    satellite %-32s %s: tts.speak via %s\n", sat, area, cfg.Notifier.TTSEntity)
		default:
			f, _ := st.Attributes["supported_features"].(float64)
			method := "none: cannot announce"
			switch {
			case int(f)&2 != 0:
				method = "start_conversation (listens afterwards)"
			case int(f)&1 != 0:
				method = "announce (does not listen afterwards)"
			}
			fmt.Printf("    satellite %-32s %s: %s\n", sat, area, method)
		}
	}
	want := map[string]string{}
	for area, sensor := range cfg.Notifier.MorningTrigger.Presence {
		want[sensor] = "presence " + area
	}
	for id, p := range cfg.Persons {
		if p.Entity != "" {
			want[p.Entity] = "person " + id
		}
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if st, ok := byID[id]; ok {
			fmt.Printf("    %-42s %s (now %s)\n", id, want[id], st.State)
		} else {
			fmt.Printf("  ! %s (%s) does not exist\n", id, want[id])
		}
	}
}
