// Command mira is the household agent.
//
//	mira [-config mira.yaml] run          start the agent (default)
//	mira [-config mira.yaml] chat TEXT    one turn through the full stack, printed
//	mira [-config mira.yaml] check        validate the config and probe every dependency
//	mira [-config mira.yaml] pair         a new pairing code for Home Assistant
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/miraassist/mira/internal/config"
)

// version is set at build time with -ldflags "-X main.version=...".
// version is Mira's version: raised with every change that goes live, so
// the web UI and /v1/health show at once which build runs. A release build
// may still set it with -ldflags "-X main.version=...".
var version = "0.6.52"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mira:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultConfig := os.Getenv("MIRA_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "mira.yaml"
	}
	configPath := flag.String("config", defaultConfig, "path to the YAML config (env MIRA_CONFIG)")
	channel := flag.String("channel", "telegram", "chat: channel whose style and slot to use (voice, telegram)")
	session := flag.String("session", "cli", "chat: conversation id; reuse it to continue a conversation")
	device := flag.String("device", "", "chat: satellite device id, for -channel voice")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: mira [flags] [run | chat TEXT | check | profile | skill NAME [N] | pair]\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// mira.yaml is optional: without it the integration sets the agent up
	// (docs/spec/einrichtung.md).
	p, err := prepare(ctx, *configPath)
	if err != nil {
		return err
	}
	cfg := p.cfg

	args := flag.Args()
	cmd := "run"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "run":
		if !p.ready {
			return runSetupMode(ctx, p)
		}
		return runAgent(ctx, cfg, p)
	case "pair":
		return runPair(ctx, p)
	}
	if !p.ready {
		return errNotSetUp
	}
	switch cmd {
	case "chat":
		if len(args) == 0 {
			return errors.New("chat needs a message, e.g. mira chat \"Wie warm ist es im Wohnzimmer?\"")
		}
		return runChat(ctx, cfg, config.Channel(*channel), *session, *device, strings.Join(args, " "))
	case "check":
		return runCheck(ctx, cfg)
	case "profile":
		return runProfile(ctx, cfg)
	case "skill":
		if len(args) == 0 {
			return errors.New("skill needs a name, e.g. mira skill abfall")
		}
		index := 0
		if len(args) > 1 {
			if index, err = strconv.Atoi(args[1]); err != nil {
				return fmt.Errorf("schedule number: %w", err)
			}
		}
		return runSkill(ctx, cfg, args[0], index)
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}
