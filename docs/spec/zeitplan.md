# Schedules on Mira's page – spec

Status 2026-11-01: steps 1 and 2 built, not released. Scenarios: [zeitplan.feature](zeitplan.feature). Builds on
[Skills and agents at a glance](uebersicht.md) (the page, read only so far) and
[Editing models, agents and skills](bearbeiten.md) (a skill's entry in `mira.yaml`).

## Goal

Mira runs jobs at fixed times with her own scheduler: the schedules of skills (the morning briefing at 05:30, the
weather warnings every hour, the waste collection at 18:00) and her own (rebuilding the profiles at night). That is
fine: moving each of them into a Home Assistant automation that only calls Mira back would move the clock, not
take work off Mira, and make it harder to follow.

What is missing: one cannot see them in one place, and cannot change them. The time of a bundled skill's schedule
sits in its `SKILL.md`, which the page does not write (an update would overwrite it), although the schedule is
metadata like the skill's settings, which are changed in `mira.yaml`. Whether a job ran, and what came of it, is in
the log only.

## Decided (2026-11-01)

| # | Question | Decision |
|---|---|---|
| 1 | Who runs the schedules | Mira's own scheduler stays; no Home Assistant automation per schedule |
| 2 | How a time is changed | Time of day and weekdays on the page; a cron expression that cannot be shown so is shown in words and changed only in `mira.yaml` |
| 3 | Where they are shown | A page of its own, "Schedules" ("Zeitplan"), one folding entry per schedule, like the skills |
| 4 | Run now, last run | Part of this spec |
| 5 | A per-person schedule | One time for all persons |
| 6 | Mira's own schedules | Listed too; their time can be changed, but they cannot be switched off |
| 7 | Where a change is kept | The skill's entry in `mira.yaml` (the one truth), by the schedule's name; without an entry the skill's own time holds |
| 8 | Not on the page | Tasks (Home Assistant automations, edited there) and one-off reminders: they are not Mira's schedules |

## What it does

### A schedule has a name

- Each schedule in a `SKILL.md` gets a `name` (a key, fixed, unique within the skill: `morgen`, `abend`,
  `persoenlich`) and a `description` (a few words for the page: "Morgen-Briefing fürs Haus"). Mira's own skills get
  both.
- A schedule without a name (a user's own skill written before) runs as before and is listed, but cannot be changed
  or switched off on the page; its entry says: "Give the schedule a name in SKILL.md to change it." No name is made
  up from its position.
- Two schedules of a skill with the same name: the skill does not start, with the reason (as any broken skill).

### Changing it in `mira.yaml`

Under the skill's entry, beside its settings:

```yaml
skills:
  briefing:
    kalender: Familie
    schedules:
      morgen:
        cron: "45 6 * * 1-5"   # instead of the skill's 30 5 * * *
      abend:
        enabled: false         # off; the time is kept
```

- `cron` replaces the skill's time; `enabled: false` switches the schedule off and keeps its time. Without an entry
  the skill's own schedule holds, so a new version of the skill brings its new default time.
- A name that the skill has no schedule of: the entry is refused with the reason (as an unknown setting).
- Mira's own: `memory.profile_schedule`, as today.

### The page "Schedules"

One entry per schedule, grouped by skill, Mira's own last ("Mira"). Folded:

- the description, the skill's name, "for each person" where it runs per person;
- **when** in words: "Mo–Fr 06:45", "täglich 18:00", "stündlich";
- the **last run** as a sign: done, failed, not yet; with its time;
- a **switch** on/off. Mira's own have none. A schedule of a skill that is off is greyed with "Skill is off" and
  its switch does nothing.

Unfolded:

- **Last run:** started, how long it took; the result: the message it queued (the spoken text), "nothing to
  report" (the job found nothing worth saying), or the error. Per person for a per-person schedule (one line each).
  For Mira's own: how many profiles were rebuilt.
- **Next run:** date and time; none while off.
- **Time:** time of day and the weekdays (all, Mo–Fr, Sa–So, or single days), with **Save**. A cron expression that
  this cannot show (every hour, every 15 minutes) is shown in words and has no fields here.
- **Run now:** runs the job at once, in the background; the entry shows "running" until it ends, then the new last
  run. A message it queues is the same as the scheduled run's of that day: running twice the same day updates it,
  it does not add a second one.

Only admins see the page (it changes `mira.yaml`).

### When a change holds

At once, without a restart: after a save Mira applies the new `mira.yaml` to the running scheduler, which takes the
next run of every schedule anew. A skill switched on or off still needs the restart, as before (bearbeiten.md).

### Run now and the scheduler

- The scheduler runs one job after the other, never two at once (as today). Run now goes into the same line: while
  another job runs, it waits; the entry says "waiting".
- Run now on a schedule that is running or waiting is refused: "It is running."
- Run now on a schedule that is off runs it all the same (to try it out before switching it on).

### The last run is kept

Per schedule (and per person) the last run only: started, ended, result (done, nothing to report, failed), the
spoken text or the error. It replaces the one before, so it does not grow with time. Kept in Mira's key-value table
(`kv`), key `schedule:<skill>:<name>[:<person>]`, `schedule:mira:profiles` for Mira's own.

## The interface

Between the page and the service only (the integration is not involved):

- `GET /v1/ui/schedules`: the list, as above: skill, name, description, per person, cron, in words, enabled, can be
  switched off, can be changed (a name, a cron the fields can show), the time and weekdays, next run, last run(s),
  running/waiting.
- `PUT /v1/ui/schedules/{skill}/{name}`: `{enabled?, cron?}`; writes `mira.yaml`, applies it.
  `{skill}` is `mira` for Mira's own.
- `POST /v1/ui/schedules/{skill}/{name}/run`: run now; answers at once (202) or "running" (409).

## Costs

- **The list:** reads `mira.yaml` as loaded and one `kv` row per schedule and person; no call to Home Assistant or
  to a model. Grows with the number of schedules times persons (today 5 schedules, two of them per person).
- **Save:** writes `mira.yaml` once and recomputes the next runs; nothing else.
- **Run now:** what the scheduled run costs: one background agent run of the skill (with its tool calls to Home
  Assistant and the web as the job's task says), per person one each for a per-person schedule; for Mira's own, one
  model call per person with facts. Nothing waits for it: the page asks again for the list after it.
- **Each run** writes one `kv` row per schedule (and person).

## Steps

1. **Names and the change in `mira.yaml`.** `name` and `description` in the schedules of Mira's skills; the entry
   `schedules` under a skill (`cron`, `enabled`), checked on load; the scheduler applies a new `mira.yaml` at once.
   Scenarios ZP-01 to ZP-06.
2. **The last run.** Each run (scheduled or run now) keeps its result; Mira's own too. Scenarios ZP-10 to ZP-12.
3. **The page.** `GET`, `PUT`, `POST …/run`; the page "Schedules" with its entries. Scenarios ZP-20 to ZP-26.
