package tasks

import (
	"context"
	"testing"

	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The task's past judgements cannot be read: it is judged without them, and
// the log names the task.
func TestFB21_past_judgements_that_cannot_be_read_are_in_the_log(t *testing.T) {
	e := setup(t)
	log, lines := logcap.Logger()
	e.m.log = log
	if _, err := e.st.DB().Exec(`DROP TABLE assessments`); err != nil {
		t.Fatal(err)
	}
	if got := e.m.earlier(context.Background(), "wasser-nachts"); got != "" {
		t.Errorf("earlier %q", got)
	}
	if lines.Line("level=WARN", "past judgements", "task=wasser-nachts", "error=") == "" {
		t.Errorf("no line for the judgements:\n%s", lines)
	}
}
