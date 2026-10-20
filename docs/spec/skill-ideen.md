# Skills zum Bauen – Ideenliste

Stand 2026-10-18. Nichts davon ist gebaut oder beschlossen; die Liste sammelt, was sich lohnt, und
sagt, was jeweils dafür gebraucht wird. Die Reihenfolge ist ein Vorschlag. Wie Skills aufgebaut sind:
[Guide, „Eigenen Skill erstellen“](../guide/de.md); was Assist schon kann und was Mira darüber hinaus baut:
[Plan: weitere Werkzeuge und Skills](skills-plan.md).

Das Muster: Ein Skill bündelt Anleitung, Einstellungen und Zeitpläne. Er nutzt die Werkzeuge, die Mira
ohnehin hat (Kalender, Listen, Suche, Haussteuerung, Gedächtnis, Nachrichten). Die Stärke gegenüber
Alexa-Skills ist der Dialog, das Wissen über Haus und Personen und das Melden von selbst.

## Must have (Alexa kann es, Mira soll es besser können)

### Hörbuch weiterhören

„Lies mein Hörbuch weiter.“ – „Welches Kapitel war ich?“ – „Fang das Buch von vorn an.“ – „Spiel Kapitel 5.“

- **Über Music Assistant.** Er kennt Hörbücher (Ordner mit Kapiteln, m4b/m4a oder viele MP3 in CD1, CD2 …), führt die
  Warteschlange und merkt sich die Position selbst. Wer die Funktion will, installiert Music Assistant; ohne ihn gibt es
  den Skill nicht. Der Show kann in Home Assistant nicht an eine Stelle springen und keine Warteschlange führen
  (die Funktionen des Players), deshalb reicht ein direktes Abspielen nicht.
- **Mira ist die Stimme davor:** Sie versteht den Wunsch, wählt Buch und Player (Raum), startet das Hörbuch und
  weiß, welches Buch zu wem gehört („Annas Buch“, „das Buch von Ben“). Sie sagt ehrlich, wenn Music Assistant fehlt
  oder das Buch nicht gefunden wird.
- **Zu klären vor dem Bauen:** Kann Music Assistant im Haushalt Hörbücher mit Position abspielen (Test im Live-HA)?
  Welche Werkzeuge bietet er Mira an, oder braucht es ein eigenes Werkzeug wie bei `radio_play`?
- **Dazu der Einschlaf-Timer** (unten), denn Hörbuch und Einschlafen gehören zusammen.

### Einschlaf-Timer

Gebaut in 0.6.45, siehe [Geplante Aktionen](geplante-aktionen.md).

„Schlaf-Timer 30 Minuten.“ – „Schalte in 20 Minuten alles aus.“ – „Noch zehn Minuten.“ – „Doch abbrechen.“

- Nach der gewählten Zeit hört das Abspielen auf (Hörbuch, Radio, Musik), auf Wunsch leiser werdend, und das Licht geht
  aus. Beim Hörbuch bleibt die Stelle erhalten.
- **Neu im Kern:** Mira kann heute zu einer Zeit **etwas sagen** (Erinnerung), aber nicht zu einer Zeit **etwas tun**.
  Der Timer braucht einmalige, geplante Aktionen: etwa als kurzlebige Automation in Home Assistant, die danach
  verschwindet, oder als geplanter Aufruf im Agenten. Das gleiche brauchen „Mach in einer Stunde das Licht aus“ und
  Szenen mit Verzögerung.
- Verlängern und Abbrechen im Gespräch („Noch zehn Minuten“).

## Voraussetzungen, die mehrere Skills brauchen

- **Adresse abspielen.** Ein Werkzeug, das einen Ton oder Stream auf dem Lautsprecher eines Raums startet,
  wie `radio_play`, aber für beliebige Adressen (Geräusche, Nachrichten als Ton). Braucht: Szenen, Einschlafen,
  Nachrichten.
- **Alter der Personen (optional).** Wer ein Kind ist, bekommt andere Geschichten, Quizfragen und Aufgaben.
  Das Alter ist freiwillig; ohne Angabe nimmt Mira an, es sei ein Erwachsener. Braucht: Geschichte, Quiz, Lernen.
