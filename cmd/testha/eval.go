package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/miraassist/mira/internal/trace"
)

// The eval runner (docs/spec/evals.md): it plays fixed scenarios end to end
// against its own agent, with a real model and the test HA, and reports per
// model setup what works and how fast.

// Scenario is one file in evals/scenarios/.
type Scenario struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Core scenarios pass at 3 of 3 runs, the others at 2 of 3.
	Core  bool          `yaml:"core"`
	Setup ScenarioSetup `yaml:"setup"`
	Steps []Step        `yaml:"steps"`
}

type ScenarioSetup struct {
	Events []SetupEvent `yaml:"events"`
	Todo   []SetupTodo  `yaml:"todo"`
	Facts  []SetupFact  `yaml:"facts"`
	// Home lists who is at home (the test HA's sim switches); nil leaves
	// presence as it is.
	Home []string `yaml:"home"`
	// States sets entities before the scenario; they are restored after.
	States map[string]string `yaml:"states"`
	// Restore are further entities the scenario may change.
	Restore []string `yaml:"restore"`
}

type SetupEvent struct {
	Calendar string `yaml:"calendar"`
	Summary  string `yaml:"summary"`
	Day      string `yaml:"day"`   // "+3"
	Start    string `yaml:"start"` // "15:00"; empty is all day
	End      string `yaml:"end"`
}

type SetupTodo struct {
	List string `yaml:"list"`
	Item string `yaml:"item"`
	Due  string `yaml:"due"`
}

type SetupFact struct {
	Owner string `yaml:"owner"`
	Text  string `yaml:"text"`
}

// Step is one thing said. At is the area of the satellite it is said at;
// otherwise Person on Channel (telegram).
type Step struct {
	Say        string  `yaml:"say"`
	At         string  `yaml:"at"`
	Person     string  `yaml:"person"`
	Channel    string  `yaml:"channel"`
	MaxSeconds float64 `yaml:"max_seconds"`
	Expect     Expect  `yaml:"expect"`
}

// Time limits at which a turn fails for the user: the integration's answer
// timeout for voice, a generous minute for Telegram.
const (
	voiceLimit    = 15 * time.Second
	telegramLimit = 60 * time.Second
	runsDefault   = 3
)

type evalRun struct {
	dir, root string
	env       env
	agentURL  string
	token     string
	haToken   string
	setupName string
	setup     map[string]any
	devices   map[string]string // area -> satellite device id
	client    *http.Client
	agentLog  string
	version   string
}

func evalCmd(ctx context.Context, dir string, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	setupPath := fs.String("setup", "", "model setup, e.g. evals/setups/gemini-flash.yaml")
	only := fs.String("scenario", "", "only these scenarios, comma-separated")
	runs := fs.Int("runs", runsDefault, "runs per scenario")
	keep := fs.Bool("no-report", false, "do not write a report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *setupPath == "" {
		return fmt.Errorf("usage: testha eval --setup evals/setups/NAME.yaml [--scenario a,b] [--runs 3]")
	}
	root := filepath.Dir(dir)
	r := &evalRun{dir: dir, root: root, env: loadEnv(dir), client: &http.Client{Timeout: 2 * time.Minute}}
	r.haToken = r.env["TESTHA_ADMIN_TOKEN"]
	raw, err := os.ReadFile(filepath.Join(root, *setupPath))
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, &r.setup); err != nil {
		return fmt.Errorf("%s: %w", *setupPath, err)
	}
	r.setupName = strings.TrimSuffix(filepath.Base(*setupPath), filepath.Ext(*setupPath))

	scenarios, err := loadScenarios(filepath.Join(root, "evals", "scenarios"), *only)
	if err != nil {
		return err
	}
	stop, err := r.startAgent(ctx)
	if err != nil {
		return err
	}
	defer stop()
	if err := r.prepare(ctx); err != nil {
		r.printLogTail()
		return err
	}
	report := Report{Setup: r.setupName, Models: modelsOf(r.setup), Mira: r.version, Date: time.Now().Format("2006-01-02 15:04"), Runs: *runs}
	for _, sc := range scenarios {
		res := ScenarioResult{Name: sc.Name, Description: sc.Description, Core: sc.Core}
		for i := 0; i < *runs; i++ {
			run := r.runScenario(ctx, sc, i)
			res.Runs = append(res.Runs, run)
			fmt.Printf("%-28s run %d: %s\n", sc.Name, i+1, run.summary())
			if !run.Passed {
				for _, st := range run.Steps {
					fmt.Printf("    » %s\n    « %s\n      %s\n", st.Say, st.Text, strings.Join(st.Trace, " "))
				}
			}
		}
		res.finish(*runs)
		report.Scenarios = append(report.Scenarios, res)
	}
	report.print()
	if *keep {
		return nil
	}
	return report.write(filepath.Join(root, "evals"))
}

