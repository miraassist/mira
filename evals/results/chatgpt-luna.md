# Eval chatgpt-luna – 2026-11-08 07:07

Mira 0.18.0 · 3 runs per scenario · **9 of 12 scenarios passed**

Models:
- main: gpt-6-luna

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | yes | ✗ | 2/3 | 1/3 | 3.9 / 5.0 s, 3.9 s (usable) | 92394 |
| termin-eintragen | yes | ✓ | 3/3 | 3/3 | 7.5 / 10.4 s, 7.5 s (usable) | 140267 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 3/3 | 8.2 / 9.2 s, 8.2 s (slow)<br>5.3 / 5.5 s, 5.2 s (usable) | 217087 |
| termin-absagen | yes | ✓ | 3/3 | 3/3 | 7.6 / 7.9 s, 7.5 s (usable)<br>3.7 / 3.8 s, 3.7 s (usable) | 209322 |
| todo-faellig |  | ✗ | 1/3 | 3/3 | 5.8 / 6.2 s, 5.7 s (usable)<br>3.6 / 5.1 s, 3.6 s (usable) | 173904 |
| abfall | yes | ✓ | 3/3 | 3/3 | 5.5 / 12.4 s, 5.4 s (usable) | 103742 |
| wecker | yes | ✓ | 3/3 | 3/3 | 5.5 / 6.3 s, 5.5 s (usable)<br>6.6 / 7.7 s, 6.6 s (usable) | 245066 |
| merken | yes | ✓ | 3/3 | 3/3 | 2.9 / 3.1 s, 2.9 s (good)<br>3.3 / 4.9 s, 3.3 s (usable) | 133495 |
| aufgabe |  | ✓ | 3/3 | 3/3 | 12.6 / 14.1 s, 8.7 s (slow)<br>2.9 / 3.1 s, 2.8 s (good) | 215989 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.7 / 4.4 s, 3.7 s (usable) | 66023 |
| licht | yes | ✓ | 3/3 | 3/3 | 3.5 / 4.0 s, 3.5 s (usable)<br>3.4 / 3.8 s, 3.4 s (usable) | 136434 |
| garagentor | yes | ✗ | 0/3 | 3/3 | 4.4 / 5.9 s, 4.3 s (usable)<br>4.3 / 5.8 s, 4.3 s (usable) | 136792 |

## Findings

- kalender-lesen, run 1 (answer): step 1: answer lacks "10"
- kalender-lesen, run 2: step 1: missing tool:calendar_events
- kalender-lesen, run 2 (answer): step 1: answer lacks "10"
- todo-faellig, run 1: step 1: must not action:todo.add_item
- todo-faellig, run 1: step 2: missing action:todo.add_item
- todo-faellig, run 3: step 1: must not action:todo.add_item
- todo-faellig, run 3: step 2: missing action:todo.add_item
- garagentor, run 1: step 2: cover.garage_door is closed, not opening|open
- garagentor, run 2: step 2: cover.garage_door is closed, not opening|open
- garagentor, run 3: step 2: cover.garage_door is closed, not opening|open
