package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sker65/lares-agent/internal/config"
	"github.com/sker65/lares-agent/internal/ha/structure"
	haws "github.com/sker65/lares-agent/internal/ha/ws"
	"github.com/sker65/lares-agent/internal/llm"
	"github.com/sker65/lares-agent/internal/mcp"
	"github.com/sker65/lares-agent/internal/telemetry"
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
	cache := structure.NewCache(structure.Options{Source: c, Logger: tel.Logger})
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
}