- **Zustand für Skills.** Ein Skill soll sich etwas merken können, was nicht im Gespräch steht: welches Hörbuch, welche
  Geschichten schon gehört, Quizpunkte, was beim Lernen sitzt. Heute kann ein Skill nur das Gedächtnis (Fakten als Text)
  nutzen, ohne gezieltes Ändern; dazu der Abschnitt „Wie ein Skill sich etwas merkt“ unten.
- **Gedächtnis importieren und exportieren.** Haus-Wissen als Datei, die man von Hand pflegt. Braucht: Handbuch.

## 1. Szenen: „Gute Nacht“, „Guten Morgen“, „Heimkino geht los“

Bei Alexa heißen sie Szenen oder Routinen: ein Satz löst mehrere Aktionen aus (Licht aus, Rollläden zu,
Alarmanlage scharf, Wecker, Geräusche zum Einschlafen).

- **Home Assistant kennt Szenen und Skripte** als Entitäten, die man für Assist freigeben kann. Ob
  Mira sie damit schon über „Schalte die Szene Gute Nacht ein“ aktiviert, ist zu prüfen.
- **Was Mira dazu beitragen kann:** Eine Szene im Gespräch anlegen („Lern die Szene Gute Nacht: …“), geprüft
  und nach einem Ja angelegt, so wie Aufgaben als Automationen angelegt werden. Sie liegt dann in Home Assistant und
  wird dort geändert.
- **Wichtige Aktionen** (Schloss, Alarm, Garagentor) in einer Szene fragen weiter nach, wie überall.

## 2. Morgen- und Abendbriefing

Gebaut in 0.6.51 als Skill `briefing` (`skills/briefing`): Zeitpläne 5:30 und 18:00 fürs Haus, auf Nachfrage auch mit dem Kalender der sprechenden Person. Seit 0.6.52 zusätzlich 5:35 je Person, die eine persönliche Nachricht erreicht (`per_person`): eigene Termine (Kalender heißt wie die Person) und Schlagzeilen zu ihren Themen aus dem persönlichen Gedächtnis.

Zur gewählten Zeit meldet sich Mira von selbst: Wetter, Termine, Müllabfuhr, wartende Nachrichten, ein paar
Nachrichten des Tages. Das ist der Flash-Briefing-Klassiker, nur persönlich.

- Zeitplan im Skill, Einstellungen: welche Bausteine, in welchem Raum.
- Nutzt vorhandene Werkzeuge: Kalender, Wetter, Suche, Nachrichten. Auf Wunsch zusätzlich als Ton (Adresse abspielen).

## 3. Kochen mit Dialog

Rezept suchen, Schritt für Schritt vorlesen, Timer stellen, Zutaten auf die Einkaufsliste setzen. Allergien und
Vorlieben kommen aus dem Gedächtnis („Maria isst keine Nüsse“). Mira antwortet auf „Was kommt als Nächstes?“ und
„Wie lange noch?“ statt eines festen Ablaufs.

## 4. Vorlesen: Geschichten für Kinder

Mira liest eine Geschichte vor, passend zum Alter, zur Länge und zum Anlass. Es gibt zwei Quellen, die sich
ergänzen:

- **Selbst erzählt.** Mira erfindet eine Geschichte nach Wunsch („eine mit einem Drachen, zehn Minuten“), mit den Namen
  der Kinder. Keine Rechtefrage, aber jedes Mal neu.
