# Scheduled jobs – spec

Status 2026-11-20: steps 1 to 3 built (not released). Scenarios: [scheduled-jobs.feature](scheduled-jobs.feature).

## Goal

What a skill does at a time is fixed today by its author: the skill's `SKILL.md` declares its schedules, each with a
time and a task ([Schedules on Mira's page](schedules.md)). The user can change a time and switch a schedule off on
the page "Zeitplan", nothing more. Whether a skill has one schedule or two is the author's choice and makes no sense
to the user: `x-recherche` has two (morning and evening, each with its own window), `youtube` one. Where the
schedules are written, the user does not find: in the skill's file.

A **scheduled job** brings together what belongs together: what runs, with which parameters, when, and whether it is
on. The user makes and manages them on the skill; the skill only offers what can be scheduled, as **job templates**.

## Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | Where scheduled jobs are kept | In `mira.yaml` under the skill (1A): the one truth, the page writes it. Not chosen: Mira's database (B) |
| 2 | What a skill brings | Its job templates and first scheduled jobs made from them, which the user changes or deletes (2B). Not chosen: only templates, nothing running until the user makes a job (A) |
| 3 | Parameters such as topics and recipient | Belong to the scheduled job (3A); the skill's settings keep only what holds for all its jobs (a login, a calendar of the house) |
| 4 | Skills that have schedules today | All are changed to job templates (4A): `abfall`, `briefing`, `warnungen`, `youtube`, and the user's own (`x-recherche`) |
| 5 | The terms | "scheduled job" / „geplanter Auftrag“ and "job template" / „Auftragsvorlage“ (glossary). Not „Aufgabe“: that is a Home Assistant automation |
| 6 | Where they are managed | On the skill's entry in "Fähigkeiten": make, change, switch on and off, delete. The page "Zeitplan" stays: it lists the scheduled jobs by name, and there their time is changed and they are run now |
| 7 | More than one time | One time per job (1B); a second time is a second job, which may have other parameters (the evening with other topics) |
| 8 | When the first jobs are written | When the skill is switched on on the page (2A): its `first` jobs go into `mira.yaml` at once, so the file shows what runs |
| 9 | A first job whose required parameter has no value | It is not made (3B); the skill's section says that nothing is scheduled yet and offers **Neu** with the template |
| 10 | The window of a job that runs more often | The task is told when this job last ran (4A), `{{since}}`, so that a second time does not bring the same items again |
| 11 | Mira's own schedule | Stays as it is (5A): rebuilding the profiles at night is no skill's job |
| 12 | What exists today | Stays working (6A, below, Compatibility): `schedules` in a `SKILL.md` is read as before, `schedules` in `mira.yaml` is moved to `jobs` the next time the page writes the skill's entry. Deprecated later, with its own decision; not now |

## The concept it hangs on

- **The skills' schedules** ([Schedules on Mira's page](schedules.md)): the scheduler, the background run of a skill
  with its tools, the result as a message (`importance`, `spoken`, `details`), `per_person`, `to`, `urgent`, the last
  run in `kv`, run now. All of that stays; what changes is where a schedule comes from: from a scheduled job in
  `mira.yaml` instead of the skill's file.
- **A skill's entry in `mira.yaml`** written by the page ([Editing](editing.md)), as for settings and the time of a
  schedule today (`skills.<skill>.schedules.<name>`, which this replaces).

## In the skill: job templates

```yaml
# skills/youtube/SKILL.md
metadata:
  mira:
    jobs:
      liste:                                   # the template's key
        description: Neue Videos auf YouTube zu Themen
        parameters:
          themen:     { description: 'Themen, mit Komma, z. B. "Home Assistant Voice, lokale KI"', required: true }
          empfaenger: { description: Wer die Liste bekommt (Name der Person). Leer heißt das ganze Haus. }
        to: "{{empfaenger}}"
        task: >-
          Stell die Liste neuer YouTube-Videos zusammen, wie es die Anleitung unter „Tägliche Liste“ sagt.
          Die Themen: {{themen}}
        first:                                 # decision 2: the first scheduled job, made when the skill is switched on
          - { name: morgens, cron: "0 7 * * *", parameters: { empfaenger: "" } }
```

- **A template** has a `description` (for the page), `parameters` (as a skill's settings: `description`,
  `required`, `secret`), the `task` with `{{parameter}}` and the skill's settings, and as today `per_person`, `to`,
  `urgent`.
- **`first`**: scheduled jobs the skill brings (decision 2), with a name, a time and parameters; a required parameter
  without a value makes the first job wait until the user gives it (open point 3).
- **`{{since}}`** in a task: when this scheduled job last ran, as a time (`2026-11-20 07:00`, in Mira's time zone);
  at its first run, or after its last run was lost, the template's `window` before now (`window: 24h`, default
  `24h`). The template's prose says what it means ("alles seit {{since}}"); a tool that takes a time (`youtube__suche`
  with `seit`) gets it from there (decision 10).
- The shipped skills are changed to `jobs` in the same step (decision 4). `schedules` in a `SKILL.md` keeps working
  (decision 12, Compatibility).

## In `mira.yaml`: scheduled jobs

