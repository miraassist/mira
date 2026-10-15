# Benutzer: Haus und Personen – Spezifikation

Stand 2026-10-15. Spezifiziert, noch nicht gebaut. Gilt auch für die
[Aufgaben](aufgaben.md).

## Grundsatz

Ein Mira, zwei Arten von Bereichen:

- der **Haus-Bereich**, gemeinsam für alle;
- je ein **persönlicher Bereich** pro Person.

Gedächtnis, Aufgaben, Meldungen, Erinnerungen und Gesprächsverläufe haben einen **Besitzer**: `house` oder eine Person.

Es gibt keine getrennten Agenten. Das Hauswissen (Struktur, Geräte) bleibt eines.

| | Haus | Person |
|---|---|---|
| Gedächtnis (Fakten, Profil) | Haushaltsfakten | eigene Fakten, eigenes Profil |
| Aufgaben | „Achte auf den Wasserverbrauch“ | „Sag **mir** Bescheid, wenn …“ |
| Meldungen | Ansage im Raum, Anzeige-Lampe, „Übrigens“ | privat (Telegram, Handy-Push), am Satelliten nur nach der Regel „allein zu Hause“ |
| Erinnerungen | im Raum | auf den privaten Kanal |
| spricht von sich aus im Haus | ja | nur nach der Regel „allein zu Hause“ |

Rollen (etwa Kinder ohne Schlösser) und Freigaben per Handy gibt es vorerst nicht.

## Identität: wer spricht

Die Identität kommt vom Kanal:

| Kanal | Identität |
|---|---|
| Telegram | Person über die Telegram-ID (wie heute) |
| HA-App, Assist-Chat | Person über den angemeldeten HA-Benutzer. Die Integration reicht `user_input.context.user_id` weiter, Mira ordnet ihn über das Attribut `user_id` der HA-Personen-Entität zu. |
| Satellit | anonym, also Haus-Bereich. Ausnahme: die Regel „allein zu Hause“ |

Zu prüfen beim Bau: Satelliten-Gespräche kommen ohne Benutzer an.

## Regel „allein zu Hause“ (pro Person schaltbar)

Ist der Schalter einer Person an und ist sie laut ihrer Personen-Entität als einzige Person zu Hause, **gelten die Satelliten als diese Person**:
- Mira liest und schreibt dann ihr Gedächtnis;
- liest ihre vertraulichen Meldungen vor und darf sie von sich aus ansprechen;
- legt ihre persönlichen Aufgaben an.

Ist niemand oder mehr als eine Person zu Hause, sind die Satelliten anonym.

Grenze: Gäste ohne Personen-Entität zählen nicht. Ist Anna mit einem Gast zu Hause, gilt sie als allein. Das ist der Preis der Bequemlichkeit, deshalb ist der Schalter pro Person und standardmäßig aus.

Die bisherige globale Einstellung für vertrauliche Meldungen (`confidential_voice: when_alone`) geht in diesem Schalter auf.

## Gedächtnis

- **Haus:** lesen alle Kanäle, auch der anonyme Satellit. Schreiben dürfen alle.
- **Person X:** lesen und schreiben nur X. X muss dazu über ihren Kanal identifiziert sein, oder die Regel „allein zu Hause“ greift.
- Niemand schreibt in das Gedächtnis eines anderen. „Merk dir für Ben …“ wird zur Nachricht an Ben (siehe unten). Ben entscheidet dann selbst, ob er es sich merken lässt.
- Im Gespräch sieht das Modell das Hausgedächtnis und, bei bekannter Person, deren Gedächtnis. Die nächtliche Verdichtung läuft pro Besitzer.

## Nachrichten an andere

„Erinnere Ben morgen um 7 an den Müll“, „Sag Anna, dass es später wird.“

- Das ist eine **Meldung** mit **Empfänger** und **Absender**. Absender ist die Person, wenn sie bekannt ist, sonst „jemand im Wohnzimmer“.
- Mit Zeitpunkt wird daraus eine Erinnerung für den Empfänger, sonst wird sie gleich zugestellt.
- Wortlaut bei der Zustellung: „Anna erinnert dich: …“, „Jemand im Wohnzimmer lässt dir ausrichten: …“

**Einstellungen des Empfängers** (pro Person):

| Einstellung | Werte | Standard |
|---|---|---|
| Kanäle | Telegram-ID, Handy (Notify-Dienst der Companion-App) | wie heute |
| Allein zu Hause gelten die Satelliten als ich | an/aus | aus |
| Nachrichten annehmen | von allen (auch anonym am Satelliten) / nur von Personen / von niemandem | von allen |

Zugestellt wird immer über den privaten Kanal. Am Satelliten kommt eine Nachricht nur an, wenn die Regel „allein zu Hause“ für den Empfänger gilt.

## Aufgaben

- Eine Aufgabe hat einen Besitzer (`owner: house | <person>`).
  - Im eigenen Kanal angelegt („sag **mir** Bescheid“) gehört sie standardmäßig der Person.
  - Am anonymen Satelliten angelegt gehört sie dem Haus.
- `mira.assess` und `mira.decide` bekommen `owner` (Standard `house`):
  - Er bestimmt, welches Gedächtnis mitgeht (Haus, bei Personen dazu ihr eigenes).
  - Er bestimmt, wie zugestellt wird (Haus: Raum; Person: privat oder nach der Regel „allein zu Hause“).
- `audience` bleibt für Haus-Aufgaben, die nur eine Person erreichen sollen.

## Rechte

Mira schaltet weiter mit ihrem eigenen HA-Benutzer. Ob ein Befehl erlaubt ist, entscheidet ihr Schutz, wie heute. Eine Unterscheidung nach Person gibt es vorerst nicht.

## Datenmodell (Skizze)

- `owner` (house oder Personen-Schlüssel) an: Fakten, Profil (eins pro Besitzer), Aufgaben, Erinnerungen, Sitzungen.
- Meldungen: `owner`/`audience` wie bisher, dazu `sender`.
- Personen: dazu `alone_is_me` (bool) und `accept_messages` (all, persons, none).
- Testdaten werden gelöscht, eine Migration ist nicht nötig.
