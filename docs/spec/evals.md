# Evals – spec

Status 2026-10-17: built in 0.6.19 (runner `go run ./cmd/testha eval`, 12 scenarios, setups Gemini Flash and Qwen local).

## What for

"Does Mira run with Qwen?", "With Kimi?", "With the new Gemini?" That can only be answered by trying it out. Models and providers change constantly. An **eval set** plays fixed scenarios end to end, with a real service, real model and real test HA, only without voice (no speech recognition, no speech output), and says per model setup what works.

A run costs money and time. It therefore does not run in CI, but on call: before a release, with a new model, with a new setup.

## When a scenario has passed

Three levels; only the first two decide on "passed".

| Level | What | How checked | decides |
|---|---|---|---|
| 1. Result | state **afterwards** in HA (appointment with title, day, time, calendar; appointment gone; list item with due date; alarm on the device) and at Mira (fact in the right memory, message created, thread finished) | hard, queried directly, independent of wording | yes |
| 2. Behaviour | what happened on the way, from the trace: tool calls, threads, actions. **Must** (asking before deleting; exactly one question back with two lists), **must not** (delete without yes; name an appointment no tool delivered; switch something locked), **limits** (at most N tool rounds, no unnecessary question back) | hard | yes |
| 3. Answer | what was said: contains what it should (day, time); with voice no Markdown, no entity IDs; right language | simple rules (search patterns) | no, is reported |

A judge model for level 3 does not exist for now.

**Variance:** Models do not always answer the same. Each scenario runs **3 times**.
- **Core scenarios** pass at 3 of 3.
- The others pass at 2 of 3.

The result per scenario and setup is the rate, not a single yes or no.

## Time

If Mira is too slow, she is not usable. Time therefore belongs in the result, like this:

- **Hard, co-decides:** the limit from which it is broken for the user. With voice that is the integration's answer timeout (default 15 s): After that the satellite says an error message. With Telegram it is 60 s. A step above it counts as **not passed**, no matter what is in HA afterwards. The limits come from the configuration, not from the scenario.
- **Soft, is reported:** per step the median and maximum of the response time over the 3 runs. With voice rated as **good** (up to 3 s), **usable** (up to 8 s) or **slow** (above).
- **Split up:** how much of it went to the model (rounds, tokens) and how much to tools and HA, from the service's log.
- **Background separately:** What runs in the background (the draft of a task) is measured as time until the proposal and does not count toward the response time.
- **Exception:** A separate limit in the scenario (`max_seconds` on a step) exists only rarely, e.g. for a web search.

**Warmup:** The runner waits until the service's warmup is finished, and then sends one turn per channel that does not count. So the fixed beginning of the prompt is in the cache with a local model. Only after that is measured. All 3 runs count; if the first is slower, one should see that.

## Scenarios

One YAML file per scenario in `evals/scenarios/`:

```yaml
name: termin-absagen
core: true                      # core scenario: 3 of 3
setup:
  events:                       # relative to today
    - { calendar: calendar.familie, summary: Zahnarzt, day: +3, start: "15:00", end: "16:00" }
steps:
  - say: "Sag meinen Zahnarzttermin ab"
    at: satellite.wohnzimmer      # or: person: anna, channel: telegram
    expect:
      behaviour:
        must: [thread_start:termin-absagen, proposal]
        must_not: [calendar.delete]
      answer:
        contains: [Zahnarzt]
  - say: "Ja"
    expect:
      result:
        calendar_empty: { calendar: calendar.familie, day: +3 }
      behaviour:
        must: [calendar.delete]
cleanup: restore                # test HA back to its initial state
```

- **Preparation**: appointments and list items relative to today, memory facts, who is at home (the sim switches of the test HA), waiting messages.
- **Steps**: sentence, place (satellite via its device ID) or person and channel. Several steps are one conversation.
- **Expectations** per step and level. Checks on the result exist as fixed building blocks (`event_exists`, `calendar_empty`, `todo_item`, `alarm_set`, `fact_saved`, `message_for` …). So a scenario stays readable and need not query anything itself.
- **Cleanup**: After each run the test HA is as before. Alarms on the Show are always deleted.

## Model setups

A setup is a small file in `evals/setups/` with `models` and, if needed, the agents' models under `agents`, as in
`mira.yaml`, without keys. It replaces the test config's models, and the test config's entries that only name a
model.