func loadScenarios(dir, only string) ([]Scenario, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	want := map[string]bool{}
	for _, n := range strings.Split(only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			want[n] = true
		}
	}
	var out []Scenario
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var sc Scenario
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&sc); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		if len(want) > 0 && !want[sc.Name] {
			continue
		}
		out = append(out, sc)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no scenarios in %s", dir)
	}
	return out, nil
}

func modelsOf(setup map[string]any) []string {
	var out []string
	if llm, ok := setup["llm"].(map[string]any); ok {
		out = append(out, fmt.Sprintf("main: %v", llm["model"]))
	}
	if models, ok := setup["models"].(map[string]any); ok {
		names := make([]string, 0, len(models))
		for n := range models {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if m, ok := models[n].(map[string]any); ok {
				out = append(out, fmt.Sprintf("%s: %v", n, m["model"]))
			}
		}
	}
	if routing, ok := setup["routing"].(map[string]any); ok {
		keys := make([]string, 0, len(routing))
		for k := range routing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, routing[k]))
		}
		out = append(out, "routing: "+strings.Join(parts, ", "))
	}
	return out
}

// startAgent builds Mira and runs it with the test config, the setup's
// models, eval mode and a fresh database.
func (r *evalRun) startAgent(ctx context.Context) (func(), error) {
	tmp, err := os.MkdirTemp("", "mira-eval-")
	if err != nil {
		return nil, err
	}
	bin := filepath.Join(tmp, "mira")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/mira")
	build.Dir, build.Stdout, build.Stderr = r.root, os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	raw, err := os.ReadFile(filepath.Join(r.dir, "mira.yaml"))
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	for _, k := range []string{"llm", "models", "routing"} {
		if v, ok := r.setup[k]; ok {
			cfg[k] = v
		} else {
			delete(cfg, k)
		}
	}
	server, _ := cfg["server"].(map[string]any)
	if server == nil {
		server = map[string]any{}
	}
	server["listen"] = "127.0.0.1:8790"
	server["eval"] = true
	server["announce"] = false
	cfg["server"] = server
	cfg["store"] = map[string]any{"path": filepath.Join(tmp, "eval.db")}
	// The eval agent announces nothing on its own: the test Mira does that.
	delete(cfg, "notifier")
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(tmp, "mira.yaml")
	if err := os.WriteFile(cfgPath, out, 0o600); err != nil {
		return nil, err
	}
	r.agentLog = filepath.Join(tmp, "agent.log")
	logFile, err := os.Create(r.agentLog)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "-config", cfgPath, "run")
	cmd.Dir, cmd.Stdout, cmd.Stderr = r.root, logFile, logFile
	cmd.Env, err = r.agentEnv(string(out))
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			cmd.Process.Kill()
		}
		logFile.Close()
		os.RemoveAll(tmp)
	}
	r.agentURL = "http://127.0.0.1:8790"
	r.token = r.env["MIRA_API_TOKEN"]
	deadline := time.Now().Add(90 * time.Second)
	for {
		var h struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}
		if err := r.agent(ctx, "GET", "/v1/health", nil, &h); err == nil && h.Status != "" {
			r.version = h.Version
			break
		}
		if time.Now().After(deadline) {
			stop()
			return nil, fmt.Errorf("the eval agent did not come up; log: %s", r.agentLog)
		}
		time.Sleep(time.Second)
	}
	fmt.Printf("eval agent %s on %s, setup %s\n", r.version, r.agentURL, r.setupName)
	return stop, nil
}

