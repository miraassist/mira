// Package updates asks Home Assistant to look for the integration's update
// at once when the integration is older than the Mira service
// (docs/spec/versions.md, Both updates offered together): the marketplace
// (Home Assistant 2026.11) or HACS check installed repositories only every
// 48 hours and at a start of Home Assistant, so after the service's update the
// integration's would be offered late.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"sync"

	haws "github.com/miraassist/mira/internal/ha/ws"
)

// Repository is Mira's repository at GitHub, as the marketplace lists it.
const Repository = "miraassist/mira"

// HA is the part of Home Assistant's client used.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
}

// Watcher sees the integration's version on requests and asks once per
// version of the service.
type Watcher struct {
	Version string // the service's
	HA      HA
	Log     *slog.Logger

	mu    sync.Mutex
	asked bool
}

var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.(\d+))?`)

// parts is major, minor and patch of a version like 0.9.1 or v0.9 (a missing
// patch is 0).
func parts(v string) ([3]int, bool) {
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var p [3]int
	for i := range p {
		p[i], _ = strconv.Atoi(m[i+1])
	}
	return p, true
}

// Older says whether version a is older than b, a patch counting too: the
// repair leaves a patch apart alone, but the update is offered for it as well,
// and asking for it disturbs nobody (docs/spec/versions.md).
func Older(a, b string) bool {
	pa, ok1 := parts(a)
	pb, ok2 := parts(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

// Seen is called with the integration's version from a request
// (X-Mira-Integration); "" (an older integration) does nothing. It returns
// at once: the asking runs on its own.
func (w *Watcher) Seen(ctx context.Context, integration string) {
	if w == nil || integration == "" || w.HA == nil || !Older(integration, w.Version) {
		return
	}
	w.mu.Lock()
	if w.asked {
		w.mu.Unlock()
		return
	}
	w.asked = true
	w.mu.Unlock()
	go w.ask(context.WithoutCancel(ctx), integration)
}

func (w *Watcher) ask(ctx context.Context, integration string) {
	via, err := w.refresh(ctx, "marketplace")
	var he *haws.Error
	if errors.As(err, &he) && he.Code == "unknown_command" {
		// Before Home Assistant 2026.11: HACS has the same commands.
		via, err = w.refresh(ctx, "hacs")
	}
	if err != nil {
		w.Log.Warn("could not ask for the integration's update; it comes at Home Assistant's next check",
			"integration", integration, "service", w.Version, "error", err)
		return
	}
	w.Log.Info("asked for the integration's update", "via", via, "integration", integration, "service", w.Version)
}

// refresh finds Mira's repository in the list of prefix (marketplace or
// hacs) and has it looked at again.
func (w *Watcher) refresh(ctx context.Context, prefix string) (string, error) {
	cmd := map[string]any{"type": prefix + "/repositories/list"}
	if prefix == "marketplace" {
		cmd["categories"] = []string{"integration"}
	}
	raw, err := w.HA.Call(ctx, cmd)
	if err != nil {
		return prefix, err
	}
	var list []struct {
		ID       any    `json:"id"`
		FullName string `json:"full_name"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return prefix, fmt.Errorf("%s's list not understood: %w", prefix, err)
	}
	for _, r := range list {
		if r.FullName == Repository {
			if _, err := w.HA.Call(ctx, map[string]any{"type": prefix + "/repository/refresh", "repository": fmt.Sprint(r.ID)}); err != nil {
				return prefix, err
			}
			return prefix, nil
		}
	}
	return prefix, fmt.Errorf("%s does not list %s", prefix, Repository)
}