```yaml
skills:
  youtube:
    client_id: …                               # settings of the skill
    jobs:
      morgens:                                 # the job's name, chosen by the user
        template: liste
        cron: "0 7 * * *"
        enabled: true
        themen: Home Assistant voice satellite, Echo Dot Home Assistant
        empfaenger: stefan
      abends:
        template: liste
        cron: "0 18 * * *"
        themen: lokale KI
```

- **`jobs` missing** under a skill (a skill switched on by hand in `mira.yaml`, or before this change): the skill's
  `first` jobs hold, as the skill's own time holds today. **`jobs` present** (also empty, `jobs: {}`): only these;
  deleting the last one leaves `jobs: {}`. Switching a skill on on the page writes its `first` jobs (decision 8);
  one whose required parameter has no value is left out (decision 9).
- **A name** is a key: small letters, digits and `-`, unique within the skill; it is the schedule's name on the page
  "Zeitplan" and in `kv` (`schedule:<skill>:<name>`).
- **`skills.<skill>.schedules`** (a changed time, written by the page since 0.15.0) is read as a change of the job
  of that name, and moved to `jobs` when the page next writes the entry (Compatibility).

## Compatibility (decision 12)

Since 0.15.0 users may have three things: their own skills with `schedules` in `SKILL.md` (the guide describes it),
changed times under `skills.<skill>.schedules.<name>` in `mira.yaml`, and the last runs in `kv`. All keep working,
without a restart doing anything and without the user doing anything:

- **`schedules` in a `SKILL.md`** is read as before: each schedule becomes a job template of its name with no
  parameters of its own (its `{{setting}}` filled from the skill's settings, as today) and a first job of the same
  name with its `cron`. Mira cannot change these files (they are the user's, or a skill source's in memory). A skill
  with both `schedules` and `jobs` does not start, with the reason.
- **`schedules.<name>` in `mira.yaml`**: its `cron` and `enabled` change the job `<name>`. When the page next writes
  the skill's entry (any change of the skill, a job or a time), it writes the jobs as `jobs` and drops `schedules`;
  the log says so once (`schedules moved to jobs`). Nothing else rewrites the file.
- **A name the skill no longer has** in `schedules` (an old entry): as today, a problem of the skill's entry; when
  the page moves the entry, it drops that name and the log names it.
- **The last run** stays: the key `schedule:<skill>:<name>` is the job's name.
- **Deprecated later**: `schedules` in `SKILL.md` and `mira.yaml` stay readable until a decision of their own; the
  guide describes `jobs` and says that `schedules` still works.

For `x-recherche` that means: it keeps running with its jobs "morgen" and "abend", managed on the skill like any;
to give the evening other topics, its `SKILL.md` is changed to `jobs` by its owner.

## On the page

On the skill's entry in "Fähigkeiten", a section **Geplante Aufträge**:

- one line per scheduled job: name, template, when in words, on/off, the last run (as on "Zeitplan");
- **Neu**: choose a template (when the skill has more than one), a name, its parameters (a form as for settings),
  time of day and weekdays; **Speichern**;
- per job: change the parameters and the time, switch on and off, **Löschen** (with the page's dialog), **Jetzt
  ausführen**.
- A job made, changed or deleted here holds after Mira's restart, which the page offers as for a changed setting; a
  time changed on "Zeitplan" holds at once, as today.

The page "Zeitplan" lists every scheduled job as it lists schedules today, under its skill and by its name, with
time, switch and "run now", and a link to the skill's section.

## Errors

| What is wrong | Who learns of it | What happens |
|---|---|---|
| A job names a template the skill does not have | the skill's entry and Repairs; the log at load | the job does not run; the other jobs and the skill do |
| A required parameter has no value | the job's line on the page ("Themen fehlt") | the job does not run until it has one |
| A time that is no cron | refused on saving; at load a problem of the job | as above |
| A job's name twice | refused on saving | — |
| `schedules` and `jobs` both in a `SKILL.md` | the skill's problem with the reason | the skill does not start |
| `schedules.<name>` in `mira.yaml` for a name the skill does not have | the skill's problem, as today | the entry is refused; moving it drops the name with a log line |
| Moving `schedules` to `jobs` fails (the file cannot be written) | the page's error, as for every save | `mira.yaml` stays as it was; the old entry keeps working |
| A run fails | as today: the last run, the log | as today |

## Costs

As today per run. Loading reads the jobs from `mira.yaml`; nothing grows but with the number of jobs.

## Steps

1. **Templates and jobs in the service** (`internal/skills`): `jobs` in `SKILL.md` and in `mira.yaml`, the schedules
   made from the jobs, `{{since}}` filled by the runner, the compatibility of `schedules` (both places), a wrong job
   as a problem of the skill that leaves it running (a repair). Scenarios SJ-01 to SJ-08.
2. **The page**: the section "Geplante Aufträge" on the skill (`GET` with the catalog, `PUT` and `DELETE
   /v1/ui/skills/{skill}/jobs/{name}`), switching on writes the first jobs, every write of the skill's entry moves
   `schedules` to `jobs`, "Zeitplan" writes a time under `jobs`. Scenarios SJ-10 to SJ-14.
3. **The shipped skills on `jobs`**: `abfall`, `briefing` (its settings stay settings), `warnungen`, `youtube` (its
   topics and recipient become the job's parameters, its search takes `seit`); the guide (de, en). Scenarios SJ-20
   and SJ-21.
