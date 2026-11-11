package notifier

import (
	"context"
	"slices"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
)

// Tests for docs/spec/messages.md, Who sees which open messages.

// withAnna adds Anna, tracked by person.anna, to the persons.
func withAnna(c *config.Config) {
	c.Persons["anna"] = config.Person{Entity: "person.anna", Channels: []string{"notify.anna"}}
}

func (e *env) open(area, receiver string) []string {
	e.t.Helper()
	list, err := e.n.Open(context.Background(), area, receiver)
	if err != nil {
		e.t.Fatal(err)
	}
	var ids []string
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestMS01_APrivateMessageIsNeverListedAtASatellite(t *testing.T) {
	e := newEnv(t, withAnna)
	// Given a private message to Anna, who is home alone
	e.ha.set("person.anna", "home", nil)
	e.ha.set("person.ben", "not_home", nil)
	e.message("secret", 1, func(n *store.Message) { n.Receiver = "anna"; n.Confidentiality = store.ConfidentialityPrivate })
	// When messages_list runs at the kitchen's satellite
	// Then the message is not listed
	if got := e.open("kueche", "anna"); len(got) != 0 {
		t.Errorf("listed at the satellite: %v", got)
	}
	// And in Anna's own chat it is listed
	if got := e.open("", "anna"); !slices.Equal(got, []string{"secret"}) {
		t.Errorf("in Anna's chat: %v", got)
	}
}

func TestMS02_APersonsMessageIsNotListedAtASatelliteWhileThePersonIsAway(t *testing.T) {
	e := newEnv(t, nil)
	// Given a message to Ben, who is away
	e.ha.set("person.ben", "not_home", nil)
	e.message("for-ben", 1, func(n *store.Message) { n.Receiver = "ben" })
	// When messages_list runs at the kitchen's satellite
	// Then it is not listed
	if got := e.open("kueche", ""); len(got) != 0 {
		t.Errorf("listed while Ben is away: %v", got)
	}
	// And once Ben is home it is
	e.ha.set("person.ben", "home", nil)
	if got := e.open("kueche", ""); !slices.Equal(got, []string{"for-ben"}) {
		t.Errorf("Ben home: %v", got)
	}
}

func TestMS03_AConfidentialMessageIsListedAtASatelliteOnlyAsTheSettingAllows(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		withAnna(c)
		c.Notifier.ConfidentialVoice = config.ConfidentialVoiceWhenAlone
	})
	// Given a confidential message to Anna, confidential_voice "when_alone", Anna and Ben home
	e.ha.set("person.anna", "home", nil)
	e.ha.set("person.ben", "home", nil)
	e.message("conf", 1, func(n *store.Message) { n.Receiver = "anna"; n.Confidentiality = store.ConfidentialityConfidential })
	// When messages_list runs at the kitchen's satellite
	// Then it is not listed
	if got := e.open("kueche", ""); len(got) != 0 {
		t.Errorf("listed with two at home: %v", got)
	}
	// And with Anna home alone it is
	e.ha.set("person.ben", "not_home", nil)
	if got := e.open("kueche", ""); !slices.Equal(got, []string{"conf"}) {
		t.Errorf("Anna alone: %v", got)
	}
}

func TestMS04_AMessageHeardButNotDoneIsStillListed(t *testing.T) {
	e := newEnv(t, nil)
	// Given a message to the house that was read out
	e.message("heard", 1, nil)
	if err := e.st.MarkDelivered(context.Background(), "heard", "voice:kueche"); err != nil {
		t.Fatal(err)
	}
	// When messages_list runs at the kitchen's satellite
	// Then it is listed as heard
	list, err := e.n.Open(context.Background(), "kueche", "")
	if err != nil || len(list) != 1 || list[0].ID != "heard" || list[0].State != store.MessageDelivered {
		t.Fatalf("%+v, %v", list, err)
	}
}

func TestMS05_APersonsOwnChatListsTheHousesAndThePersonsOwn(t *testing.T) {
	e := newEnv(t, withAnna)
	// Given messages to the house, to Anna (private) and to Ben
	e.message("house", 1, nil)
	e.message("anna", 1, func(n *store.Message) { n.Receiver = "anna"; n.Confidentiality = store.ConfidentialityPrivate })
	e.message("ben", 1, func(n *store.Message) { n.Receiver = "ben" })
	// When messages_list runs in Anna's chat
	got := e.open("", "anna")
	// Then the house's and Anna's are listed, Ben's not
	slices.Sort(got)
	if !slices.Equal(got, []string{"anna", "house"}) {
		t.Errorf("Anna's chat: %v", got)
	}
}

func TestMS06_WithoutARoomOrAPersonOnlyTheHousesAreListed(t *testing.T) {
	e := newEnv(t, withAnna)
	// Given messages to the house and to Anna
	e.message("house", 1, nil)
	e.message("anna", 1, func(n *store.Message) { n.Receiver = "anna" })
	// And a reminder is not listed
	e.message("reminder", 1, func(n *store.Message) { n.Source = store.SourceReminder })
	// When messages_list runs in an agent's run
	// Then only the house's is listed
	if got := e.open("", store.ReceiverHouse); !slices.Equal(got, []string{"house"}) {
		t.Errorf("no room, no person: %v", got)
	}
}
