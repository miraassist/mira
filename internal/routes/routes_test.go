package routes

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/channels/haconv"
	"github.com/miraassist/mira/internal/setup"
	"github.com/miraassist/mira/internal/ui"
)

// described is the generated description as JSON.
func described(t *testing.T, routes []api.Route) map[string]any {
	t.Helper()
	raw, err := api.JSON(routes, "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func operation(doc map[string]any, method, path string) map[string]any {
	paths, _ := doc["paths"].(map[string]any)
	item, _ := paths[path].(map[string]any)
	op, _ := item[strings.ToLower(method)].(map[string]any)
	return op
}

func TestOA01_EveryRouteIsDescribed(t *testing.T) {
	all := All("test")
	if err := api.Check(all); err != nil {
		t.Fatal(err)
	}
	doc := described(t, all)
	for _, r := range all {
		if !strings.HasPrefix(r.Path, "/v1/") {
			t.Errorf("%s is not under /v1/", r.Pattern())
		}
		if r.Summary == "" || r.Tag == "" {
			t.Errorf("%s has no summary or tag", r.Pattern())
		}
		path := strings.ReplaceAll(r.Path, "...}", "}")
		op := operation(doc, r.Method, path)
		if op == nil {
			t.Errorf("%s is not in the description", r.Pattern())
			continue
		}
		if r.Request != nil && op["requestBody"] == nil {
			t.Errorf("%s: its request is missing", r.Pattern())
		}
		if resp, _ := op["responses"].(map[string]any); len(resp) == 0 {
			t.Errorf("%s: no answers", r.Pattern())
		}
	}
	// Every route the service registers, in every setup, is among them: the
	// running service's routes with every option on.
	want := map[string]bool{}
	for _, r := range all {
		want[r.Pattern()] = true
	}
	for _, p := range []string{"GET /v1/ha/states/{entity_id}", "POST /v1/ha/services/{domain}/{service}",
		"POST /v1/conversation", "GET /v1/status", "GET /v1/whoami", "POST /v1/inbox/paperless",
		"POST /v1/agent", "GET /v1/backup", "GET /v1/oauth/{skill}", "POST /v1/eval/reset",
		"GET /v1/ui/tokens", "POST /v1/pair", "POST /v1/a2a/push/{call}", "GET /v1/openapi.yaml", "GET /v1/openapi.json"} {
		if !want[p] {
			t.Errorf("%s is missing", p)
		}
	}
	// The setup's own routes and the page's files are left out on purpose.
	hidden := Mux(Parts{Integration: haconv.New(haconv.Options{}), Page: ui.New(ui.Options{}), Setup: &setup.Handler{}}).HiddenPatterns()
	for p, why := range hidden {
		fields := strings.Fields(p)
		if strings.HasPrefix(fields[len(fields)-1], "/v1/") {
			t.Errorf("%s under /v1/ is left out (%s)", p, why)
		}
	}
}

// answerV1 and answerV2 are a route's answer before and after it gained a
// field.
type answerV1 struct {
	State string `json:"state"`
}

type answerV2 struct {
	State string `json:"state"`
	Since string `json:"since"`
}

func TestOA02_TheFileIsCurrent(t *testing.T) {
	route := func(answer any) []api.Route {
		return []api.Route{{Method: "GET", Path: "/v1/thing", Summary: "A thing", Tag: api.TagIntegration, Response: answer}}
	}
	onDisk, err := api.YAML(route(answerV1{}), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := api.YAML(route(answerV2{}), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare("docs/openapi.yaml", onDisk, onDisk); err != nil {
		t.Fatalf("equal files differ: %v", err)
	}
	err = Compare("docs/openapi.yaml", onDisk, generated)
	if err == nil || !strings.Contains(err.Error(), "since") || !strings.Contains(err.Error(), "go run ./cmd/openapi") {
		t.Fatalf("want the difference and the command named, got %v", err)
	}
	// The repository's file itself.
	version, err := ServiceVersion("../../cmd/mira/main.go")
	if err != nil {
		t.Fatal(err)
	}
	want, err := Description(version)
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("%v; run go run ./cmd/openapi", err)
	}
	if err := Compare("docs/openapi.yaml", have, want); err != nil {
		t.Fatal(err)
	}
}

// pastTheFunction finds routes registered on a mux directly, past
// internal/api, in the Go files below root: Go's ServeMux cannot list its
// routes, so the source is read. It skips tests, internal/api (which holds
// the mux) and internal/testsupport (fakes of other servers).
func pastTheFunction(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		if d.IsDir() {
			if strings.HasSuffix(slash, "internal/api") || strings.HasSuffix(slash, "internal/testsupport") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
				return true
			}
			if pattern, ok := patternOf(call.Args[0]); ok {
				found = append(found, fmt.Sprintf("%s: %s", fset.Position(call.Pos()), pattern))
			}
			return true
		})
		return nil
	})
	return found, err
}

