package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/memfile"
	"github.com/miraassist/mira/internal/store"
)

// runMemory is "mira memory export [-o FILE]" and "mira memory import FILE
// [-yes]": the memory as a file, for a fresh setup by script
// (docs/spec/gedaechtnis-datei.md). The command line sees every memory.
func runMemory(parent context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return errors.New("memory needs export or import, e.g. mira memory export -o gedaechtnis.md")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a, err := build(ctx, cfg, buildOptions{})
	if err != nil {
		return err
	}
	defer a.close()
	defer cancel()
	// Home Assistant's persons give the headings their names.
	if _, err := a.people.refresh(ctx, a.ha); err != nil {
		a.log.Warn("could not read the persons from home assistant", "error", err)
	}
	owners, err := a.memoryOwners(ctx)
	if err != nil {
		return err
	}
	switch args[0] {
	case "export":
		text, err := memfile.Write(ctx, a.store, owners)
		if err != nil {
			return err
		}
		if len(args) == 3 && args[1] == "-o" {
			return os.WriteFile(args[2], []byte(text), 0o600)
		}
		if len(args) != 1 {
			return errors.New("usage: mira memory export [-o FILE]")
		}
		_, err = io.WriteString(os.Stdout, text)
		return err
	case "import":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "-yes") {
			return errors.New("usage: mira memory import FILE [-yes]")
		}
		raw, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		sections, err := memfile.Parse(string(raw))
		if err != nil {
			return err
		}
		changes, err := memfile.Plan(ctx, a.store, sections, owners, owners)
		if err != nil {
			return err
		}
		changed := false
		for _, c := range changes {
			name := c.Owner.Name
			if name == "" {
				name = c.Owner.Key
			}
			if c.Owner.Key == store.OwnerHouse {
				name = memfile.HouseHeadings[0]
			}
			fmt.Printf("%s: %d neu, %d entfallen, %d bleiben\n", name, len(c.Add), len(c.Remove), len(c.Keep))
			for _, l := range c.Add {
				fmt.Println("  + " + l.Text)
			}
			for _, f := range c.Remove {
				fmt.Println("  − " + f.Text)
			}
			changed = changed || len(c.Add) > 0 || len(c.Remove) > 0
		}
		if !changed {
			fmt.Println("Die Datei ändert nichts.")
			return nil
		}
		if len(args) == 2 {
			fmt.Print("Übernehmen? [j/N] ")
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if yes := strings.ToLower(strings.TrimSpace(answer)); yes != "j" && yes != "ja" && yes != "y" && yes != "yes" {
				fmt.Println("Nichts geändert.")
				return nil
			}
		}
		if err := memfile.Apply(ctx, a.store, changes, ""); err != nil {
			return err
		}
		fmt.Println("Übernommen.")
		return nil
	default:
		return fmt.Errorf("unknown memory command %q, use export or import", args[0])
	}
}

// memoryOwners are every memory there is: the house, the persons of Home
// Assistant and anyone else with facts, the house first.
func (a *app) memoryOwners(ctx context.Context) ([]memfile.Owner, error) {
	persons := a.persons()
	keys := make([]string, 0, len(persons))
	for k := range persons {
		keys = append(keys, k)
	}
	with, err := a.store.FactOwners(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range with {
		if k != store.OwnerHouse && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	out := []memfile.Owner{{Key: store.OwnerHouse}}
	for _, k := range keys {
		out = append(out, memfile.Owner{Key: k, Name: persons[k].Name})
	}
	return out, nil
}
