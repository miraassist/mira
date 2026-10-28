# Skills with HA actions – spec

Status 2026-10-17: built in 0.6.18. Builds on [threads](vorgaenge.md) and [tasks](aufgaben.md). Example: the alarm.

## Principle

A request does not necessarily become an automation. A **skill** describes the concern:
- how Mira recognizes it;
- which details she needs, as fields of the thread;
- how it is carried out in the end.

An automation (`tasks.automation`) is only one possibility. Another is to **call an HA action immediately** that the skill itself specifies. So a device function becomes a skill, without code: the Show's alarm, a timer, "show the camera".

The skill comes from the operator, who knows what he is doing. The skill specifies the action, not the model; the model only fills the fields.

## The finish `action`

Besides the built-in finishes (`memory.save`, `tasks.automation`) a skill can specify one or more HA actions as finish:

```yaml
metadata:
  mira:
    target:
      timeout: 5m
      fields:
        - name: time
          description: Uhrzeit, HH:MM
        - name: days
          description: Wochentage, leer für einmalig
          optional: true
        - name: label
          description: Bezeichnung, z. B. „Arbeit“
          optional: true
      finish:
        action: esphome.{{ satellite.device }}_alarm_set
        data:
          time: "{{ time }}"
          days: "{{ days }}"
          label: "{{ label }}"
---
# Wecker
Erkennen: „Weck mich …“, „Stell einen Wecker …“. Steht die übliche Zeit im Gedächtnis, nimm sie; sonst frag kurz: „Wann denn?“
Sag danach, was du gestellt hast: Uhrzeit, Tag, Raum.
```

- **Empty values stay**: ESPHome actions want every field, and empty means something (on the Show, `days` empty means: once, at the next occurrence of this time; otherwise `weekdays` or `mon,wed`).
- **Placeholders** in `action` and `data`: the fields of the thread as well as the **place**, i.e. `area` (ID and name), `satellite` (entity), `satellite.device` (ESPHome device name), and `person`, if known.
- **If the action does not exist** (e.g. an Echo with echolocal has no alarm), Mira says honestly that it does not work on this satellite. There is no fixed fallback. If the model finds a suitable way itself with its tools, it may propose it.
- **The finish runs immediately in the conversation**, like `memory.save`, not in the background.
- What needs a yes in the conversation applies here too: the action passes the hooks as a call of its own (`skill_action`); asking, it is proposed as with `confirm: true`; refused, the thread ends ([hooks.md](hooks.md), Skill actions; since 2026-10-28, before nothing checked it).

## Confirming: optional, as before

`confirm` can already be set per skill today.
- **`confirm: true`**: Mira asks before the call ("Ich wecke dich morgen um 6, passt das?").
- **Without `confirm`**: Mira executes and says what she has done ("Wecker steht: morgen 6 Uhr, Schlafzimmer."). That is how Alexa does it with the alarm, and that fits almost always.

For the alarm: without `confirm`.

## Correcting instead of starting over

If Mira misunderstood, one corrects in the conversation: "Nein, halb sieben." Mira knows from the history what she has just set. For that, the skill can name a second action that takes back what was set, e.g. `alarm_delete` with time and label. Mira then takes it back and sets it anew, without one having to repeat everything.

```yaml
      undo:
        action: esphome.{{ satellite.device }}_alarm_delete
        data: { time: "{{ time }}", label: "" }
```

The tool for this is called `thread_undo`: It takes back the newest executed action of a thread at the same place from the last 15 minutes; after that Mira restarts the thread with the right thing. Tried on the Show: `alarm_delete` only deletes with an empty label, with a label nothing.

Where Home Assistant has no action to take something back, `undo` may name a building block of the core instead. The first is `calendar.delete_created` for the skill `termin`: it finds the event just added by calendar, title and start and deletes it (kalender.md).

## Scenarios

- "Weck mich morgen zur üblichen Zeit" at the satellite in the bedroom, person known, in memory "steht werktags um 6 auf": Mira sets 6:00 on the Show in the bedroom and says so.
- "Bitte weck mich morgen": "Wann denn?" – "Um 6": Mira sets 6:00 and says so.
- "Nein, halb sieben": Mira takes back 6:00 and sets 6:30.
- On the Echo with echolocal: "Hier kann ich keinen Wecker stellen."

## Needed for this (once, general)

1. The finish `action` (and `undo`) with placeholders.
2. The place as placeholder: area, satellite, ESPHome device. The same information should also go into the draft of tasks (see list: place and person in the draft context).

## Deliberately not

- **An alarm melody via the media player** as fallback: left out for now.
- **Checking what a skill author calls**: He is the operator.
