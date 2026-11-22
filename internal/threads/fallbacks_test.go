package threads

import (
	"context"
	"testing"

	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/testsupport/logcap"
)

// The work is done after the turn stopped waiting, and the thread cannot be
// read again: the result is dropped, and the log says so.
func TestFB14_a_threads_result_whose_thread_cannot_be_read_again_is_in_the_log(t *testing.T) {
	lines := logcap.Default(t)
	e := setup(t)
	e.fin.hold = true
	ctx := context.Background()
	inv, _ := e.turn(t, "voice:a")
	if _, err := e.m.Start(ctx, inv, "aufgabe", "Sag Bescheid, wenn das Licht angeht"); err != nil {
		t.Fatal(err)
	}
	e.m.Fill(ctx, inv, map[string]string{"watch": "licht im schlafzimmer"})
	e.clk.Drive(func() { e.m.Fill(ctx, inv, map[string]string{"measure": "nachts"}) }) // the turn stops waiting
	e.st.Close()                                                                       // the store fails while the work runs
	e.fin.release()
	<-lines.Await("level=ERROR", "cannot be read again", "result is dropped", "thread=")
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
