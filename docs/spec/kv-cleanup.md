# Cleaning up the key-value table – Spec

Status 2026-11-13: steps 1 and 2 built (not released): the dead keys, the description in [Database](../database.md) with its test, the log line, the five tables with their rows carried over; checked on the test Mira's own database (healthy, the page's last runs and the languages as before, one gone device forgotten). Scenarios: [kv-cleanup.feature](kv-cleanup.feature).

## Goal

The table `kv` holds what has no table of its own. Over time it became the place for everything: configuration beside
`mira.yaml`, lists of what was read or said as JSON in one value, families of keys with ids in the key, and keys no
code reads any more. Nobody sees their structure, one of them grows without end, and [Database](../database.md)
says `kv` does not grow. After this spec every key in `kv` is described, every list has a table, the one exception to
`mira.yaml` (the setup's values) is named and visible in the log, and nothing dead is left.

## What is there (2026-11-13)

Read in the code, the test HA's database and a backup of the maintainer's live database (2026-11-03).

| Group | Keys | What | Writes / reads | Grows |
|---|---|---|---|---|
| 1 Single values | `instance_id` | Mira's fixed id for Home Assistant | store | no |
| | `quiet_mode` | the switch "Ruhemodus" | integration's switch, notifier | no |
| | `found.started` | the found-devices noticing ran once (the backlog is not read out) | `internal/devices` | no |
| | `own_automations.taken_over` | the automations from before `own_automations` were taken over, once | store | no |
| 2 Configuration in the database | `setup.ha_url`, `setup.ha_token`, `setup.llm` | Home Assistant's address and token, the main model, as the integration sends them (`POST /v1/setup`) | `internal/setup`; laid over `mira.yaml` at start (`setup.Apply`) | no |
| | `setup.token`, `setup.paired`, `setup.code`, `setup.code_tries`, `setup.rejoin` | Mira's API token and the state of the pairing | `internal/setup`, backup | no |
| 3 Copies of Home Assistant | `ha_exposed` | the entities exposed to Assist, as the integration last sent them (live about 6 KB) | integration → `cmd/mira/exposed.go`, structure | with the house |
| | `ha_logins` | person → login name, for the calendars | integration → `cmd/mira/exposed.go` | with the persons |
| 4 Tables in disguise | `schedule:<skill>:<name>[:<person>]` | a schedule's last run (JSON) | `internal/skills`, the page | per schedule and person; a removed person's stay |
| | `language.area.<id>`, `language.person.<id>`, `language.last` | the language last spoken per room and person | `internal/language` | per room and person |
| | `found.said` | every found device ever said (JSON map) | `internal/devices` | **without end**: nothing is removed |
| | `inbox_mails` | hashes of the mails read, the last 100 per IMAP entry (JSON) | inbox (`cmd/mira/inbox.go`) | bounded |
| | `inbox_owners` | Paperless owner number → user name (JSON) | inbox | with the users |
| 5 Dead | `ui_config:notifier`, `ui_config:persona`, `ui_config:persons`, `ui_config:satellites`, `ui_config:watchers`, `ui_managed_by_ha` | the old settings overlay, gone since 0.14.0 | nothing | — |
| | `telegram.offset` | Telegram before it went through Home Assistant; only a constant and a test are left | nothing | — |
| | `feed_seen:<feed>` | an old feed reader | nothing | — |

## Decisions (2026-11-13)

| # | Question | Decision |
|---|---|---|
| 1 | What belongs in `kv` | Single values, and families of keys with a prefix, **when [Database](../database.md) describes each**: the key, who writes and reads it, what it holds, whether it grows. A key that is not described there is not written |
| 2 | Configuration in the database | Home Assistant's address and token and the main model **stay in `kv` and are laid over `mira.yaml`** at start, as today: a named exception to "`mira.yaml` is the one truth". Reason: the integration sets them, not the user (the setup, a new token the admin pastes, the integration's options for the model; Mira's page shows the main model as "from Home Assistant"); they belong to the pairing, which stays in `kv`; and a setup cannot fail on a `mira.yaml` that cannot be written. Changed the same day: first they were to move into `mira.yaml` |
| 3 | Copies of Home Assistant | Stay in `kv`: the integration sends them, and they must hold across a restart of the service until it sends again. Named as copies in [Database](../database.md) |
| 4 | Tables in disguise | Each gets a table: the schedules' last runs, the languages, the found devices said, the mails read, the Paperless owners |
| 5 | Dead keys | A migration deletes them; the constant `telegram.offset` and its test go |
| 6 | Order | This spec first, then the steps |
| 7 | `mira reset` | Unchanged: the setup's values go with the database, as today (decision 2) |
| 8 | A read-only `mira.yaml` | Unchanged: the setup works as today (decision 2) |
| 9 | A value of `mira.yaml` the setup's values cover | Said: a log line at start names each setting of `mira.yaml` that a value from the setup replaces (`ha.url`, `ha.token`, the main model's endpoint, model, key), never the secret value. Before, it was replaced silently |

