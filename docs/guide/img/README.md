# Bilder und Aufnahmen der Anleitung

Screenshots: `<abschnitt>-<was>-<de|en>.png`. Sie liegen hier, gehören zur Version und sind ins Programm eingebettet; die Web-Oberfläche zeigt sie unter `/ui/guide/img/`.

Aufnahmen (`.mp4`) liegen **nicht** hier: Sie würden Programm und Git-Geschichte aufblähen. Sie liegen auf einem eigenen Server (https, mit Teilabrufen/Range) und werden in der Anleitung mit voller Adresse eingebunden: `![Unterschrift](https://…/name-de.mp4)`. Beschlossen am 2026-10-17:
- **Eigener Server, nicht YouTube eingebettet**: Ein YouTube-Player lädt schon beim Öffnen der Seite Skripte von Google; das passt nicht zu einem lokalen Assistenten. Auf YouTube dürfen die Videos zusätzlich stehen, verlinkt, nicht eingebettet.
- **Datenschutz**: Wer ein Video abspielt, ruft den Server auf; die Oberfläche schickt dabei keinen Referer mit.
