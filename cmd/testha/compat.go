package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/assistprompt"
	"github.com/miraassist/mira/internal/ha/structure"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/mcp"
)

// compat checks what Mira relies on against the running test Home Assistant,
// with Mira's own code and as Mira's user (docs/spec/ha-compat.md).

// check is the outcome of one check: its id, whether it passed, and what
// came or was missing.
type check struct {
	id, what string
	ok       bool
	detail   string
}

func (c check) String() string {
	state := "ok  "
	if !c.ok {
		state = "FAIL"
	}
	s := fmt.Sprintf("%s %s %s", state, c.id, c.what)
	if c.detail != "" {
		s += ": " + c.detail
	}
	return s
}

// The tools Mira relies on by name, and of them the ones Home Assistant must
// mark read-only.
var (
	compatTools    = []string{"GetLiveContext", "GetDateTime", "HassTurnOn", "HassTurnOff"}
	compatReadOnly = []string{"GetLiveContext", "GetDateTime"}
	// The actions Mira calls by name.
	compatActions = []string{"assist_satellite.announce", "assist_satellite.start_conversation", "tts.speak",
		"media_player.play_media", "media_player.media_stop", "notify.send_message", "light.turn_on", "light.turn_off"}
)

func compatCmd(ctx context.Context, dir string) error {
	e := loadEnv(dir)
	token := e["HA_TOKEN"]
	if token == "" {
		return fmt.Errorf("no HA_TOKEN in testenv/.env; run testha up first")
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	var results []check

	host := mcp.New(mcp.Options{Logger: quiet, Servers: []config.MCPServer{
		{Name: "ha", Transport: config.TransportHTTP, URL: haURL + "/api/mcp/assist", Token: token},
	}})
	defer host.Close()
	err := host.Start(ctx)
	results = append(results, check{id: "HC-01", what: "MCP server answers Mira's user", ok: err == nil, detail: errText(err)})

	c, stop, err := connect(ctx, token)
	if err != nil {
		return fmt.Errorf("websocket as Mira's user: %w", err)
	}
	defer stop()
	ws, snap := checkWebsocket(ctx, c)
	if results[0].ok {
		results = append(results, checkTools(host.Tools()), checkPrompt(ctx, host, snap.SatelliteDevices))
	}
	results = append(results, ws)
	services, err := c.Services(ctx)
	if err != nil {
		results = append(results, check{id: "HC-05", what: "actions Mira calls", detail: err.Error()})
	} else {
		results = append(results, checkActions(services))
	}
	// The log since the container's last start.
	started, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.StartedAt}}", "mira-test-ha").Output()
	var logs []byte
	if err == nil {
		logs, err = exec.CommandContext(ctx, "docker", "logs", "--since", strings.TrimSpace(string(started)), "mira-test-ha").CombinedOutput()
	}
	if err != nil {
		results = append(results, check{id: "HC-06", what: "integration loads cleanly", detail: "docker logs: " + err.Error()})
	} else {
		results = append(results, checkLog(string(logs)))
	}
	out, err := exec.CommandContext(ctx, "docker", "exec", "mira-test-ha", "python3", "-c",
		"from homeassistant.helpers import config_entry_oauth2_flow as f; print(hasattr(f.LocalOAuth2Implementation, 'token_auth_method'))").CombinedOutput()
	results = append(results, checkOAuth(string(out), err))

	failed := 0
	for _, r := range results {
		fmt.Println(r)
		if !r.ok {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(results))
	}
	return nil
}

// checkTools: HC-02. The tools Mira names are there with any prefix, and Home
// Assistant marks the reading ones read-only itself.
func checkTools(tools []mcp.Tool) check {
	r := check{id: "HC-02", what: "tools Mira relies on"}
	var missing, writable []string
	for _, name := range compatTools {
		i := slices.IndexFunc(tools, func(t mcp.Tool) bool { return t.Name == name || strings.HasSuffix(t.Name, "__"+name) })
		switch {
		case i < 0:
			missing = append(missing, name)
		case slices.Contains(compatReadOnly, name) && !tools[i].ReadOnly:
			writable = append(writable, tools[i].Name)
		}
	}
	switch {
	case len(missing) > 0:
		r.detail = "missing " + strings.Join(missing, ", ")
	case len(writable) > 0:
		r.detail = "not read-only by Home Assistant: " + strings.Join(writable, ", ")
	default:
		r.ok = true
		r.detail = fmt.Sprintf("%d tools", len(tools))
	}
	return r
}

