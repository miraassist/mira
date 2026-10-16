# Sprache – Spezifikation

Stand 2026-10-16, gebaut in 0.6.14. Gilt für alle Kanäle. Siehe auch [Kontext](kontext.md) und [Nachrichten](nachrichten.md).

## Grundsatz

Mira spricht die Sprache, die gesprochen wird. Im Haus hat nicht „das Haus“ eine Sprache, sondern jedes Gespräch. Eine eigene Einstellung für die Sprache gibt es nicht.

Die Sprache bestimmt Miras eigene Texte: ihre Regeln und den Kanalstil im Prompt, feste Sätze („Übrigens: …“, Hinweise), Ansagen und was sie von sich aus schreibt. Dass sie in der Sprache antwortet, in der man sie anspricht, regelt wie bisher eine Zeile in ihren Regeln.

## Welche Sprache wann

| Wann | Sprache |
|---|---|
| **Gespräch** (Satellit, Chat der HA-App) | die Sprache, die HA mit der Anfrage schickt (die der Assist-Pipeline) |
| **Ansage an einem Satelliten** | die Sprache, die an diesem Satelliten zuletzt gesprochen wurde |
| **Nachricht an eine Person** (Kanäle, Eskalation, Telegram) | die Sprache, die diese Person zuletzt gesprochen hat |
| **alles ohne Satellit und Person** (Haus-Profil verdichten, Urteile der Aufgaben) | die zuletzt gesprochene Sprache überhaupt |
| **noch nie gesprochen** (erster Start) | die Server-Sprache von HA (Einstellungen → System → Allgemein) |

- **Telegram** hat keine Pipeline. Ein Gespräch dort nimmt die zuletzt gesprochene Sprache der Person.
- **Die Person ist nur bekannt, wenn sie eindeutig ist**: per Telegram, im Chat der HA-App, am Satelliten nach der Regel „allein zu Hause“. Weitere Logik zum Erkennen kommt nicht dazu.
- Die Server-Sprache liest der Agent über HAs WebSocket (`get_config`), ohne Admin-Rechte.

## Zuletzt gesprochen: dauerhaft

Sitzungen laufen ab und werden gelöscht. Die zuletzt gesprochene Sprache wird deshalb **getrennt gespeichert** und überlebt das:
- je Satellit,
- je Person,
- insgesamt.

Jeder Turn mit einer Sprache von HA aktualisiert die drei Einträge, soweit Satellit und Person bekannt sind.

## Was sich ändert

- `prompt.language` gilt nur noch **ohne HA**. Mit HA wird es nicht gebraucht und nicht mehr gelesen.
- Der Prompt hat je Sprache seinen eigenen festen Anfang. Bei einem lokalen Modell bricht ein Wechsel zwischen Sprachen den Cache; bei einer Sprache im Haus ändert sich nichts.
- Gespeichertes bleibt in der Sprache, in der es entstand: Fakten, Nachrichtentexte. Eine Ansage formuliert das Modell ohnehin neu, in der Sprache der Ansage.

## Was nur das Modell liest

Nicht alles folgt der Sprache des Gesprächs. Was nur das Modell liest, wird beim Start einmal gebaut, in der dann zuletzt gesprochenen Sprache (sonst der Server-Sprache): die Beschreibungen von Miras Werkzeugen, die Texte, die sie dem Modell zurückgeben, der Wächter für Rückfragen, Miras eigener Strukturblock, die Abschlüsse von Vorgängen. Was der Mensch hört oder liest, folgt dagegen dem Gespräch, dem Raum oder der Person: der Prompt (Regeln, Kanalstil, Beispiele, Hinweise im Turn), feste Sätze, Ansagen, Erinnerungen, der Text auf dem Bildschirm.

**Offen für gemischtsprachige Haushalte:** Für das Modell ist eine deutsche Werkzeugbeschreibung in einem englischen Gespräch normalerweise kein Problem. Es kann aber sein, dass es in einem Haushalt, in dem zwei Sprachen gesprochen werden, doch stört, etwa wenn ein kleines lokales Modell dann Wörter aus Werkzeug-Ergebnissen in die Antwort mischt. Dann wären auch Werkzeuge, Wächter und Strukturblock je Sprache bereitzuhalten.

## Bewusst nicht

- **Die Sprache aus dem HA-Profil einer Person**: Sie liegt in HAs Benutzerdaten, die Miras HA-Benutzer für andere nicht lesen darf. Was jemand zuletzt gesprochen hat, ist ohnehin näher dran.
- **Die Pipeline je Satellit nachschlagen**: Die zuletzt dort gesprochene Sprache reicht.
- **Eine Einstellung dafür.**
