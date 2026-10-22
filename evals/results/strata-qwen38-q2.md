# Eval strata-qwen38-q2 – 2026-10-22 16:02

Mira 0.6.59 · 1 Durchläufe je Szenario · **4 von 12 Szenarien bestanden**

Modelle:
- main: qwen3.8-flash-next-q2_0

| Szenario | Kern | Bestanden | Ergebnis + Verhalten | Antwort | Zeit je Schritt (Median / Max, Modell) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | ja | ✓ | 1/1 | 1/1 | 12.0 / 12.0 s, 12.0 s (langsam) | 21764 |
| termin-eintragen | ja | ✗ | 0/1 | 1/1 | 21.2 / 21.2 s, 21.1 s (langsam) | 45165 |
| termin-kalender-waehlen |  | ✗ | 0/1 | 0/1 | 25.8 / 25.8 s, 25.8 s (langsam)<br>6.5 / 6.5 s, 6.5 s (brauchbar) | 81216 |
| termin-absagen | ja | ✗ | 0/1 | 1/1 | 18.6 / 18.6 s, 18.6 s (langsam)<br>6.8 / 6.8 s, 6.8 s (brauchbar) | 67694 |
| todo-faellig |  | ✗ | 0/1 | 1/1 | 24.2 / 24.2 s, 24.1 s (langsam)<br>8.1 / 8.1 s, 8.1 s (langsam) | 80395 |
| abfall | ja | ✗ | 0/1 | 1/1 | 18.8 / 18.8 s, 18.8 s (langsam) | 33727 |
| wecker | ja | ✗ | 0/1 | 0/1 | 53.0 / 53.0 s, 52.9 s (langsam)<br>81.9 / 81.9 s, 81.8 s (langsam) | 197824 |
| merken | ja | ✓ | 1/1 | 1/1 | 10.2 / 10.2 s, 10.2 s (langsam)<br>9.4 / 9.4 s, 9.4 s (langsam) | 42927 |
| aufgabe |  | ✗ | 0/1 | 0/1 | 89.8 / 89.8 s, 84.7 s (langsam)<br>72.6 / 72.6 s, 72.5 s (langsam) | 69861 |
| nachricht |  | ✓ | 1/1 | 1/1 | 8.3 / 8.3 s, 8.3 s (langsam) | 21271 |
| licht | ja | ✓ | 1/1 | 1/1 | 12.9 / 12.9 s, 12.8 s (langsam)<br>6.2 / 6.2 s, 6.2 s (brauchbar) | 55101 |
| garagentor | ja | ✗ | 0/1 | 1/1 | 7.1 / 7.1 s, 7.1 s (brauchbar)<br>6.5 / 6.5 s, 6.5 s (brauchbar) | 32827 |

## Auffälligkeiten

- termin-eintragen, Lauf 1: step 1 took 21.2s, over 15s
- termin-kalender-waehlen, Lauf 1: step 1 took 25.8s, over 15s
- termin-kalender-waehlen, Lauf 1: step 1: must not action:calendar.create_event
- termin-kalender-waehlen, Lauf 1: step 2: missing action:calendar.create_event
- termin-kalender-waehlen, Lauf 1 (Antwort): step 1: answer lacks "Familie"
- termin-absagen, Lauf 1: step 1 took 18.6s, over 15s
- termin-absagen, Lauf 1: step 2: event "Zahnarzt" still in calendar.familie on +3
- todo-faellig, Lauf 1: step 1 took 24.2s, over 15s
- abfall, Lauf 1: step 1 took 18.8s, over 15s
- wecker, Lauf 1: step 1 took 53.0s, over 15s
- wecker, Lauf 1: step 1: missing action:esphome.fake_show_alarm_set
- wecker, Lauf 1: step 1: no alarm at 06:00 on fake_show
- wecker, Lauf 1: step 2 took 81.9s, over 15s
- wecker, Lauf 1: step 2: missing tool:thread_undo
- wecker, Lauf 1: step 2: no alarm at 06:30 on fake_show
- wecker, Lauf 1 (Antwort): step 1: answer lacks "6"
- aufgabe, Lauf 1: step 1 took 89.8s, over 15s
- aufgabe, Lauf 1: step 1: missing thread:aufgabe:proposed
- aufgabe, Lauf 1: step 2 took 72.6s, over 15s
- aufgabe, Lauf 1 (Antwort): step 1: answer lacks "Nachttischlampe"
- garagentor, Lauf 1: step 2: cover.garage_door is closed, not opening|open
