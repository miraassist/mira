package startools

import (
	"fmt"

	starjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// maxJSONDepth bounds how deep arrays and objects may nest in a text that is
// decoded (docs/spec/api-tools.md, What the file may do). Starlark's decoder
// recurses once per level without a bound of its own: two megabytes of "["
// overflow the goroutine's stack, which ends the whole process, not only the
// call; it is one step for the step counter and invisible to the memory brake.
const maxJSONDepth = 1000

// jsonDepth is the deepest nesting of arrays and objects in s, counting
// brackets outside strings only; it stops counting past limit.
func jsonDepth(s string, limit int) int {
	depth, deepest := 0, 0
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
		case c == '[' || c == '{':
			depth++
			if depth > deepest {
				deepest = depth
				if deepest > limit {
					return deepest
				}
			}
		case c == ']' || c == '}':
			depth--
		}
	}
	return deepest
}

// tooDeep is the error of a text that nests deeper than maxJSONDepth.
func tooDeep(what string) error {
	return fmt.Errorf("%s: the JSON nests deeper than %d levels", what, maxJSONDepth)
}

// jsonModule is Starlark's json with a decode that refuses a text nesting
// deeper than maxJSONDepth before it is parsed; encode and indent are
// Starlark's own (encode is bound by the steps that built the value, indent
// does not recurse).
var jsonModule = &starlarkstruct.Module{Name: "json", Members: starlark.StringDict{
	"encode": starjson.Module.Members["encode"],
	"indent": starjson.Module.Members["indent"],
	"decode": starlark.NewBuiltin("json.decode", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) > 0 {
			if s, ok := starlark.AsString(args[0]); ok && jsonDepth(s, maxJSONDepth) > maxJSONDepth {
				return nil, tooDeep(b.Name())
			}
		}
		return starlark.Call(thread, starjson.Module.Members["decode"], args, kwargs)
	}),
}}