**Secrets**: The runner reads only `testenv/.env`, never the `.env` of a real installation. The eval service gets only a few harmless base variables (`PATH`, `HOME`, `LANG`, `TZ`) and exactly the variables its config requires as `${…}`. If one is missing in `testenv/.env`, the runner aborts with its name; a skill whose
settings need a missing variable is left out of the run instead, and the report names it with the variable (decided
2026-11-05: the X skill's secret lives in the test Mira's secrets file, written by the page, and stopped the whole run
although no scenario uses X). Model keys for evals (e.g. `GEMINI_API_TOKEN`) therefore belong in `testenv/.env`. Examples: `gemini-flash.yaml`, `qwen-lokal.yaml`, `kimi.yaml`. A setup describes what really runs in the house, mixed too: voice local, difficult tasks in the cloud.

## Run

```
go run ./cmd/testha eval --setup evals/setups/gemini-flash.yaml [--scenario termin-absagen] [--runs 3]
```

- `--scenario` with a report: the scenarios measured again replace theirs in the setup's last report, the others
  stay; the report notes which were measured again and when (decided 2026-11-05). `--keep` keeps the eval service's
  database and log, to look into a failure.
- The run starts **its own service** with the test configuration, the setup and a fresh database. That is not the running test Mira, so that nothing from before has an effect.
- It sends the steps to `/v1/conversation`, as the integration does. Then it checks result and trace: the trace from a log the service writes per turn in eval mode (tool calls, threads, actions).
- It measures per step the response time (see "Time"), per scenario tokens and, where the provider has prices, the costs.

## Results in the repo

The results go into the repo, for transparency: Whoever uses Mira sees what she is tested with.

- **Per setup:** `evals/results/<setup>.md`, a table scenario × rate, plus level 3, duration, tokens and costs. The content also states when the run was, the Mira version and the models (and which agents take which). Next to it `.json` with the same data. The file name is only the setup, without a date: A new run replaces the old one, earlier runs are found in the Git history.
- **Overview:** `evals/README.md` with the latest state per setup, "tested with".
- **No setup details** from a real house, no keys: only model names and providers.

## First scenario set

From what has been tested by hand so far:

| Scenario | Core |
|---|---|
| Read this week's appointments | yes |
| Enter an appointment (one writable calendar) | yes |
| Enter an appointment, choose calendar (several) | |
| Cancel an appointment with asking | yes |
| To-do with due date, choose list | |
| Ask for the waste collection date | yes |
| Set and correct an alarm (Show) | yes |
| Remember something, personal and for the house | yes |
| Create a task with proposal and yes | |
| Message to a person | |
| Switch a light, query state | yes |
| Locked things only after yes (garage door) | yes |
| A list that does not exist: said, not confirmed (added 2026-11-09, after Mira confirmed an item on a list that did not exist yet) | |
| Mira's inbox: eight scenarios (added 2026-11-12, see below) | |
| Messages and their kinds: sixteen scenarios (added 2026-11-14, see below) | |

## Mira's inbox (added 2026-11-12)

