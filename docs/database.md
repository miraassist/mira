# Mira's database

Mira keeps her state in one SQLite file (`store.path`, by default `mira.db` in the data folder), written only by the
Mira service through `internal/store`. The schema grows by migrations (`internal/store/migrations.go`, applied at
start, listed in `schema_migrations`).

## Overview

Only `chat_messages` → `sessions` is a foreign key in SQLite; the other relations are ids kept as text.

```mermaid
erDiagram
    sessions ||--o{ chat_messages : "history"
    sessions ||--o{ confirmations : "waits for yes"
    threads |o--o{ sessions : "a turn of"
    threads |o--o{ messages : "about"
    sessions |o--o{ messages : "answered in"
    threads |o--o{ announcements : "said for"
    announcements }o--o{ messages : "said (message_ids)"
    assessments |o--o| messages : "led to"
    agent_runs ||--o{ agent_run_steps : "steps"
    agent_runs |o--o{ agent_runs : "parent_id"
    memory_profiles ||--o{ memory_facts : "owner"

    sessions {
        text key PK "channel:conversation"
        text channel "voice, telegram"
        text device_id
        text area_id
        text thread_id
        text skills "opened skills, JSON"
        text expires_at
    }
    chat_messages {
        int id PK
        text session_key FK
        int seq
        text role
        text content
        text tool_calls
    }
    confirmations {
        text id PK
        text session_key
        text tool_name
        text arguments
        text expires_at
    }
    threads {
        text id PK
        text target "the skill"
        text state
        text person
        text area
        text device
        text fields "JSON"
        text log "JSON"
        text expires_at
    }
    messages {
        text id PK
        text source
        text dedupe_key
        int importance
        text receiver "household or a person"
        text spoken
        text state
        text thread_id
        text session_key
        text deliver_at
        text expires_at
        text origin_area
        text origin_device
    }
    announcements {
        text id PK
        text device_id
        text area_id
        text channel
        text message_ids "JSON"
        text thread_id
        text created_at
    }
    assessments {
        text id PK
        text task "the automation"
        text kind
        int importance
        text message_id
        int dry_run
        text created_at
    }
    agent_runs {
        text call_id PK
        text parent_id
        text agent
        text status
        text model
        int prompt_tokens
        text ended_at
    }
    agent_run_steps {
        text call_id PK
        int seq PK
        text kind
        text body
    }
    memory_facts {
        text id PK
        text text
        text tags
        text owner "house or a person"
        text author
    }
    memory_profiles {
        text owner PK
        text text
    }
    kv {
        text key PK
        text value
        text updated_at
    }
    schedule_runs {
        text skill PK
        text schedule PK
        text person PK "'' for the house"
        text started_at
        text ended_at
        text status
    }
    languages {
        text scope PK "area, person, last"
        text id PK
        text language
    }
    found_said {
        text key PK "device:<id>, flow:<handler>:<unique id>"
        text name
        text said_at
    }
    inbox_read {
        text entry PK "the IMAP entry"
        text hash PK
        text read_at
    }
    inbox_owners {
        text owner PK "Paperless's number"
        text user
    }
    watch_declined {
        text entity_id PK "the device's sensor"
        text blueprint PK "its name: daily_routine"
        text declined_at
    }
```

## The tables

