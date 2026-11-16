// Command openapi writes the Mira service's OpenAPI description,
// docs/openapi.yaml, from the routes as the service registers them
// (docs/spec/openapi.md). Run it from the repository's root:
//
//	go run ./cmd/openapi
package main

import (
	"fmt"
	"os"

	"github.com/miraassist/mira/internal/routes"
)

// File is where the description goes, and the service's version comes from.
const (
	File      = "docs/openapi.yaml"
	VersionGo = "cmd/mira/main.go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		os.Exit(1)
	}
}

func run() error {
	version, err := routes.ServiceVersion(VersionGo)
	if err != nil {
		return err
	}
	out, err := routes.Description(version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(File, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", File, err)
	}
	fmt.Printf("wrote %s (version %s)\n", File, version)
	return nil
}
