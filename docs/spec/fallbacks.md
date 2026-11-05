# Silent fallbacks – Spec

Status 2026-11-05: steps 1 to 3 built, not released; step 4 to decide; step 5 (FB-13 to FB-28) and step 6 (FB-29) built, not released. Scenarios: [fallbacks.feature](fallbacks.feature).

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
3. **Most of these are weak error handling, not fallbacks:** an error that ends in nothing, an empty result taken as
   "nothing there", a quiet default. Error paths are part of every spec: for each step that can fail, what Mira does
   then and who learns of it. Code handles a failure only as its spec says.
4. **Every error handler leaves a trace** (2026-11-05, after a "Stopp" turn of 23 s whose first model call took
   15.5 s and the log could not say whether Gemini was slow or the client had retried): a retry, an unexpected
   answer of a server, a request broken off, an error caught and carried on from writes a log line where it
   happens, with what failed (the status or the error), what it cost (how long the attempt took) and what happens
   next (the next attempt and the wait, the substitute, the end). An error passed to the caller needs no line of its
   own where the caller logs it.

Decided in the rounds of 2026-11-05 (rows below): rows 4 to 12 as follows; the deprecation found by `testha
compat` (HC-06) is fixed for Home Assistant 2026.10 and 2026.11 alike. Rows 13 to 20 are decided next.

- **Row 4:** when the tool rounds are used up, Mira says she did not finish ("Ich bin damit nicht ganz fertig
  geworden.") and the log names the dropped calls; an empty answer says the same, not "keine Antwort". Since
  2026-11-05 an answer is empty only when nothing at all was said in the turn ([Streaming](streaming.md), decision
  10): text spoken in a round with a tool is an answer.
- **Row 5:** when a satellite's own version of Home Assistant's prompt was never fetched, its turn fetches it; if
  that fails, the turn answers with the fixed sentence of a missing house description, not with the lines of the
  version without a device.
- **Row 6:** when the tools for a satellite could not be listed, its turn lists them again; if that fails, the turn
  answers with the fixed sentence of a missing house description ([Context](context.md), 1.2).
- **Row 7:** when the calendar list cannot be read, the mail is an error back to the integration, not "no calendar";
  the log names it. The integration sends a mail once (on IMAP's event for a new mail); after an error it logs a
  warning ("The agent could not read the mail of …") and the mail does not come again. Sending it again is not
  decided.
- **Row 8:** the language stays as it is, now written down in [Languages](language.md): a language Home Assistant
  sends that Mira does not speak is replaced by the person's last one, an unreadable server language by German; both
  with a log line.
- **Row 9:** when the satellite's state cannot be read before speaking a message, the delivery fails and the message
  stays open for the next attempt; no plain announcement instead of a conversation.
- **Row 10:** when `start_conversation` runs into its deadline, the satellite's state decides: it was speaking
  (`responding`) → spoken; otherwise not delivered, and the message stays open. Cost: a subscription to the
  satellite's state while the call runs, ended with it.
- **Row 11:** a reminder that cannot be spoken at the satellite is sent privately; decided, with its log line: the
  reminder arrives, on another way.
- **Row 12:** when the satellite is still busy after the wait, the message stays open and comes once the satellite
  reports `idle`; nothing is cut off.

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
2. **Rows 4 to 12**, scenarios FB-04 to FB-11 (rows 8 and 11 are decisions written down, no change of behaviour
   beyond the log lines). Built.
3. **The deprecation found by `testha compat`** (`managed.py`, the device registry read the same under 2026.10 and
   2026.11), scenario FB-12, tested in pytest. Built.
4. Rows 13 to 20, decided with the maintainer. Not built.
5. **Error handlers that leave no trace** (decision 4): FB-13, and rows 21 to 35 as FB-14 to FB-28. Built.
6. **Row 36** (the integration), FB-29, tested in pytest. Built.

## Step 5: error handlers that leave no trace

| Id | Where | Before | Now |
|---|---|---|---|
| FB-13 | the model client (`internal/llm/client.go`, retries) | a 429 or 5xx, or a failed connection, was retried up to twice without a word; the log showed only the round's whole time | each retry writes a warning: the model, the endpoint's host, the attempt, the status or error, how long the failed attempt took, and the wait before the next. A retry that is not made because part of a streamed answer was passed on is the caller's error ("model failed while answering") |

Decided 2026-11-05: each of rows 21 to 36 gets its log line where it happens (scenarios FB-14 to FB-29, in the order
of the rows); what Mira does stays as it is. A read that finds nothing (a skills folder not made yet, a message
deleted since, no announcement in the window) is no error and stays without a line.

Found 2026-11-05 by a search of the code for errors that end in `continue`, an empty return, `_ =` or `x, _ :=`,
retry loops, and `except` branches of the integration that neither log nor raise; not changed yet. Places where
the error goes to the caller, who logs it, and places where a failure cannot happen (`json.Marshal` of own
values) are left out.

| # | Where | What fails → what happens, without a trace |
|---|---|---|
| 21 | `threads/agents.go` (`finishInBackground`), `threads/threads.go` (`work`) | a thread's background work is done, reading the thread again fails: the result is dropped |
| 22 | `voicemail/voicemail.go` (`play`) | playing a message on the speaker fails: the playback ends, the message stays new |
| 23 | `ha/ws/waitstate.go`, `cmd/mira/voicemail.go` (`playedThrough`) | subscribing to an entity's state fails: "not reached", the same as running out of time |
| 24 | `ha/ws/statetrigger.go` (`apply`) | an entities event from Home Assistant cannot be decoded: it is dropped (an unexpected answer of the server) |
| 25 | `notifier/notifier.go` (after a failed delivery), `threads/threads.go` (withdrawing a proposal's messages) | writing the message's state back fails: the error is dropped, the message may stay "delivering" or a moot proposal stay announced |
| 26 | `conversation/engine.go` (`skillTools`, the skills a session opened) | the session's open skills cannot be read: the turn runs without their tools |
| 27 | `conversation/layers.go` (`messageContext`) | a waiting message cannot be read: it is left out of what the turn knows |
| 28 | `tasks/assess.go` (`earlier`) | the task's past judgements cannot be read: it is judged without them (row 13 is the cooldown) |
| 29 | `skills/skills.go` (`Available`), `cmd/mira/schedules.go` | a skills folder or a `SKILL.md` cannot be read: the skill is missing from the list and its schedules from the page |
| 30 | `backup/backup.go` (`Restore`) | a skill named in the manifest is not in the archive: skipped, the restore reports success |
| 31 | `cmd/mira/edit.go` (`moveSecret`, reading and restoring the secrets) | the secrets file cannot be read or written back while a page edits: a secret is not moved, counts as empty, or a failed undo is dropped (row 19 is the start) |
| 32 | `cmd/mira/exposed.go` | the stored list of exposed entities cannot be read: "not known" |
| 33 | `store/threads.go`, `threads/plan.go`, `tasks/manager.go`, `tools/policy.go`, `hooks/rules.go`, `agent/async.go` | stored JSON, or an automation's config from Home Assistant, cannot be decoded: it counts as empty; a run taken up after a restart skips the step |
| 34 | `threads/threads.go` (saving a thread) | a conflict is retried without a line |
| 35 | `websearch/websearch.go` (`excerpt`) | a hit's page cannot be read: the next hit is taken |
| 36 | integration `api.py` (restore), `__init__.py` (`_adopt`) | Mira answers a restore with a body that is not JSON: success with nothing; Mira not reachable at setup: the adoption waits for the next start |
