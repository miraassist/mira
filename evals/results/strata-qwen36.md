# Eval strata-qwen36 – 2026-10-22 09:11

Mira 0.6.58 · 1 Durchläufe je Szenario · **1 von 12 Szenarien bestanden**

Modelle:
- main: qwen3.6-35b-a3b

| Szenario | Kern | Bestanden | Ergebnis + Verhalten | Antwort | Zeit je Schritt (Median / Max, Modell) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | ja | ✗ | 0/1 | 0/1 | 62.1 / 62.1 s, 62.1 s (langsam) | 21621 |
| termin-eintragen | ja | ✗ | 0/1 | 1/1 | 78.1 / 78.1 s, 78.1 s (langsam) | 44799 |
| termin-kalender-waehlen |  | ✗ | 0/1 | 1/1 | 79.0 / 79.0 s, 79.0 s (langsam)<br>70.3 / 70.3 s, 70.3 s (langsam) | 69375 |
| termin-absagen | ja | ✗ | 0/1 | 1/1 | 73.5 / 73.5 s, 73.5 s (langsam)<br>61.8 / 61.8 s, 61.8 s (langsam) | 44238 |
| todo-faellig |  | ✗ | 0/1 | 0/1 | 58.9 / 58.9 s, 58.8 s (langsam)<br>61.7 / 61.7 s, 61.7 s (langsam) | 43714 |
| abfall | ja | ✗ | 0/1 | 1/1 | 69.2 / 69.2 s, 69.2 s (langsam) | 33007 |
| wecker | ja | ✗ | 0/1 | 1/1 | 71.8 / 71.8 s, 71.8 s (langsam)<br>58.9 / 58.9 s, 58.8 s (langsam) | 66687 |
| merken | ja | ✗ | 0/1 | 1/1 | 55.9 / 55.9 s, 55.8 s (langsam)<br>55.5 / 55.5 s, 55.5 s (langsam) | 42681 |
| aufgabe |  | ✗ | 0/1 | 1/1 | 120.0 / 120.0 s, 0.0 s (langsam) | 0 |
| nachricht |  | ✓ | 1/1 | 1/1 | 27.6 / 27.6 s, 27.5 s (langsam) | 21232 |
| licht | ja | ✗ | 0/1 | 0/1 | 55.8 / 55.8 s, 55.8 s (langsam)<br>57.9 / 57.9 s, 57.8 s (langsam) | 43505 |
| garagentor | ja | ✗ | 0/1 | 0/1 | 57.4 / 57.4 s, 57.3 s (langsam)<br>53.6 / 53.6 s, 53.5 s (langsam) | 32498 |

## Auffälligkeiten

- kalender-lesen, Lauf 1: step 1 took 62.1s, over 15s
- kalender-lesen, Lauf 1 (Antwort): step 1: answer lacks "10"
- termin-eintragen, Lauf 1: step 1 took 78.1s, over 15s
- termin-kalender-waehlen, Lauf 1: step 1 took 79.0s, over 15s
- termin-kalender-waehlen, Lauf 1: step 2 took 70.3s, over 15s
- termin-absagen, Lauf 1: step 1 took 73.5s, over 15s
- termin-absagen, Lauf 1: step 1: missing thread:termin-absagen:proposed
- termin-absagen, Lauf 1: step 2 took 61.8s, over 15s
- termin-absagen, Lauf 1: step 2: missing action:calendar.delete
- termin-absagen, Lauf 1: step 2: event "Zahnarzt" still in calendar.familie on +3
- todo-faellig, Lauf 1: step 1 took 58.9s, over 15s
- todo-faellig, Lauf 1: step 1: missing thread:todo
- todo-faellig, Lauf 1: step 2 took 61.7s, over 15s
- todo-faellig, Lauf 1: step 2: missing action:todo.add_item
- todo-faellig, Lauf 1: step 2: no item "Batterien AAA" on todo.einkauf
- todo-faellig, Lauf 1 (Antwort): step 1: answer lacks "Einkauf"
- abfall, Lauf 1: step 1 took 69.2s, over 15s
- wecker, Lauf 1: step 1 took 71.8s, over 15s
- wecker, Lauf 1: step 1: missing action:esphome.fake_show_alarm_set
- wecker, Lauf 1: step 1: no alarm at 06:00 on fake_show
- wecker, Lauf 1: step 2 took 58.9s, over 15s
- wecker, Lauf 1: step 2: no alarm at 06:30 on fake_show
- merken, Lauf 1: step 1 took 55.9s, over 15s
- aufgabe, Lauf 1: step 1: Post "http://127.0.0.1:8790/v1/conversation": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
- licht, Lauf 1: step 1 took 55.8s, over 15s
- licht, Lauf 1: step 2 took 57.9s, over 15s
- licht, Lauf 1 (Antwort): step 2: answer lacks "an|eingeschaltet|leuchtet"
- garagentor, Lauf 1: step 1 took 57.4s, over 15s
- garagentor, Lauf 1: step 1: cover.garage_door is open, not closed
- garagentor, Lauf 1: step 2 took 53.6s, over 15s
- garagentor, Lauf 1 (Antwort): step 1: answer lacks "?"
