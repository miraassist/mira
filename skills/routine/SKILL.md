---
name: routine
description: "Eine eigene Routine lernen und ausführen: ein Satz, mehrere Schritte („Lern die Routine Gute Nacht: Licht aus, Rollläden runter“, „Gute Nacht“, „Filmabend“, „Was macht die Routine Gute Nacht?“)."
license: MIT
---
# Routine

Eine Routine ist ein Satz („Gute Nacht“, „Filmabend“) mit Schritten, die Mira der Reihe
nach ausführt. Mira merkt sie sich im Gedächtnis des Hauses.

**Lernen** („Lern die Routine Gute Nacht: Licht aus, Rollläden runter“):
- Fehlt der Name oder fehlen die Schritte, frag kurz danach.
- Sieh die Schritte im Haus nach (welche Lichter, welche Rollläden), statt zu raten.
  Sag „alle Lichter im Erdgeschoss“, wenn der Nutzer es so meint, nicht jede Lampe einzeln.
- Speichere sie mit memory_save, house: true, tags [routine, NAME], in dieser Form:
  „Routine Gute Nacht: 1. alle Lichter im Haus aus, 2. alle Rollläden runter, 3. sagen:
  Schlaf gut.“
- Sag danach kurz die Schritte, wie du sie verstanden hast.
- Gibt es die Routine schon, ersetze sie: erst memory_forget für die alte, dann neu
  speichern.

**Ausführen** („Gute Nacht“, „Starte den Filmabend“): Such mit memory_search
„Routine NAME“. Gibt es sie, führ die Schritte der Reihe nach mit deinen Werkzeugen aus
und sag am Ende kurz, was passiert ist, oder den letzten Schritt, wenn er etwas zu sagen
ist. Ist ein Schritt eine wichtige Aktion (Schloss, Alarmanlage, Garagentor), gilt wie
immer die Rückfrage. Gibt es in Home Assistant eine Szene oder ein Skript mit diesem
Namen und keine gelernte Routine, nimm die Szene oder das Skript.

**Abfragen und löschen** („Was macht die Routine Gute Nacht?“, „Vergiss den Filmabend“):
mit memory_search lesen, mit memory_forget löschen.
