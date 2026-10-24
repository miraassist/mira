package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/miraassist/mira/internal/announce"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/store"
)

// prepared is the config as the agent runs with it: mira.yaml, if there is
// one, with what the integration set up laid over it
// (docs/spec/einrichtung.md).
type prepared struct {
	cfg config.Config
	// pairing: the token is the agent's own, so Home Assistant pairs (the
	// first time without a code). A token in mira.yaml is entered instead.
	pairing bool
	// open: never paired, so the first pairing needs no code.
	open bool
	// ready: Home Assistant and a model are set; otherwise the agent
	// waits to be set up.
	ready    bool
	hasModel bool
	// code: a pairing code from mira pair, while it holds.
	code string
	// path is the mira.yaml Mira read and the page writes
	// (docs/spec/bearbeiten.md).
	path string
}

func prepare(ctx context.Context, path string) (prepared, error) {
	cfg, hasLLM, err := config.LoadOptional(path)
	if err != nil {
		return prepared{}, err
	}
	p := prepared{pairing: cfg.Server.Token == "", path: config.ConfigPath(path)}
	st, err := store.Open(ctx, store.Options{Path: cfg.Store.Path})
	if err != nil {
		return prepared{}, err
	}
	defer st.Close()
	s, err := setup.Load(ctx, st)
	if err != nil {
		return prepared{}, err
	}
	p.cfg = setup.Apply(cfg, s)
	if err := p.cfg.Validate(); err != nil {
		return prepared{}, err
	}
	p.ready = setup.Ready(p.cfg, hasLLM, s)
	if cfg.LacksMain() && s.LLM == nil {
		// MO-02: main comes from the integration; until it is set up, Mira
		// waits for the setup and says why.
		slog.Warn("mira.yaml has models but not main, and the integration has set no model yet: Mira waits for the setup in Home Assistant (or add models.main to mira.yaml)")
	}
	p.hasModel = hasLLM || s.LLM != nil
	if p.pairing {
		p.open = s.Open
		if p.code, err = setup.PendingCode(ctx, st, time.Now()); err != nil {
			return prepared{}, err
		}
	}
	return p, nil
}

// errNotSetUp is for commands that need a set-up agent.
var errNotSetUp = errors.New("Mira is not set up yet: add Mira in Home Assistant (Settings → Devices & services)")

// restart starts the agent anew with what setup saved: the same program,
// arguments and environment.
func restart() {
	exe, err := os.Executable()
	if err != nil {
		slog.Error("cannot restart", "error", err)
		os.Exit(1)
	}
	slog.Info("starting anew with the new settings")
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		slog.Error("cannot restart", "error", err)
		os.Exit(1)
	}
}

// model is the model set, or "" while there is none.
func (p prepared) model() string {
	if !p.hasModel {
		return ""
	}
	return p.cfg.Main().Model
}

func logPairing(log *slog.Logger, p prepared) {
	switch {
	case p.code != "":
		log.Info(fmt.Sprintf("pairing code: %s %s (Home Assistant: Settings → Devices & services → Mira; valid 15 minutes)",
			p.code[:3], p.code[3:]))
	case p.open:
		log.Info("not paired yet: add Mira in Home Assistant under Settings → Devices & services (Discovered)")
	}
}

// runSetupMode serves only what setup needs until the integration has set
// the agent up; then it starts anew.
func runSetupMode(ctx context.Context, p prepared) error {
	log := slog.Default()
	st, err := store.Open(ctx, store.Options{Path: p.cfg.Store.Path})
	if err != nil {
		return err
	}
	defer st.Close()
	id, err := st.InstanceID(ctx)
	if err != nil {
		return err
	}
	log.Info("waiting to be set up by Home Assistant", "listen", p.cfg.Server.Listen)
	logPairing(log, p)
	mux := http.NewServeMux()
	(&setup.Handler{Store: st, Token: p.cfg.Server.Token, ID: id, Version: version, Pairing: p.pairing,
		SetupMode: true, Model: p.model(), Restart: restart, Log: log}).Register(mux)
	ln, err := net.Listen("tcp", p.cfg.Server.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if p.cfg.Server.Announce && !announce.Loopback(p.cfg.Server.Listen) {
		go func() {
			if err := announce.Run(ctx, announce.Options{Instance: p.cfg.Server.Name, ID: id, Version: version,
				Listen: p.cfg.Server.Listen, Logger: log}); err != nil && ctx.Err() == nil {
				log.Warn("mdns announcement failed", "error", err)
			}
		}()
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// runPair makes a pairing code, for pairing Home Assistant anew with an
// agent that was paired before.
func runPair(ctx context.Context, p prepared) error {
	if !p.pairing {
		return errors.New("this agent's token is set in mira.yaml: enter that token in Home Assistant instead of a code")
	}
	st, err := store.Open(ctx, store.Options{Path: p.cfg.Store.Path})
	if err != nil {
		return err
	}
	defer st.Close()
	code, err := setup.NewCode(ctx, st, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("Pairing code: %s %s (valid 15 minutes; enter it in Home Assistant when adding Mira)\n", code[:3], code[3:])
	return nil
}