- **Aus einer Sammlung.** Beispiel: [Zwergenstark, Kindergeschichten](https://zwergenstark.de/kindergeschichten/).
  Die Seite ordnet ihre Geschichten so, wie ein Skill sie braucht:
  - **Themen:** Gute-Nacht-Geschichten, Tiergeschichten, Abenteuergeschichten, Fantasiereisen (zum Einschlafen),
    Beliebte Geschichten, nach Jahreszeit sowie Weihnachten und Halloween.
  - **Alter:** an jeder Geschichte steht eine Altersangabe („ab 3 Jahren“, „Ab 6-8 Jahre“, „3-7 Jahre“, „8-12 Jahre“).
    Die Schreibweisen sind uneinheitlich, der Skill rechnet sie auf eine Altersspanne um und vergleicht sie mit dem
    Alter der Person (siehe oben). Ohne Alter fragt Mira einmal nach, was passt.
  - **Dauer:** „ca. 5 Min.“ bis etwa 37 Minuten. Der Skill wählt die Länge, die zum Anlass passt („kurz vor dem
    Einschlafen“ höchstens 10 Minuten). Lange Geschichten liest Mira in Teilen („Ich lese morgen weiter“) und
    merkt sich die Stelle.
  - **Formate:** Text zum Vorlesen, außerdem Hörgeschichten (Audio), Gedichte und Mitmach-Geschichten. Audio braucht
    „Adresse abspielen“, der Rest geht über die Stimme von Mira.
- **Was Mira dazu tut:** Sie merkt sich, was ein Kind schon gehört hat, und wiederholt es nicht ungefragt. Sie schlägt
  zur Jahreszeit passende Geschichten vor (Weihnachten, Halloween). Nach der Geschichte fragt sie, ob das Licht ausgehen
  und ein Einschlafgeräusch (siehe Szenen) laufen soll.
- **Wie sie an die Geschichten kommt:** Die Seite hat nach meinem Eindruck keinen Feed und keine Schnittstelle, nur die
  Übersicht mit Kacheln (Titel, Alter, Dauer) und eine Suche. Der Skill würde mit der Internetsuche und dem Lesen der
  Seite arbeiten, begrenzt auf diese Adresse.
- **Vorher klären:** Die Inhalte sind urheberrechtlich geschützt, und zu Vorlesen oder Nutzung in einer Anwendung
  habe ich in den Angaben der Seite nichts gefunden (AGB und Impressum habe ich nicht gelesen). Vor dem Bauen die
  Bedingungen lesen und beim Betreiber nachfragen, ob das Vorlesen im eigenen Haushalt in Ordnung ist, oder ob es
  einen Weg gibt (Feed, Erlaubnis). Bis dahin bleibt die selbst erzählte Variante.

## 5. Quiz

Fragen nach Thema und Schwierigkeit, Punktestand über mehrere Spieler, die Mira an ihren Namen erkennt.
Altersgerecht, wenn das Alter bekannt ist.

## 6. Lernen und Vokabeln (Schule)

Abfragen und Üben: Vokabeln, kleines Einmaleins, Fragen zu einem Schulthema. Mira merkt sich, was jemand
schon kann und was noch nicht, und fragt Schwächeres öfter ab. Der Stoff kann aus einer Liste im Gedächtnis kommen
(Vokabeln der Woche) oder frei erfragt werden. Braucht das Alter und das Gedächtnis je Person.

## 7. Haus-Handbuch

„Wo ist der Sicherungskasten?“ – „Wie entkalke ich die Kaffeemaschine?“ – „Wann kommt der Schornsteinfeger?“
Antwort aus dem Haus-Wissen, das man ins Gedächtnis importiert hat. Mira sagt ehrlich, wenn etwas nicht drinsteht.

## 8. Fahrplan

„Wann fährt der nächste Bus?“ Über Sensoren, die eine HA-Integration für die Haltestelle liefert. Der Skill hat
eine Einstellung für den Sensor und antwortet kurz mit der Abfahrt.

## Beobachtet, aber nicht im Plan

- **Skills mit Marken-Anbindung** (Fahrdienst, Lieferdienst): brauchen Dienste und Konten, die Mira nicht hat.
- **Musik** (Interpreten, Alben): Sache von Music Assistant, siehe [Plan](skills-plan.md).

## Wie ein Skill sich etwas merkt

Ein Skill hat keinen eigenen Speicher und führt keinen Code aus. Was er hat:

1. **Das Gedächtnis** (`memory_save`, `memory_search`, `memory_forget`): Fakten als Text, je Person oder fürs Haus,
   durchsuchbar über Stichwörter. Das reicht für „Anna hört gerade ‚Der kleine Prinz‘“. Nachteil: Es gibt kein
   „ändern“, nur neu speichern und das alte vergessen, und gefunden wird über Wörter, nicht über feste Felder.
2. **Das System, das den Zustand schon führt:** Bei Hörbüchern ist das Music Assistant (Position, Warteschlange). Mira
   muss sich dann nur merken, welches Buch gemeint ist, und fragt für den Rest dort nach. Das ist der sauberste Weg.
3. **Ein einfacher Speicher je Skill (offen):** Ein Skill bekäme ein paar benannte Werte je Person („Buch“,
   „Kapitel“, „Punkte“), die Mira lesen und setzen kann. Das brauchen mehrere Ideen hier: Hörbuch (falls MA die Stelle
   nicht liefert), Quiz, Lernen, Geschichten (schon gehört). Ob das nötig ist, entscheidet sich beim ersten Skill,
   der es wirklich braucht.
