# Eval llama-qwen3-30b – 2026-11-08 22:52

Mira 0.19.0 · 3 runs per scenario · **3 of 12 scenarios passed**

Models:
- main: qwen/qwen3-30b-a3b-2507

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | yes | ✗ | 0/3 | 0/3 | 2.5 / 4.8 s, 2.5 s (good) | 96458 |
| termin-eintragen | yes | ✗ | 0/3 | 0/3 | 5.5 / 5.8 s, 5.5 s (usable) | 98207 |
| termin-kalender-waehlen |  | ✗ | 0/3 | 0/3 | 5.0 / 5.4 s, 5.0 s (usable)<br>5.5 / 5.6 s, 5.5 s (usable) | 201124 |
| termin-absagen | yes | ✗ | 0/3 | 3/3 | 4.3 / 4.4 s, 4.2 s (usable)<br>4.9 / 4.9 s, 4.9 s (usable) | 197192 |
| todo-faellig |  | ✗ | 0/3 | 2/3 | 3.6 / 3.8 s, 3.6 s (usable)<br>5.1 / 5.1 s, 5.0 s (usable) | 195366 |
| abfall | yes | ✗ | 1/3 | 3/3 | 6.0 / 8.9 s, 6.0 s (usable) | 147377 |
| wecker | yes | ✗ | 0/3 | 2/3 | 0.8 / 1.0 s, 0.8 s (good)<br>1.4 / 1.6 s, 1.4 s (good) | 96658 |
| merken | yes | ✓ | 3/3 | 3/3 | 4.9 / 4.9 s, 4.9 s (usable)<br>3.6 / 3.8 s, 3.6 s (usable) | 191461 |
| aufgabe |  | ✗ | 0/3 | 3/3 | 20.8 / 27.0 s, 15.8 s (slow)<br>6.1 / 6.5 s, 6.1 s (usable) | 286532 |
| nachricht |  | ✓ | 3/3 | 3/3 | 2.7 / 3.7 s, 2.6 s (good) | 94842 |
| licht | yes | ✗ | 0/3 | 3/3 | 0.7 / 3.0 s, 0.7 s (good)<br>1.3 / 1.3 s, 1.3 s (good) | 96654 |
| garagentor | yes | ✓ | 3/3 | 3/3 | 2.9 / 3.5 s, 2.9 s (good)<br>3.5 / 3.5 s, 3.4 s (usable) | 195252 |

## Findings

