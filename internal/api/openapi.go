package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"
)

// HeaderPartnerUser is the partner user's header (internal/access); here
// only for the description.
const HeaderPartnerUser = "X-Partner-User"

// The description's paths of itself (docs/spec/openapi.md, decision 11).
const (
	PathYAML = "/v1/openapi.yaml"
	PathJSON = "/v1/openapi.json"
)

var tagTexts = []struct{ name, text string }{
	{TagIntegration, "The Home Assistant integration's interface: conversations, messages, tasks, agents, backup."},
	{TagHA, "Home Assistant through Mira's interface (docs/spec/ha-api.md): read an entity, call an action, with Mira's own connection."},
	{TagSkills, "Skills' connections and starting a skill's thread."},
	{TagAccess, "Who calls (docs/spec/access-tokens.md)."},
	{TagSetup, "Pairing and setup of the service (docs/spec/setup.md)."},
	{TagPage, "Mira's page's own routes (/v1/ui/…): they serve the page and may change between versions."},
	{TagDescription, "This description."},
}

type document struct {
	OpenAPI    string                           `json:"openapi"`
	Info       info                             `json:"info"`
	Tags       []tag                            `json:"tags"`
	Security   []map[string][]string            `json:"security"`
	Paths      map[string]map[string]*operation `json:"paths"`
	Components components                       `json:"components"`
}

type info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type tag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type components struct {
	SecuritySchemes map[string]any `json:"securitySchemes"`
	Parameters      map[string]any `json:"parameters"`
	Schemas         map[string]any `json:"schemas"`
}

type operation struct {
	Tags        []string              `json:"tags,omitempty"`
	Summary     string                `json:"summary"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId"`
	Security    []map[string][]string `json:"security,omitempty"`
	Parameters  []any                 `json:"parameters,omitempty"`
	RequestBody *body                 `json:"requestBody,omitempty"`
	Responses   map[string]*response  `json:"responses"`
}

type body struct {
	Required bool                  `json:"required,omitempty"`
	Content  map[string]mediaEntry `json:"content"`
}

type response struct {
	Description string                `json:"description"`
	Content     map[string]mediaEntry `json:"content,omitempty"`
}

type mediaEntry struct {
	Schema any `json:"schema"`
}

var errorRef = map[string]string{"$ref": "#/components/schemas/Error"}