## The new tables

New tables of the database; each replaces a group-4 key or family. Their names follow the [Glossary](../glossary.md).

| Table | Columns | Replaces | Kept |
|---|---|---|---|
| `schedule_runs` | `skill`, `schedule`, `person` (`''` for the house), `started_at`, `ended_at`, `status` (`done`, `nothing`, `failed`), `text`, `error`; key (`skill`, `schedule`, `person`) | `schedule:*` | one row per schedule and person, each run replacing the one before; a person's rows are deleted when the per-person schedule runs and the person is no longer among its persons (today they stay); a run without any person deletes nothing |
| `languages` | `scope` (`area`, `person`, `last`), `id` (`''` for `last`), `language`, `updated_at`; key (`scope`, `id`) | `language.*` | one row per room and person |
| `found_said` | `key` (`device:<id>`, `flow:<id>`), `name`, `said_at` | `found.said` | a row whose device or flow Home Assistant no longer has is deleted when the noticing starts (it reads both lists then anyway; assumption: checked in step 2 against `internal/devices`) |
| `inbox_read` | `entry` (the IMAP entry), `hash`, `read_at`; key (`entry`, `hash`) | `inbox_mails` | the last 100 per entry, as today |
| `inbox_owners` | `owner` (Paperless's number), `user` | `inbox_owners` | one row per owner |

## What it does, step by step

### A. A migration moves the lists (step 2)

One migration per table, in SQL: `CREATE TABLE`, the rows copied from the key or family (`json_each` for the JSON
values; SQLite's JSON functions are in the driver), the keys deleted, in one transaction as every migration. Nothing is
lost: a mail read stays read, a device said stays said.

### B. The setup's values over `mira.yaml` (step 1)

`setup.Apply` stays as it is (decision 2). New: before it lays a value over the config, it compares it with what
`mira.yaml` has; where `mira.yaml` has a different value, a log line at start says so: `setup covers mira.yaml`,
with the setting's name (decision 9). Nothing changes for the setup, `mira reset`, the backup or the page.

### C. Dead keys (step 1)

A migration deletes `ui_config:*`, `ui_managed_by_ha`, `telegram.offset` and `feed_seen:*`. The constant
`store.KeyTelegramOffset` and its test go.

### D. The description (step 1)

[Database](../database.md) gets the rule (decision 1) and a table of every key and family left in `kv`: groups 1 to 3,
the pairing and the setup's values with the reason for their exception. Its sentence "`kv` does not grow" is corrected.

## What it costs

- The migrations run once, at the first start of the new version: a few hundred rows at most (the maintainer's
  house: the largest is `ha_exposed`, which stays). Milliseconds.
- At run time: nothing more than today; a read of a row instead of a key and a JSON value.

## Errors

| Step | Fails | Who learns of it |
|---|---|---|
| A, C | A migration fails (a value that is not JSON, a write error) | As every migration: the transaction is rolled back, the service does not start, the log names the migration and the error; for a value that does not parse, its key and its start |

## Checks

No device transitions; the steps are checked by tests on a copy of a migrated database
(`storetest`), and the test Mira started once on a copy of its migrated database: healthy, the page's last runs and the languages as before.

## Steps

1. **Dead keys, the description and the log line:** the migration of section C, the constant and its test,
   [Database](../database.md) with the rule, every key and the exception of decision 2; the log line of section B.
2. **The tables:** the five tables, their migrations with the rows carried over, the code reading and writing them;
   the found devices' pruning.
