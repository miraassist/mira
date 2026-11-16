// Package api registers the routes of the Mira service's HTTP interface and
// describes them as OpenAPI (docs/spec/openapi.md). Every route reaches a
// mux through Mux.Handle (or HandleIf, Before, Hidden, Mount), which knows
// its method, path, summary and the Go types of its request and answer; the
// handlers stay as they are.
package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Tags group the routes in the description.
const (
	TagIntegration = "integration"
	TagPage        = "page"
	TagSetup       = "setup"
	TagAccess      = "access"
	TagHA          = "home-assistant"
	TagSkills      = "skills"
	TagDescription = "description"
)

// Route is one route of the interface.
type Route struct {
	Method string
	// Path as Go's ServeMux takes it, with {name} and {name...} wildcards.
	Path        string
	Summary     string
	Description string
	Tag         string
	Query       []Param
	// Request is a value of the body's Go type; nil for a route without a
	// body. RequestMedia is its media type when it is not JSON.
	Request      any
	RequestMedia string
	// Response is a value of the answer's Go type; nil for an answer
	// without a body. ResponseMedia is its media type when it is not JSON.
	Response      any
	ResponseMedia string
	// Status is the answer's status when it is not 200.
	Status int
	// Errors are the statuses the route's spec names; each answers Mira's
	// error body.
	Errors []int
	// Open is a route without a token.
	Open bool
	// Only says when the route exists, for one registered only in some
	// setups ("with eval mode").
	Only string
}

// Param is one query parameter.
type Param struct {
	Name        string
	Description string
	Required    bool
}

// Pattern is the route as Go's ServeMux takes it.
func (r Route) Pattern() string { return r.Method + " " + r.Path }

// Mux is the service's only kind of mux: it keeps every route it serves.
type Mux struct {
	mux    *http.ServeMux
	routes []Route
	// off are routes described but not served in this setup (HandleIf).
	off []Route
	// hidden are patterns served but not described, with the reason.
	hidden map[string]string
	subs   []*Mux
}

// NewMux makes an empty mux.
func NewMux() *Mux { return &Mux{mux: http.NewServeMux(), hidden: map[string]string{}} }

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.mux.ServeHTTP(w, r) }

// Handle serves h at the route and keeps the route: the one way a route of
// the interface is registered.
func (m *Mux) Handle(r Route, h http.HandlerFunc) {
	m.mux.HandleFunc(r.Pattern(), h)
	m.routes = append(m.routes, r)
}

// HandleIf is Handle for a route that exists only in some setups (r.Only
// says which): on false it is described but not served.
func (m *Mux) HandleIf(on bool, r Route, h http.HandlerFunc) {
	if on {
		m.Handle(r, h)
		return
	}
	m.off = append(m.off, r)
}

// Before describes a route answered in front of this mux, by a wrapper
// that sees every call first (the access check's GET /v1/whoami, the
// inbox's webhook with its own token).
func (m *Mux) Before(r Route) { m.routes = append(m.routes, r) }

// Hidden serves a pattern that the description leaves out, with the reason
// (the page's files, the setup mode's own page).
func (m *Mux) Hidden(pattern, reason string, h http.Handler) {
	m.mux.Handle(pattern, h)
	m.hidden[pattern] = reason
}

// Mount serves h at the patterns; the routes of sub, the mux behind h,
// count as this mux's.
func (m *Mux) Mount(h http.Handler, sub *Mux, patterns ...string) {
	for _, p := range patterns {
		m.mux.Handle(p, h)
	}
	m.subs = append(m.subs, sub)
}

// Routes are the routes this mux serves, its mounted muxes' too, sorted by
// path and method.
func (m *Mux) Routes() []Route { return m.collect(false) }

// AllRoutes are Routes and the ones that exist only in other setups.
func (m *Mux) AllRoutes() []Route { return m.collect(true) }

func (m *Mux) collect(all bool) []Route {
	out := slices.Clone(m.routes)
	if all {
		out = append(out, m.off...)
	}
	for _, s := range m.subs {
		out = append(out, s.collect(all)...)
	}
	slices.SortStableFunc(out, func(a, b Route) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Method, b.Method)
	})
	return out
}

// HiddenPatterns lists the patterns served without a description, with the reason,
// mounted muxes' too.
func (m *Mux) HiddenPatterns() map[string]string {
	out := map[string]string{}
	for p, why := range m.hidden {
		out[p] = why
	}
	for _, s := range m.subs {
		for p, why := range s.HiddenPatterns() {
			out[p] = why
		}
	}
	return out
}

// Check finds a route described twice; the description holds each method
// and path once.
func Check(routes []Route) error {
	seen := map[string]bool{}
	for _, r := range routes {
		if seen[r.Pattern()] {
			return fmt.Errorf("route %s is registered twice", r.Pattern())
		}
		seen[r.Pattern()] = true
	}
	return nil
}
