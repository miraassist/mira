# Radio – Spezifikation

Stand 2026-10-18, beschlossen und gebaut in 0.6.37.

## Ziel

„Spiel SWR3“ muss gehen, wie bei Alexa. Die Assist-Werkzeuge von Home Assistant bieten dafür nichts an: Suchen und Abspielen gibt es dort nur für Player, die „Medien suchen“ können, und das kann der ESPHome-Lautsprecher der Satelliten (etwa TECHO5) nicht. Mira sucht deshalb selbst in der Quelle „Radio Browser“ von Home Assistant, die immer dabei ist, und spielt den Sender mit `media_player.play_media`.

## Das Werkzeug

**`radio_play(station, room?, player?)`**

- **Raum:** ohne Angabe der Raum des Satelliten, an dem gesprochen wird; sonst der genannte Raum. Kennt Mira keinen Raum (etwa bei Telegram ohne Angabe), sagt sie das und fragt.
- **Player:** der für Assist freigegebene `media_player` im Bereich. Gibt es keinen, sagt Mira das ehrlich. Gibt es mehrere, fragt sie, welcher (`player`); das Werkzeug nennt sie dafür mit ihrem Namen in Home Assistant und der Entität („Show Bad Speaker (media_player.show_bad_speaker)“), damit Mira nicht nach der Kennung rät.
- **Bestätigung** nennt den Lautsprecher, nicht nur den Raum („Läuft: SWR3 auf Show Bad Speaker im Raum Bad“): Mira sagt, wo es wirklich läuft, und eine Verwechslung fällt sofort auf.
- **Suche:** WebSocket `media_source/search_media` auf `media-source://radio_browser`, die beliebtesten zuerst. Nur abspielbare Treffer der Radio-Browser-Quelle zählen.
- **Auswahl:** Gleicht ein Treffer dem Namen (Groß- und Kleinschreibung, Leerzeichen und Satzzeichen egal, „SWR 3“ = „SWR3“), läuft er. Ist es genau ein Treffer, läuft er. Sonst nennt Mira bis zu fünf Namen und fragt nach.
- **Abspielen:** `media_player.play_media` mit der Adresse und dem Typ des Treffers. Home Assistant holt den Stream und wandelt ihn um.
- **Ohne Radio Browser** (Integration fehlt oder keine Verbindung) sagt Mira, dass die Suche gerade nicht geht.

Es fragt nichts nach: Radio ist nicht kritisch.

## Was nicht dazugehört

- **Beenden, leiser, lauter, Pause:** die normalen Medienwerkzeuge von Home Assistant. Gibt es ein Gerät, das auf Pause nicht sauber aufhört, kommt ein Stop-Werkzeug dazu.
- **Eigene Lieblingssender** mit Stream-Adresse und ein Mira-Label für „spielt Musik“ pro Player gibt es nicht; beides lässt sich später ergänzen.
- **Musik** (Interpreten, Alben, Playlists) ist Sache von Music Assistant und noch offen.
