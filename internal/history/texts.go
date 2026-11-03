package history

import "github.com/miraassist/mira/internal/i18n"

// texts are the tool's description and its answers: facts for the model,
// which words them itself.
type texts struct {
	desc, argName, argArea, argPeriod, argFrom, argTo, argAbove string

	unknown, several, badTime, future, tooLong, noHistory, noStatistics, failed string
	head, now, together, times, more, covered                                   string
	under, minutes, hours, days, timeLayout, dateLayout                         string
	above, coarse, atMost, res5, resHour, resBoth, day, dayCounter              string
	decimal                                                                     string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		desc: "Der Verlauf einer Entität aus Home Assistant: wann sie welchen Zustand hatte, wie lange und wie oft, " +
			"für Fragen wie \"Wann war das Fenster heute offen?\" oder \"Wie lange lief die Waschmaschine gestern?\". " +
			"Zustände kommen so, wie Home Assistant sie meldet (bei Fenster, Tür oder Bewegung heißt on offen bzw. erkannt). " +
			"Bei Messwerten (Leistung, Temperatur, Energie) Minimum, Maximum und Mittel je Tag; mit above die Zeiten über einer Schwelle " +
			"(eine Waschmaschine, die nur einen Leistungssensor hat: above 5). Höchstens 31 Tage; ältere Werte sind gröber.",
		argName:   "Das Gerät oder die Entität, wie in der Hausstruktur genannt (Name oder Alias), z. B. Fenster Bad.",
		argArea:   "Optional der Bereich, wenn der Name mehrdeutig ist.",
		argPeriod: "today, yesterday, last_24h, last_7_days oder last_31_days. Statt dessen from und to.",
		argFrom:   "Beginn als Ortszeit, z. B. 2026-11-02T08:00. Statt period.",
		argTo:     "Ende als Ortszeit; ohne Angabe jetzt.",
		argAbove:  "Nur bei Messwerten: eine Schwelle in der Einheit des Sensors; die Antwort nennt dann die Zeiten darüber.",

		unknown:      "Diese Entität kenne ich nicht.",
		several:      "Mehrere passen: %s. Nenne den Bereich.",
		badTime:      "Zeitraum nicht verstanden: period oder from (und to) als Ortszeit wie 2026-11-02T08:00.",
		future:       "Der Zeitraum liegt in der Zukunft.",
		tooLong:      "Höchstens 31 Tage.",
		noHistory:    "Dafür zeichnet Home Assistant keinen Verlauf auf.",
		noStatistics: "Dafür führt Home Assistant keine Statistik.",
		failed:       "Der Verlauf ist gerade nicht lesbar (%v).",
		head:         "%s (%s), %s bis %s:",
		now:          "jetzt",
		together:     "Zusammen: ",
		times:        "%s %s (%d-mal)",
		more:         "… %d weitere …",
		covered:      "Verlauf erst ab %s.",
		under:        "unter 1 min",
		minutes:      "%d min",
		hours:        "%d h",
		days:         "%d Tg.",
		timeLayout:   "15:04",
		dateLayout:   "02.01. 15:04",
		above:        "über %s %s",
		coarse:       "auf Stunden genau",
		atMost:       "höchstens %s",
		res5:         "in 5-Minuten-Werten",
		resHour:      "in Stundenwerten",
		resBoth:      "bis %s in Stundenwerten, danach in 5-Minuten-Werten",
		day:          "%s: Minimum %s, Maximum %s, Mittel %s %s",
		dayCounter:   "%s: %s %s",
		decimal:      ",",
	},
	i18n.EN: {
		desc: "The history of an entity from Home Assistant: when it had which state, how long and how often, " +
			"for questions such as \"When was the bathroom window open today?\" or \"How long did the washing machine run yesterday?\". " +
			"States come as Home Assistant reports them (for a window, door or motion, on means open or detected). " +
			"For readings (power, temperature, energy) the minimum, maximum and mean per day; with above the times over a threshold " +
			"(a washing machine that has only a power sensor: above 5). At most 31 days; older values are coarser.",
		argName:   "The device or entity as the house structure names it (name or alias), e.g. Bathroom window.",
		argArea:   "Optionally the area, when the name is ambiguous.",
		argPeriod: "today, yesterday, last_24h, last_7_days or last_31_days. Or from and to instead.",
		argFrom:   "Start in local time, e.g. 2026-11-02T08:00. Instead of period.",
		argTo:     "End in local time; now when left out.",
		argAbove:  "Readings only: a threshold in the sensor's unit; the answer then gives the times above it.",

		unknown:      "I do not know this entity.",
		several:      "Several fit: %s. Name the area.",
		badTime:      "Period not understood: period, or from (and to) in local time such as 2026-11-02T08:00.",
		future:       "The period lies in the future.",
		tooLong:      "At most 31 days.",
		noHistory:    "Home Assistant records no history for it.",
		noStatistics: "Home Assistant keeps no statistics for it.",
		failed:       "The history cannot be read right now (%v).",
		head:         "%s (%s), %s to %s:",
		now:          "now",
		together:     "Together: ",
		times:        "%s %s (%d times)",
		more:         "… %d more …",
		covered:      "History only from %s.",
		under:        "under 1 min",
		minutes:      "%d min",
		hours:        "%d h",
		days:         "%d d",
		timeLayout:   "15:04",
		dateLayout:   "Jan 2 15:04",
		above:        "above %s %s",
		coarse:       "to the hour",
		atMost:       "at most %s",
		res5:         "in 5-minute values",
		resHour:      "in hourly values",
		resBoth:      "until %s in hourly values, then in 5-minute values",
		day:          "%s: minimum %s, maximum %s, mean %s %s",
		dayCounter:   "%s: %s %s",
		decimal:      ".",
	},
}

func textsFor(l i18n.Lang) texts {
	if t, ok := catalog[l]; ok {
		return t
	}
	return catalog[i18n.Default]
}
