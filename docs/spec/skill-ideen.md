# Skills zum Bauen – Ideenliste

Stand 2026-10-18. Nichts davon ist gebaut oder beschlossen; die Liste sammelt, was sich lohnt, und
sagt, was jeweils dafür gebraucht wird. Die Reihenfolge ist ein Vorschlag. Wie Skills aufgebaut sind:
[Guide, „Eigenen Skill erstellen“](../guide/de.md); was Assist schon kann und was Mira darüber hinaus baut:
[Plan: weitere Werkzeuge und Skills](skills-plan.md).

Das Muster: Ein Skill bündelt Anleitung, Einstellungen und Zeitpläne. Er nutzt die Werkzeuge, die Mira
ohnehin hat (Kalender, Listen, Suche, Haussteuerung, Gedächtnis, Nachrichten). Die Stärke gegenüber
Alexa-Skills ist der Dialog, das Wissen über Haus und Personen und das Melden von selbst.

## Voraussetzungen, die mehrere Skills brauchen

- **Adresse abspielen.** Ein Werkzeug, das einen Ton oder Stream auf dem Lautsprecher eines Raums startet,
  wie `radio_play`, aber für beliebige Adressen (Geräusche, Nachrichten als Ton). Braucht: Szenen, Einschlafen,
  Nachrichten.
- **Alter der Personen (optional).** Wer ein Kind ist, bekommt andere Geschichten, Quizfragen und Aufgaben.
  Das Alter ist freiwillig; ohne Angabe nimmt Mira an, es sei ein Erwachsener. Braucht: Geschichte, Quiz, Lernen.
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

Zur gewählten Zeit meldet sich Mira von selbst: Wetter, Termine, Müllabfuhr, wartende Nachrichten, ein paar
Nachrichten des Tages. Das ist der Flash-Briefing-Klassiker, nur persönlich.

- Zeitplan im Skill, Einstellungen: welche Bausteine, in welchem Raum.
- Nutzt vorhandene Werkzeuge: Kalender, Wetter, Suche, Nachrichten. Auf Wunsch zusätzlich als Ton (Adresse abspielen).

## 3. Kochen mit Dialog

Rezept suchen, Schritt für Schritt vorlesen, Timer stellen, Zutaten auf die Einkaufsliste setzen. Allergien und
Vorlieben kommen aus dem Gedächtnis („Maria isst keine Nüsse“). Mira antwortet auf „Was kommt als Nächstes?“ und
„Wie lange noch?“ statt eines festen Ablaufs.

## 4. Gute-Nacht-Geschichte

Eine Geschichte nach Wunsch, mit den Namen der Kinder, passend zum Alter, in der Länge, die man sagt. Optional
mit Einschlafgeräuschen danach (Adresse abspielen) und Licht, das langsam ausgeht.

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
