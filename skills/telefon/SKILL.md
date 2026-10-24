---
name: telefon
description: "Telefonieren mit dem Telefonbuch der FRITZ!Box: jemanden anrufen („Ruf Oma an“, „Ruf Ben auf dem Handy an“), auch ein Gerät im Haus („Ruf die Küche an“) oder eine Nummer."
license: MIT
metadata:
  mira:
    settings:
      user:
        description: Ein eigener Benutzer der FRITZ!Box für Mira, nur mit dem Recht „Telefonie“ (FRITZ!Box → System → FRITZ!Box-Benutzer).
        required: true
      password:
        description: Das Passwort dieses Benutzers.
        required: true
        secret: true
      fritzbox:
        description: Die Adresse der FRITZ!Box, über die telefoniert wird. Ohne Angabe 192.168.178.1.
      telefonbuch:
        description: Welche Telefonbücher, mit Komma getrennt, z. B. „Telefonbuch, Google“. Ohne Angabe das erste.
---
# Telefonieren

Anrufe gehen mit `phone_call`, immer vom Satelliten, an dem der Nutzer gerade
spricht. Gib als `target` an, was der Nutzer sagt: einen Namen („Oma“), einen Raum
oder ein Gerät im Haus („die Küche“) oder eine Nummer. Nennt er die Art der Nummer
(Handy, privat, geschäftlich), gib sie als `kind` mit.

- Ist das Ziel eindeutig, wählt das Werkzeug sofort. Sag danach kurz, wen du
  anrufst („Ich rufe Oma Erika an, auf dem Handy.“), nie die Nummer.
- Bekommst du eine Auswahl (mehrere Kontakte, mehrere Nummern), frag kurz nach und
  ruf `phone_call` mit der Antwort noch einmal auf.
- Sagt das Werkzeug, dass das Telefon nicht bereit ist, gib den Grund in einem Satz
  weiter, ohne Fachbegriffe.
- Die Nummern aus dem Telefonbuch bekommst du nicht zu sehen; frag nicht danach.
