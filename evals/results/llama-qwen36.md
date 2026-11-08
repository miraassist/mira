# Eval llama-qwen36 – 2026-11-08 22:40

Mira 0.19.0 · 3 runs per scenario · **8 of 12 scenarios passed**

Models:
- main: qwen3.6-35b-a3b

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | yes | ✓ | 3/3 | 3/3 | 4.3 / 5.9 s, 4.2 s (usable) | 91929 |
| termin-eintragen | yes | ✓ | 3/3 | 3/3 | 11.4 / 12.4 s, 11.4 s (slow) | 189381 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 3/3 | 11.3 / 11.8 s, 11.2 s (slow)<br>4.8 / 4.8 s, 4.8 s (usable) | 290613 |
| termin-absagen | yes | ✗ | 0/3 | 3/3 | 8.6 / 8.7 s, 8.6 s (slow)<br>5.5 / 5.5 s, 5.5 s (usable) | 329652 |
| todo-faellig |  | ✗ | 0/3 | 1/3 | 3.7 / 4.1 s, 3.6 s (usable)<br>4.6 / 4.6 s, 4.6 s (usable) | 185550 |
| abfall | yes | ✓ | 3/3 | 3/3 | 8.0 / 8.5 s, 8.0 s (usable) | 139941 |
| wecker | yes | ✗ | 0/3 | 0/3 | 3.8 / 4.4 s, 3.7 s (usable)<br>6.7 / 6.9 s, 6.7 s (usable) | 232178 |
| merken | yes | ✓ | 3/3 | 3/3 | 4.0 / 4.0 s, 4.0 s (usable)<br>3.5 / 3.6 s, 3.5 s (usable) | 182129 |
| aufgabe |  | ✗ | 0/3 | 3/3 | 26.3 / 30.8 s, 21.2 s (slow)<br>3.7 / 4.7 s, 3.7 s (usable) | 304209 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.3 / 4.0 s, 3.3 s (usable) | 90355 |
| licht | yes | ✓ | 3/3 | 1/3 | 4.0 / 4.7 s, 4.0 s (usable)<br>3.6 / 4.1 s, 3.6 s (usable) | 185297 |
| garagentor | yes | ✓ | 3/3 | 3/3 | 3.6 / 4.0 s, 3.5 s (usable)<br>4.0 / 4.0 s, 4.0 s (usable) | 185955 |

## Findings

- termin-absagen, run 1: step 1: missing thread:termin-absagen:proposed
- termin-absagen, run 2: step 1: missing thread:termin-absagen:proposed
- termin-absagen, run 3: step 1: missing thread:termin-absagen:proposed
- todo-faellig, run 1: step 1: missing thread:todo
- todo-faellig, run 1: step 2: missing action:todo.add_item
- todo-faellig, run 1: step 2: no item "Batterien AAA" on todo.einkauf
- todo-faellig, run 1 (answer): step 1: answer lacks "Einkauf"
- todo-faellig, run 2: step 1: missing thread:todo
- todo-faellig, run 2: step 2: missing action:todo.add_item
- todo-faellig, run 2: step 2: no item "Batterien AAA" on todo.einkauf
- todo-faellig, run 3: step 1: missing thread:todo
- todo-faellig, run 3: step 2: missing action:todo.add_item
- todo-faellig, run 3: step 2: "Batterien AAA" is due "", not 2026-11-10
- todo-faellig, run 3 (answer): step 1: answer lacks "Einkauf"
- wecker, run 1: step 1: missing thread:wecker
- wecker, run 1: step 1: missing action:esphome.fake_show_alarm_set
- wecker, run 1: step 1: no alarm at 06:00 on fake_show
- wecker, run 1: step 2: missing tool:thread_undo
- wecker, run 1: step 2: no alarm at 06:30 on fake_show
- wecker, run 1 (answer): step 1: answer lacks "6"
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
- wecker, run 3 (answer): step 1: answer lacks "6"
- aufgabe, run 1: step 1 took 30.8s, over 15s
- aufgabe, run 2: step 1 took 25.9s, over 15s
- aufgabe, run 3: step 1 took 26.3s, over 15s
- licht, run 1 (answer): step 2: answer lacks "an|eingeschaltet|leuchtet"
- licht, run 3 (answer): step 2: answer lacks "an|eingeschaltet|leuchtet"
