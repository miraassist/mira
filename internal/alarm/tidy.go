package alarm

import (
	"context"
	"errors"
	"slices"

	"github.com/miraassist/mira/internal/ha/automation"
)

// Tidying up: a once-alarm goes once it rang and its run is over, if Mira
// created it. The signal is its state, from a subscription filtered to the
// alarms' entities: the run sets last_triggered, its end sets the running
// count back to 0. At the start (and after every reconnect: Poke) the alarms
// are looked at once. Cost: one subscription to at most ten alarms per person
// and room; on each change one list of the alarms and a delete for each
// once-alarm that rang.

// Poke has the alarms looked at again. It never blocks.
func (s *Alarms) Poke() { s.c.Poke() }

func (c *controller) Poke() {
	select {
	case c.pokes <- struct{}{}:
	default:
	}
}

// Run tidies up until ctx ends.
func (s *Alarms) Run(ctx context.Context) error {
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
					c.log.Warn("could not watch the alarms; a once-alarm that rang is deleted at the next start", "error", err)
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

// tidy deletes the once-alarms that rang and are over, and returns the
// entities of the others, sorted; false when the alarms cannot be read.
func (c *controller) tidy(ctx context.Context) ([]string, bool) {
	alarms, err := c.all(ctx)
	if err != nil {
		// An integration older than the alarms has no mira/alarm/list:
		// said once, then quiet until it answers.
		if !c.noIntegration {
			c.log.Warn("could not read the alarms to tidy them up; trying again at the next change or reconnect", "error", err)
			c.noIntegration = true
		}
		return nil, false
	}
	c.noIntegration = false
	var keep []string
	for _, al := range alarms {
		if al.Ran && len(al.Days) == 0 && al.Running == 0 {
			err := automation.Delete(ctx, c.o.HA, c.o.Own, c.log, al.ID)
			switch {
			case err == nil:
				c.log.Info("once-alarm that rang removed", "alarm", al.Title, "automation", al.ID)
			case errors.Is(err, automation.ErrNotOwn):
				c.log.Info("an alarm labelled 'Mira: Wecker' was not created by mira, left alone", "automation", al.ID)
			}
			continue
		}
		if al.EntityID != "" {
			keep = append(keep, al.EntityID)
		}
	}
	slices.Sort(keep)
	return keep, true
}
