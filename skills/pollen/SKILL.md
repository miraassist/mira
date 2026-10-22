---
name: pollen
description: "Pollenflug am Wohnort, heute und morgen („Fliegen heute Pollen?“, „Wie sieht es morgen mit Birke aus?“)."
license: MIT
metadata:
  mira:
    settings:
      breitengrad:
        description: Breitengrad deines Wohnorts, z. B. 49.01. Du findest ihn in Home Assistant unter Einstellungen, System, Allgemein.
        required: true
      laengengrad:
        description: Längengrad deines Wohnorts, z. B. 8.40.
        required: true
---
# Pollen

Den Pollenflug liest du mit `web_page` von Open-Meteo, als JSON:
`https://air-quality-api.open-meteo.com/v1/air-quality?latitude={{breitengrad}}&longitude={{laengengrad}}&hourly=alder_pollen,birch_pollen,grass_pollen,mugwort_pollen,ragweed_pollen&forecast_days=2&timezone=Europe/Berlin`

Du bekommst je Stunde von heute und morgen die Pollen pro Kubikmeter Luft: Erle
(alder), Birke (birch), Gräser (grass), Beifuß (mugwort) und Ambrosia (ragweed).
Nimm für einen Tag den höchsten Wert der Stunden von 6 bis 20 Uhr.

Ungefähr gilt (Pollen pro Kubikmeter):
- unter 10: kaum
- 10 bis 50: schwach
- 50 bis 200: mittel
- über 200: stark

So antwortest du:
- Nenne nur die Pollenarten, die mindestens schwach fliegen, mit der Stärke. Fliegt
  nichts, sag das in einem Satz.
- Weiß Mira, gegen welche Pollen jemand allergisch ist (Gedächtnis), nenne die
  zuerst, oder nur die, wenn es so gefragt ist.
- Das sind Vorhersagen eines Modells, keine Messung. Gesundheitliche Ratschläge gibst
  du nicht über das Übliche hinaus (Fenster zu, Wäsche drinnen trocknen).
