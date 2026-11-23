# Reading what comes in – Spec

Status 2026-11-23: decided (the rules and the start with a broken mira.yaml by the maintainer, the places following
from them); steps 1, 2 and 4 built, step 3 open. Scenarios: [parsing.feature](parsing.feature) (PA-01 to PA-08).

## Goal

Issue 8: an integration stored a device's identifier as a number, Mira's reader of the whole device registry failed,
and every turn was refused. A search found more places where one entry of an unexpected type loses a whole list, and
places where a parse error leaves no trace or stops the service. Not everything is to be read leniently: leaving an
entry out is right only where the rest still tells the truth.

## Rules (2026-11-23, the maintainer)

| # | Rule |
|---|---|
| 1 | **Not everything is read leniently**, above all not where leaving something out makes the rest wrong |
| 2 | **A parse error never ends in a crash or a hang** of the service or of a function that runs on |
| 3 | **A parse error always leaves a trace in the log**: what, where, its input's start (as [Silent fallbacks](fallbacks.md), decision 5) |
| 4 | **A parse error that keeps an important function from working for good** (it comes again at every read until someone changes something) **makes a repair**, so the admin sees it |

## How a place is decided (taken while writing, following from the rules)

- **Is the rest still true without the entry?** Yes when the entries are independent: an unreadable device only
  means "Mira does not know this device". Then the entry is left out, with a log line. No when the absence itself
  says something: a missing window makes "all closed", a missing appointment "you are free", a missing alarm "no alarm
  at six". Then the whole read fails: a log line, "could not read it" to the user or the model, and a repair when it
  stays so.
- **A field Home Assistant or the source does not enforce** (an identifier, a `unique_id`, a name) is **read as it
  comes** (a number as its digits), not left out: that is no error but the data.
- **A repair** only for what is lasting and important; it goes once it is read again.

## The places

| # | What | Where | Context | Rest true without it? | Now | Repair |
|---|---|---|---|---|---|---|
| 1 | Home Assistant's registries (areas, devices, entities) | the house structure ([Context](context.md), 1.3, decision 5) | lasting | yes | entries left out with a log line (since issue 8) | **yes**: "Mira cannot read these entries of Home Assistant: …", while it is so (PA-01) |
| 2 | Home Assistant's device registry | found devices, Matter | lasting | yes | each entry on its own, left out with a log line (PA-02) | no (one device among many) |
| 3 | An entity's `unique_id` | adding a device, Zigbee pairing | per device | – | read as text or number (PA-03) | no |
| 4 | The skill source's answer | the subscription's skills | the start | yes (skills are independent) | a skill whose entry cannot be read is left out, the others load (PA-04) | **yes**, as that skill's problem |
| 5 | A device's name as a number | the integration (`mail.py`) | every refresh | – | read as text (PA-05) | no |
| 6 | Parse errors dropped without a trace | ten places (tasks, answering machine, Zigbee, inbox, config, pages) | – | – | each logs, or says why it cannot fail (PA-06) | no |
| 7 | A `mira.yaml` that cannot be read | the start | the start | no | the service starts limited (PA-07): Home Assistant can reach it, turns are refused; the problem names the file, its line and the error; once the file is saved again, Mira starts anew | **yes** (PA-08) |

Step 3, only once proven (on the test HA or in the source of the other side; an assumption is not built): the alarms',
timers' and tasks' automation lists and the group watch's members (rest not true: the whole fails, a repair), a
calendar's events and the answering machine's messages (rest not true: the read fails and is said; a repair when it
stays), the phone book, stations, search hits, the model list, config flows and a server's tool list (rest true: left
out with a log line), and another format of a model server's answer (read correctly if it exists).

## A `mira.yaml` that cannot be read (decided 2026-11-23, the maintainer: A)

Until now the service ended at its start, and the HA app started it again and again; the user saw it only in the
app's log. Now:

1. The service starts limited: the setup mode's server (`runSetupMode`), with the problem: Home Assistant's
   integration reaches it, pairs and reads its status; a turn gets the sentence that Mira's settings cannot be read.
2. The status carries the problem (source `config`, the file, the line and the error); the integration makes the
   repair "Mira's settings cannot be read" with them.
3. The file is watched as the hooks' folder is (its time, on the clock): once it changes and reads, the service starts
   anew (as after the setup, `restart`).

**Fails:** the store cannot be opened either → the service ends as before (nothing to serve); the file stays broken →
the repair stays, the log has the error once per change of the file.

## Steps

1. **What is proven in the service** (rows 1 to 4 and 6): the repair of the registries, the found devices' and
   Matter's lists, the `unique_id`, the skill source, the ten places. PA-01 to PA-04, PA-06 in Go. Built
   2026-11-23. Found on the way: Home Assistant does not check a device's name either; it is read as it comes in the
   house structure and the found devices (`ws.Text`), and the test HA has a device whose name is a number. The
   forgetting of gone found devices forgets nothing in a round where a device was left out: left out is not gone
   (rule 1).
2. **A broken `mira.yaml`** (row 7): the limited start, the problem in the status, the repair, the watch of the file.
   PA-07, PA-08 in Go. Built 2026-11-23: `config.Limited` takes the listen address, the token and the store's path
   from a file whose YAML parses; `runLimited` serves the pairing, `/v1/status` with the problem (source `config`)
   and `/v1/conversation` with the sentence; the integration's repair `config_problem` (de, en). Run on the test HA:
   a copy of the test Mira's config with an unknown field started it limited, the repair appeared, a voice turn at
   the fake Show got the sentence, and once the field was removed Mira started anew and the repair went.
3. **The assumptions**, each proven first. Looked at 2026-11-23: the group watch's members by a device's name is
   **disproven** (a device whose name is no text gets no entity at all: Home Assistant itself fails to make one); the
   alarms' variables are text as Mira writes them and as Home Assistant keeps them, a number only by an edit by hand
   (possible, not seen). The others are not proven yet (a calendar's events, a model server's other format, a third
   party's tool list, config flows' unique id) and stay open; the answering machine's messages and the phone book come
   from Mira FRITZ!Box, whose format is Mira's own.
4. **The integration's name** (row 5). PA-05 in pytest; the repair `registry_problem` (de, en) and its removal
   (PA-01 in pytest too). Built 2026-11-23.