// checkPrompt: HC-03. Mira's prompt cache gets a prompt, and each satellite's
// own lines are a small part at its end.
func checkPrompt(ctx context.Context, src assistprompt.Source, satellites []string) check {
	r := check{id: "HC-03", what: "Home Assistant's prompt"}
	cache := assistprompt.New(assistprompt.Options{Source: src, Server: "ha",
		Devices: func() []string { return satellites }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := cache.Refresh(ctx); err != nil {
		r.detail = err.Error()
		return r
	}
	if err := cache.Problem(); err != nil {
		r.detail = err.Error()
		return r
	}
	text := cache.Text()
	for _, dev := range satellites {
		if tail := cache.Tail(dev); len(tail)*10 > len(text) {
			r.detail = fmt.Sprintf("the lines of satellite %s are %d of %d characters", dev, len(tail), len(text))
			return r
		}
	}
	r.ok = true
	r.detail = fmt.Sprintf("%d characters, %d satellites", len(text), len(satellites))
	return r
}

// checkWebsocket: HC-04. The commands the service sends answer Mira's user.
// It returns the house structure built from the registries, as Mira builds it.
func checkWebsocket(ctx context.Context, c *haws.Client) (check, structure.Snapshot) {
	r := check{id: "HC-04", what: "websocket commands of the service"}
	areas, err := c.Areas(ctx)
	if err != nil || len(areas) == 0 {
		r.detail = fmt.Sprintf("areas: %d, %v", len(areas), err)
		return r, structure.Snapshot{}
	}
	devices, err := c.Devices(ctx)
	if err != nil || len(devices) == 0 {
		r.detail = fmt.Sprintf("devices: %d, %v", len(devices), err)
		return r, structure.Snapshot{}
	}
	entities, err := c.Entities(ctx)
	if err != nil || len(entities) == 0 {
		r.detail = fmt.Sprintf("entities: %d, %v", len(entities), err)
		return r, structure.Snapshot{}
	}
	snap := structure.Build(structure.Registry{Areas: areas, Devices: devices, Entities: entities})
	states, err := c.GetStates(ctx, haws.StateFilter{Domains: []string{"sensor"}})
	if err != nil || len(states) == 0 {
		r.detail = fmt.Sprintf("states: %d, %v", len(states), err)
		return r, snap
	}
	now := time.Now()
	if _, err := c.History(ctx, states[0].EntityID, now.Add(-time.Hour), now); err != nil {
		r.detail = "history: " + err.Error()
		return r, snap
	}
	if _, err := c.Statistics(ctx, []string{states[0].EntityID}, now.Add(-24*time.Hour), "hour"); err != nil {
		r.detail = "statistics: " + err.Error()
		return r, snap
	}
	r.ok = true
	r.detail = fmt.Sprintf("%d areas, %d devices, %d entities, %d sensors", len(areas), len(devices), len(entities), len(states))
	return r, snap
}

// checkActions: HC-05. The actions Mira calls by name exist.
func checkActions(services map[string]map[string][]string) check {
	r := check{id: "HC-05", what: "actions Mira calls"}
	var missing []string
	for _, a := range compatActions {
		domain, name, _ := strings.Cut(a, ".")
		if _, ok := services[domain][name]; !ok {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		r.detail = "missing " + strings.Join(missing, ", ")
		return r
	}
	r.ok = true
	return r
}

// checkLog: HC-06. Home Assistant's log (since its last start) has no
// deprecation of the integration's use of Home Assistant and no failed setup.
// That the Mira service was not reachable for a while is no matter of
// compatibility.
func checkLog(log string) check {
	r := check{id: "HC-06", what: "integration loads cleanly"}
	var bad []string
	for _, l := range strings.Split(log, "\n") {
		mira := strings.Contains(l, "custom_components.mira") || strings.Contains(l, "custom_components/mira") ||
			strings.Contains(l, "custom integration 'mira'") || strings.Contains(l, "integration mira")
		if !mira {
			continue
		}
		low := strings.ToLower(l)
		if strings.Contains(low, "deprecated") || strings.Contains(low, "breaks in") || strings.Contains(low, "will stop working") ||
			strings.Contains(low, "error setting up") || strings.Contains(low, "setup failed") || strings.Contains(low, "error loading") {
			bad = append(bad, "\n     "+strings.TrimSpace(ansi.ReplaceAllString(l, "")))
		}
	}
	if len(bad) > 0 {
		r.detail = fmt.Sprintf("%d lines:%s", len(bad), strings.Join(bad, ""))
		return r
	}
	r.ok = true
	return r
}

// checkOAuth: HC-07. Home Assistant's OAuth helper can send the secret as
// HTTP Basic (2026.11).
func checkOAuth(out string, err error) check {
	r := check{id: "HC-07", what: "OAuth helper sends the secret as HTTP Basic"}
	if err != nil {
		r.detail = strings.TrimSpace(out + " " + err.Error())
		return r
	}
	r.ok = strings.TrimSpace(out) == "True"
	if !r.ok {
		r.detail = "token_auth_method is missing"
	}
	return r
}

// ansi are the colour codes of Home Assistant's log.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
