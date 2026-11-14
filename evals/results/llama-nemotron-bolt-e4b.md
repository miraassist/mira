# Eval llama-nemotron-bolt-e4b – 2026-11-14 23:02

Mira 0.24.0 · 1 runs per scenario · **11 of 17 scenarios passed**

Models:
- main: nvidia/nemotron-3.5-bolt-e4b

Measured again on their own: licht (2026-11-14 23:46, Mira 0.24.0, 1 runs)

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| nachricht-eine-lesen |  | ✗ | 0/1 | 0/1 | 0.0 / 0.0 s, 0.0 s (good)<br>23.5 / 23.5 s, 3.5 s (slow) | 29497 |
| nachricht-eine-uebrigens |  | ✓ | 1/1 | 0/1 | 0.0 / 0.0 s, 0.0 s (good)<br>2.3 / 2.3 s, 2.3 s (good) | 14748 |
| nachricht-zwei-lesen |  | ✗ | 0/1 | 0/1 | 0.0 / 0.0 s, 0.0 s (good)<br>0.0 / 0.0 s, 0.0 s (good)<br>22.6 / 22.6 s, 2.6 s (slow) | 29497 |
| nachricht-zwei-uebrigens |  | ✓ | 1/1 | 0/1 | 0.0 / 0.0 s, 0.0 s (good)<br>0.0 / 0.0 s, 0.0 s (good)<br>2.2 / 2.2 s, 2.2 s (good) | 14748 |
| post-eine-lesen |  | ✗ | 0/1 | 0/1 | 9.6 / 9.6 s, 0.0 s (slow)<br>22.7 / 22.7 s, 22.7 s (slow) | 16002 |
| post-eine-uebrigens |  | ✓ | 1/1 | 0/1 | 8.8 / 8.8 s, 0.0 s (slow)<br>4.4 / 4.4 s, 4.4 s (usable) | 32088 |
| post-zwei-lesen |  | ✗ | 0/1 | 0/1 | 8.4 / 8.4 s, 0.0 s (slow)<br>8.4 / 8.4 s, 0.0 s (slow)<br>4.1 / 4.1 s, 4.1 s (usable) | 16897 |
| post-zwei-uebrigens |  | ✓ | 1/1 | 0/1 | 9.0 / 9.0 s, 0.0 s (slow)<br>10.2 / 10.2 s, 0.0 s (slow)<br>5.9 / 5.9 s, 5.9 s (usable) | 33932 |
| ab-eine-lesen |  | ✗ | 0/1 | 1/1 | 0.2 / 0.2 s, 0.0 s (good)<br>23.1 / 23.1 s, 23.1 s (slow) | 29802 |
| ab-eine-uebrigens |  | ✓ | 1/1 | 0/1 | 0.2 / 0.2 s, 0.0 s (good)<br>2.7 / 2.7 s, 2.6 s (good) | 29574 |
| ab-zwei-lesen |  | ✓ | 1/1 | 0/1 | 0.2 / 0.2 s, 0.0 s (good)<br>2.7 / 2.7 s, 2.7 s (good) | 29835 |
| ab-zwei-uebrigens |  | ✓ | 1/1 | 0/1 | 0.2 / 0.2 s, 0.0 s (good)<br>2.4 / 2.4 s, 2.4 s (good) | 29573 |
| geraet-eins-lesen |  | ✓ | 1/1 | 1/1 | 60.1 / 60.1 s, 0.0 s (slow)<br>3.3 / 3.3 s, 3.3 s (usable) | 29788 |
| geraet-eins-uebrigens |  | ✓ | 1/1 | 0/1 | 60.1 / 60.1 s, 0.0 s (slow)<br>2.6 / 2.6 s, 2.6 s (good) | 29573 |
| geraet-zwei-lesen |  | ✓ | 1/1 | 1/1 | 60.1 / 60.1 s, 0.0 s (slow)<br>3.9 / 3.9 s, 3.9 s (usable) | 29839 |
| geraet-zwei-uebrigens |  | ✓ | 1/1 | 0/1 | 60.1 / 60.1 s, 0.0 s (slow)<br>2.7 / 2.7 s, 2.7 s (good) | 29574 |
| licht | yes | ✗ | 0/1 | 0/1 | 1.4 / 1.4 s, 1.4 s (good)<br>4.7 / 4.7 s, 4.6 s (usable) | 44698 |

## Findings

- nachricht-eine-lesen, run 1: step 2 took 23.5s, over 15s
- nachricht-eine-lesen, run 1 (answer): step 2: answer lacks "Waschmaschine"
- nachricht-eine-uebrigens, run 1 (answer): step 2: answer lacks "Waschmaschine"
- nachricht-zwei-lesen, run 1: step 3 took 22.6s, over 15s
- nachricht-zwei-lesen, run 1 (answer): step 3: answer lacks "Waschmaschine"
- nachricht-zwei-lesen, run 1 (answer): step 3: answer lacks "Trockner"
- nachricht-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Waschmaschine"
- nachricht-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Trockner"
- post-eine-lesen, run 1: step 2 took 22.7s, over 15s
- post-eine-lesen, run 1: step 2: missing tool:messages_read
- post-eine-lesen, run 1 (answer): step 2: answer lacks "Elternabend"
- post-eine-lesen, run 1 (answer): step 2: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-eine-uebrigens, run 1 (answer): step 2: answer lacks "Elternabend"
- post-eine-uebrigens, run 1 (answer): step 2: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-lesen, run 1: step 3: missing tool:messages_read
- post-zwei-lesen, run 1 (answer): step 3: answer lacks "Elternabend"
- post-zwei-lesen, run 1 (answer): step 3: answer lacks "Kaya|Kieferorthop|Kontroll"
- post-zwei-lesen, run 1 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Elternabend"
- post-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Kaya|Kieferorthop|Kontroll"
- post-zwei-uebrigens, run 1 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- ab-eine-lesen, run 1: step 2 took 23.1s, over 15s
- ab-eine-uebrigens, run 1 (answer): step 2: answer lacks "Oma"
- ab-eine-uebrigens, run 1 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-zwei-lesen, run 1 (answer): step 2: answer lacks "Oma"
- ab-zwei-lesen, run 1 (answer): step 2: answer lacks "Ben"
- ab-zwei-lesen, run 1 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Oma"
- ab-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Ben"
- ab-zwei-uebrigens, run 1 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- geraet-eins-uebrigens, run 1 (answer): step 2: answer lacks "Rauchmelder"
- geraet-eins-uebrigens, run 1 (answer): step 2: answer lacks "einbinden|einrichten"
- geraet-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Rauchmelder"
- geraet-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Bewegungsmelder"
- geraet-zwei-uebrigens, run 1 (answer): step 2: answer lacks "einbinden|einrichten"
- licht, run 1: step 1: light.bed_light is off, not on
- licht, run 1 (answer): step 2: answer lacks "an|eingeschaltet|leuchtet"
