package ws_test

import (
	"context"
	"testing"
	"time"

	haws "github.com/miraassist/mira/internal/ha/ws"
	"github.com/miraassist/mira/internal/testsupport/fakeha"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The state cannot be watched: it counts as not reached, and the log names
// the entity and why.
func TestFB16_a_state_that_cannot_be_watched_is_in_the_log(t *testing.T) {
	log, lines := logcap.Logger()
	srv := fakeha.New()
	c, err := haws.New(haws.Options{URL: srv.URL, Token: "test-token", Logger: log, CallTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	ready, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	if err := c.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	cancel() // the subscription cannot be made any more
	if c.WaitState(ctx, "media_player.show_speaker", time.Second, func(string) bool { return true }) {
		t.Error("reached without a subscription")
	}
	if lines.Line("level=WARN", "could not watch an entity's state", "entity=media_player.show_speaker", "error=") == "" {
		t.Errorf("no line for the failed watch:\n%s", lines)
	}
}
