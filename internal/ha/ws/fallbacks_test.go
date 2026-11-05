package ws

import (
	"encoding/json"
	"testing"

	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// An entities event that is no JSON is dropped, and the log says so.
func TestFB17_an_entities_event_that_cannot_be_read_is_in_the_log(t *testing.T) {
	log, lines := logcap.Logger()
	table := stateTable{log: log}
	if got := table.apply(json.RawMessage(`{"a": [broken`)); got != nil {
		t.Errorf("changes from a broken event: %v", got)
	}
	if lines.Line("level=WARN", "entities event", "cannot be read", "error=", `{\"a\": [broken`) == "" {
		t.Errorf("no line for the broken event:\n%s", lines)
	}
}
