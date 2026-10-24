package phone

import (
	"context"
	"sync"
	"time"
)

// Book is the house's phone book, read from the FRITZ!Box and kept for an
// hour; a name not found reads it once more.
type Book struct {
	// Source is the skill telefon's access; false while the skill is off.
	Source func() (Fritz, bool)
	Clock  func() time.Time

	mu       sync.Mutex
	readAt   time.Time
	tried    time.Time
	books    []string
	contacts []Contact
	err      error
}

const (
	keepFor = time.Hour
	// retryAfter keeps a box that failed, or a name not found, from being
	// asked again at once.
	retryAfter = time.Minute
)

// Status is what Mira's page shows of the phone book: never the contacts.
type Status struct {
	On     bool
	Link   string
	Books  []string
	ReadAt time.Time
	Error  string
}

func (b *Book) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}
	return time.Now()
}

// On says whether the skill telefon is on.
func (b *Book) On() bool {
	if b == nil || b.Source == nil {
		return false
	}
	_, on := b.Source()
	return on
}

// Contacts are the contacts, read when they are older than an hour; fresh
// reads again (a name not found), at most once a minute.
func (b *Book) Contacts(ctx context.Context, fresh bool) ([]Contact, error) {
	f, on := b.Source()
	if !on {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	stale := b.readAt.IsZero() || now.Sub(b.readAt) > keepFor || fresh
	if stale && now.Sub(b.tried) >= retryAfter {
		b.tried = now
		books, contacts, err := f.Read(ctx)
		if err != nil {
			b.err = err
		} else {
			b.books, b.contacts, b.err, b.readAt = books, contacts, nil, now
		}
	}
	if b.contacts == nil && b.err != nil {
		return nil, b.err
	}
	return b.contacts, nil
}

// Status is the phone book's state for the page.
func (b *Book) Status() Status {
	f, on := b.Source()
	b.mu.Lock()
	defer b.mu.Unlock()
	st := Status{On: on, Link: f.Link(), Books: append([]string(nil), b.books...), ReadAt: b.readAt}
	if b.err != nil {
		st.Error = b.err.Error()
	}
	return st
}
