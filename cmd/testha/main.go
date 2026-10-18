// Command testha runs the test Home Assistant in testenv/ for tests against a
// real HA: same version as the house, demo devices, simulated sensors, and a
// non-admin user "mira" like in the house.
//
//	go run ./cmd/testha up            start, onboard and seed; writes testenv/.env
//	go run ./cmd/testha set ID STATE  flip a simulated sensor (on/off) or set any state
//	go run ./cmd/testha env           print testenv/.env
//	go run ./cmd/testha down          stop the container, keep its state
//	go run ./cmd/testha reset         stop and delete all state; next up starts fresh
//
// Everything up does is idempotent: it only creates what is missing.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "testha:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dir, err := testenvDir()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"up"}
	}
	switch args[0] {
	case "up":
		return up(ctx, dir)
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: testha set ENTITY_ID STATE")
		}
		return set(ctx, dir, args[1], args[2])
	case "env":
		b, err := os.ReadFile(filepath.Join(dir, ".env"))
		if err != nil {
			return fmt.Errorf("no testenv/.env yet, run up first: %w", err)
		}
		_, err = os.Stdout.Write(b)
		return err
	case "down":
		return compose(ctx, dir, "down")
	case "reset":
		return reset(ctx, dir)
	case "dashboard":
		return dashboardCmd(ctx, dir)
	case "eval":
		return evalCmd(ctx, dir, args[1:])
	default:
		return fmt.Errorf("unknown command %q (up, set, env, down, reset, dashboard, eval)", args[0])
	}
}

// testenvDir finds testenv/ from the working directory or one of its parents,
// so the command works from anywhere in the repo.
func testenvDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; d = filepath.Dir(d) {
		if st, err := os.Stat(filepath.Join(d, "testenv", "compose.yaml")); err == nil && !st.IsDir() {
			return filepath.Join(d, "testenv"), nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("testenv/compose.yaml not found above %s", wd)
		}
	}
}

func isSimulated(entityID string) (string, bool) {
	// binary_sensor.lecksensor_keller is driven by input_boolean.sim_leck_keller.
	for sensor, sim := range simulated {
		if sensor == entityID {
			return sim, true
		}
	}
	if strings.HasPrefix(entityID, "input_boolean.sim_") {
		return entityID, true
	}
	return "", false
}