| Table | What it holds | Written by | Read by |
|---|---|---|---|
| `sessions` | One per conversation (a satellite's turn, a Telegram chat): its device, area, language, open thread and opened skills | the channels (`haconv`, Telegram) | the conversation engine |
| `chat_messages` | The history of a session, turn by turn, with tool calls | the conversation engine; history compaction drops the oldest | the conversation engine |
| `confirmations` | A tool call that waits for the user's yes | the tool policy | the next turn of the session |
| `threads` | A thread ("Vorgang"): its target, fields, log and state ([Threads](spec/threads.md)) | `internal/threads` | threads, the notifier, the page "Plans" |
| `messages` | What Mira has to say: reminders, task and schedule results, messages left; its state from scheduled to acked ([Messages](spec/messages.md)) | tasks, schedules, reminders, threads, voicemail | the notifier, the page "Messages" |
| `announcements` | What was said where and when, for a follow-up ("what did you say?") | the notifier | the conversation engine |
| `assessments` | The model's judgements of a task, with what came of them ([Tasks](spec/tasks.md)) | `internal/tasks` | the page "Tasks" |
| `agent_runs`, `agent_run_steps` | Runs of Mira's helpers with their steps and cost ([Agents](spec/agents.md)) | the agent runtime | the page "Agents", a run taken up after a restart |
| `memory_facts` (+ `memory_facts_fts`) | What Mira remembers, per owner (the house or a person), with a full-text index | the memory tools, the page "Memory" | the memory tools, the profile rebuild |
| `memory_profiles` | The profile per owner, built from the facts at night | the profile rebuild | every prompt |
| `schedule_runs` | A schedule's last run, per person for a per-person one ([Schedules](spec/schedules.md)) | the skills' runner | the page "Schedule" |
| `languages` | The language last spoken per room and person, and at all ([Language](spec/language.md)) | the language of a turn | every turn without a language of its own |
| `found_said` | The found devices and discoveries already said, each once ([Found devices](spec/found-devices.md)) | the found-devices noticing | the noticing |
| `inbox_read` | The mails the inbox read, the last 100 per IMAP entry ([Inbox](spec/inbox.md)) | the inbox | the inbox |
| `inbox_owners` | Paperless owner numbers learned from the webhooks, with their user names ([Inbox](spec/inbox.md)) | the inbox | the inbox |
| `watch_declined` | The device watches the user declined, per sensor and blueprint: not offered again ([Device watch](spec/device-watch.md), decision 21) | `watch_decline`, the user's no to a watch offer; a watch saved by voice deletes its rows | the candidates on request (`watch_candidates`) |
| `kv` | Single values and described families of keys (section "The key-value table") | several | several |
| `schema_migrations` | The migrations applied | the store at start | the store at start |

## What is kept, and for how long

Decided 2026-10-30 (code review); built in `internal/store/prune.go` and `PurgeExpiredSessions`. Every 10 minutes
Mira removes what has long been finished. Nothing open is ever removed.

| Table | Removed | After |
|---|---|---|
| `messages` | not pending or scheduled any more (delivered, acked, expired, cancelled, failed); a pending one expires by its `expires_at` first | 90 days without a change |
| `assessments` | all | 90 days |
| `announcements` | all | 7 days |
| `threads` | done, cancelled or expired | 30 days without a change |
| `sessions` with their `chat_messages` | past their time: a voice session after `sessions.voice_ttl` (5 minutes) without a turn; a Telegram session has none | at once |
| `confirmations` | with their session, or when the next turn takes or drops them | at once |
| `agent_runs` with their steps | ended | 60 minutes |
| `memory_facts`, `memory_profiles` | never by Mira: what she remembers is removed by the user (page "Memory", "forget") | – |
| `schedule_runs` | a person's rows, when their per-person schedule runs and they are no longer among its persons | at once |
| `languages` | never; one row per room and person | – |
| `found_said` | a device or discovery Home Assistant no longer has, when the noticing starts | at once |
| `inbox_read` | the oldest of an IMAP entry beyond its last 100 | at once |
| `inbox_owners` | never; one row per Paperless owner | – |
| `watch_declined` | a row, when the watch is made by voice all the same (step 8 of [Device watch](spec/device-watch.md)) | at once |
| `kv` | never; each key is overwritten; the keys and what they grow with: section "The key-value table" | – |

## The key-value table

Decided 2026-11-13 ([Cleaning up the key-value table](spec/kv-cleanup.md)): `kv` holds single values, and families of
keys with a prefix **when this table describes them**; a key that is not here is not written. A list with parts in its
key or a JSON list in its value gets a table of its own. A test (`docs`, KV-02) checks that every key the code
writes is here.

| Key | What | Written by | Read by | Grows |
|---|---|---|---|---|
| `instance_id` | Mira's fixed id: Home Assistant knows the Mira service by it, whatever its address | the store, once | the mDNS announcement and the API | no |
| `quiet_mode` | The switch "Ruhemodus" | the integration's switch (`PUT /v1/quiet_mode`) | the notifier, the API | no |
| `found.started` | The found-devices noticing ran once: what Home Assistant had found before is not read out | `internal/devices` | `internal/devices` | no |
| `own_automations.taken_over` | The automations labelled "Mira" from before the table `own_automations` were taken over, once | the store | the store | no |
| `ha_exposed` | **A copy of Home Assistant:** the entities exposed to Assist, as the integration last sent them; kept so it holds across a restart until the integration sends again | the integration, through the API | the house's context | with the house |
| `ha_logins` | **A copy of Home Assistant:** person → login name, as the integration last sent them, for the calendars | the integration | calendars | with the persons |
| `setup.token` | Mira's API token when `mira.yaml` has none, made at the first start | `internal/setup` | the API | no |
| `setup.paired`, `setup.code`, `setup.code_tries`, `setup.rejoin` | The state of the pairing with Home Assistant ([Setup](spec/setup.md)) | `internal/setup` | `internal/setup`; a backup's restore keeps the live ones | no |
| `setup.ha_url`, `setup.ha_token`, `setup.llm` | Home Assistant's address and token and the main model, as the integration sent them. **The one exception to "`mira.yaml` is the one truth":** they are laid over `mira.yaml` at start, because the integration sets them, not the user, they belong to the pairing, and a setup must not fail on a `mira.yaml` that cannot be written. Where they cover a different value of `mira.yaml`, the start's log says which setting (`setup covers mira.yaml`) | `internal/setup` (`/v1/setup`) | the start | no |

A Telegram session keeps its session row; its history does not grow without end, since compaction folds old turns
into a summary and drops them ([Context](spec/context.md), section 5).

SQLite reuses the space of removed rows: the file does not shrink, but stays near its largest size.
