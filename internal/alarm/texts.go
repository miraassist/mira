package alarm

import (
	"strings"

	"github.com/miraassist/mira/internal/i18n"
)

type texts struct {
	desc, actionDesc, idDesc                    string
	badAction, noSatellite, badTime, badDays    string
	cannot, failed, needAccess, tooMany         string
	title, couldNotRing                         string
	noLight, noMusic, severalMusic, withLight   string
	musicUnavailable, withMusic                 string
	needsMusicAssistant                         string
	once, daily, repeats, today, tomorrow, days string
	none, listLine, several, noMatch, deleted   string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		desc: "Miras Wecker zeigen oder löschen: list zeigt die Wecker der Person („Welche Wecker habe ich?“), delete löscht einen („Lösch den Wecker“), auch während er klingelt. " +
			"Gestellt wird ein Wecker im Skill wecker, nicht hier. Nur Wecker: Schlaf-Timer zeigt sleep_timer, Kurzzeit-Timer HassTimerStatus; bei „Welche Timer habe ich?“ also nicht dieses Werkzeug.",
		actionDesc:          "list oder delete.",
		idDesc:              "delete: Kennung aus list; weglassen, wenn die Person nur einen Wecker hat.",
		badAction:           "Unbekannte action; erlaubt sind list, delete.",
		noSatellite:         "Ergebnis: kein Wecker. Grund: Einen Wecker stelle ich nur an einem Satelliten, in dem Raum, wo er klingeln soll. Sag das dem Nutzer.",
		badTime:             "Ergebnis: kein Wecker. Grund: Die Uhrzeit %q ist nicht HH:MM.",
		badDays:             "Ergebnis: kein Wecker. Grund: Die Tage %q verstehe ich nicht; erlaubt: leer, weekdays, weekends, daily oder Tage wie mon,wed.",
		cannot:              "Ergebnis: kein Wecker. Grund: Dieser Satellit kann keinen Wecker: ihm fehlt ein Lautsprecher, dessen Lautstärke sich setzen lässt und der Ansagen spielt. Sag dem Nutzer, dass du hier keinen Wecker stellen kannst.",
		failed:              "Ergebnis: kein Wecker. Grund: %s.",
		needAccess:          "Ergebnis: kein Wecker. Grund: Mira braucht erst wieder einen Zugang: Einstellungen → Reparaturen.",
		tooMany:             "Ergebnis: kein Wecker. Grund: Die Person hat in diesem Raum schon %d Wecker; erst einen löschen.",
		title:               "Wecker %s",
		couldNotRing:        "Dein Wecker %s im Raum %s konnte nicht klingeln: der Lautsprecher war nicht erreichbar oder die Musik spielte nicht.",
		withMusic:           "mit %s",
		withLight:           "mit Licht",
		noLight:             "Ergebnis: kein Wecker. Grund: Im Raum %s ist kein Licht für Mira freigegeben. Frag den Nutzer, ob der Wecker ohne Licht klingeln soll.",
		noMusic:             "Ergebnis: kein Wecker. Grund: Zu %q habe ich nichts gefunden. Frag den Nutzer, womit er geweckt werden will.",
		needsMusicAssistant: "Ergebnis: kein Wecker. Grund: Mit %q wecken geht nur mit Music Assistant, und im Raum %s gibt es keinen Player davon; ohne geht nur ein Radiosender. Frag den Nutzer, ob ein Sender oder der Weckton recht ist.",
		severalMusic:        "Ergebnis: kein Wecker. Grund: Zu %q passt mehreres: %s. Frag den Nutzer, was es sein soll, und stell den Wecker mit dem genauen Namen.",
		musicUnavailable:    "Ergebnis: kein Wecker. Grund: Die Suche nach %q geht gerade nicht (%s). Frag den Nutzer, ob der Wecker mit dem Weckton klingeln soll.",
		once:                "%s (%s, %d.%d.) um %s",
		daily:               "täglich um %s",
		repeats:             "%s um %s",
		today:               "heute",
		tomorrow:            "morgen",
		days:                "Mo,Di,Mi,Do,Fr,Sa,So",
		none:                "Keine Wecker.",
		listLine:            "%s: %s, Raum %s",
		several:             "Es gibt mehrere Wecker: %s. Frag den Nutzer, welcher es sein soll, und ruf das Werkzeug dann mit id auf.",
		noMatch:             "Dazu gibt es keinen Wecker.",
		deleted:             "Der Wecker ist gelöscht.",
	},
	i18n.EN: {
		desc: "Show or delete Mira's alarms: list shows the person's alarms (\"which alarms do I have?\"), delete deletes one (\"delete the alarm\"), also while it rings. " +
			"An alarm is set in the skill wecker, not here. Alarms only: sleep timers are listed by sleep_timer, countdown timers by HassTimerStatus; so not this tool for \"which timers do I have?\".",
		actionDesc:          "list or delete.",
		idDesc:              "delete: id from list; leave out if the person has only one alarm.",
		badAction:           "Unknown action; allowed are list, delete.",
		noSatellite:         "Result: no alarm. Reason: I set an alarm only at a satellite, in the room where it is to ring. Tell the user.",
		badTime:             "Result: no alarm. Reason: the time %q is not HH:MM.",
		badDays:             "Result: no alarm. Reason: I do not understand the days %q; allowed: empty, weekdays, weekends, daily or days like mon,wed.",
		cannot:              "Result: no alarm. Reason: this satellite cannot wake: it has no speaker whose volume can be set and that plays announcements. Tell the user you cannot set an alarm here.",
		failed:              "Result: no alarm. Reason: %s.",
		needAccess:          "Result: no alarm. Reason: Mira needs an access again first: Settings → Repairs.",
		tooMany:             "Result: no alarm. Reason: the person already has %d alarms in this room; delete one first.",
		title:               "Alarm %s",
		couldNotRing:        "Your alarm %s in %s could not ring: the speaker could not be reached or the music did not play.",
		withMusic:           "with %s",
		withLight:           "with light",
		noLight:             "Result: no alarm. Reason: no light in %s is released for Mira. Ask the user whether the alarm should ring without light.",
		noMusic:             "Result: no alarm. Reason: I found nothing for %q. Ask the user what to wake with.",
		needsMusicAssistant: "Result: no alarm. Reason: waking with %q needs Music Assistant, and %s has no player of it; without it only a radio station works. Ask the user whether a station or the wake sound will do.",
		severalMusic:        "Result: no alarm. Reason: several things fit %q: %s. Ask the user which one, then set the alarm with its exact name.",
		musicUnavailable:    "Result: no alarm. Reason: searching for %q does not work right now (%s). Ask the user whether the alarm should ring with the wake sound.",
		once:                "%s (%s, %d/%d) at %s",
		daily:               "every day at %s",
		repeats:             "%s at %s",
		today:               "today",
		tomorrow:            "tomorrow",
		days:                "Mon,Tue,Wed,Thu,Fri,Sat,Sun",
		none:                "No alarms.",
		listLine:            "%s: %s, room %s",
		several:             "There are several alarms: %s. Ask the user which one, then call the tool with id.",
		noMatch:             "There is no alarm for that.",
		deleted:             "The alarm is deleted.",
	},
}

// dayName is a weekday's short name (0 Monday) in the language.
func (c *controller) dayName(i int) string {
	names := strings.Split(c.t.days, ",")
	if i < 0 || i >= len(names) {
		return ""
	}
	return names[i]
}