// patternOf is a mux pattern written as a string, or joined from one.
func patternOf(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return x.Value, true
		}
	case *ast.BinaryExpr:
		l, lok := patternOf(x.X)
		r, rok := patternOf(x.Y)
		if lok || rok {
			return l + "+" + r, true
		}
	}
	return "", false
}

func TestOA03_ARoutePastTheFunction(t *testing.T) {
	// A route registered directly on a mux is named.
	dir := t.TempDir()
	src := "package x\n\nimport \"net/http\"\n\nfunc routes(mux *http.ServeMux, h http.HandlerFunc) {\n\tmux.HandleFunc(\"GET /v1/secret\", h)\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := pastTheFunction(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || !strings.Contains(found[0], "GET /v1/secret") || !strings.Contains(found[0], "x.go:6") {
		t.Fatalf("want the route named, got %v", found)
	}
	// The service has none.
	for _, root := range []string{"../../cmd", "../../internal"} {
		found, err := pastTheFunction(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			t.Errorf("a route registered past internal/api: %s", f)
		}
	}
}

func TestOA04_ErrorsAndSecurity(t *testing.T) {
	doc := described(t, All("test"))
	schemes, _ := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	if bearer, _ := schemes["bearer"].(map[string]any); bearer["scheme"] != "bearer" {
		t.Fatalf("no bearer scheme: %v", schemes)
	}
	if global := fmt.Sprint(doc["security"]); global != "[map[bearer:[]]]" {
		t.Fatalf("the bearer scheme is not the default: %s", global)
	}
	for _, r := range All("test") {
		op := operation(doc, r.Method, strings.ReplaceAll(r.Path, "...}", "}"))
		_, ownSecurity := op["security"]
		if ownSecurity != r.Open {
			t.Errorf("%s: open %v, but security of its own %v", r.Pattern(), r.Open, ownSecurity)
		}
		if !r.Open && !strings.Contains(fmt.Sprint(op["parameters"]), "#/components/parameters/PartnerUser") {
			t.Errorf("%s: the partner user's header is not named", r.Pattern())
		}
		responses, _ := op["responses"].(map[string]any)
		if !r.Open && responses["401"] == nil {
			t.Errorf("%s: no 401", r.Pattern())
		}
		for code, resp := range responses {
			if code[0] != '4' && code[0] != '5' {
				continue
			}
			if !strings.Contains(fmt.Sprint(resp), "#/components/schemas/Error") {
				t.Errorf("%s %s: not Mira's error body", r.Pattern(), code)
			}
		}
		for _, code := range r.Errors {
			if responses[fmt.Sprint(code)] == nil {
				t.Errorf("%s: %d is missing", r.Pattern(), code)
			}
		}
	}
}

