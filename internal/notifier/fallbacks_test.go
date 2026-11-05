package notifier

import (
	"context"
	"errors"
	"testing"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The delivery fails, and setting its message back to open fails too: the
// delivery's error goes to the caller, and the log names the message.
func TestFB18_a_message_state_that_cannot_be_written_back_is_in_the_log(t *testing.T) {
	e := newEnv(t, nil)
	log, lines := logcap.Logger()
	e.n.log = log
	e.message("m1", 2, nil)
	msg, err := e.st.GetMessage(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	e.ha.callErr = errors.New("satellite unavailable")
	e.ha.onCall = func() { e.st.Close() } // the store fails while the satellite speaks
	// The bathroom's satellite starts a conversation; announce runs under
	// the notifier's lock, as its callers hold it.
	e.n.mu.Lock()
	err = e.n.announce(context.Background(), "bad", e.n.primary("bad"), []store.Message{msg}, false)
	e.n.mu.Unlock()
	if err == nil {
		t.Fatal("the failed delivery is no error")
	}
	if lines.Line("level=ERROR", "cannot be set back to open", "message=m1", "error=") == "" {
		t.Errorf("no line for the lost state (error %v):\n%s", err, lines)
	}
}
