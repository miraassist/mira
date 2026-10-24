package phone

import (
	"context"
	"sync"
	"time"
)

// Book is the house's phone book, read from the FRITZ!Box before every
// call, so a changed number counts at once; the contacts read last stand in
// while the box does not answer.
type Book struct {
	// Source is the skill telefon's access; false while the skill is off.
	Source func() (Fritz, bool)
	Clock  func() time.Time

	mu       sync.Mutex
	readAt   time.Time
	books    []string
	contacts []Contact
	err      error
}

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

// Contacts reads the phone book afresh. When the box does not answer, the
// contacts read last stand in, if there are any; the error is kept for the
// page.
func (b *Book) Contacts(ctx context.Context) ([]Contact, error) {
	f, on := b.Source()
	if !on {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	books, contacts, err := f.Read(ctx)
	if err != nil {
		b.err = err
		if b.contacts != nil {
			return b.contacts, nil
		}
		return nil, err
	}
	b.books, b.contacts, b.err, b.readAt = books, contacts, nil, b.now()
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
