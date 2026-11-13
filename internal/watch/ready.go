package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/miraassist/mira/internal/excerpt"
	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/retry"
)

// HA is what the blueprints need of Home Assistant: its blueprint commands.
type HA interface {
	Call(ctx context.Context, command map[string]any) (json.RawMessage, error)
}

// Policy is decision 17: a lost connection or a timeout is tried again, at
// most 3 times, waiting 1 s, then 2 s; a refusal ends at once.
var Policy = retry.Policy{Attempts: 3, First: time.Second, Growth: retry.Exponential}

type Options struct {
	HA HA
	// Templates renders a group watch's selection and the house's floors,
	// areas and group helpers (docs/spec/group-watch.md); without it no
	// group watch is drafted.
	Templates Templates
	Lang      i18n.Lang
	Logger    *slog.Logger
	// Policy replaces the package's Policy (tests: no waits).
	Policy *retry.Policy
}

// Blueprints makes sure Home Assistant has Mira's blueprints in this
// version and tells which are ready.
type Blueprints struct {
	opts   Options
	log    *slog.Logger
	policy retry.Policy

	mu sync.Mutex
	// ready are the blueprints Home Assistant has in this version; failed
	// the ones that could not be made ready, with why.
	ready  map[string]bool
	failed map[string]error
}

func New(opts Options) *Blueprints {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	p := Policy
	if opts.Policy != nil {
		p = *opts.Policy
	}
	return &Blueprints{opts: opts, log: opts.Logger.With("component", "watch"), policy: p,
		ready: map[string]bool{}, failed: map[string]error{}}
}

// call sends one command, tried again by decision 17. A refusal of Home
// Assistant classifies itself (ws.Error.Retryable); refuse marks every
// refusal as fatal, for a command whose refusals are about its request.
func (w *Blueprints) call(ctx context.Context, what, path string, cmd map[string]any, refuse bool) (json.RawMessage, error) {
	var out json.RawMessage
	err := retry.Do(ctx, w.policy, func(ctx context.Context) error {
		raw, err := w.opts.HA.Call(ctx, cmd)
		if err != nil {
			var refusal *haws.Error
			if refuse && errors.As(err, &refusal) {
				return retry.Fatal(err)
			}
			return err
		}
		out = raw
		return nil
	}, retry.Warn(w.log, what, w.policy, "blueprint", path))
	return out, err
}

// listed is an entry of blueprint/list: the metadata, or the error Home
// Assistant found in the file.
type listed struct {
	Metadata struct {
		Description string `json:"description"`
	} `json:"metadata"`
	Error string `json:"error"`
}

// Ready is step A2: it lists the blueprints Home Assistant has and saves
// every one of Mira's that is missing or of another version (a changed one
// with allow_override, step D; Home Assistant then reloads the automations
// using it itself). Each failure is logged with the blueprint's path and
// Home Assistant's answer; the error joins them. Done at the service's
// start and again at first use while one is not ready.
func (w *Blueprints) Ready(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.readyLocked(ctx)
}

func (w *Blueprints) readyLocked(ctx context.Context) error {
	raw, err := w.call(ctx, "listing the blueprints", Folder, map[string]any{"type": "blueprint/list", "domain": "automation"}, false)
	var have map[string]listed
	if err == nil {
		if err = json.Unmarshal(raw, &have); err != nil {
			err = fmt.Errorf("blueprint/list answered %q: %w", excerpt.Bytes(raw), err)
		}
	}
	if err != nil {
		w.log.Error("could not list home assistant's blueprints; no watch is drafted from a blueprint until they are ready", "error", err)
		for _, b := range All {
			if !w.ready[b.Name] {
				w.failed[b.Name] = err
			}
		}
		return err
	}
	var errs []error
	for _, b := range All {
		entry, exists := have[b.Path()]
		if exists && entry.Error == "" && VersionOf(entry.Metadata.Description) == b.Version {
			w.ready[b.Name] = true
			delete(w.failed, b.Name)
			continue
		}
		cmd := map[string]any{"type": "blueprint/save", "domain": "automation", "path": Folder + b.Name, "yaml": b.yaml}
		if exists {
			cmd["allow_override"] = true
		}
		if _, err := w.call(ctx, "saving a blueprint", b.Path(), cmd, false); err != nil {
			if exists {
				// Step D: the old one stays in use; tried again at the next start.
				w.log.Warn("could not save a changed blueprint; the old one stays in use", "blueprint", b.Path(),
					"have", VersionOf(entry.Metadata.Description), "want", b.Version, "error", err)
				w.ready[b.Name] = false
			} else {
				w.log.Error("could not save a blueprint; no watch is drafted from it", "blueprint", b.Path(), "error", err)
			}
			w.failed[b.Name] = err
			errs = append(errs, fmt.Errorf("%s: %w", b.Path(), err))
			continue
		}
		if exists {
			w.log.Info("blueprint updated; home assistant reloads the automations using it", "blueprint", b.Path(),
				"from", VersionOf(entry.Metadata.Description), "to", b.Version)
		} else {
			w.log.Info("blueprint saved", "blueprint", b.Path(), "version", b.Version)
		}
		w.ready[b.Name] = true
		delete(w.failed, b.Name)
	}
	return errors.Join(errs...)
}

// Available are the blueprints ready in Home Assistant; while one is not,
// they are made ready first (first use). A failure is logged by Ready; the
// ones that are ready are still offered.
func (w *Blueprints) Available(ctx context.Context) []Blueprint {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.ready) < len(All) || len(w.failed) > 0 {
		_ = w.readyLocked(ctx) // logged there
	}
	var out []Blueprint
	for _, b := range All {
		if w.ready[b.Name] {
			out = append(out, b)
		}
	}
	return out
}

// isReady says whether a blueprint is ready, and if not, why.
func (w *Blueprints) isReady(name string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ready[name], w.failed[name]
}

// Substitute is step A4: Home Assistant renders the blueprint with the
// inputs and refuses a missing one. A refusal is about the inputs and is not
// tried again.
func (w *Blueprints) Substitute(ctx context.Context, b Blueprint, inputs map[string]any) (map[string]any, error) {
	raw, err := w.call(ctx, "rendering a blueprint", b.Path(), map[string]any{
		"type": "blueprint/substitute", "domain": "automation", "path": b.Path(), "input": inputs}, true)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Path(), err)
	}
	var res struct {
		Config map[string]any `json:"substituted_config"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.Config == nil {
		return nil, fmt.Errorf("blueprint/substitute of %s answered %q: %v", b.Path(), excerpt.Bytes(raw), err)
	}
	return res.Config, nil
}
