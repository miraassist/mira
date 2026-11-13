package docs

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs/spec/kv-cleanup.md, decision 1: a key of kv that is not described in
// docs/database.md is not written.

var (
	reKVUse = regexp.MustCompile(`\b(GetKV|SetKV|GetBool|SetBool|GetInt64|SetInt64|DeleteKV|KVWithPrefix)\(`)
	reKVKey = regexp.MustCompile(`\b(?:[kK]ey\w*|kv\w*|setupPrefix)\s*=\s*"([^"]+)"`)
	reTick  = regexp.MustCompile("`([^`]+)`")
)

func TestKV02_EveryKeyTheCodeWritesIsDescribed(t *testing.T) {
	raw, err := os.ReadFile("database.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "## The key-value table")
	if start < 0 {
		t.Fatal(`database.md has no section "The key-value table"`)
	}
	section := doc[start:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	var described []string
	for _, m := range reTick.FindAllStringSubmatch(section, -1) {
		described = append(described, m[1])
	}
	isDescribed := func(key string) bool {
		for _, d := range described {
			if d == key || (strings.HasSuffix(key, ".") || strings.HasSuffix(key, ":")) && strings.HasPrefix(d, key) {
				return true
			}
		}
		return false
	}
	found := 0
	for _, root := range []string{"../internal", "../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !reKVUse.Match(src) {
				return nil
			}
			for _, m := range reKVKey.FindAllStringSubmatch(string(src), -1) {
				found++
				if !isDescribed(m[1]) {
					t.Errorf("%s: the key %q of kv is not described in docs/database.md (The key-value table)", path, m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found < 10 {
		t.Fatalf("only %d keys found in the code: the search is broken", found)
	}
}
