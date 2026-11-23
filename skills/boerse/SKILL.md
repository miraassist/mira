---
name: boerse
description: "Börsenkurse nachsehen: der Kurs einer Aktie, eines Fonds oder eines Index mit der Veränderung seit gestern („Wie steht die SAP-Aktie?“, „Wo steht der DAX?“, „Was macht Siemens heute?“). Kurse von Yahoo Finance, bis zu 15 Minuten verzögert. Ohne Anmeldung."
license: MIT
metadata:
  mira:
    chapter: "## Börsenkurse"
    api:
      - url: https://query1.finance.yahoo.com/
        methods: [GET]
---
# Börsenkurse

Mit `boerse__kurs` siehst du den Kurs eines Wertpapiers nach: einer Aktie, eines
Fonds (ETF) oder eines Index. Gib ihm den Namen, das Kürzel oder die ISIN so,
wie der Nutzer sie sagt („SAP“, „Siemens Energy“, „DAX“, „DE0007164600“). Für
eine Frage nach mehreren Werten ruf es je Wert einmal auf.

**Erst prüfen, ob der Wert klar ist.** Stehen unter `alternativen` andere
Wertpapiere, deren Name genauso gut zu dem passt, was der Nutzer gesagt hat
(„die Deutsche“ passt zu Telekom, Deutscher Bank, Lufthansa und Börse), dann
nenn keinen Kurs, sondern frag kurz, welches er meint, mit zwei, drei Namen zur
Auswahl. Nur wenn der Name eindeutig ist („SAP“, „Siemens Energy“), nimm den
gefundenen und erwähne die anderen nicht.

**Dann die Antwort**, in einem, höchstens zwei gesprochenen Sätzen, immer mit
diesen vier Teilen:

1. der Name,
2. der Kurs mit zwei Nachkommastellen und der Währung (bei einem Index Punkte),
3. die Veränderung seit dem Vortag in Prozent mit einer Nachkommastelle („plus
   1,2 Prozent“),
4. wann der Kurs gilt, aus `zeit`: von heute nur die Uhrzeit („Stand 15:20
   Uhr“), sonst mit dem Tag („Stand gestern, 17:39 Uhr“, „Stand Freitag, 17:35
   Uhr“).

Die Kurse können bis zu 15 Minuten alt sein: Sag das nur, wenn der Nutzer nach
dem genauen Kurs jetzt fragt. Tageshoch und -tief nur, wenn danach gefragt ist.

Du gibst Kurse, keine Ratschläge: Ob man kaufen oder verkaufen soll, sagst du
nicht, und du sagst nichts voraus.

Meldet das Werkzeug einen Fehler, sag ihn in einem Satz: Findet es kein
Wertpapier, frag nach dem genauen Namen oder der ISIN. Ist Yahoo nicht
erreichbar, sag, dass die Kurse gerade nicht abrufbar sind. Versuch es nicht
gleich noch einmal.
