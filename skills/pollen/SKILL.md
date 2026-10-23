---
name: pollen
description: "Pollenflug zu Hause, heute und morgen („Fliegen heute Pollen?“, „Wie sieht es morgen mit Birke aus?“)."
license: MIT
---
# Pollen

Den Pollenflug am Haus liest du mit `web_page` von Open-Meteo, als JSON:
`https://air-quality-api.open-meteo.com/v1/air-quality?latitude={{ home.latitude }}&longitude={{ home.longitude }}&hourly=alder_pollen,birch_pollen,grass_pollen,mugwort_pollen,ragweed_pollen&forecast_days=2&timezone=Europe/Berlin`

Steht dort statt einer Zahl ein „-“, weiß Mira nicht, wo das Haus liegt: sag das, statt
zu raten (der Ort steht in Home Assistant unter Einstellungen, System, Allgemein).

Du bekommst je Stunde von heute und morgen die Pollen pro Kubikmeter Luft: Erle
(alder), Birke (birch), Gräser (grass), Beifuß (mugwort) und Ambrosia (ragweed). Für
einen Tag nimmst du den **Mittelwert** seiner 24 Stunden und ordnest ihn so ein
(Belastungsklassen von MeteoSchweiz, Pollen pro Kubikmeter im Tagesmittel):

| Pollen | schwach | mäßig | stark | sehr stark |
|---|---|---|---|---|
| Birke | 1–10 | 11–69 | 70–299 | ab 300 |
| Erle | 1–10 | 11–69 | 70–249 | ab 250 |
| Gräser | 1–19 | 20–49 | 50–149 | ab 150 |
| Beifuß | 1–5 | 6–14 | 15–49 | ab 50 |
| Ambrosia | 1–5 | 6–10 | 11–39 | ab 40 |

Unter 1 fliegt die Art nicht.

So antwortest du:
- Nenne nur die Pollenarten, die fliegen, mit ihrer Stufe. Fliegt nichts, sag das in
  einem Satz.
- Weiß Mira, gegen welche Pollen jemand allergisch ist (Gedächtnis), nenne die
  zuerst, oder nur die, wenn es so gefragt ist.
- Das sind Vorhersagen eines Modells, keine Messung. Gesundheitliche Ratschläge gibst
  du nicht über das Übliche hinaus (Fenster zu, Wäsche drinnen trocknen).
