# Kanäle – Spezifikation

Stand 2026-10-16, gebaut in 0.6.2–0.6.3. Zwei Abstraktionen tragen Miras Kommunikation: **Kanal** (dieses Dokument) und **Nachricht** ([Nachrichten](nachrichten.md)). Baut auf [Mira in Home Assistant](ha-anbindung.md) auf: Was HA kann, baut Mira nicht nach.

## Grundsatz

Ein **Kanal** ist ein Weg, über den Mira jemanden erreicht oder hört. Jeder Kanal kann entweder
- **nur senden**: Er reicht, um eine Nachricht zuzustellen; oder
- **ein Gespräch führen**: Er sendet und empfängt, also auch Turns.

Der Agent kennt nur dieses Modell: „eingehender Text von Kanal X, Gespräch Y, Person Z“ und „Antwort/Nachricht an Kanal X, Ziel Y“. Welche Events, Aktionen oder APIs dahinterstehen, weiß er nicht.

## Die Kanäle

| Kanal | Richtung | Woher | Person |
|---|---|---|---|
| **notify** – jedes HA-notify-Ziel (Companion-App, Telegram-Chat, Signal, Slack, Threema, …) | nur senden | HAs `notify`, Mira baut nichts | die Person, der das Ziel zugeordnet ist |
| **Satellit** | Gespräch (Sprache); Zustellung als Ansage oder Anzeige-Lampe | HAs Assist-Pipeline und `assist_satellite` | anonym; erkannt nur nach der Regel „allein zu Hause“ (weiteres offen) |
| **HA-Chat** (Assist in der App, Mira im Seitenmenü) | Gespräch | HAs Gesprächsagent | über den angemeldeten HA-Benutzer |
| **Telegram** | Gespräch und senden | **Adapter auf HAs Integration `telegram_bot`** | über den Chat, der der Person zugeordnet ist |

Der HA-Chat taugt nicht zum Zustellen: In ihn kann Mira nichts von sich aus schreiben.

## Telegram über Home Assistant

- Telegram wird einmal in HA eingerichtet (Integration `telegram_bot`: Bot-Token, erlaubte Chats). Mira betreibt **keinen eigenen Bot mehr**; Miras eigene Telegram-Logik (`internal/channels/telegram`, `telegram:` in `mira.yaml`) entfällt ganz, auch ohne Ersatz für Installationen ohne HA.
- Ein **Adapter in der Mira-Integration** (ein abgeschlossenes Stück im HA-Prozess) verbindet beides:
  - eingehend: hört auf `telegram_text` (Chat-ID, Absender, Text), ordnet den Chat einer Person zu und gibt den Text als Turn an Mira (Kanal `telegram`, Gespräch = Chat);
  - ausgehend: schickt Miras Antwort mit HAs Telegram-Aktionen zurück (Formatierung, Aufteilen langer Antworten), zeigt solange „schreibt …“ (`send_chat_action`).
- **Nur Chats, die einer Person gehören**, sprechen mit Mira. Andere (auch in HA erlaubte) Chats ignoriert der Adapter; die Absicherung ist damit HAs Liste erlaubter Chats plus die Zuordnung zu einer Person.
- Die Abhängigkeit von HAs Event-Namen und -Struktur steckt nur in diesem Adapter und seinen Tests. `telegram_text` ist eine öffentliche, dokumentierte Schnittstelle; Änderungen kündigt HA als „breaking change“ an.
- Später: **Sprachnachrichten** (`telegram_attachment`, herunterladen, Spracherkennung, als Turn). Vorgemerkt, nicht jetzt.

## Kanäle einer Person

An der HA-Person (`options["mira"]`, siehe [ha-anbindung](ha-anbindung.md)):
- **Kanäle**: eine Liste von notify-Zielen, die dieser Person gehören, z. B. `notify.mobile_app_anna` und der Telegram-Chat `notify.telegram_anna`. Ersetzt die bisherigen Felder „Telegram-ID“ und „Push-Dienst“.
- Ist ein Telegram-Chat darunter, ist er zugleich Annas **Gesprächskanal** über Telegram: Darüber erkennt der Adapter, dass ein eingehender Chat Anna ist.
- Zugestellt wird privat über die Kanäle der Person (alle, oder der erste, der klappt – beim Bau festlegen), am Satelliten nur, wenn sie dort erkannt ist.

## Idee für Home Assistant

HA hat für Messenger nur die halbe Abstraktion: `notify` zum Senden; Empfangen ist je Integration ein eigenes Event (Telegram vollständig, Matrix nur Befehle, Signal/Discord/Slack/Threema/XMPP gar nicht). Ein Vorschlag an HA (PR oder Architektur-Diskussion) für einen gemeinsamen Empfang – etwa „eingehende Nachricht“ als einheitliches Event oder eine Gesprächs-Plattform für Messenger – würde Adapter wie unseren überflüssig machen. Nur eine Idee, vorgemerkt.
