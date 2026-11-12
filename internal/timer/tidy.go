package timer

import (
	"context"
	"errors"
	"slices"

	"github.com/miraassist/mira/internal/ha/automation"
)

// Tidying up (docs/spec/hacs-default.md, 3 and 4): a timer's automation
// goes once it has run and is no longer running, if Mira created it (her own
// note, decision 7); before the first tidy the automations labelled as hers
// from before the note are taken over once. The signal is its state, from a
// subscription filtered to the timers' entities: the run sets last_triggered,
// its end sets the running count back to 0. At the start (and after every
// reconnect: Poke) the timers are looked at once, for what ran while Mira
// was off. Cost: one subscription to at most ten timers per person and room;
// on each change one list of the timers and a delete for each finished one.

// Poke has the timers looked at again: after a reconnect, a new or
// cancelled timer, a change of a timer's state. It never blocks.
func (s *Timers) Poke() { s.c.Poke() }

func (c *controller) Poke() {
	select {
	case c.pokes <- struct{}{}:
	default:
	}
}

// Run tidies up until ctx ends.
func (s *Timers) Run(ctx context.Context) error {
	c := s.c
	var watched []string
	stop := func() {}
	defer func() { stop() }()
	for {
		if ids, ok := c.tidy(ctx); ok && !slices.Equal(ids, watched) && c.o.Watch != nil {
			stop()
			stop = func() {}
			watched = nil
			if len(ids) > 0 {
				if s, err := c.o.Watch(ctx, ids, c.Poke); err != nil {
					c.log.Warn("could not watch the timers; a finished one is tidied away at the next start", "error", err)
				} else {
					stop, watched = s, ids
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-c.pokes:
		}
	}
}

// tidy deletes the timers that ran and are finished, and returns the
// entities of the others, sorted; false when the timers cannot be read.
func (c *controller) tidy(ctx context.Context) ([]string, bool) {
	if !c.takenOver {
		if err := automation.TakeOver(ctx, c.o.HA, c.o.Own, c.log); err != nil {
			c.log.Warn("could not take over mira's automations from before; nothing is tidied, trying again at the next change or reconnect", "error", err)
			return nil, false
		}
		c.takenOver = true
	}
	timers, err := c.all(ctx)
	if err != nil {
		c.log.Warn("could not read the timers to tidy them up; trying again at the next change or reconnect", "error", err)
		return nil, false
	}
	var keep []string
	for _, tm := range timers {
		if tm.Ran && tm.Running == 0 {
			// A failure is logged in Delete; the next look tries again.
			err := automation.Delete(ctx, c.o.HA, c.o.Own, c.log, tm.ID)
			switch {
			case err == nil:
				c.log.Info("finished timer removed", "timer", tm.Title, "automation", tm.ID)
			case errors.Is(err, automation.ErrNotOwn):
				if !c.leftAlone[tm.ID] {
					c.log.Info("a timer labelled 'Mira: einmalig' was not created by mira, left alone", "automation", tm.ID)
					if c.leftAlone == nil {
						c.leftAlone = map[string]bool{}
					}
					c.leftAlone[tm.ID] = true
				}
			}
			continue
		}
		if tm.EntityID != "" {
			keep = append(keep, tm.EntityID)
		}
	}
	slices.Sort(keep)
	return keep, true
}