// prepare hands the eval agent the satellites and person settings the
// integration gave the test Mira, finds the satellites' devices, and warms
// the model: a turn per channel that does not count.
func (r *evalRun) prepare(ctx context.Context) error {
	var managed json.RawMessage
	testMira := "http://127.0.0.1:8788"
	if err := r.call(ctx, "GET", testMira+"/v1/ui/ha", r.token, nil, &managed); err != nil {
		return fmt.Errorf("read the test Mira's settings from Home Assistant: %w", err)
	}
	if err := r.agent(ctx, "PUT", "/v1/ui/ha", managed, nil); err != nil {
		return fmt.Errorf("hand the settings to the eval agent: %w", err)
	}
	var tpl string
	if err := r.ha(ctx, "POST", "/api/template", map[string]any{"template": `{% for s in states.assist_satellite %}{{ area_id(s.entity_id) }}={{ device_id(s.entity_id) }};{% endfor %}`}, &tpl); err != nil {
		return err
	}
	r.devices = map[string]string{}
	for _, part := range strings.Split(tpl, ";") {
		if area, dev, ok := strings.Cut(part, "="); ok && area != "None" && dev != "None" {
			r.devices[area] = dev
		}
	}
	if err := r.agent(ctx, "POST", "/v1/eval/reset", nil, nil); err != nil {
		return err
	}
	fmt.Print("warmup …")
	started := time.Now()
	for area := range r.devices {
		if _, err := r.say(ctx, Step{Say: "Hallo", At: area}, "warmup-voice"); err != nil {
			return fmt.Errorf("warmup: %w", err)
		}
		break
	}
	if _, err := r.say(ctx, Step{Say: "Hallo", Person: "anna", Channel: "telegram"}, "warmup-telegram"); err != nil {
		return fmt.Errorf("warmup: %w", err)
	}
	fmt.Printf(" %s\n", time.Since(started).Round(100*time.Millisecond))
	return nil
}

// turn is what one step got back.
type turn struct {
	Text  string        `json:"text"`
	Trace []trace.Event `json:"trace"`
	Took  time.Duration `json:"-"`
}

func (r *evalRun) say(ctx context.Context, s Step, conversation string) (turn, error) {
	body := map[string]any{"text": s.Say, "conversation_id": conversation, "language": "de"}
	switch {
	case s.Channel == "telegram":
		body["channel"], body["person"] = "telegram", s.Person
	default:
		dev, ok := r.devices[s.At]
		if !ok {
			return turn{}, fmt.Errorf("no satellite in area %q (have %v)", s.At, keys(r.devices))
		}
		body["device_id"] = dev
	}
	var t turn
	started := time.Now()
	err := r.agent(ctx, "POST", "/v1/conversation", body, &t)
	t.Took = time.Since(started)
	return t, err
}

func (r *evalRun) agent(ctx context.Context, method, path string, body, out any) error {
	return r.call(ctx, method, r.agentURL+path, r.token, body, out)
}

func (r *evalRun) ha(ctx context.Context, method, path string, body, out any) error {
	return r.call(ctx, method, haURL+path, r.haToken, body, out)
}

func (r *evalRun) call(ctx context.Context, method, url, token string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, ok := body.(json.RawMessage)
		if !ok {
			var err error
			if raw, err = json.Marshal(body); err != nil {
				return err
			}
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d %s", method, url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if s, ok := out.(*string); ok {
		*s = string(raw)
		return nil
	}
	return json.Unmarshal(raw, out)
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)

// agentEnv is the eval agent's whole environment: a few harmless basics
// and exactly the variables its config refers to, from testenv/.env only.
// Nothing else reaches it, least of all the live instance's secrets.
func (r *evalRun) agentEnv(config string) ([]string, error) {
	var env []string
	for _, k := range []string{"PATH", "HOME", "LANG", "TZ"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, m := range envRef.FindAllStringSubmatch(config, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		v, ok := r.env[name]
		if !ok || v == "" {
			missing = append(missing, name)
			continue
		}
		env = append(env, name+"="+v)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing in testenv/.env: %s", strings.Join(missing, ", "))
	}
	return env, nil
}

// printLogTail shows the end of the eval agent's log, for a failure.
func (r *evalRun) printLogTail() {
	raw, err := os.ReadFile(r.agentLog)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	fmt.Println("--- eval agent log ---\n" + strings.Join(lines, "\n"))
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