- kalender-lesen, run 1: step 1: missing tool:calendar_events
- kalender-lesen, run 1 (answer): step 1: answer lacks "Zahnarzt"
- kalender-lesen, run 1 (answer): step 1: answer lacks "10"
- kalender-lesen, run 2: step 1: missing tool:calendar_events
- kalender-lesen, run 2 (answer): step 1: answer lacks "Zahnarzt"
- kalender-lesen, run 2 (answer): step 1: answer lacks "10"
- kalender-lesen, run 3: step 1: missing tool:calendar_events
- kalender-lesen, run 3 (answer): step 1: answer lacks "Zahnarzt"
- kalender-lesen, run 3 (answer): step 1: answer lacks "10"
- termin-eintragen, run 1: step 1: missing action:calendar.create_event
- termin-eintragen, run 1: step 1: no event "Friseur" at +2 15:00 in calendar.familie
- termin-eintragen, run 1 (answer): step 1: answer lacks "15"
- termin-eintragen, run 2: step 1: missing action:calendar.create_event
- termin-eintragen, run 2: step 1: no event "Friseur" at +2 15:00 in calendar.familie
- termin-eintragen, run 2 (answer): step 1: answer lacks "15"
- termin-eintragen, run 3: step 1: missing action:calendar.create_event
- termin-eintragen, run 3: step 1: no event "Friseur" at +2 15:00 in calendar.familie
- termin-eintragen, run 3 (answer): step 1: answer lacks "15"
- termin-kalender-waehlen, run 1: step 2: no event "Training" at +2 17:00 in calendar.ben
- termin-kalender-waehlen, run 1 (answer): step 1: answer lacks "Familie"
- termin-kalender-waehlen, run 1 (answer): step 1: answer lacks "Ben"
- termin-kalender-waehlen, run 2: step 2: no event "Training" at +2 17:00 in calendar.ben
- termin-kalender-waehlen, run 2 (answer): step 1: answer lacks "Familie"
- termin-kalender-waehlen, run 2 (answer): step 1: answer lacks "Ben"
- termin-kalender-waehlen, run 3: step 2: no event "Training" at +2 17:00 in calendar.ben
- termin-kalender-waehlen, run 3 (answer): step 1: answer lacks "Familie"
- termin-kalender-waehlen, run 3 (answer): step 1: answer lacks "Ben"
- termin-absagen, run 1: step 1: missing tool:calendar_events
- termin-absagen, run 1: step 1: missing thread:termin-absagen:proposed
- termin-absagen, run 1: step 2: missing action:calendar.delete
- termin-absagen, run 1: step 2: event "Zahnarzt" still in calendar.familie on +3
- termin-absagen, run 2: step 1: missing tool:calendar_events
- termin-absagen, run 2: step 1: missing thread:termin-absagen:proposed
- termin-absagen, run 2: step 2: missing action:calendar.delete
- termin-absagen, run 2: step 2: event "Zahnarzt" still in calendar.familie on +3
- termin-absagen, run 3: step 1: missing tool:calendar_events
- termin-absagen, run 3: step 1: missing thread:termin-absagen:proposed
- termin-absagen, run 3: step 2: missing action:calendar.delete
- termin-absagen, run 3: step 2: event "Zahnarzt" still in calendar.familie on +3
- todo-faellig, run 1: step 1: missing thread:todo
- todo-faellig, run 1: step 2: missing action:todo.add_item
- todo-faellig, run 1: step 2: no item "Batterien AAA" on todo.einkauf
- todo-faellig, run 2: step 1: missing thread:todo
- todo-faellig, run 2: step 2: missing action:todo.add_item
- todo-faellig, run 2: step 2: no item "Batterien AAA" on todo.einkauf
- todo-faellig, run 2 (answer): step 1: answer lacks "Einkauf"
- todo-faellig, run 3: step 1: missing thread:todo
- todo-faellig, run 3: step 2: missing action:todo.add_item
- todo-faellig, run 3: step 2: no item "Batterien AAA" on todo.einkauf
- abfall, run 2: step 1: missing tool:calendar_events
- abfall, run 3: step 1: missing tool:calendar_events
- wecker, run 1: step 1: missing thread:wecker
- wecker, run 1: step 1: missing action:esphome.fake_show_alarm_set
- wecker, run 1: step 1: no alarm at 06:00 on fake_show
- wecker, run 1: step 2: missing tool:thread_undo
- wecker, run 1: step 2: no alarm at 06:30 on fake_show
- wecker, run 2: step 1: missing thread:wecker
- wecker, run 2: step 1: missing action:esphome.fake_show_alarm_set
- wecker, run 2: step 1: no alarm at 06:00 on fake_show
- wecker, run 2: step 2: missing tool:thread_undo
- wecker, run 2: step 2: no alarm at 06:30 on fake_show
- wecker, run 2 (answer): step 1: answer lacks "6"
- wecker, run 3: step 1: missing thread:wecker
- wecker, run 3: step 1: missing action:esphome.fake_show_alarm_set
- wecker, run 3: step 1: no alarm at 06:00 on fake_show
- wecker, run 3: step 2: missing tool:thread_undo
- wecker, run 3: step 2: no alarm at 06:30 on fake_show
- aufgabe, run 1: step 1 took 27.0s, over 15s
- aufgabe, run 1: step 2: no task like "Nachttischlampe" (have Wohnzimmerlampe länger als eine Stunde an;)
- aufgabe, run 2: step 1 took 20.2s, over 15s
- aufgabe, run 3: step 1 took 20.8s, over 15s
- licht, run 1: step 1: light.bed_light is off, not on
- licht, run 2: step 1: light.bed_light is off, not on
- licht, run 3: step 1: light.bed_light is off, not on
