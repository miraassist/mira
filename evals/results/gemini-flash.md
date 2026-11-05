# Eval gemini-flash – 2026-11-05 22:20

Mira 0.16.0 · 3 Durchläufe je Szenario · **11 von 12 Szenarien bestanden**

Modelle:
- main: gemini-flash-latest

Ohne diese Skills (Variable fehlt in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Szenario | Kern | Bestanden | Ergebnis + Verhalten | Antwort | Zeit je Schritt (Median / Max, Modell) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | ja | ✓ | 3/3 | 3/3 | 4.0 / 12.3 s, 4.0 s (brauchbar) | 82741 |
| termin-eintragen | ja | ✓ | 3/3 | 3/3 | 9.2 / 11.6 s, 9.2 s (langsam) | 171277 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 3/3 | 9.3 / 10.7 s, 9.3 s (langsam)<br>5.6 / 6.2 s, 5.6 s (brauchbar) | 263933 |
| termin-absagen | ja | ✗ | 0/3 | 3/3 | 7.6 / 8.0 s, 7.5 s (brauchbar)<br>2.3 / 10.3 s, 2.3 s (gut) | 168509 |
| todo-faellig |  | ✓ | 2/3 | 3/3 | 13.9 / 17.1 s, 13.9 s (langsam)<br>5.0 / 5.9 s, 4.9 s (brauchbar) | 230071 |
| abfall | ja | ✓ | 3/3 | 3/3 | 7.3 / 7.6 s, 7.2 s (brauchbar) | 126960 |
| wecker | ja | ✓ | 3/3 | 3/3 | 5.6 / 7.6 s, 5.5 s (brauchbar)<br>8.8 / 10.9 s, 8.7 s (langsam) | 298736 |
| merken | ja | ✓ | 3/3 | 3/3 | 4.4 / 5.5 s, 4.4 s (brauchbar)<br>4.4 / 10.0 s, 4.3 s (brauchbar) | 163717 |
| aufgabe |  | ✓ | 2/3 | 3/3 | 11.8 / 32.9 s, 8.6 s (langsam)<br>3.9 / 7.9 s, 3.9 s (brauchbar) | 216935 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.5 / 3.8 s, 3.5 s (brauchbar) | 81059 |
| licht | ja | ✓ | 3/3 | 2/3 | 3.6 / 3.8 s, 3.5 s (brauchbar)<br>3.7 / 4.3 s, 3.6 s (brauchbar) | 166905 |
| garagentor | ja | ✓ | 3/3 | 3/3 | 7.3 / 12.5 s, 7.3 s (brauchbar)<br>3.7 / 3.8 s, 3.7 s (brauchbar) | 168136 |

## Auffälligkeiten

- termin-absagen, Lauf 1: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 1: step 2: missing action:calendar.delete
- termin-absagen, Lauf 1: step 2: event "Zahnarzt" still in calendar.familie on +3
- termin-absagen, Lauf 2: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 2: step 2: missing action:calendar.delete
- termin-absagen, Lauf 2: step 2: event "Zahnarzt" still in calendar.familie on +3
- termin-absagen, Lauf 3: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 3: step 2: missing action:calendar.delete
- termin-absagen, Lauf 3: step 2: event "Zahnarzt" still in calendar.familie on +3
- todo-faellig, Lauf 1: step 1 took 17.1s, over 15s
- aufgabe, Lauf 2: step 1 took 32.9s, over 15s
- licht, Lauf 1 (Antwort): step 2: answer lacks "an|eingeschaltet|leuchtet"