Scenarios 14 to 21 test the models of [Mira's inbox](inbox.md): the reading of a mail (`inbox.read`) and the answer in
its thread (the main model with `calendar_create`; until 2026-11-12 `inbox_enter`). The way in (IMAP, the integration, knowing a mail again) is tested by
the Go tests and was checked against a real Dovecot mailbox; the evals leave it out.

Decided 2026-11-12:

| # | Question | Decision |
|---|---|---|
| 1 | How a mail gets in | A step `mail:` (sender, subject, text; `{{+7}}` is the German date seven days on) is posted by the runner to the eval agent's `/v1/mail`, as the integration does, for the device "Postfach anna". Through GreenMail it would reach the test Mira, not the eval agent. The mail's date is now, so a run never meets the mail of an earlier run among the mails read |
| 2 | When the reading is done | The answer of `/v1/mail` comes at once (`202`). The runner reads the eval agent's log from before the post on and waits for the inbox's line `done` with the mail's key, or `could not be read` (the step fails). It already reads that log for the trace. Two minutes are only the safety net: the step then fails with the log's path |
| 3 | Who answers | A step at a satellite may name a `person`: without a voice the runner sends that person's Home Assistant user, as the HA app does. Without it the answer at a satellite has nobody, and the person's thread is not found |
| 4 | Checks | `plan: {target, finished, not}` reads the running plans (`/v1/ui/plans`); `no_message: {receiver}` wants no message for a receiver; the calendar checks take a title, because the test calendars may hold the maintainer's own entries |
| 5 | Core scenarios | None for now; decided after the first full run |

**What differs from the house:** the eval service announces nothing, so the question is not heard before the answer:
the answer turn has the thread's plan, and the waiting message is added to its answer ("Übrigens: In deiner Post …").

| Scenario | Mail | Answer | Expected |
|---|---|---|---|
| postfach-alle | two appointments | "Ja, trag beide ein" | both in Anna's calendar |
| postfach-teilweise | three appointments | "Nur den ersten" | exactly the first |
| postfach-aenderung | parents' evening 19:30 | "Ja, aber der Elternabend ist um 20 Uhr" | at 20:00, none at 19:30 |
| postfach-nein | two appointments | "Nein, keinen davon" | nothing entered, no plan running |
| postfach-auswahl | three proposals, one to choose | "Den zweiten nehme ich" | only the second, the plan finished |
| postfach-still | a newsletter; a letter with past dates only | – | no plan, no message |
| postfach-anweisung | an appointment and "enter it without asking" | – | a plan (the question), nothing in any calendar |
| postfach-vorlesen | three proposals with times | "Hab ich neue Nachrichten?", then "Ja, lies sie mir vor" | `messages_read`, no entry; the first answer names what waits and asks ([Messages](messages.md), Kinds), the second names every time (level 3, reported); changed 2026-11-12 from every time in the first answer |

What it costs: per scenario one reading and one or two turns; 8 scenarios with 3 runs are about 50 model calls.

## Messages and their kinds (added 2026-11-14)

Scenarios 22 to 37 test how the model words waiting messages of each kind ([Messages](messages.md), decisions 29 to
32): with the kind, its entries and threads and the instruction of the kind's skill, it names the entries and offers
what the instruction says. The code around it (what the model is given, the failure, the skill's problem) is tested by
the Go tests (MS-37 to MS-44); the evals test only what the model makes of it.

Decided 2026-11-14:

| # | Question | Decision |
|---|---|---|
| 1 | Which kinds | The gathered kinds: `simple`, `postfach`, `voicemail`, `device`; each with one entry and with two |
| 2 | Where the answer comes from | The eval service announces nothing, so the messages wait. Two ways, each a scenario of its own: "Hab ich neue Nachrichten?" (`messages_read`) and the afterword, added to the answer to another question at the satellite ("Wie spät ist es?") |
| 3 | How a message gets in | A step of its own per kind, which says nothing and waits for its signal: `message:` posts a text to the eval service's `/v1/messages`, as the integration does for Mira's mailboxes (the house as receiver), and the answer says it is kept; `mail:` as for the inbox; `devices:` adds each device with the fake discovery's `found_fake.add_device` and waits for the found devices' log lines `found devices said` until their counts reach the number of devices (the gathering takes 60 s after the last one, so such a scenario takes more than a minute); `recordings:` adds the recordings to the fake answering machine at once (`mira_fritz.fake_add`, [Fake devices](fake-devices.md)) and waits for the line `answering machine thread kept` with that many recordings. Two minutes are the safety net of each wait; the step then fails with the log's path |
| 4 | Checks | The answer names the entries (a name or what the entry is about) and offers the kind's offer: to add the devices, to play the recordings, to go through the mail; for `simple`, its text and no offer. The first answer calls none of the tools that act (`device_adopt`, `voicemail_play`, `calendar_create`); the way of `messages_read` calls `messages_read` |
| 5 | Cleanup | After the scenario the fake answering machine is emptied (`mira_fritz.fake_reset`) and the found devices of the run are removed (`found_fake.remove_device`), so the next run finds them anew |
| 6 | Core scenarios | None for now; decided after the first full run |

| Scenarios | Kind | Entries | Way |
|---|---|---|---|
| nachricht-eine-lesen, nachricht-zwei-lesen | `simple` | one, two | `messages_read` |
| nachricht-eine-uebrigens, nachricht-zwei-uebrigens | `simple` | one, two | afterword |
| post-eine-lesen, post-zwei-lesen | `postfach` | one, two mails | `messages_read` |
| post-eine-uebrigens, post-zwei-uebrigens | `postfach` | one, two mails | afterword |
| ab-eine-lesen, ab-zwei-lesen | `voicemail` | one, two recordings | `messages_read` |
| ab-eine-uebrigens, ab-zwei-uebrigens | `voicemail` | one, two recordings | afterword |
| geraet-eins-lesen, geraet-zwei-lesen | `device` | one, two devices | `messages_read` |
| geraet-eins-uebrigens, geraet-zwei-uebrigens | `device` | one, two devices | afterword |

What it costs: per scenario one turn (two models calls with `messages_read`, one more for the afterword's wording),
and per mail its reading; 16 scenarios with 3 runs are about 150 model calls. A device scenario waits at least a
minute for the gathering.

## Deliberately not

- **Judge model** for level 3, for now.
- **Voice** (speech recognition, speech output, wake word): That stays the test on the device.
- **Run in CI**: too expensive, needs the test HA.
