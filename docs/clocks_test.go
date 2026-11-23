package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goFiles are the Go files below the repository's internal and cmd.
func goFiles(t *testing.T, tests bool) []string {
	t.Helper()
	var out []string
	for _, root := range []string{"../internal", "../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") != tests {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// TS-01 The code reads the time only through a clock (docs/spec/tests.md,
// rule 7): no time.Now, time.Since or time.Until outside internal/clock,
// unless marked "real time:" on its line or the line above, as a measure of
// a duration. cmd/testha (a tool against a real Home Assistant) and the
// fakes of internal/testsupport are left out.
func TestTS01_TheCodeReadsTheTimeOnlyThroughAClock(t *testing.T) {
	var found []string
	for _, path := range goFiles(t, false) {
		slash := filepath.ToSlash(path)
		if strings.Contains(slash, "/internal/clock/") || strings.Contains(slash, "/cmd/testha/") ||
			strings.Contains(slash, "/internal/testsupport/") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "time" || (sel.Sel.Name != "Now" && sel.Sel.Name != "Since" && sel.Sel.Name != "Until") {
				return true
			}
			line := fset.Position(call.Pos()).Line
			marked := strings.Contains(lines[line-1], "real time:") ||
				(line >= 2 && strings.Contains(lines[line-2], "real time:"))
			if !marked {
				found = append(found, strings.TrimPrefix(slash, "../")+":"+itoa(line)+" time."+sel.Sel.Name)
			}
			return true
		})
	}
	if len(found) > 0 {
		t.Errorf("the time is read outside a clock (docs/spec/tests.md, rule 7); take the clock, or mark a measure of a duration \"// real time: …\":\n%s",
			strings.Join(found, "\n"))
	}
}

// TS-02 Every store in a test names its clock (docs/spec/tests.md, rule 7):
// a store.Options literal in a Go test sets Clock.
func TestTS02_EveryStoreInATestNamesItsClock(t *testing.T) {
	var found []string
	for _, path := range goFiles(t, true) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "store.Options{") && !strings.Contains(string(src), "Options{Path:") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Options" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "store" {
				return true
			}
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Clock" {
						return true
					}
				}
			}
			found = append(found, strings.TrimPrefix(filepath.ToSlash(path), "../")+":"+itoa(fset.Position(lit.Pos()).Line))
			return true
		})
	}
	if len(found) > 0 {
		t.Errorf("a store in a test without its clock (docs/spec/tests.md, rule 7); name it, the same as the test's other parts:\n%s",
			strings.Join(found, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
