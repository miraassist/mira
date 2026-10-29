// Package phone is phone_call (docs/spec/telefon.md): Mira calls a contact
// of the FRITZ!Box's phone book, a device in the house or a number from the
// satellite being spoken at. The phone book comes from the integration Mira
// FRITZ!Box (miraassist/mira-fritz), its action get_phonebook.
package phone

import (
	"errors"
	"strings"
)

// Phonebook is what get_phonebook answers: the books read, the box's page and
// the contacts.
type Phonebook struct {
	Books    []string  `json:"books"`
	URL      string    `json:"url"`
	Contacts []Contact `json:"contacts"`
}

// ErrNoIntegration: Home Assistant has no Mira FRITZ!Box to ask.
var ErrNoIntegration = errors.New("the integration Mira FRITZ!Box is missing")

// Contact is a phone book entry.
type Contact struct {
	Name    string   `json:"name"`
	Numbers []Number `json:"numbers"`
}

// Number is one of a contact's numbers.
type Number struct {
	Number string `json:"number"`
	// Type is the FRITZ!Box's: home, mobile, work, intern, fax_work,
	// fixedline, label:…, or empty.
	Type string `json:"type"`
	// Main is the box's main number of the contact (prio="1").
	Main bool `json:"main"`
}

// dialable is a number as a phone dials it: digits, * and #, a leading +
// kept.
func dialable(n string) string {
	var b strings.Builder
	n = strings.TrimSpace(n)
	if strings.HasPrefix(n, "+") {
		b.WriteByte('+')
	}
	for _, r := range n {
		if (r >= '0' && r <= '9') || r == '*' || r == '#' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
