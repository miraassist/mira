// Package routes puts the service's HTTP interface together from its parts,
// for the running service and for its description (docs/spec/openapi.md):
// both build the same mux, so the description is the service's.
package routes

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/miraassist/mira/internal/a2a"
	"github.com/miraassist/mira/internal/access"
	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/ui"
)

// Parts are the handlers of the interface.
type Parts struct {
	// Integration is the integration's interface, behind its token check.
	Integration *haconv.Server
	// Page is Mira's page and its routes.
	Page *ui.Handler
	// Setup is pairing and setup.
	Setup *setup.Handler
	// Push takes the remote agents' pushes, with their call's token
	// (docs/spec/remote-agents.md, E); nil answers 404.
	Push http.HandlerFunc
	// Version is the service's, for the description.
	Version string
	Log     *slog.Logger
}

// Mux puts the parts together.
func Mux(p Parts) *api.Mux {
	m := api.NewMux()
	m.Mount(p.Page, p.Page.Mux(), "/ui", "/ui/", "/v1/ui/")
	p.Setup.Register(m)
	push := p.Push
	if push == nil {
		push = http.NotFound
	}
	m.Handle(api.Route{Method: http.MethodPost, Path: a2a.PushPath + "{call}", Tag: api.TagIntegration, Open: true,
		Summary: "A remote agent's push for a call (A2A)",
		Description: "Takes the call's own token in A2A-Notification-Token or as bearer, not the service's (docs/spec/remote-agents.md). " +
			"The body is an A2A StreamResponse as the A2A protocol defines it (a task, a message, a status or an artifact update): " +
			"the remote agent's protocol, not Mira's, so it is described as an open object.",
		Request: a2a.Push{}, Errors: []int{400, 401}}, push)
	// Answered by the access check in front of the mux
	// (docs/spec/access-tokens.md).
	m.Before(api.Route{Method: http.MethodGet, Path: "/v1/whoami", Tag: api.TagAccess,
		Summary: "The calling token's name and kind", Response: access.Who{}})
	log := p.Log
	if log == nil {
		log = slog.Default()
	}
	api.Serve(m, m.Routes, p.Version, func(path string, err error) {
		log.Error("the description could not be made; the caller gets 500", "path", path, "error", err)
	})
	m.Mount(p.Integration, p.Integration.Mux(), "/")
	return m
}

// Description is the description of every route the service can have,
// built from its parts as the service builds them, without connecting
// anything: what go run ./cmd/openapi writes.
func Description(version string) ([]byte, error) {
	return api.YAML(All(version), version)
}

// All is every route the service can have, the ones only some setups
// serve too.
func All(version string) []api.Route {
	m := Mux(Parts{
		Integration: haconv.New(haconv.Options{}),
		Page:        ui.New(ui.Options{}),
		Setup:       &setup.Handler{},
		Version:     version,
	})
	return m.AllRoutes()
}

// ServiceVersion reads the service's version from the Go file that sets it
// (cmd/mira's `var version = "…"`), for the description's info.version.
func ServiceVersion(file string) (string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		return "", fmt.Errorf("read the service's version from %s: %w", file, err)
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range g.Specs {
			v, ok := s.(*ast.ValueSpec)
			if !ok || len(v.Names) != 1 || v.Names[0].Name != "version" || len(v.Values) != 1 {
				continue
			}
			if lit, ok := v.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				return strconv.Unquote(lit.Value)
			}
		}
	}
	return "", fmt.Errorf("%s sets no version as a string (var version = \"…\")", file)
}

// Compare says how a description on disk differs from the generated one:
// nil when they are equal, else the first line that differs and the
// command that writes it anew.
func Compare(file string, onDisk, generated []byte) error {
	if bytes.Equal(onDisk, generated) {
		return nil
	}
	have, want := strings.Split(string(onDisk), "\n"), strings.Split(string(generated), "\n")
	for i := 0; i < len(have) || i < len(want); i++ {
		var h, w string
		if i < len(have) {
			h = have[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if h != w {
			return fmt.Errorf("%s is not the code's description: line %d is %q, the code says %q; run go run ./cmd/openapi",
				file, i+1, h, w)
		}
	}
	return fmt.Errorf("%s is not the code's description; run go run ./cmd/openapi", file)
}
