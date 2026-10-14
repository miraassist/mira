// Command lares is the household agent.
//
//	lares [-config lares.yaml] run          start the agent (default)
//	lares [-config lares.yaml] chat TEXT    one turn through the full stack, printed
//	lares [-config lares.yaml] check        validate the config and probe every dependency
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sker65/lares-agent/internal/config"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lares:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultConfig := os.Getenv("LARES_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "lares.yaml"
	}
	configPath := flag.String("config", defaultConfig, "path to the YAML config (env LARES_CONFIG)")
	channel := flag.String("channel", "telegram", "chat: channel whose style and slot to use (voice, telegram)")
	session := flag.String("session", "cli", "chat: conversation id; reuse it to continue a conversation")
	device := flag.String("device", "", "chat: satellite device id, for -channel voice")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: lares [flags] [run | chat TEXT | check]\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := flag.Args()
	cmd := "run"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "run":
		return runAgent(ctx, cfg)
	case "chat":
		if len(args) == 0 {
			return errors.New("chat needs a message, e.g. lares chat \"Wie warm ist es im Wohnzimmer?\"")
		}
		return runChat(ctx, cfg, config.Channel(*channel), *session, *device, strings.Join(args, " "))
	case "check":
		return runCheck(ctx, cfg)
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}
