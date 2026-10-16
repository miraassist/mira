# Nachrichten – Spezifikation

Stand 2026-10-16, gebaut in 0.6.0–0.6.4 (bis auf „erkannt am Satelliten“, siehe Offen). Zwei Abstraktionen tragen Miras Kommunikation: [Kanal](kanaele.md) und **Nachricht** (dieses Dokument). Baut auf [Benutzer](benutzer.md) und [Mira in Home Assistant](ha-anbindung.md) auf.

## Grundsatz

Aus den **Meldungen** (notices) werden **Nachrichten** (messages). Eine Nachricht hat einen **Absender**, einen **Empfänger** und einen **Inhalt**. Aus Empfänger, Absender und den Einstellungen des Empfängers folgt, wie und wann sie zugestellt wird.

Es gibt **einen** Begriff für alles, was Mira jemandem ausrichtet: Was heute die Meldung einer Aufgabe ist („Der Gasverbrauch ist ungewöhnlich hoch“), ist dann eine Nachricht mit dem Absender „Gas-Aufgabe“. „Sag Anna, dass es später wird“ ist eine Nachricht mit dem Absender Ben. Kein zweites System daneben.

Nicht dazu gehört die **Durchsage** („Sag allen, das Essen ist fertig“). Sie ist ein Lautsprecher, keine Zustellung: jetzt oder gar nicht, an niemand Bestimmten, nicht gespeichert, ohne Einstellungen des Empfängers. HA bringt sie als Werkzeug mit (`HassBroadcast`), Mira hat es über HAs MCP schon. Höchstens später: Absender nennen und Ruhezeit beachten.

Vorerst nicht: „an alle einzeln“, also dieselbe Nachricht über den privaten Kanal jeder Person.

## Die Nachricht

| Teil | Werte |
|---|---|
| **Absender** | eine Person (erkannt über ihren Kanal), ein Bereich („jemand im Wohnzimmer“, anonym am Satelliten) oder Mira selbst: eine Aufgabe, ein Skill, eine Automation (`mira.send_message`), eine Erinnerung |
| **Empfänger** | eine Person oder **das Haus** |
| **Inhalt** | Text; dazu wie heute Details, Wichtigkeit (0–3), vertraulich ja/nein, Ablauf, Schlüssel gegen Doppelte |
| **Zustand** | wartet → zugestellt → gehört/erledigt, oder abgelaufen |

## Empfänger: das Haus

Wie die Meldungen heute: Mira stellt im Haus zu.
- Ansage, wenn jemand einen Raum betritt (Bewegungsmelder am Satelliten), sofern der Raum auf „ansagen“ steht;
- sonst leuchtet die **Anzeige-Lampe**, bis jemand fragt, ob es etwas Neues gibt;
- im Anschluss an eine Antwort („Licht ist an. Übrigens: …“);
- Morgenansage, Ruhezeit, Eskalation, Dringendes sofort – alles wie gehabt.

Einstellungen eines Empfängers gibt es hier nicht; das Haus nimmt alles an.

## Empfänger: eine Person

**Einstellungen der Person** (an der HA-Person, `options["mira"]`):

| Einstellung | Werte | Standard |
|---|---|---|
| Nachrichten annehmen | von allen (auch anonym am Satelliten) / von anderen Personen / von niemandem | von allen |
| Kanäle | notify-Ziele der Person (Companion-App, Telegram-Chat, Signal, …), siehe [Kanäle](kanaele.md) | keine |
| Allein zu Hause sind die Satelliten ich | an/aus | aus |

- „Annehmen“ gilt für Nachrichten **von Menschen**. Was Mira selbst schickt (Aufgaben, Skills, Erinnerungen, Automationen), kommt immer an; sonst könnte man sich von der Lecksensor-Warnung abmelden.
- „Von anderen Personen“ heißt: Der Absender ist über seinen Kanal als HA-Person erkannt. Anonym am Satelliten („jemand im Wohnzimmer“) zählt nicht.
- Nimmt der Empfänger nicht an, sagt Mira dem Absender das ehrlich („Ben nimmt gerade keine Nachrichten an“) und speichert nichts.

**Zustellung:**
- privat: über die [Kanäle](kanaele.md) der Person (HA-notify-Ziele), wie heute bei persönlichen und vertraulichen Meldungen;
- am Satelliten (Ansage oder Anzeige-Lampe), **wenn die Person dort erkannt ist**. Heute heißt erkannt nur: Regel „allein zu Hause“. **Was „erkannt am Satelliten“ künftig heißt, besprechen wir gesondert.**

