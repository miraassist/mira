package inbox

import "github.com/miraassist/mira/internal/i18n"

// texts are the inbox's messages to the people.
type texts struct {
	// Messages; %s is the document's title.
	unclear, more, cut, noText, unreachable, unreadable, notMarked string
}

var catalog = map[i18n.Lang]texts{
	i18n.DE: {
		unclear:     "In der Post: %s. Darin: %s. Was soll ich damit machen?",
		more:        "%d weitere im Dokument",
		cut:         " Ich habe nur den Anfang gelesen.",
		noText:      "In „%s“ konnte ich keinen Text lesen.",
		unreachable: "Ich komme an „%s“ in Paperless nicht heran.",
		unreadable:  "Ich konnte „%s“ gerade nicht lesen, ich versuche es beim nächsten Start noch einmal.",
		notMarked:   "Ich konnte „%s“ in Paperless nicht als gelesen markieren; bitte setz das Tag Mira gelesen selbst.",
	},
	i18n.EN: {
		unclear:     "In the post: %s. In it: %s. What shall I do with it?",
		more:        "%d more in the document",
		cut:         " I only read the beginning.",
		noText:      "I could not read any text in “%s”.",
		unreachable: "I cannot get at “%s” in Paperless.",
		unreadable:  "I could not read “%s” just now; I will try again at the next start.",
		notMarked:   "I could not mark “%s” as read in Paperless; please set the tag Mira gelesen yourself.",
	},
}
