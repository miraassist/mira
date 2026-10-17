# Eval gemini-flash – 2026-10-17 11:28

Mira 0.6.19 · 3 Durchläufe je Szenario · **10 von 12 Szenarien bestanden**

Modelle:
- main: gemini-flash-latest

| Szenario | Kern | Bestanden | Ergebnis + Verhalten | Antwort | Zeit je Schritt (Median / Max, Modell) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | ja | ✓ | 3/3 | 2/3 | 3.5 / 3.7 s, 3.5 s (brauchbar) | 52191 |
| termin-eintragen | ja | ✗ | 0/3 | 0/3 | 4.6 / 5.1 s, 4.5 s (brauchbar) | 82374 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 3/3 | 4.7 / 5.3 s, 4.7 s (brauchbar)<br>3.4 / 4.6 s, 3.3 s (brauchbar) | 143151 |
| termin-absagen | ja | ✓ | 3/3 | 3/3 | 6.0 / 6.1 s, 5.9 s (brauchbar)<br>3.3 / 3.5 s, 3.2 s (brauchbar) | 163219 |
| todo-faellig |  | ✓ | 2/3 | 3/3 | 4.9 / 23.0 s, 4.9 s (brauchbar)<br>2.9 / 3.3 s, 2.9 s (gut) | 136742 |
| abfall | ja | ✓ | 3/3 | 3/3 | 4.2 / 5.4 s, 4.2 s (brauchbar) | 80706 |
| wecker | ja | ✓ | 3/3 | 3/3 | 5.7 / 5.9 s, 5.7 s (brauchbar)<br>7.0 / 7.1 s, 6.9 s (brauchbar) | 191267 |
| merken | ja | ✓ | 3/3 | 3/3 | 2.9 / 3.0 s, 2.9 s (gut)<br>3.8 / 3.8 s, 3.7 s (brauchbar) | 103301 |
| aufgabe |  | ✓ | 2/3 | 3/3 | 7.7 / 26.7 s, 5.1 s (brauchbar)<br>3.1 / 3.7 s, 3.0 s (brauchbar) | 140139 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.5 / 4.6 s, 3.5 s (brauchbar) | 51110 |
| licht | ja | ✓ | 3/3 | 3/3 | 2.8 / 4.2 s, 2.8 s (gut)<br>3.4 / 3.8 s, 3.4 s (brauchbar) | 105444 |
| garagentor | ja | ✗ | 0/3 | 3/3 | 1.7 / 2.1 s, 1.6 s (gut)<br>3.8 / 5.2 s, 3.8 s (brauchbar) | 78621 |

## Auffälligkeiten

- kalender-lesen, Lauf 2 (Antwort): step 1: answer lacks "10"
- termin-eintragen, Lauf 1: step 1: missing action:calendar.create_event
- termin-eintragen, Lauf 1: step 1: no event "Friseur" at +2 15:00 in calendar.familie
- termin-eintragen, Lauf 1 (Antwort): step 1: answer lacks "15"
- termin-eintragen, Lauf 2: step 1: missing action:calendar.create_event
- termin-eintragen, Lauf 2: step 1: no event "Friseur" at +2 15:00 in calendar.familie
- termin-eintragen, Lauf 2 (Antwort): step 1: answer lacks "15"
- termin-eintragen, Lauf 3: step 1: missing action:calendar.create_event
- termin-eintragen, Lauf 3: step 1: no event "Friseur" at +2 15:00 in calendar.familie
- termin-eintragen, Lauf 3 (Antwort): step 1: answer lacks "15"
- todo-faellig, Lauf 3: step 1 took 23.0s, over 15s
- aufgabe, Lauf 2: step 1 took 26.7s, over 15s
- garagentor, Lauf 1: step 2: cover.garage_door is closed, not opening|open
- garagentor, Lauf 2: step 2: cover.garage_door is closed, not opening|open
- garagentor, Lauf 3: step 2: cover.garage_door is closed, not opening|open
