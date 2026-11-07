package clip

import "github.com/miraassist/mira/internal/i18n"

// texts are the tool's description and results: facts for the model, which
// words them itself.
type texts struct {
	desc, argURL, argTitle, argSubtitle string
	notVoice, noSpeaker, queued, failed string
	source                              string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		desc:        "Spielt einen kurzen Clip (die Adresse einer Audiodatei) am Satelliten, an dem gerade gesprochen wird, direkt nach deiner Antwort, als Musik. Sein Bildschirm zeigt dabei nur Titel und Untertitel, die du angibst, nicht, was spielt. Ein neuer Clip beendet den, der noch spielt. Nur dort, wo ein Skill es sagt.",
		argURL:      "Die Adresse des Clips, https://…",
		argTitle:    "Was der Bildschirm als Titel zeigt, z. B. „Musik-Quiz“; nie, was im Clip spielt.",
		argSubtitle: "Was der Bildschirm darunter zeigt, z. B. „Titel 3 von 10“.",
		notVoice:    "Ergebnis: nichts abgespielt. Grund: Clips spielen nur an einem Satelliten mit Lautsprecher, nicht hier.",
		noSpeaker:   "Ergebnis: nichts abgespielt. Grund: Dieser Satellit hat keinen Lautsprecher, auf dem Mira abspielen kann.",
		queued:      "Ergebnis: Der Clip spielt am Satelliten direkt nach deiner Antwort. Sag nicht, was darin spielt.",
		failed:      "Der letzte Clip ließ sich am Satelliten nicht abspielen.",
		source:      "Mira",
	},
	i18n.EN: {
		desc:        "Plays a short clip (the address of an audio file) at the satellite being spoken at, right after your answer, as music. Its screen shows only the title and subtitle you give, not what plays. A new clip ends the one still playing. Only where a skill says so.",
		argURL:      "The clip's address, https://…",
		argTitle:    "What the screen shows as the title, e.g. \"Music quiz\"; never what plays in the clip.",
		argSubtitle: "What the screen shows below it, e.g. \"Song 3 of 10\".",
		notVoice:    "Result: nothing played. Reason: clips play only at a satellite with a speaker, not here.",
		noSpeaker:   "Result: nothing played. Reason: this satellite has no speaker Mira can play on.",
		queued:      "Result: the clip plays at the satellite right after your answer. Do not say what plays in it.",
		failed:      "The last clip could not be played at the satellite.",
		source:      "Mira",
	},
}
