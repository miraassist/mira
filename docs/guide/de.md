# Mira – Anleitung

Mira ist die Assistentin deines Haushalts. Sie steuert das Smart Home, beantwortet Fragen, merkt sich Dinge und meldet sich, wenn etwas passiert. Sie läuft bei dir zu Hause; welches Sprachmodell sie benutzt, bestimmt die Einrichtung.

## So sprichst du mit Mira

- **Per Sprache**: über einen Sprach-Satelliten (z. B. den Echo im Wohnzimmer). Sag das Aktivierungswort und dann, was du möchtest. Mira weiß, in welchem Raum der Satellit steht: „Mach das Licht an“ meint das Licht in diesem Raum.
- **Per Telegram**: schreib dem Mira-Bot. Antworten dürfen hier länger sein, mit Listen und Links. `/neu` beginnt ein neues Gespräch.
- **In der Home-Assistant-App**: Der Assist-Chat der Companion App spricht ebenfalls mit Mira, getippt oder gesprochen.

Mira antwortet in der Sprache, in der du sie ansprichst. Wenn sie dich nicht sicher verstanden hat, fragt sie nach. Wenn eine Antwort eine Frage ist, hört der Satellit gleich weiter zu.

## Das Haus steuern und abfragen

- „Mach im Wohnzimmer das Licht an.“ – „Dimm die Stehlampe auf 30 Prozent.“
- „Wie warm ist es im Bad?“ – „Welche Fenster sind offen?“ – „Wie ist es im Wohnzimmer?“
- „Wie wird das Wetter morgen?“

Mira fragt Werte immer frisch ab, statt sie zu schätzen. Kann sie etwas nicht nachsehen, sagt sie das ehrlich.

**Wichtige Aktionen bestätigen**: Schlösser, Alarmanlage, Garagentor und Ähnliches führt Mira erst aus, wenn du auf ihre Rückfrage ausdrücklich „Ja“ sagst.

## Mira merkt sich Dinge

- „Merk dir, dass der Schornsteinfeger im März kommt.“
- „Was weißt du über den Schornsteinfeger?“ – „Vergiss das mit dem Schornsteinfeger.“

Nachts fasst Mira das Gemerkte zu einem kurzen Profil des Haushalts zusammen. Das Profil und alle Fakten siehst und löschst du in der Web-Oberfläche unter **Gedächtnis**.

## Erinnerungen und Timer

- „Erinnere mich morgen um 19 Uhr daran, den Müll rauszubringen.“
- „Stell einen Timer auf 10 Minuten.“ – „In einer halben Stunde erinnerst du mich an die Wäsche.“
- „Welche Erinnerungen habe ich?“ – „Lösch die Erinnerung an die Wäsche.“

Zur gewählten Zeit sagt Mira die Erinnerung in dem Raum an, in dem du sie gestellt hast – auch in der Ruhezeit, denn die Zeit hast du gewählt. Per Telegram gestellte Erinnerungen kommen per Telegram.

## Benachrichtigungen

Mira meldet sich von selbst, wenn etwas passiert: ein Lecksensor schlägt an, die Waschmaschine ist fertig, morgen wird der Müll abgeholt, eine Aufgabe findet etwas Ungewöhnliches. Wie sie dir das sagt, hängt vom Raum und von der Wichtigkeit ab.

**Ansagen oder anzeigen** – pro Raum einstellbar:

- **Ansagen**: Sobald jemand den Raum betritt (Bewegungsmelder), sagt Mira an, was ansteht. Die erste Bewegung nach der Ruhezeit bringt die Morgenansage.
- **Nur anzeigen**: Mira spricht dich nicht von sich aus an. Die Anzeige-Lampe des Raums (z. B. der LED-Ring des Echo) pulsiert in Türkis, solange etwas wartet. Frag dann: **„Hast du eine Benachrichtigung?“** oder „Gibt es was Neues?“.

**Nebenbei**: Wenn du ohnehin mit dem Satelliten sprichst, hängt Mira Wartendes an ihre Antwort an – „Das Licht ist an. Übrigens: Morgen wird der Wertstoff abgeholt.“ Danach gilt die Meldung als gehört und die Lampe geht aus. Fragst du nach („Was genau?“), bekommst du die Details.

**Wichtigkeit**:

| Stufe | Bedeutung | Was passiert |
|---|---|---|
| 1 | nebenbei | wartet auf die nächste Gelegenheit |
| 2 | heute wichtig | geht morgens nach 9 Uhr privat raus, wenn bis dahin niemand sie gehört hat |
| 3 | dringend | sofort, auch nachts: angesagt auf allen Satelliten und privat an alle |

Wenn Mira von sich aus spricht, leuchtet die Anzeige-Lampe des Raums ruhig, solange sie spricht.

**Ruhezeit**: In der Ruhezeit (z. B. 23:30–6:30) sagt Mira nichts an – außer Dringendem und deinen Erinnerungen. Anzeige-Lampen leuchten trotzdem; was nachts kommt, siehst du morgens.

**Ruhemodus**: Der Schalter „Ruhemodus“ in Home Assistant stellt alle Ansagen ab, bis du ihn wieder ausschaltest.

