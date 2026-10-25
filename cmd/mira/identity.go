package main

import (
	"context"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/conversation"
	haws "github.com/miraassist/mira/internal/ha/ws"
)

// identify says who is speaking in a turn: a person key, or "" for someone
// anonymous at a satellite.
//   - Telegram: the person with that Telegram id.
//   - The HA app and its chat: the person whose HA person entity belongs to
//     the signed-in HA user.
//   - A satellite: the person the voice was recognized as (speaker-id, see
//     docs/spec/sprecher.md), else the one person at home who switched on
//     the "alone at home" rule, else nobody.
func (a *app) identify(ctx context.Context, req conversation.Request) string {
	persons := a.persons()
	switch req.Channel {
	case config.ChannelTelegram:
		// Home Assistant's Telegram integration names the person: the
		// integration matched the chat to a person's channels.
		if _, ok := persons[req.Person]; ok {
			return req.Person
		}
	case config.ChannelVoice:
		if req.HAUser != "" {
			return a.personOfHAUser(ctx, req.HAUser)
		}
		return atSatellite(persons, a.voiceOf(ctx, req.Text), func() string {
			if !anyAloneIsMe(persons) {
				return ""
			}
			states, err := a.ha.States(ctx)
			if err != nil {
				a.log.Warn("could not read person states", "error", err)
				return ""
			}
			return aloneAtHome(persons, states)
		})
	}
	return ""
}

// atSatellite is who speaks at a satellite: the person the voice was
// recognized as, if Home Assistant has that person, else what the "alone at
// home" rule says.
func atSatellite(persons map[string]config.Person, voice string, alone func() string) string {
	if _, ok := persons[voice]; ok && voice != "" {
		return voice
	}
	return alone()
}

// voiceOf is the person the speaker-id service recognized as having said
// text, or "" (no service, nobody for sure, no answer in time).
func (a *app) voiceOf(ctx context.Context, text string) string {
	if a.speaker == nil {
		return ""
	}
	person, score, err := a.speaker.Person(ctx, text)
	if err != nil {
		a.log.Warn("speaker-id did not answer", "error", err)
		return ""
	}
	a.log.Debug("speaker-id", "person", person, "score", score)
	return person
}

func anyAloneIsMe(persons map[string]config.Person) bool {
	for _, p := range persons {
		if p.AloneIsMe && p.Entity != "" {
			return true
		}
	}
	return false
}

// personOfUser maps a HA user to the person Home Assistant linked it to.
func personOfUser(persons map[string]config.Person, user string) string {
	for key, p := range persons {
		if p.UserID != "" && p.UserID == user {
			return key
		}
	}
	return ""
}

// aloneAtHome is the person the satellites count as: the only configured
// person at home, if they switched the rule on. A person whose presence is
// unknown might be home, so then nobody is alone.
func aloneAtHome(persons map[string]config.Person, states []haws.State) string {
	byEntity := map[string]string{}
	for _, s := range states {
		byEntity[s.EntityID] = s.State
	}
	home := ""
	for key, p := range persons {
		if p.Entity == "" {
			continue
		}
		switch byEntity[p.Entity] {
		case "home":
			if home != "" {
				return ""
			}
			home = key
		case "", "unknown", "unavailable":
			return ""
		}
	}
	if home == "" || !persons[home].AloneIsMe {
		return ""
	}
	return home
}
