# A schedule for one person – Spec

Status 2026-11-09: steps 1 and 2 built (not released). Scenarios: [schedule-receiver.feature](schedule-receiver.feature).

## Goal

A skill's schedule reports to the house, or with `per_person` once for each person
([Schedules](schedules.md), [Skills](../guide/en.md#creating-your-own-skill)). What is missing: a schedule whose
result is for **one** person and is read, not heard: a list of posts with links twice a day, sent to the person's
phone. As a message to the house it is spoken to whoever walks in; as a message to the person it is spoken to them
at a satellite, and reaches their phone only when it is important and nobody heard it by the morning; as a
confidential message it is spoken when the person is alone at home. A list with links read aloud is of no use.

## Decided (2026-11-09)

| # | Question | Decision |
|---|---|---|
| 1 | How the list reaches the person | A Mira message to the person, over their channels (Telegram, the companion app); no Home Assistant automation that calls an agent and sends the result itself |
| 2 | Who the person is | A schedule names one person with `to`, usually from a setting of the skill (`to: "{{empfaenger}}"`) |
| 3 | How the message is delivered | **Private**: at once and only over the person's channels; never spoken, never shown on a satellite or a light, never attached to an answer. In quiet hours it waits for their end, like every message that is not urgent |

## Building blocks

- **The concept: a skill's schedule with its message** ([Schedules](schedules.md), the runner in
  `internal/skills/runner.go`): the job runs in the background, ends with `importance`, `spoken`, `details`, and its
  message is queued. `to` changes only to whom, and how it is delivered.
- **The message's confidentiality** ([Messages](messages.md)): `normal` may be spoken to anyone in the room,
  `confidential` only as `notifier.confidential_voice` allows. A third level, **`private`**, is never spoken: the
  notifier sends it over the person's channels as it does today with a confidential message under
  `confidential_voice: never`. No new table column; the column holds text.
- **The person's channels** ([Channels](channels.md)): the first of them that works, as for every private delivery.
- Departure from per-person schedules: `to` runs the job **once** (not once per person) and adds no profile of the
  person to the task; the task says what it needs.

## What it does

### In the skill

```yaml
metadata:
  mira:
    settings:
      empfaenger: { description: "Wer die Liste bekommt (Name der Person in Home Assistant)", required: true }
    schedules:
      - name: morgen
        description: Posts des Morgens
        cron: "0 8 * * *"
        to: "{{empfaenger}}"
        task: >-
          ...
```

- `to` is filled with the skill's settings like `task`. Empty after filling (no setting, an empty value): the
  schedule is for the house, as without `to`.
- `to` together with `per_person: true`: the skill does not start, with the reason ("to and per_person exclude each
  other"), shown under Repairs like every broken skill.
- Changing `to` on the Schedule page is not offered; it is a setting of the skill.

### When it runs

1. The runner looks up the person among those Mira can reach privately: by the person's key (`stefan`), name
   ("Stefan") or Home Assistant login, case and spaces aside. Only persons with at least one channel count; "alone at
   home is me" does not, as a private message needs a channel.
2. The job runs once, as any schedule (one background agent run with the skill's tools).
3. Its message goes to that person with confidentiality `private`; the key against duplicates is per day and
   schedule, so "Run now" twice on one day replaces a message not yet sent.
4. The notifier, at its next tick (once a minute) outside quiet hours, sends the spoken text and the details over
   the person's first working channel and marks it delivered there. When someone enters a room, a private message
   is sent the same way, never spoken. It does not count as waiting: no light, no screen, no "Übrigens".
5. An urgent private message (`urgent: true` and importance 3) goes over the channels at once, also in quiet hours,
   and is not announced on the satellites.

**Cost:** looking up the person reads the persons Mira knows already and the calendar list (as per-person schedules
do; one call to Home Assistant). The job itself costs what its task costs. Delivering is one `notify` call per
message, plus one read of the channels' states (as for every private delivery). Nothing grows with the number of
items beyond the length of the list in the message.

### When a step fails

| Fails | Who learns of it |
|---|---|
| The person in `to` is unknown, or has no channel | The run fails with `no person "<to>" with a channel of their own`; the log says `scheduled task failed`, the Schedule page shows the run as failed with that sentence. Nothing is sent to the house instead |
| The job fails (model, tools) | As for every schedule: the log line, the failed run on the page |
| The job finds nothing (`importance` 0 or `spoken` empty) | "Nothing to report" on the page; no message |
| Sending over the channels fails | The notifier logs `private delivery failed` with the error and tries again at its next tick, until the message expires (`expires_in_hours`, 12 hours by default); expired, it shows as expired on the Messages tab |

## Between the integration and the service

Nothing changes there: `to` is in the skill, the message stays in the service. The Messages tab shows the label
"privat" / "private" for such a message; an older integration only proxies the page.

## Steps

1. **Private messages** (built 2026-11-09): confidentiality `private` in the store and the notifier (sent at the
   tick and on presence, never spoken, not waiting, urgent ones not announced), the label on the Messages tab.
   PS-01 to PS-04.
2. **`to` in a schedule** (built 2026-11-09): reading and checking it, the person looked up, the message queued as
   private, the guide. PS-05 to PS-08.
