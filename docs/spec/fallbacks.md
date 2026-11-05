# Silent fallbacks – Spec

Status 2026-11-05: step 1 built, not released. Scenarios: [fallbacks.feature](fallbacks.feature).

## Goal

Where something Mira needs fails, she must not carry on as if it had worked with a substitute nobody decided on.
The case that started this: without Home Assistant's prompt Mira used her own structure block for months, and when
Home Assistant 2026.11 renamed the prompt she ran with less and nobody learnt of it ([Context](context.md), 1.2). A
search of the code (2026-11-05) found more such places. This spec lists them and decides them one by one; a failure
is an error, or a decided behaviour with its reason, never a silent substitute.

## Decisions (2026-11-05)

1. **The rule:** a substitute is allowed only when a spec decides it and says why (Bing → DuckDuckGo,
   [Web search](web-search.md)). Otherwise the failure is an error where it is seen: a log line, a problem or repair,
   a sentence to the user, the caller's error.
2. **Small first:** step 1 fixes the three that make Mira say or decide something wrong without anybody noticing.
   The rest is listed below and decided step by step.

## Step 1: the three that change what Mira says or decides

| Id | Where | Before | Now |
|---|---|---|---|
| FB-01 | assessing and deciding a task (`internal/tasks/assess.go`, presence) | the person states could not be read: the model was told nobody is home and nobody is away, and judged on that ("nobody is home and water runs") | the assessment fails with the error; the automation's call `mira.assess` / `mira.decide` fails and Home Assistant's trace shows it; nothing is judged on a house without people |
| FB-02 | a run's result as a message (`cmd/mira/agents.go`) | the receiver was not among the known persons (persons not read yet, or gone): the result went to the whole house | the message keeps its receiver; it waits for that person, and the log says the receiver is unknown. A result for the house stays for the house |
| FB-03 | "Gibt es Neues?" and the answering machine (`internal/voicemail`) | the answering machine could not be read: the answer left it out, as if nothing were new | the answer says that the answering machine could not be read, with the reason, as the skill itself does |

## The rest, to decide

Found 2026-11-05, not changed yet. "Spec" says whether a spec decides the case.

| # | Where | What fails → what happens | Spec |
|---|---|---|---|
| 4 | `conversation/engine.go` | tool rounds used up: the model's last tool calls are dropped without a word; an empty answer becomes "Entschuldigung, darauf habe ich gerade keine Antwort." | no |
| 5 | `ha/assistprompt` (`Tail`) | a satellite's own prompt was never fetched: it gets the lines of the version without a device ("cannot start timers", no area) | no (the kept piece of an outage is decided) |
| 6 | `mcp/host.go` | the tools for one satellite cannot be listed: it gets the tools without a device (no timers) | no |
| 7 | `mailcal` | the calendar list fails: a parcel or appointment mail is "ignored, no calendar" for good | no |
| 8 | `language` | an unsupported language from Home Assistant falls to the person's last one, an unreadable server language to German | partly |
| 9 | `notifier` (`satelliteMethod`) | the satellite's state cannot be read: `announce` instead of `start_conversation`, the satellite does not listen | no |
| 10 | `notifier` | `start_conversation` runs into its deadline: the message counts as spoken | no |
| 11 | `notifier/remind.go` | speaking a reminder at the satellite fails: it is sent privately instead | no |
| 12 | `notifier/remind.go` | the satellite is still busy after the wait: delivered anyway, cutting the answer off | partly (satellite.md: once idle) |
| 13 | `tasks/assess.go` (cooldown) | the past assessments cannot be read: no cooldown, it notifies again | no |
| 14 | `notifier/compose.go`, `cmd/mira/agents.go` | the model cannot word a message: a fixed sentence | no |
| 15 | `cmd/mira/lookup.go` (`hasAction`) | Home Assistant's actions cannot be read: the satellite counts as one without phone, screen, station names | no |
| 16 | `calendar` | an event's start cannot be parsed: the event is left out, a day may look free | no |
| 17 | `conversation/layers.go` | memory search or personal profile fails: the turn runs without them | no |
| 18 | `config` (`ModelFor`) | an agent names a model that does not exist: main, reported as a problem | contradicts models.md ("never implied") |
| 19 | `config/secrets.go` | the secrets file cannot be read: the environment only | no |
| 20 | `tasks/manager.go`, `cmd/mira/home.go` | Home Assistant's `get_config` fails: Mira's own address of Home Assistant in links | partly |

Decided already, and therefore no silent fallback: Bing → DuckDuckGo ([Web search](web-search.md)); speaker id
without an answer → nobody speaks, the rule "alone at home" decides ([Speaker id](speaker-id.md)); a hook that fails
on an entity → it counts as critical, erring safe ([Hooks](hooks.md)); a matcher in doubt → a new entry
([Mail calendar](mail-calendar.md)); the person states unknown at a delivery → treated as someone home, erring towards
privacy (`notifier`).

Also corrected: the comment in `internal/ha/structure` that claimed a build without the exposure list includes
everything; it includes nothing ([Home Assistant integration](ha-integration.md)).

## Steps

1. **FB-01 to FB-03.** Built.
2. The rest of the list, decided one by one with the maintainer. Not built.
