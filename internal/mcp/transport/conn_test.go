package transport

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// MX-01 The transport knows nothing of the host
// (docs/spec/mcp-connections.md, decision 3).
func TestMX01_TheTransportKnowsNothingOfTheHost(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasSuffix(path, "/internal/mcp") || strings.Contains(path, "/internal/config") ||
				strings.Contains(path, "/internal/prompt") || strings.Contains(path, "/internal/tools") {
				t.Errorf("%s imports %s", f, path)
			}
		}
	}
}
