# Benutzer: Haus und Personen – Spezifikation

Stand 2026-10-16. Gilt auch für die [Aufgaben](aufgaben.md).

Gebaut (Agent 0.5.0, Integration 0.4.0): Identität, Regel „allein zu Hause“,
Gedächtnis und Profil pro Besitzer mit Urheber. Gebaut (0.5.7): Personen
kommen aus Home Assistant. Offen: Aufgaben, Meldungen, Erinnerungen und
Verläufe pro Besitzer, Nachrichten an andere.

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

## Personen kommen aus Home Assistant

Mit Home Assistant ist **jede HA-Person automatisch eine Person bei Mira**. Mira legt keine eigenen Personen an und hält nichts synchron.

- **Anker ist die HA-Person, nicht der HA-Benutzer.** Die Person ist das Haushaltsmitglied mit Anwesenheit (home/away), Name und Bild; der Benutzer ist nur ein Login. Es gibt Personen ohne Login (Kinder, Oma am Satelliten) und Logins ohne Person (Miras eigener HA-Benutzer, Service-Konten). HA verknüpft beides selbst über das Attribut `user_id` der Person, und legt beim Anlegen eines Benutzers standardmäßig eine Person an.
- **Schlüssel** ist die interne ID der HA-Person (`attributes.id` der `person.*`-Entität, z. B. `stefan_rinke`). Sie bleibt, wenn die Entität umbenannt wird. Gedächtnis, Meldungen, Erinnerungen und Sitzungen hängen an diesem Schlüssel.
- **Mira liest die Personen aus den Zuständen** (`person.*`, mit `id`, `user_id`, Name, Anwesenheit). Dafür braucht ihr HA-Benutzer keine Admin-Rechte. Neue Personen sieht sie beim nächsten Abgleich (jede Minute) oder sofort, wenn ein unbekannter HA-Benutzer auftaucht.
- **Datensätze entstehen bei Bedarf:** Gedächtnis, Profil und Co. einer Person gibt es, sobald es etwas zu speichern gibt. Es gibt nichts anzulegen.
- **Was nur Mira braucht, sind optionale Einstellungen an der Person:** Telegram-ID, Push-Dienst (Companion-App), „Allein zu Hause sind die Satelliten ich“. In der Integration heißt das „Mira-Einstellungen für eine Person“: man wählt eine HA-Person und setzt, was man braucht. Wer nichts davon braucht, stellt nichts ein.
- **Ein HA-Benutzer ohne Person** (selten) ist für Mira niemand Bestimmtes: Im Panel sieht er das Haus-Gedächtnis und einen Hinweis, im App-Chat gilt er als anonym.
- **Ohne Home Assistant** (nur Telegram) bleiben die Personen in `mira.yaml` unter `persons:` mit eigenem Schlüssel, wie bisher. Mit Home Assistant werden dort nur noch die Einstellungen pro HA-Person gelesen (Schlüssel = Personen-ID; ein älterer Eintrag mit `entity:` wird über die Entität zugeordnet).

## Identität: wer spricht

Die Identität kommt vom Kanal:

| Kanal | Identität |
|---|---|
| Telegram | Person über die Telegram-ID (wie heute) |
| HA-App, Assist-Chat, Mira im HA-Seitenmenü | Person über den angemeldeten HA-Benutzer. Die Integration reicht die Benutzer-ID weiter, Mira ordnet sie über das Attribut `user_id` der HA-Person zu. |
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
- Eine bekannte Person merkt sich immer für sich; ins Haus nur, wenn sie es ausdrücklich sagt.
- Jeder Fakt hat einen **Urheber**: die Person, sonst der Bereich des Satelliten („jemand im Wohnzimmer“). Die Web-Oberfläche zeigt Urheber und Zeitpunkt.
- Das Hausprofil steht in Schicht 5 (gemeinsamer Prompt-Anfang), das persönliche Profil in Schicht 7 des Turns, damit der Cache für alle gleich bleibt.

## Nachrichten an andere

Eigene Spezifikation: [Nachrichten](nachrichten.md). Kurz: Aus den Meldungen werden Nachrichten mit Absender und Empfänger (eine Person oder das Haus); jede Person stellt ein, von wem sie Nachrichten annimmt. Niemand schreibt in das Gedächtnis eines anderen: „Merk dir für Ben …“ wird zur Nachricht an Ben.

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

- `owner` (house oder Personen-ID aus HA) an: Fakten, Profil (eins pro Besitzer), Aufgaben, Erinnerungen, Sitzungen.
- Meldungen: `owner`/`audience` wie bisher, dazu `sender`.
- Personen: die HA-Person (ID, Entität, Name, Benutzer) plus Miras Einstellungen `telegram_user_id`, `notify`, `alone_is_me` und später `accept_messages` (all, persons, none).
- Bisher ist alles Test: Daten werden nicht migriert. Alte Schlüssel (`stefan` statt `stefan_rinke`) bleiben einfach liegen.
