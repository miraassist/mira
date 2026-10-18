# Bilder und Aufnahmen der Anleitung

Screenshots: `<abschnitt>-<was>-<de|en>.png`. Sie liegen hier, gehören zur Version und sind ins Programm eingebettet; die Web-Oberfläche zeigt sie unter `/ui/guide/img/`.

Aufnahmen (`.mp4`) liegen **nicht** hier: Sie würden Programm und Git-Geschichte aufblähen. Sie liegen auf dem Server der Website und werden in der Anleitung mit voller Adresse eingebunden: `![Unterschrift](https://mira-assist.com/videos/<name>-de.mp4)`. Ein langes Video lässt sich in mehreren Abschnitten einbinden: Hängt man `#t=Start,Ende` an die Adresse (Sekunden oder `m:ss`, das Ende ist optional), springt der Player zum Start und hält am Ende an, zum Beispiel `![Unterschrift](https://mira-assist.com/videos/<name>-de.mp4#t=135,160)`. Vorher verkleinern, z. B.:

```
ffmpeg -i aufnahme.mp4 -c:v libx264 -crf 28 -preset slow -pix_fmt yuv420p -c:a aac -b:a 96k -movflags +faststart <name>-de.mp4
```

(16 Sekunden: aus 20 MB werden 1,3 MB.) Beschlossen am 2026-10-17:
- **Eigener Server, nicht YouTube eingebettet**: Ein YouTube-Player lädt schon beim Öffnen der Seite Skripte von Google; das passt nicht zu einem lokalen Assistenten. Auf YouTube dürfen die Videos zusätzlich stehen, verlinkt, nicht eingebettet.
- **Datenschutz**: Wer ein Video abspielt, ruft den Server auf; die Oberfläche schickt dabei keinen Referer mit.
