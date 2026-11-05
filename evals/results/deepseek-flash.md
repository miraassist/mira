# Eval deepseek-flash – 2026-11-05 22:08

Mira 0.15.2 · 3 Durchläufe je Szenario · **8 von 12 Szenarien bestanden**

Modelle:
- main: deepseek/deepseek-v4-flash

Ohne diese Skills (Variable fehlt in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Szenario | Kern | Bestanden | Ergebnis + Verhalten | Antwort | Zeit je Schritt (Median / Max, Modell) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | ja | ✗ | 2/3 | 1/3 | 3.1 / 16.6 s, 3.0 s (brauchbar) | 108142 |
| termin-eintragen | ja | ✓ | 3/3 | 3/3 | 8.4 / 11.4 s, 8.4 s (langsam) | 240293 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 3/3 | 7.4 / 7.9 s, 7.4 s (brauchbar)<br>3.1 / 4.0 s, 3.0 s (brauchbar) | 310612 |
| termin-absagen | ja | ✗ | 0/3 | 3/3 | 4.6 / 6.2 s, 4.6 s (brauchbar)<br>2.8 / 3.9 s, 2.8 s (gut) | 172629 |
| todo-faellig |  | ✗ | 0/3 | 3/3 | 12.3 / 16.9 s, 12.3 s (langsam)<br>4.6 / 4.7 s, 4.6 s (brauchbar) | 337021 |
| abfall | ja | ✓ | 3/3 | 3/3 | 4.2 / 4.6 s, 4.2 s (brauchbar) | 141839 |
| wecker | ja | ✓ | 3/3 | 1/3 | 4.3 / 5.6 s, 4.3 s (brauchbar)<br>5.6 / 5.6 s, 5.5 s (brauchbar) | 349780 |
| merken | ja | ✓ | 3/3 | 3/3 | 2.6 / 2.9 s, 2.5 s (gut)<br>2.7 / 3.0 s, 2.6 s (gut) | 182586 |
| aufgabe |  | ✗ | 0/3 | 3/3 | 10.0 / 21.3 s, 4.9 s (langsam)<br>3.1 / 3.3 s, 3.0 s (brauchbar) | 311068 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.3 / 3.5 s, 3.3 s (brauchbar) | 90535 |
| licht | ja | ✓ | 3/3 | 2/3 | 2.5 / 3.0 s, 2.5 s (gut)<br>2.6 / 2.9 s, 2.6 s (gut) | 185831 |
| garagentor | ja | ✓ | 3/3 | 3/3 | 3.1 / 3.6 s, 3.1 s (brauchbar)<br>2.6 / 4.5 s, 2.6 s (gut) | 202991 |

## Auffälligkeiten

- kalender-lesen, Lauf 1 (Antwort): step 1: answer lacks "10"
- kalender-lesen, Lauf 2 (Antwort): step 1: answer lacks "10"
- kalender-lesen, Lauf 3: step 1 took 16.6s, over 15s
- kalender-lesen, Lauf 3: step 1: missing tool:calendar_events
- termin-absagen, Lauf 1: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 1: step 2: missing action:calendar.delete
- termin-absagen, Lauf 1: step 2: event "Zahnarzt" still in calendar.familie on +3
- termin-absagen, Lauf 2: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 2: step 2: missing action:calendar.delete
- termin-absagen, Lauf 2: step 2: event "Zahnarzt" still in calendar.familie on +3
- termin-absagen, Lauf 3: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 3: step 2: missing action:calendar.delete
- termin-absagen, Lauf 3: step 2: event "Zahnarzt" still in calendar.familie on +3
- todo-faellig, Lauf 1: step 1: must not action:todo.add_item
- todo-faellig, Lauf 1: step 2: missing action:todo.add_item
- todo-faellig, Lauf 2: step 1 took 16.9s, over 15s
- todo-faellig, Lauf 2: step 1: must not action:todo.add_item
- todo-faellig, Lauf 2: step 2: "Batterien AAA" is due "", not 2026-11-07
- todo-faellig, Lauf 3: step 1: must not action:todo.add_item
- todo-faellig, Lauf 3: step 2: "Batterien AAA" is due "", not 2026-11-07
- wecker, Lauf 1 (Antwort): step 1: answer lacks "6"
- wecker, Lauf 2 (Antwort): step 1: answer lacks "6"
- aufgabe, Lauf 1: step 1: missing thread:aufgabe:proposed
- aufgabe, Lauf 2: step 1: missing thread:aufgabe:proposed
- aufgabe, Lauf 2: step 2: no task like "Nachttischlampe" (have Wohnzimmerlampe länger als eine Stunde an;)
- aufgabe, Lauf 3: step 1 took 21.3s, over 15s
- licht, Lauf 2 (Antwort): step 2: answer lacks "an|eingeschaltet|leuchtet"
