# Database migrations

Status 2026-10-26: written down after the fact. The mechanism is built and has been in use since the first
release; it was never specified. This document describes what is there, without changing it; the gaps at the end
are open, not decided.

## What is migrated

Mira keeps her state in one SQLite file (`store.path`, by default `mira.db` in the data folder): sessions and their
messages, memory and profiles, messages to people, threads, assessments, agent runs, key-value settings. Only this
file is migrated. `mira.yaml` is not (see the gaps).

## How it works

`internal/store/migrations.go`:

- **Steps in code.** A list of named steps (`0001_sessions` … `0021_memory_trigram`), each one or more SQL
  statements, compiled into the binary. No migration files beside it, nothing to ship separately.
- **Forward only.** A shipped step is never edited; a change is a new step at the end. There are no steps back.
- **At every start.** `store.Open` creates the table `schema_migrations` if it is missing, reads which steps are
  recorded there, and applies every missing one in order, before anything else uses the database.
- **One transaction per step.** A step and its line in `schema_migrations` (name, time applied) are written
  together: a step is applied completely or not at all.
- **A failing step stops the start.** The step is rolled back, the agent does not start and logs the step's name
  and the error. The steps before it stay applied.
- **Tested:** opening a database twice applies every step once (`store_test.go`); every other test starts with a
  database migrated from empty.

The same mechanism as Prisma's or goose's migrations, without a library and without steps back.

## What the steps did with data

| Steps | What changed | Data |
|---|---|---|
| 0001–0007 | the first tables: sessions, memory, notices, key-value, confirmations, reminders | new |
| 0008–0009 | tasks of the agent's own | new |
| 0010 | assessments of tasks | **existing tasks deleted** (test data, docs/spec/aufgaben.md) |
| 0011 | threads ("Vorgänge") | **table tasks dropped**: tasks became Home Assistant automations |
| 0012 | memory per owner, profiles per owner | kept: the house's profile copied into the new table, then the old one dropped |
| 0013 | notices become messages (receiver, sender) | kept: tables and columns renamed, "household" becomes "house" |
| 0014 | reminders become scheduled messages | pending reminders moved into messages, the rest dropped with the table |
| 0015–0020 | columns added (origin, area, agent runs and their usage, session skills, thread area) | kept |
| 0021 | the memory's search index on trigrams | index rebuilt from the facts, facts kept |

Two steps discarded data on purpose (0010, 0011): there were no users then (docs/spec/nachrichten.md: "No data
migration (everything is test)"). Since 0012 the steps carry what is there over.

## What happens when

- **Update** (a newer agent on an older database): the missing steps are applied at start. Seen on 2026-10-26 with
  the live agent from 0.7.0 to 0.9.0.
- **Going back** (an older agent on a newer database): the older agent finds steps in `schema_migrations` it does
  not know and ignores them; it runs against tables and columns it did not expect. Nothing stops it or warns.
- **A failed step:** the agent does not start; the database stays at the last good step.
- **Backup:** none by the agent. Whoever runs it backs up the data folder (as done by hand before the live update,
  `~/mira-live/backup-0.7.0`); an add-on would be in Home Assistant's backups.

## Gaps (open, not decided)

1. **A copy before migrating:** before applying missing steps, the agent copies the database (`VACUUM INTO
   mira.db.bak-<version>`), so an update can be undone without a manual backup.
2. **Refusing a newer database:** an agent that finds steps it does not know stops with a clear message instead of
   running against an unexpected schema.
3. **Carrying data over as the rule:** since 0.9.0 is out through HACS there are users; a step must convert what
   is there, not discard it. Today this is practice since 0012, not a written rule.
4. **An upgrade test:** a database as a released version left it, kept as a test file, migrated to the current
   steps and read. A broken step shows before the release, not at a user's start.
5. **`mira.yaml`:** a renamed or removed key makes the strict check refuse an older file. A rule for that is
   missing (the old key accepted for a release, with a warning, for example).
