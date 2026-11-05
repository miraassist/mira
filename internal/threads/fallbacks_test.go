package threads

import (
	"context"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The work is done after the turn stopped waiting, and the thread cannot be
// read again: the result is dropped, and the log says so.
func TestFB14_a_threads_result_whose_thread_cannot_be_read_again_is_in_the_log(t *testing.T) {
	lines := logcap.Default(t)
	e := setup(t)
	e.fin.delay = 200 * time.Millisecond
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	if _, err := e.m.Start(ctx, inv, "aufgabe", "Sag Bescheid, wenn das Licht angeht"); err != nil {
		t.Fatal(err)
	}
	e.m.Fill(ctx, inv, map[string]string{"watch": "licht im schlafzimmer"})
	e.m.Fill(ctx, inv, map[string]string{"measure": "nachts"})
	e.st.Close() // the store fails while the work runs

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if line := lines.Line("level=ERROR", "cannot be read again", "result is dropped", "thread="); line != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no line for the dropped result:\n%s", lines)
}

// A thread changed by another run in between is read again and saved; the
// log names the try.
func TestFB27_a_thread_saved_again_after_a_conflict_is_in_the_log(t *testing.T) {
	lines := logcap.Default(t)
	e := setup(t)
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	if _, err := e.m.Start(ctx, inv, "aufgabe", "Sag Bescheid, wenn das Licht angeht"); err != nil {
		t.Fatal(err)
	}
	open, err := e.st.OpenThreads(ctx, "")
	if err != nil || len(open) != 1 {
		t.Fatalf("threads %v %v", open, err)
	}
	id, first := open[0].ID, true
	if _, err := e.m.change(ctx, id, func(th *store.Thread) error {
		if first {
			// Another run writes the thread between reading and saving.
			first = false
			other, _ := e.st.GetThread(ctx, id)
			if err := e.st.PutThread(ctx, &other); err != nil {
				t.Fatal(err)
			}
		}
		th.Request = "geändert"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if lines.Line("changed meanwhile", "thread="+id, "try=1") == "" {
		t.Errorf("no line for the conflict:\n%s", lines)
	}
}