func TestOA05_TheRunningServiceDescribesItself(t *testing.T) {
	// A service in a setup without agents, backups or eval mode.
	m := Mux(Parts{Integration: haconv.New(haconv.Options{Token: "secret"}), Page: ui.New(ui.Options{Token: "secret"}),
		Setup: &setup.Handler{Token: "secret"}, Version: "9.9.9"})
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	get := func(path string) (int, string, []byte) {
		resp, err := http.Get(srv.URL + path) // without a token
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), raw
	}
	code, media, yamlBody := get(api.PathYAML)
	want, err := api.YAML(m.Routes(), "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusOK || media != "application/yaml" || string(yamlBody) != string(want) {
		t.Fatalf("GET %s: %d %s, equal to the generator's: %v", api.PathYAML, code, media, string(yamlBody) == string(want))
	}
	code, media, jsonBody := get(api.PathJSON)
	wantJSON, err := api.JSON(m.Routes(), "9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusOK || media != "application/json" || string(jsonBody) != string(wantJSON) {
		t.Fatalf("GET %s: %d %s", api.PathJSON, code, media)
	}
	text := string(yamlBody)
	for _, p := range []string{"/v1/openapi.yaml:", "/v1/openapi.json:", "/v1/ha/states/{entity_id}:", "version: 9.9.9"} {
		if !strings.Contains(text, p) {
			t.Errorf("%q is not in the served description", p)
		}
	}
	// A route this setup does not serve is not in it.
	if strings.Contains(text, "/v1/eval/reset:") || strings.Contains(text, "/v1/backup:") {
		t.Errorf("the description holds routes this service does not serve")
	}
	// With every option on, it is the generator's file.
	if all, err := api.YAML(All("9.9.9"), "9.9.9"); err != nil || len(all) <= len(want) {
		t.Fatalf("the generator's description should hold the routes of every setup: %v", err)
	}
}

// inner and outer are an answer with a struct inside, as most answers are.
type inner struct {
	Name string `json:"name"`
}

type outer struct {
	One  inner            `json:"one"`
	Many []inner          `json:"many"`
	ByID map[string]inner `json:"by_id"`
}

func TestOA06_SchemasAcceptAnAddedField(t *testing.T) {
	routes := []api.Route{{Method: "POST", Path: "/v1/thing", Summary: "A thing", Tag: api.TagIntegration,
		Request: outer{}, Response: outer{}}}
	raw, err := api.JSON(routes, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"name"`) {
		t.Fatalf("the inner struct is not described:\n%s", raw)
	}
	for name, doc := range map[string][]byte{"a struct inside a struct, a list and a map": raw} {
		if strings.Contains(string(doc), `"additionalProperties": false`) {
			t.Errorf("%s: a schema refuses added fields:\n%s", name, doc)
		}
	}
	// The service's own description.
	all, err := api.JSON(All("test"), "test")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(all), `"additionalProperties": false`); n > 0 {
		t.Errorf("the service's description refuses added fields in %d schemas", n)
	}
}

// freeForm are the bodies and answers that are open objects on purpose,
// each with its reason in the route's description (docs/spec/openapi.md,
// decision 12).
var freeForm = map[string]bool{
	"POST /v1/a2a/push/{call} request":                true, // an A2A StreamResponse, the A2A protocol's
	"POST /v1/ha/services/{domain}/{service} request": true, // an action's data, each action its own
	"GET /v1/openapi.json response":                   true, // an OpenAPI document, OpenAPI's schema
	"PUT /v1/ui/config request":                       true, // settings' sections as mira.yaml holds them
}

func TestOA07_EveryBodyAndAnswerIsNamed(t *testing.T) {
	doc := described(t, All("test"))
	bare := func(s any) bool {
		m, _ := s.(map[string]any)
		if m == nil || m["$ref"] != nil {
			return false
		}
		props, _ := m["properties"].(map[string]any)
		extra, _ := m["additionalProperties"].(map[string]any)
		return fmt.Sprint(m["type"]) == "object" && len(props) == 0 && len(extra) == 0
	}
	for _, r := range All("test") {
		op := operation(doc, r.Method, strings.ReplaceAll(r.Path, "...}", "}"))
		check := func(what string, content any) {
			for _, media := range asMap(content) {
				if bare(asMap(media)["schema"]) && !freeForm[r.Pattern()+" "+what] {
					t.Errorf("%s: its %s is a JSON object without its keys; name its Go type", r.Pattern(), what)
				}
			}
		}
		check("request", asMap(op["requestBody"])["content"])
		for code, resp := range asMap(op["responses"]) {
			if code[0] == '2' {
				check("response", asMap(resp)["content"])
			}
		}
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
