package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// envRef matches ${NAME} and ${NAME:-fallback}.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// Lookup resolves an environment variable. Tests inject their own.
type Lookup func(string) (string, bool)

// OSLookup reads the real process environment.
func OSLookup(name string) (string, bool) { return os.LookupEnv(name) }

// expandEnv substitutes ${VAR} references in the raw YAML before parsing, so
// secrets never have to live in the file. A reference without a fallback whose
// variable is unset is an error: silently running with an empty token would
// fail much later and much less clearly.
func expandEnv(raw []byte, lookup Lookup) ([]byte, error) {
	missing := map[string]bool{}
	out := envRef.ReplaceAllFunc(raw, func(m []byte) []byte {
		groups := envRef.FindSubmatch(m)
		name := string(groups[1])
		if v, ok := lookup(name); ok {
			return []byte(v)
		}
		if len(groups[2]) > 0 { // ":-fallback" was given
			return groups[3]
		}
		missing[name] = true
		return nil
	})
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unset environment variables referenced by the config: %s", strings.Join(names, ", "))
	}
	return out, nil
}