var wildcard = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)(\.\.\.)?\}`)

var schemaOptions = &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
	// Raw JSON is whatever it holds.
	reflect.TypeFor[json.RawMessage](): {},
}}

// JSON is the OpenAPI description of the routes as JSON; an error names
// the route and the Go type it could not describe.
func JSON(routes []Route, version string) ([]byte, error) {
	if err := Check(routes); err != nil {
		return nil, err
	}
	doc := document{
		OpenAPI: "3.1.0",
		Info: info{Title: "Mira", Version: version,
			Description: "The Mira service's HTTP interface. Generated from the code (go run ./cmd/openapi, docs/spec/openapi.md); " +
				"every error answers Mira's error body. A partner token names its partner user in " + HeaderPartnerUser + "."},
		Security: []map[string][]string{{"bearer": {}}},
		Paths:    map[string]map[string]*operation{},
		Components: components{
			SecuritySchemes: map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer",
				"description": "The integration's token or an access token made on Mira's page (docs/spec/access-tokens.md)."}},
			Parameters: map[string]any{"PartnerUser": map[string]any{"name": HeaderPartnerUser, "in": "header", "required": false,
				"description": "The partner user who makes the call; read only with a partner token, ignored with any other.",
				"schema":      map[string]string{"type": "string"}}},
			Schemas: map[string]any{"Error": map[string]any{"type": "object", "required": []string{"error"},
				"properties": map[string]any{"error": map[string]string{"type": "string", "description": "What failed, naming its input."}}}},
		},
	}
	for _, t := range tagTexts {
		doc.Tags = append(doc.Tags, tag{Name: t.name, Description: t.text})
	}
	for _, r := range routes {
		op, err := describe(r)
		if err != nil {
			return nil, err
		}
		path := wildcard.ReplaceAllString(r.Path, "{$1}")
		if doc.Paths[path] == nil {
			doc.Paths[path] = map[string]*operation{}
		}
		doc.Paths[path][strings.ToLower(r.Method)] = op
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode the description: %w", err)
	}
	return buf.Bytes(), nil
}

// YAML is JSON as YAML, in the same order.
func YAML(routes []Route, version string) ([]byte, error) {
	raw, err := JSON(routes, version)
	if err != nil {
		return nil, err
	}
	node, err := nodeOf(json.NewDecoder(bytes.NewReader(raw)))
	if err != nil {
		return nil, fmt.Errorf("turn the description into YAML: %w", err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, fmt.Errorf("encode the description as YAML: %w", err)
	}
	return buf.Bytes(), nil
}

func describe(r Route) (*operation, error) {
	op := &operation{Summary: r.Summary, Description: r.Description,
		OperationID: operationID(r), Responses: map[string]*response{}}
	if r.Tag != "" {
		op.Tags = []string{r.Tag}
	}
	if r.Only != "" {
		op.Description = strings.TrimSpace(op.Description + "\n\nOnly " + r.Only + ".")
	}
	if r.Open {
		op.Security = []map[string][]string{{}}
	}
	for _, m := range wildcard.FindAllStringSubmatch(r.Path, -1) {
		p := map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]string{"type": "string"}}
		if m[2] != "" {
			p["description"] = "May hold slashes."
		}
		op.Parameters = append(op.Parameters, p)
	}
	for _, q := range r.Query {
		p := map[string]any{"name": q.Name, "in": "query", "schema": map[string]string{"type": "string"}}
		if q.Required {
			p["required"] = true
		}
		if q.Description != "" {
			p["description"] = q.Description
		}
		op.Parameters = append(op.Parameters, p)
	}
	if !r.Open {
		op.Parameters = append(op.Parameters, map[string]string{"$ref": "#/components/parameters/PartnerUser"})
	}
	if r.Request != nil {
		media, schema, err := content(r, r.Request, r.RequestMedia)
		if err != nil {
			return nil, err
		}
		op.RequestBody = &body{Required: true, Content: map[string]mediaEntry{media: {Schema: schema}}}
	}
	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}
	ok := &response{Description: http.StatusText(status)}
	if r.Response != nil {
		media, schema, err := content(r, r.Response, r.ResponseMedia)
		if err != nil {
			return nil, err
		}
		ok.Content = map[string]mediaEntry{media: {Schema: schema}}
	}
	op.Responses[strconv.Itoa(status)] = ok
	errs := append([]int(nil), r.Errors...)
	if !r.Open {
		errs = append(errs, http.StatusUnauthorized)
	}
	sort.Ints(errs)
	for _, code := range errs {
		op.Responses[strconv.Itoa(code)] = &response{Description: http.StatusText(code),
			Content: map[string]mediaEntry{"application/json": {Schema: errorRef}}}
	}
	return op, nil
}

// content is a body's media type and schema: JSON by reflection of its Go
// type, any other media type as it comes.
func content(r Route, v any, media string) (string, any, error) {
	if media != "" && media != "application/json" {
		if strings.HasPrefix(media, "text/") || strings.Contains(media, "json") || strings.Contains(media, "yaml") {
			return media, map[string]string{"type": "string"}, nil
		}
		return media, map[string]string{"type": "string", "format": "binary"}, nil
	}
	s, err := jsonschema.ForType(reflect.TypeOf(v), schemaOptions)
	if err != nil {
		return "", nil, fmt.Errorf("route %s: the type %T cannot be described: %w", r.Pattern(), v, err)
	}
	openUp(reflect.ValueOf(s))
	raw, err := json.Marshal(s)
	if err != nil {
		return "", nil, fmt.Errorf("route %s: the schema of %T: %w", r.Pattern(), v, err)
	}
	return "application/json", json.RawMessage(raw), nil
}

// openUp takes "additionalProperties: false" out of a schema and every
// schema inside it (docs/spec/openapi.md, decision 13): a client written
// against an older description then takes a newer service's answer with a
// field added. The reflection closes every struct; a map's schema of its
// values stays.
func openUp(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		if s, ok := v.Interface().(*jsonschema.Schema); ok && s.AdditionalProperties != nil {
			if raw, err := json.Marshal(s.AdditionalProperties); err == nil && string(raw) == "false" {
				s.AdditionalProperties = nil
			}
		}
		openUp(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				openUp(v.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			openUp(v.Index(i))
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			openUp(it.Value())
		}
	}
}

// operationID is the method and the path's words: getV1HaStatesEntityId.
func operationID(r Route) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(r.Method))
	for _, part := range strings.FieldsFunc(r.Path, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9')
	}) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// nodeOf reads one JSON value into a YAML node, keeping the order of keys.
func nodeOf(dec *json.Decoder) (*yaml.Node, error) {
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return nodeFrom(dec, tok)
}

func nodeFrom(dec *json.Decoder, tok json.Token) (*yaml.Node, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := k.(string)
				vt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := nodeFrom(dec, vt)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		case '[':
			n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				vt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := nodeFrom(dec, vt)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if len(n.Content) == 0 {
				n.Style = yaml.FlowStyle
			}
			return n, nil
		}
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}, nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(t.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: t.String()}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// Document is an OpenAPI document as JSON: open, its schema is OpenAPI's.
type Document map[string]any

// Serve answers the description of the routes at PathYAML and PathJSON
// (docs/spec/openapi.md, decision 11). routes is asked at each call, so
// the answer is the running service's; a failure is a 500 naming it.
func Serve(m *Mux, routes func() []Route, version string, onError func(path string, err error)) {
	serve := func(media string, gen func([]Route, string) ([]byte, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			out, err := gen(routes(), version)
			if err != nil {
				if onError != nil {
					onError(r.URL.Path, err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "the description could not be made: " + err.Error()})
				return
			}
			w.Header().Set("Content-Type", media)
			_, _ = io.Copy(w, bytes.NewReader(out))
		}
	}
	m.Handle(Route{Method: http.MethodGet, Path: PathYAML, Tag: TagDescription, Open: true,
		Summary:  "This description as YAML (OpenAPI 3.1), of the running service's routes",
		Response: "", ResponseMedia: "application/yaml", Errors: []int{http.StatusInternalServerError}},
		serve("application/yaml", YAML))
	m.Handle(Route{Method: http.MethodGet, Path: PathJSON, Tag: TagDescription, Open: true,
		Summary:     "This description as JSON (OpenAPI 3.1), of the running service's routes",
		Description: "The answer is an OpenAPI 3.1 document, whose own schema is OpenAPI's, not Mira's: an open object here.",
		Response:    Document{}, Errors: []int{http.StatusInternalServerError}},
		serve("application/json", JSON))
}
