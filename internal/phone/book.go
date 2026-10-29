package phone

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Book is the house's phone book, read from Mira FRITZ!Box before every
// call, so a changed number counts at once; the contacts read last stand in
// while the box does not answer.
type Book struct {
	// On says whether the skill telefon is on.
	On func() bool
	// Read asks the integration for the phone book.
	Read  func(ctx context.Context) (Phonebook, error)
	Clock func() time.Time

	mu     sync.Mutex
	readAt time.Time
	last   Phonebook
	read   bool
	err    error
}

// Status is what Mira's page shows of the phone book: never the contacts.
// Missing: the integration is not there.
type Status struct {
	On      bool
	Link    string
	Books   []string
	ReadAt  time.Time
	Error   string
	Missing bool
}

func (b *Book) now() time.Time {
	if b.Clock != nil {
		return b.Clock()
	}
	return time.Now()
}

// IsOn says whether the skill telefon is on.
func (b *Book) IsOn() bool {
	return b != nil && b.On != nil && b.On()
}

// Contacts reads the phone book afresh. When it cannot be read, the
// contacts read last stand in, if there are any; the error is kept for the
// page.
func (b *Book) Contacts(ctx context.Context) ([]Contact, error) {
	if !b.IsOn() {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	pb, err := b.Read(ctx)
	if err != nil {
		b.err = err
		if b.read {
			return b.last.Contacts, nil
		}
		return nil, err
	}
	b.last, b.read, b.err, b.readAt = pb, true, nil, b.now()
	return pb.Contacts, nil
}

// Status is the phone book's state for the page.
func (b *Book) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := Status{On: b.IsOn(), Link: b.last.URL, Books: append([]string(nil), b.last.Books...), ReadAt: b.readAt}
	if b.err != nil {
		st.Error = b.err.Error()
		st.Missing = errors.Is(b.err, ErrNoIntegration)
	}
	return st
}