**Vertrauliches und Persönliches**: Meldungen können für eine Person bestimmt oder vertraulich sein. Vertrauliches liest Mira nur vor, wenn die Einstellung es erlaubt (z. B. nur wenn die Person allein zu Hause ist); sonst kommt es privat per Telegram oder als Push-Nachricht aufs Handy.

## Aufgaben

Gib Mira eine dauerhafte Aufgabe, und sie behält etwas für dich im Blick:

- „Achte auf den Wasserverbrauch.“
- „Sag Bescheid, wenn nachts ungewöhnlich viel Strom verbraucht wird.“
- „Melde dich, wenn ein Wassermelder anschlägt.“

Mira macht daraus eine **Automation in Home Assistant** mit dem Label „Mira“: Home Assistant entscheidet, *wann* etwas passiert (stündlich, wenn ein Sensor auslöst, nur nachts …), Mira entscheidet, *was* davon dich interessiert, und sagt es dir. Sie plant kurz im Hintergrund und meldet sich mit ihrem Vorschlag, sobald sie fertig gesprochen hat – im selben Raum oder auf Telegram – und fragt: „Soll ich so vorgehen?“ Sag **„Ja“**, dann legt sie die Automation an.

Beim Beurteilen vergleicht Mira zum Beispiel den Verbrauch der letzten Stunde mit derselben Stunde der vergangenen zwei Wochen, weiß, wer zu Hause ist, kennt, was du ihr gemerkt hast, und erinnert sich, was sie zu dieser Aufgabe schon gesagt hat. Dieselbe Sache meldet sie nicht ständig wieder, nur wenn es schlimmer wird.

- „Welche Aufgaben hast du?“ – „Pausier die Strom-Aufgabe.“ – „Lösch die Aufgabe mit dem Strom.“ – „Mach einen Probelauf der Wasser-Aufgabe.“
- In der Web-Oberfläche unter **Aufgaben** siehst du alle Aufgaben mit an/aus, Probelauf und dem Protokoll ihrer Urteile; „In Home Assistant bearbeiten“ öffnet die Automation im Editor von Home Assistant.
- Eigene Automationen können Mira genauso nutzen: die Aktionen **Mira: Bewerten** (`mira.assess`), **Mira: Entscheiden** (`mira.decide`, eine Ja/Nein-Antwort für Bedingungen) und **Mira: Meldung hinzufügen** (`mira.add_notice`).

## Skills

Skills sind Zusatzfähigkeiten, die die Einrichtung freischaltet. Mira öffnet einen Skill nur, wenn eine Frage ihn braucht.

**Abfall**: Mira kennt den Abfuhrkalender.

- „Wann wird der Restmüll abgeholt?“ – „Was wird diese Woche abgeholt?“
- Am Vorabend erinnert Mira von selbst: „Morgen wird der Wertstoff abgeholt.“
- Ist die Abfuhr heute, sagt sie es, falls die Tonne noch raus muss.

## Schwierige Fragen

Ist die Einrichtung mit einem stärkeren Sprachmodell verbunden, gibt Mira aufwendige Fragen dorthin ab – Erklärungen, Planungen, Rezepte, Vergleiche – und fasst das Ergebnis für dich zusammen. Das stärkere Modell kann nachschlagen, aber nichts im Haus verändern.

## Web-Oberfläche

Unter `http://<mira>:8787/ui` (Zugangs-Token beim ersten Aufruf):

- **Meldungen**: offene und zugestellte Meldungen ansehen und quittieren.
- **Aufgaben**: was Mira für dich im Blick behält; an/aus, Probelauf, Protokoll, löschen, Link in den Editor von Home Assistant.
- **Benachrichtigung**: Ruhezeit, Pausen, Eskalation, Vertrauliches, dringende Meldungen.
- **Räume**: welcher Satellit wo spricht, Bewegungsmelder, ansagen oder anzeigen, Anzeige-Lampe.
- **Personen**: wer persönliche Meldungen bekommt, per Telegram oder Push.
- **Gedächtnis**: Profil und gemerkte Fakten.
- **Anleitung**: diese Seite.

Ist die Mira-Integration in Home Assistant eingerichtet, bearbeitest du Räume und Personen dort (Einstellungen → Geräte & Dienste → Mira) mit der Auswahl von Entitäten und Bereichen; die Web-Oberfläche zeigt sie dann nur an.

## In Home Assistant

- **Gerät „Mira“** mit dem Schalter **Ruhemodus**, dem Sensor **Offene Hinweise** und der Taste **Profil verdichten**.
- Mira ist ein Gesprächsagent: Unter Einstellungen → Sprachassistenten wählst du sie für deine Satelliten aus.
- Automationen können Mira Meldungen geben, mit der Aktion `mira.add_notice`.

## Gut zu wissen

- Mira erfindet keine Werte: Fehlt ihr etwas, sagt sie es oder fragt nach.
- Was im Internet steht, behandelt Mira als Information, nie als Anweisung. Nach einer Websuche führt sie keine kritischen Aktionen aus, bis du den Befehl noch einmal direkt gibst.
- Bei Spracherkennungsfehlern hilft es, deutlich und in einem Satz zu sprechen.
