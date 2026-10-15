package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	haws "github.com/sker65/lares/internal/ha/ws"
)

const (
	haURL    = "http://localhost:8123"
	clientID = "http://lares-testha/"
)

// env is testenv/.env: the generated passwords and tokens.
type env map[string]string

func loadEnv(dir string) env {
	e := env{}
	f, err := os.Open(filepath.Join(dir, ".env"))
	if err != nil {
		return e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if k, v, ok := strings.Cut(s.Text(), "="); ok && !strings.HasPrefix(k, "#") {
			e[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return e
}

func (e env) save(dir string) error {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Written by `go run ./cmd/testha up`. Test-only secrets; not in git.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, e[k])
	}
	return os.WriteFile(filepath.Join(dir, ".env"), []byte(b.String()), 0o600)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func compose(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", filepath.Join(dir, "compose.yaml")}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// reset deletes the runtime state. HA runs as root in the container, so its
// files are root-owned; a throwaway container removes them.
func reset(ctx context.Context, dir string) error {
	if err := compose(ctx, dir, "down"); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", filepath.Join(dir, "ha")+":/config",
		"--entrypoint", "sh", "ghcr.io/home-assistant/home-assistant:2026.10.0",
		"-c", "cd /config && find . -mindepth 1 -maxdepth 1 ! -name configuration.yaml -exec rm -rf {} +")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, ".env")); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("test HA reset; `testha up` starts it fresh")
	return nil
}

// api does one REST call. body is JSON unless it is url.Values.
func api(ctx context.Context, method, path, token string, body any, out any) error {
	var r io.Reader
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case url.Values:
		r, contentType = strings.NewReader(b.Encode()), "application/x-www-form-urlencoded"
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, haURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func waitForHA(ctx context.Context) error {
	deadline := time.Now().Add(3 * time.Minute)
	for {
		var steps []map[string]any
		err := api(ctx, "GET", "/api/onboarding", "", nil, &steps)
		if err == nil {
			return nil
		}
		// Onboarded instances answer the onboarding view with 401/404 once
		// they are up; anything from HA itself means it is running.
		if strings.Contains(err.Error(), ": 40") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("home assistant did not come up: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// exchange turns an auth code into a short-lived access token.
func exchange(ctx context.Context, code string) (string, error) {
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	err := api(ctx, "POST", "/auth/token", "", url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
	}, &tok)
	return tok.AccessToken, err
}

// login runs the login flow of the homeassistant auth provider.
func login(ctx context.Context, username, password string) (string, error) {
	var flow struct {
		FlowID string `json:"flow_id"`
		Type   string `json:"type"`
		Result string `json:"result"`
		Errors any    `json:"errors"`
	}
	if err := api(ctx, "POST", "/auth/login_flow", "", map[string]any{
		"client_id": clientID, "handler": []any{"homeassistant", nil}, "redirect_uri": clientID,
	}, &flow); err != nil {
		return "", err
	}
	if err := api(ctx, "POST", "/auth/login_flow/"+flow.FlowID, "", map[string]any{
		"client_id": clientID, "username": username, "password": password,
	}, &flow); err != nil {
		return "", err
	}
	if flow.Type != "create_entry" {
		return "", fmt.Errorf("login of %s failed: %v", username, flow.Errors)
	}
	return exchange(ctx, flow.Result)
}

// onboard creates the owner and finishes the remaining onboarding steps.
func onboard(ctx context.Context, password string) (string, error) {
	var steps []struct {
		Step string `json:"step"`
		Done bool   `json:"done"`
	}
	if err := api(ctx, "GET", "/api/onboarding", "", nil, &steps); err != nil {
		return "", err
	}
	var token string
	for _, s := range steps {
		if s.Done {
			continue
		}
		switch s.Step {
		case "user":
			var res struct {
				AuthCode string `json:"auth_code"`
			}
			if err := api(ctx, "POST", "/api/onboarding/users", "", map[string]any{
				"client_id": clientID, "name": "Admin", "username": "admin", "password": password, "language": "de",
			}, &res); err != nil {
				return "", err
			}
			var err error
			if token, err = exchange(ctx, res.AuthCode); err != nil {
				return "", err
			}
		case "core_config", "analytics":
			if err := api(ctx, "POST", "/api/onboarding/"+s.Step, token, map[string]any{}, nil); err != nil {
				return "", err
			}
		case "integration":
			if err := api(ctx, "POST", "/api/onboarding/integration", token, map[string]any{
				"client_id": clientID, "redirect_uri": clientID,
			}, nil); err != nil {
				return "", err
			}
		}
	}
	if token == "" {
		return login(ctx, "admin", password)
	}
	return token, nil
}

// connect opens a websocket client with token and returns it with its stop.
func connect(ctx context.Context, token string) (*haws.Client, func(), error) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := haws.New(haws.Options{URL: haURL, Token: token, Logger: log})
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	wait, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()
	if err := c.WaitReady(wait); err != nil {
		cancel()
		select {
		case runErr := <-done:
			if runErr != nil && ctx.Err() == nil {
				return nil, nil, runErr
			}
		default:
		}
		return nil, nil, fmt.Errorf("websocket: %w", err)
	}
	return c, cancel, nil
}

// call is Client.Call decoding the result into out (may be nil).
func call(ctx context.Context, c *haws.Client, cmd map[string]any, out any) error {
	raw, err := c.Call(ctx, cmd)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd["type"], err)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func longLivedToken(ctx context.Context, c *haws.Client, name string) (string, error) {
	var token string
	err := call(ctx, c, map[string]any{"type": "auth/long_lived_access_token", "client_name": name, "lifespan": 3650}, &token)
	return token, err
}

// tokenWorks reports whether token is accepted by the REST API.
func tokenWorks(ctx context.Context, token string) bool {
	return token != "" && api(ctx, "GET", "/api/", token, nil, nil) == nil
}
