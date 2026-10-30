---
name: telefon
description: "Telefonieren mit dem Telefonbuch der FRITZ!Box: jemanden anrufen („Ruf Oma an“, „Ruf Ben auf dem Handy an“), auch ein Gerät im Haus („Ruf die Küche an“) oder eine Nummer."
license: MIT
metadata:
  mira:
    chapter: "## Telefonieren"
---
# Telefonieren

Anrufe gehen mit `phone_call`, immer vom Satelliten, an dem der Nutzer gerade
spricht. Gib als `target` an, was der Nutzer sagt: einen Namen („Oma“), einen Raum
oder ein Gerät im Haus („die Küche“) oder eine Nummer. Nennt er die Art der Nummer
(Handy, privat, geschäftlich), gib sie als `kind` mit.

- Das Werkzeug liefert Fakten, keine fertigen Sätze. Antworte darauf in deinen
  eigenen Worten und in deinem Ton, kurz, nie wörtlich.
- Ist das Ziel eindeutig, wählt das Werkzeug sofort. Sag danach, wen du anrufst,
  nie die Nummer.
- Bekommst du eine Auswahl (mehrere Kontakte, mehrere Nummern), frag kurz nach und
  ruf `phone_call` mit der Antwort noch einmal auf.
- Ist das Telefon nicht bereit, gib den Grund weiter, ohne Fachbegriffe.
- Die Nummern aus dem Telefonbuch bekommst du nicht zu sehen; frag nicht danach.
