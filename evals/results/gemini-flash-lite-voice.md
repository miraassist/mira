# Eval gemini-flash-lite-voice – 2026-11-14 20:02

Mira 0.24.0 · 3 runs per scenario · **17 of 18 scenarios passed**

Models:
- flashlite: gemini-flash-lite-latest
- main: gemini-flash-latest
- agents: conversation.voice=flashlite

Measured again on their own: hermes-recherche (2026-11-14 23:48, Mira 0.24.0, 3 runs); waermepumpe-erklaeren (2026-11-14 23:48, Mira 0.24.0, 3 runs); hermes-recherche (2026-11-15 00:07, Mira 0.24.0, 3 runs)

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| nachricht-eine-lesen |  | ✓ | 3/3 | 3/3 | 0.0 / 0.0 s, 0.0 s (good)<br>3.7 / 3.8 s, 1.4 s (usable) | 90640 |
| nachricht-eine-uebrigens |  | ✓ | 3/3 | 3/3 | 0.0 / 0.0 s, 0.0 s (good)<br>3.0 / 3.6 s, 1.3 s (good) | 60332 |
| nachricht-zwei-lesen |  | ✓ | 3/3 | 3/3 | 0.0 / 0.0 s, 0.0 s (good)<br>0.0 / 0.0 s, 0.0 s (good)<br>3.6 / 4.4 s, 1.6 s (usable) | 105871 |
| nachricht-zwei-uebrigens |  | ✓ | 3/3 | 3/3 | 0.0 / 0.0 s, 0.0 s (good)<br>0.0 / 0.0 s, 0.0 s (good)<br>3.7 / 3.8 s, 1.4 s (usable) | 90604 |
| post-eine-lesen |  | ✓ | 3/3 | 2/3 | 2.4 / 2.4 s, 0.0 s (good)<br>2.4 / 2.7 s, 2.4 s (good) | 169734 |
| post-eine-uebrigens |  | ✓ | 3/3 | 3/3 | 2.8 / 2.8 s, 0.0 s (good)<br>4.4 / 4.7 s, 1.6 s (usable) | 98665 |
| post-zwei-lesen |  | ✓ | 3/3 | 2/3 | 2.6 / 3.4 s, 0.0 s (good)<br>3.2 / 4.0 s, 0.0 s (usable)<br>1.3 / 1.9 s, 1.3 s (good) | 123357 |
| post-zwei-uebrigens |  | ✓ | 3/3 | 3/3 | 2.6 / 3.4 s, 0.0 s (good)<br>2.6 / 2.6 s, 0.0 s (good)<br>3.6 / 3.7 s, 1.2 s (usable) | 103833 |
| ab-eine-lesen |  | ✓ | 3/3 | 3/3 | 0.2 / 0.2 s, 0.0 s (good)<br>1.4 / 1.5 s, 1.4 s (good) | 91407 |
| ab-eine-uebrigens |  | ✓ | 2/3 | 3/3 | 0.2 / 0.2 s, 0.0 s (good)<br>4.5 / 18.9 s, 1.3 s (usable) | 90707 |
| ab-zwei-lesen |  | ✓ | 3/3 | 3/3 | 0.2 / 0.2 s, 0.0 s (good)<br>2.2 / 3.3 s, 2.1 s (good) | 141846 |
| ab-zwei-uebrigens |  | ✓ | 3/3 | 3/3 | 0.2 / 0.2 s, 0.0 s (good)<br>4.0 / 4.2 s, 1.3 s (usable) | 90702 |
| geraet-eins-lesen |  | ✓ | 3/3 | 3/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>1.5 / 1.6 s, 1.5 s (good) | 91357 |
| geraet-eins-uebrigens |  | ✓ | 3/3 | 3/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>3.9 / 4.1 s, 1.4 s (usable) | 90705 |
| geraet-zwei-lesen |  | ✓ | 3/3 | 3/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>1.6 / 1.8 s, 1.6 s (good) | 91506 |
| geraet-zwei-uebrigens |  | ✓ | 3/3 | 3/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>5.0 / 5.6 s, 1.7 s (usable) | 90712 |
| hermes-recherche |  | ✗ | 1/3 | 2/3 | 6.6 / 8.1 s, 1.5 s (usable) | 92581 |
| waermepumpe-erklaeren |  | ✓ | 3/3 | 3/3 | 6.3 / 6.3 s, 1.2 s (usable) | 92410 |

## Findings

- post-eine-lesen, run 3 (answer): step 2: answer lacks "Elternabend"
- post-zwei-lesen, run 3 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- ab-eine-uebrigens, run 3: step 2 took 18.9s, over 15s
- hermes-recherche, run 1: step 1: missing thread:hermes
- hermes-recherche, run 1: step 1: must not tool:delegate
- hermes-recherche, run 1: step 1: the fake A2A agent received no message with "Wärmepumpe" (got 0: )
- hermes-recherche, run 2: step 1: missing thread:hermes
- hermes-recherche, run 2: step 1: must not tool:delegate
- hermes-recherche, run 2: step 1: the fake A2A agent received no message with "Wärmepumpe" (got 0: )
- hermes-recherche, run 3 (answer): step 1: answer lacks "Hermes"
