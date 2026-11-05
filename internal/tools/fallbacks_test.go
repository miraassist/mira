package tools

import (
	"encoding/json"
	"testing"

	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// A tool call's arguments that are no JSON object: the hooks see none, and
// the log says why; no arguments at all stay without a line.
func TestFB26_json_that_cannot_be_read_is_in_the_log(t *testing.T) {
	lines := logcap.Default(t)
	if got := parseArgs(json.RawMessage(`{"entity": "light.`)); len(got) != 0 {
		t.Errorf("arguments %v", got)
	}
	if lines.Line("level=WARN", "arguments are no JSON object", `arguments="{\"entity\": \"light.`) == "" {
		t.Errorf("no line for the arguments:\n%s", lines)
	}
	before := lines.String()
	parseArgs(nil)
	if lines.String() != before {
		t.Errorf("a line for no arguments:\n%s", lines)
	}
}
