# Eval gemini-flash – 2026-11-05 22:20

Mira 0.16.0 · 3 runs per scenario · **12 of 12 scenarios passed**

Models:
- main: gemini-flash-latest

Measured again on their own: termin-absagen (2026-11-05 22:34, Mira 0.16.0, 3 runs); wecker (2026-11-22 17:07, Mira 0.29.0, 1 runs)

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | yes | ✓ | 3/3 | 3/3 | 4.0 / 12.3 s, 4.0 s (brauchbar) | 82741 |
| termin-eintragen | yes | ✓ | 3/3 | 3/3 | 9.2 / 11.6 s, 9.2 s (langsam) | 171277 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 3/3 | 9.3 / 10.7 s, 9.3 s (langsam)<br>5.6 / 6.2 s, 5.6 s (brauchbar) | 263933 |
| termin-absagen | yes | ✓ | 3/3 | 3/3 | 7.2 / 7.6 s, 7.2 s (brauchbar)<br>4.0 / 6.3 s, 3.9 s (brauchbar) | 254582 |
| todo-faellig |  | ✓ | 2/3 | 3/3 | 13.9 / 17.1 s, 13.9 s (langsam)<br>5.0 / 5.9 s, 4.9 s (brauchbar) | 230071 |
| abfall | yes | ✓ | 3/3 | 3/3 | 7.3 / 7.6 s, 7.2 s (brauchbar) | 126960 |
| wecker | yes | ✓ | 1/1 | 1/1 | 7.3 / 7.3 s, 7.3 s (usable)<br>11.3 / 11.3 s, 11.2 s (slow) | 123253 |
| merken | yes | ✓ | 3/3 | 3/3 | 4.4 / 5.5 s, 4.4 s (brauchbar)<br>4.4 / 10.0 s, 4.3 s (brauchbar) | 163717 |
| aufgabe |  | ✓ | 2/3 | 3/3 | 11.8 / 32.9 s, 8.6 s (langsam)<br>3.9 / 7.9 s, 3.9 s (brauchbar) | 216935 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.5 / 3.8 s, 3.5 s (brauchbar) | 81059 |
| licht | yes | ✓ | 3/3 | 2/3 | 3.6 / 3.8 s, 3.5 s (brauchbar)<br>3.7 / 4.3 s, 3.6 s (brauchbar) | 166905 |
| garagentor | yes | ✓ | 3/3 | 3/3 | 7.3 / 12.5 s, 7.3 s (brauchbar)<br>3.7 / 3.8 s, 3.7 s (brauchbar) | 168136 |

## Findings

- todo-faellig, run 1: step 1 took 17.1s, over 15s
- aufgabe, run 2: step 1 took 32.9s, over 15s
- licht, run 1 (answer): step 2: answer lacks "an|eingeschaltet|leuchtet"
