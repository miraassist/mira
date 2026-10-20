package mailcal

import (
	"context"
	"strings"
	"time"
)

// Cleanup removes from the mail calendars what is done and its day over,
// and anything whose day is more than a week past. Only Mira's entries.
func (s *Service) Cleanup(ctx context.Context) error {
	names, err := s.o.Calendar.Names(ctx)
	if err != nil {
		return err
	}
	now := s.o.Clock().In(s.o.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.o.Location)
	for _, name := range names {
		if s.App(name) == "" {
			continue
		}
		entity, ok := s.o.Calendar.Find(ctx, name)
		if !ok {
			continue
		}
		events, err := s.o.Calendar.Events(ctx, entity, today.AddDate(0, 0, -60), today)
		if err != nil {
			s.log.Warn("could not read a mail calendar to clean it", "calendar", name, "error", err)
			continue
		}
		for _, e := range events {
			_, _, status, ok := marks(e.Description)
			day := e.Day.In(s.o.Location)
			if !ok || !day.Before(today) {
				continue
			}
			if status == statusDone || day.Before(today.AddDate(0, 0, -7)) {
				if err := s.o.Calendar.Delete(ctx, entity, e.UID); err != nil {
					s.log.Warn("could not remove an old entry", "calendar", name, "error", err)
					continue
				}
				s.log.Info("old entry removed", "calendar", name, "title", strings.TrimSpace(e.Summary))
			}
		}
	}
	return nil
}

// Run cleans up every hour until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil {
			s.log.Warn("cleaning the mail calendars failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}
