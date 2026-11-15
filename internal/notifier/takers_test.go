package notifier

import "github.com/miraassist/mira/internal/store"

// The shipped skills take these kinds (docs/spec/messages.md, decision 33);
// the service sets them from the skills' takes.
func init() {
	store.SetTakers(map[string]string{store.KindAppointment: "termin", store.KindTodo: "todo",
		store.KindVoicemail: "anrufbeantworter", store.KindDevice: "geraet-einbinden"})
}