## Wortlaut

Mira nennt den Absender, wenn es ein Mensch ist:
- „Anna lässt dir ausrichten: Es wird später.“
- „Jemand im Wohnzimmer lässt ausrichten: …“ (an das Haus)
- Nachrichten von Mira selbst ohne Vorspann, wie heute.

## Anlegen

- Im Gespräch: „Sag Anna, dass es später wird“, „Hinterlass fürs Haus: Der Schornsteinfeger kommt um 10.“ Das Modell ruft dafür ein Werkzeug (Empfänger, Text); der Absender kommt aus der Identität des Turns, nie vom Modell.
- Aus HA: Die Aktion `mira.add_notice` heißt dann `mira.send_message` (Empfänger statt `audience`).
- Mit Zeitpunkt („Erinnere Ben morgen um 7 an den Müll“, „Erinnere mich in 10 Minuten“) ist es eine Nachricht mit **Zustellzeit**: Sie wartet bis dahin und wird dann zugestellt wie jede andere. **Erinnerungen gehen ganz in Nachrichten auf**; die eigene Tabelle und der eigene Zustellweg für Erinnerungen entfallen. Eine Erinnerung an sich selbst ist eine Nachricht von mir an mich (sie wird immer angenommen); im Raum gefragt, wird sie dort gesprochen, wie heute.

## Umbenennung

Diesmal überall, weil der Begriff sich ändert:
- Code: `notices` → `messages` (Tabelle, Store, Notifier-Texte, Werkzeuge wie `notices_read` → `messages_read`); `reminders` gehen darin auf, `reminder_set` wird zum Senden mit Zustellzeit.
- HA: Aktion `mira.send_message`, Sensor „Wartende Nachrichten“ (`sensor.mira_pending_messages`).
- Web-Oberfläche und Anleitung: Reiter und Texte „Nachrichten“.
- Keine Migration der Daten (alles Test).

## Was Home Assistant schon hat

Geprüft gegen HA 2026.10, damit wir nichts nachbauen:

| HA | Was es ist | Für Mira |
|---|---|---|
| `notify.*` (Companion-App, Telegram-Bot, …) und `notify.send_message` | Transporte: eine Nachricht an ein Gerät oder einen Chat schicken | **Nutzen** als Kanal ([Kanäle](kanaele.md)) |
| `assist_satellite.announce` / `start_conversation` | am Satelliten sprechen | **Nutzen**, wie heute |
| `persistent_notification` | Hinweis in HAs Glocke, für alle gleich | kein Kanal pro Person; höchstens für Admin-Hinweise |
| `alert` | wiederholte Benachrichtigung, solange ein Zustand anhält | nichts Gleiches; unsere Eskalation bleibt |
| `todo` | geteilte Listen | etwas anderes |
| Assist-Timer (`HassStartTimer`) | Timer am Gerät | bleibt HAs Sache; Miras Erinnerungen sind Nachrichten |

**Was HA nicht hat:** ein Nachrichtenmodell mit Absender und Empfänger, Warten bis der Empfänger erreichbar ist, Zustellung nach Anwesenheit im Raum, Einstellungen pro Person, von wem sie etwas annimmt. Das bauen wir; die **Transporte** nimmt Mira von HA.

**HA-nah anbieten:** Die Integration stellt `notify`-Entitäten bereit, eine fürs Haus (`notify.mira_haus`) und eine pro Person (`notify.mira_anna`). Dann schicken Automationen mit HAs eigener Aktion `notify.send_message` eine Nachricht über Mira, ohne Mira-eigene Aktion; Absender ist dann die Automation. `mira.send_message` bleibt für mehr Felder (Wichtigkeit, vertraulich, Ablauf).

Telegram läuft über HAs Integration `telegram_bot`; Miras eigener Bot entfällt ([Kanäle](kanaele.md)).

## Entschieden

- Erinnerungen gehen in Nachrichten auf (Nachricht mit Zustellzeit).
- **Keine Lesebestätigung** für den Absender („Anna hat es gehört“): aus Rücksicht auf die Privatsphäre des Empfängers.

## Offen

1. **Erkannt am Satelliten**: wann eine Person an einem Satelliten als anwesend gilt (gesondert besprechen). Bis dahin: nur die Regel „allein zu Hause“.
