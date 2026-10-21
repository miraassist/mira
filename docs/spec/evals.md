# Evals – spec

Status 2026-10-17: built in 0.6.19 (runner `go run ./cmd/testha eval`, 12 scenarios, setups Gemini Flash and Qwen local).

## What for

"Does Mira run with Qwen?", "With Kimi?", "With the new Gemini?" That can only be answered by trying it out. Models and providers change constantly. An **eval set** plays fixed scenarios end to end, with a real agent, real model and real test HA, only without voice (no speech recognition, no speech output), and says per model setup what works.

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
- **Split up:** how much of it went to the model (rounds, tokens) and how much to tools and HA, from the agent's log.
- **Background separately:** What runs in the background (the draft of a task) is measured as time until the proposal and does not count toward the response time.
- **Exception:** A separate limit in the scenario (`max_seconds` on a step) exists only rarely, e.g. for a web search.

**Warmup:** The runner waits until the agent's warmup is finished, and then sends one turn per channel that does not count. So the fixed beginning of the prompt is in the cache with a local model. Only after that is measured. All 3 runs count; if the first is slower, one should see that.

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

**Secrets**: The runner reads only `testenv/.env`, never the `.env` of a real installation. The eval agent gets only a few harmless base variables (`PATH`, `HOME`, `LANG`, `TZ`) and exactly the variables its config requires as `${…}`. If one is missing in `testenv/.env`, the runner aborts with its name. Model keys for evals (e.g. `GEMINI_API_TOKEN`) therefore belong in `testenv/.env`. Examples: `gemini-flash.yaml`, `qwen-lokal.yaml`, `kimi.yaml`. A setup describes what really runs in the house, mixed too: voice local, difficult tasks in the cloud.

## Run

```
go run ./cmd/testha eval --setup evals/setups/gemini-flash.yaml [--scenario termin-absagen] [--runs 3]
```

- The run starts **its own agent** with the test configuration, the setup and a fresh database. That is not the running test Mira, so that nothing from before has an effect.
- It sends the steps to `/v1/conversation`, as the integration does. Then it checks result and trace: the trace from a log the agent writes per turn in eval mode (tool calls, threads, actions).
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

## Deliberately not

- **Judge model** for level 3, for now.
- **Voice** (speech recognition, speech output, wake word): That stays the test on the device.
- **Run in CI**: too expensive, needs the test HA.
