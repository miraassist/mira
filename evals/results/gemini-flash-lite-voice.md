# Eval gemini-flash-lite-voice – 2026-11-15 01:04

Mira 0.24.0 · 3 runs per scenario · **32 of 39 scenarios passed**

Models:
- flashlite: gemini-flash-lite-latest
- main: gemini-flash-latest
- agents: conversation.voice=flashlite

Without these skills (variable missing in testenv/.env): x (MIRA_SKILL_X_CLIENT_SECRET)

| Scenario | Core | Passed | Result + behaviour | Answer | Time per step (median / max, model) | Tokens |
|---|---|---|---|---|---|---|
| kalender-lesen | yes | ✗ | 0/3 | 0/3 | 2.4 / 2.4 s, 2.3 s (good) | 184416 |
| termin-eintragen | yes | ✓ | 3/3 | 3/3 | 2.6 / 2.7 s, 2.5 s (good) | 189693 |
| termin-kalender-waehlen |  | ✓ | 3/3 | 2/3 | 2.5 / 2.6 s, 2.5 s (good)<br>1.3 / 1.3 s, 1.3 s (good) | 291187 |
| termin-absagen | yes | ✓ | 3/3 | 3/3 | 3.2 / 3.4 s, 3.2 s (usable)<br>1.2 / 1.3 s, 1.2 s (good) | 329443 |
| todo-faellig |  | ✗ | 0/3 | 3/3 | 3.1 / 3.8 s, 3.0 s (usable)<br>1.9 / 2.8 s, 1.9 s (good) | 363425 |
| abfall | yes | ✓ | 3/3 | 3/3 | 2.5 / 3.0 s, 2.4 s (good) | 172408 |
| wecker | yes | ✓ | 3/3 | 1/3 | 1.8 / 2.4 s, 1.8 s (good)<br>2.4 / 3.1 s, 2.3 s (good) | 347429 |
| merken | yes | ✓ | 3/3 | 3/3 | 1.2 / 1.4 s, 1.2 s (good)<br>4.1 / 6.2 s, 4.0 s (usable) | 182376 |
| aufgabe |  | ✓ | 3/3 | 2/3 | 5.5 / 5.9 s, 2.5 s (usable)<br>1.3 / 1.7 s, 1.3 s (good) | 301969 |
| nachricht |  | ✓ | 3/3 | 3/3 | 3.9 / 4.1 s, 3.9 s (usable) | 90349 |
| licht | yes | ✓ | 3/3 | 3/3 | 1.3 / 1.3 s, 1.2 s (good)<br>1.3 / 1.4 s, 1.3 s (good) | 185731 |
| garagentor | yes | ✓ | 3/3 | 3/3 | 1.3 / 2.2 s, 1.2 s (good)<br>1.3 / 1.5 s, 1.2 s (good) | 186348 |
| liste-fehlt |  | ✓ | 3/3 | 3/3 | 1.9 / 2.2 s, 1.9 s (good) | 154710 |
| postfach-alle |  | ✗ | 0/3 | 3/3 | 2.8 / 3.2 s, 0.0 s (good)<br>2.8 / 5.3 s, 2.7 s (good) | 155284 |
| postfach-teilweise |  | ✗ | 0/3 | 3/3 | 2.8 / 3.0 s, 0.0 s (good)<br>2.4 / 2.7 s, 2.3 s (good) | 172395 |
| postfach-aenderung |  | ✗ | 0/3 | 3/3 | 2.6 / 2.8 s, 0.0 s (good)<br>3.2 / 4.2 s, 3.2 s (usable) | 241685 |
| postfach-nein |  | ✓ | 3/3 | 3/3 | 2.8 / 3.0 s, 0.0 s (good)<br>1.1 / 2.4 s, 1.1 s (good) | 134972 |
| postfach-auswahl |  | ✗ | 0/3 | 3/3 | 2.8 / 2.8 s, 0.0 s (good)<br>2.6 / 5.0 s, 2.6 s (good) | 209251 |
| postfach-still |  | ✓ | 3/3 | 3/3 | 1.8 / 2.2 s, 0.0 s (good)<br>2.2 / 2.2 s, 0.0 s (good) | 0 |
| postfach-anweisung |  | ✓ | 3/3 | 3/3 | 2.6 / 2.6 s, 0.0 s (good) | 0 |
| postfach-vorlesen |  | ✓ | 3/3 | 3/3 | 2.8 / 3.0 s, 0.0 s (good)<br>1.8 / 2.0 s, 1.8 s (good)<br>1.3 / 1.4 s, 1.2 s (good) | 269083 |
| nachricht-eine-lesen |  | ✓ | 3/3 | 3/3 | 0.0 / 0.0 s, 0.0 s (good)<br>3.4 / 4.0 s, 1.1 s (usable) | 91932 |
| nachricht-eine-uebrigens |  | ✓ | 3/3 | 0/3 | 0.0 / 0.0 s, 0.0 s (good)<br>1.2 / 1.8 s, 1.2 s (good) | 91893 |
| nachricht-zwei-lesen |  | ✓ | 3/3 | 3/3 | 0.0 / 0.0 s, 0.0 s (good)<br>0.0 / 0.0 s, 0.0 s (good)<br>3.6 / 4.4 s, 1.2 s (usable) | 107363 |
| nachricht-zwei-uebrigens |  | ✓ | 3/3 | 0/3 | 0.0 / 0.0 s, 0.0 s (good)<br>0.0 / 0.0 s, 0.0 s (good)<br>1.1 / 1.2 s, 1.1 s (good) | 91895 |
| post-eine-lesen |  | ✓ | 3/3 | 3/3 | 2.4 / 2.8 s, 0.0 s (good)<br>1.8 / 1.8 s, 1.7 s (good) | 153914 |
| post-eine-uebrigens |  | ✓ | 3/3 | 0/3 | 2.4 / 2.4 s, 0.0 s (good)<br>1.4 / 1.8 s, 1.3 s (good) | 99832 |
| post-zwei-lesen |  | ✓ | 3/3 | 1/3 | 2.8 / 2.8 s, 0.0 s (good)<br>2.8 / 2.8 s, 0.0 s (good)<br>1.1 / 1.2 s, 1.1 s (good) | 106176 |
| post-zwei-uebrigens |  | ✓ | 3/3 | 0/3 | 2.8 / 3.0 s, 0.0 s (good)<br>2.8 / 3.2 s, 0.0 s (good)<br>1.1 / 1.7 s, 1.1 s (good) | 105360 |
| ab-eine-lesen |  | ✓ | 3/3 | 3/3 | 0.2 / 0.2 s, 0.0 s (good)<br>1.0 / 1.2 s, 1.0 s (good) | 92684 |
| ab-eine-uebrigens |  | ✓ | 3/3 | 0/3 | 0.2 / 0.2 s, 0.0 s (good)<br>1.1 / 1.2 s, 1.1 s (good) | 91989 |
| ab-zwei-lesen |  | ✓ | 3/3 | 3/3 | 0.2 / 0.2 s, 0.0 s (good)<br>1.2 / 1.9 s, 1.2 s (good) | 92845 |
| ab-zwei-uebrigens |  | ✓ | 3/3 | 0/3 | 0.2 / 0.2 s, 0.0 s (good)<br>1.2 / 1.3 s, 1.1 s (good) | 91992 |
| geraet-eins-lesen |  | ✓ | 3/3 | 3/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>1.5 / 1.8 s, 1.5 s (good) | 92642 |
| geraet-eins-uebrigens |  | ✓ | 3/3 | 0/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>1.4 / 1.4 s, 1.3 s (good) | 76621 |
| geraet-zwei-lesen |  | ✓ | 3/3 | 3/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>1.3 / 1.4 s, 1.3 s (good) | 92806 |
| geraet-zwei-uebrigens |  | ✓ | 3/3 | 0/3 | 60.1 / 60.1 s, 0.0 s (slow)<br>1.3 / 1.4 s, 1.3 s (good) | 91999 |
| hermes-recherche |  | ✗ | 0/3 | 0/3 | 7.1 / 7.3 s, 2.1 s (usable) | 139785 |
| waermepumpe-erklaeren |  | ✓ | 3/3 | 3/3 | 6.8 / 7.0 s, 1.8 s (usable) | 139714 |

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
- termin-kalender-waehlen, run 1 (answer): step 1: answer lacks "Familie"
- termin-kalender-waehlen, run 1 (answer): step 1: answer lacks "Ben"
- todo-faellig, run 1: step 1: must not action:todo.add_item
- todo-faellig, run 2: step 1: must not action:todo.add_item
- todo-faellig, run 2: step 2: missing action:todo.add_item
- todo-faellig, run 3: step 1: must not action:todo.add_item
- todo-faellig, run 3: step 2: missing action:todo.add_item
- wecker, run 1 (answer): step 1: answer lacks "6"
- wecker, run 2 (answer): step 1: answer lacks "6"
- aufgabe, run 2 (answer): step 1: answer lacks "Nachttischlampe"
- postfach-alle, run 1: step 2: no event "Elternabend" at +8 19:30 in calendar.anna
- postfach-alle, run 1: step 2: no event "Laterne" at +15 17:00 in calendar.anna
- postfach-alle, run 2: step 2: no event "Elternabend" at +8 19:30 in calendar.anna
- postfach-alle, run 2: step 2: no event "Laterne" at +15 17:00 in calendar.anna
- postfach-alle, run 3: step 2: no event "Elternabend" at +8 19:30 in calendar.anna
- postfach-alle, run 3: step 2: no event "Laterne" at +15 17:00 in calendar.anna
- postfach-teilweise, run 1: step 2: no event "Eltern" at +6 18:00 in calendar.anna
- postfach-teilweise, run 2: step 2: no event "Eltern" at +6 18:00 in calendar.anna
- postfach-teilweise, run 3: step 2: no event "Eltern" at +6 18:00 in calendar.anna
- postfach-aenderung, run 1: step 2: no event "Elternabend" at +9 20:00 in calendar.anna
- postfach-aenderung, run 2: step 2: no event "Elternabend" at +9 20:00 in calendar.anna
- postfach-aenderung, run 3: step 2: no event "Elternabend" at +9 20:00 in calendar.anna
- postfach-auswahl, run 1: step 2: no event "Kontroll" at +12 14:30 in calendar.anna
- postfach-auswahl, run 2: step 2: no event "Kontroll" at +12 14:30 in calendar.anna
- postfach-auswahl, run 3: step 2: no event "Kontroll" at +12 14:30 in calendar.anna
- nachricht-eine-uebrigens, run 1 (answer): step 2: answer lacks "Waschmaschine"
- nachricht-eine-uebrigens, run 2 (answer): step 2: answer lacks "Waschmaschine"
- nachricht-eine-uebrigens, run 3 (answer): step 2: answer lacks "Waschmaschine"
- nachricht-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Waschmaschine"
- nachricht-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Trockner"
- nachricht-zwei-uebrigens, run 2 (answer): step 3: answer lacks "Waschmaschine"
- nachricht-zwei-uebrigens, run 2 (answer): step 3: answer lacks "Trockner"
- nachricht-zwei-uebrigens, run 3 (answer): step 3: answer lacks "Waschmaschine"
- nachricht-zwei-uebrigens, run 3 (answer): step 3: answer lacks "Trockner"
- post-eine-uebrigens, run 1 (answer): step 2: answer lacks "Elternabend"
- post-eine-uebrigens, run 1 (answer): step 2: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-eine-uebrigens, run 2 (answer): step 2: answer lacks "Elternabend"
- post-eine-uebrigens, run 2 (answer): step 2: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-eine-uebrigens, run 3 (answer): step 2: answer lacks "Elternabend"
- post-eine-uebrigens, run 3 (answer): step 2: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-lesen, run 2 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-lesen, run 3 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Elternabend"
- post-zwei-uebrigens, run 1 (answer): step 3: answer lacks "Kaya|Kieferorthop|Kontroll"
- post-zwei-uebrigens, run 1 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-uebrigens, run 2 (answer): step 3: answer lacks "Elternabend"
- post-zwei-uebrigens, run 2 (answer): step 3: answer lacks "Kaya|Kieferorthop|Kontroll"
- post-zwei-uebrigens, run 2 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- post-zwei-uebrigens, run 3 (answer): step 3: answer lacks "Elternabend"
- post-zwei-uebrigens, run 3 (answer): step 3: answer lacks "Kaya|Kieferorthop|Kontroll"
- post-zwei-uebrigens, run 3 (answer): step 3: answer lacks "durchgehen|eintragen|anfangen|Kalender|soll ich"
- ab-eine-uebrigens, run 1 (answer): step 2: answer lacks "Oma"
- ab-eine-uebrigens, run 1 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-eine-uebrigens, run 2 (answer): step 2: answer lacks "Oma"
- ab-eine-uebrigens, run 2 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-eine-uebrigens, run 3 (answer): step 2: answer lacks "Oma"
- ab-eine-uebrigens, run 3 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Oma"
- ab-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Ben"
- ab-zwei-uebrigens, run 1 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-zwei-uebrigens, run 2 (answer): step 2: answer lacks "Oma"
- ab-zwei-uebrigens, run 2 (answer): step 2: answer lacks "Ben"
- ab-zwei-uebrigens, run 2 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- ab-zwei-uebrigens, run 3 (answer): step 2: answer lacks "Oma"
- ab-zwei-uebrigens, run 3 (answer): step 2: answer lacks "Ben"
- ab-zwei-uebrigens, run 3 (answer): step 2: answer lacks "abspielen|anhören|vorspielen|hören"
- geraet-eins-uebrigens, run 1 (answer): step 2: answer lacks "Rauchmelder"
- geraet-eins-uebrigens, run 1 (answer): step 2: answer lacks "einbinden|einrichten"
- geraet-eins-uebrigens, run 2 (answer): step 2: answer lacks "Rauchmelder"
- geraet-eins-uebrigens, run 2 (answer): step 2: answer lacks "einbinden|einrichten"
- geraet-eins-uebrigens, run 3 (answer): step 2: answer lacks "Rauchmelder"
- geraet-eins-uebrigens, run 3 (answer): step 2: answer lacks "einbinden|einrichten"
- geraet-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Rauchmelder"
- geraet-zwei-uebrigens, run 1 (answer): step 2: answer lacks "Bewegungsmelder"
- geraet-zwei-uebrigens, run 1 (answer): step 2: answer lacks "einbinden|einrichten"
- geraet-zwei-uebrigens, run 2 (answer): step 2: answer lacks "Rauchmelder"
- geraet-zwei-uebrigens, run 2 (answer): step 2: answer lacks "Bewegungsmelder"
- geraet-zwei-uebrigens, run 2 (answer): step 2: answer lacks "einbinden|einrichten"
- geraet-zwei-uebrigens, run 3 (answer): step 2: answer lacks "Rauchmelder"
- geraet-zwei-uebrigens, run 3 (answer): step 2: answer lacks "Bewegungsmelder"
- geraet-zwei-uebrigens, run 3 (answer): step 2: answer lacks "einbinden|einrichten"
- hermes-recherche, run 1: step 1: missing thread:hermes
- hermes-recherche, run 1: step 1: must not tool:skill_open:nachdenken
- hermes-recherche, run 1: step 1: must not tool:nachdenken
- hermes-recherche, run 1: step 1: the fake A2A agent received no message with "Wärmepumpe" (got 0: )
- hermes-recherche, run 1 (answer): step 1: answer lacks "Hermes"
- hermes-recherche, run 2: step 1: missing thread:hermes
- hermes-recherche, run 2: step 1: must not tool:skill_open:nachdenken
- hermes-recherche, run 2: step 1: must not tool:nachdenken
- hermes-recherche, run 2: step 1: the fake A2A agent received no message with "Wärmepumpe" (got 0: )
- hermes-recherche, run 2 (answer): step 1: answer lacks "Hermes"
- hermes-recherche, run 3: step 1: missing thread:hermes
- hermes-recherche, run 3: step 1: must not tool:skill_open:nachdenken
- hermes-recherche, run 3: step 1: must not tool:nachdenken
- hermes-recherche, run 3: step 1: the fake A2A agent received no message with "Wärmepumpe" (got 0: )
- hermes-recherche, run 3 (answer): step 1: answer lacks "Hermes"
