# Agents – Spec

Status 2026-10-21: decided. None of it is built.

The testable rules are numbered scenarios in [agenten.feature](agenten.feature); each Go test for one carries the
number in its name (`TestAG07_…`).

## Goal

Today Mira calls her models in eight places, each in its own way: the conversation, the skills' schedules,
delegating, assessing and planning tasks, reading mails, compacting the profile, wording announcements. Each of them
is really an **agent** of its own: a model with a particular context, particular tools, its own rules and its own
form of answer. Of that, only the model is in the configuration (`routing`); everything else is spread over the code
(inventory at the end).

The **agent** becomes a building block in the code. The eight existing ones become **internal agents**, described in
code, without their behavior changing. An agent is **callable like a tool**: it has a description, an input schema
and a response. So another agent can call it as a tool, Home Assistant as an action, and the code directly. Last,
optionally, a **factory** describes agents without code, in YAML.

Two things are kept apart: what belongs to an agent **for good** (its description), and what comes with **each call**
and is passed on to the agents it calls (the call context).

## The static description

What belongs to an agent, the same on every call:

| Part | Meaning | Examples |
|---|---|---|
| **Name** | what it is called in the log, in `routing` and as a tool | `conversation.voice`, `mail.read`, `tasks.plan` |
| **Description** | when to call it; for the calling model, as with a tool | "Reads a shipping mail and returns title, day and keys." |
| **Input** | schema of what it gets on a call | `{text}`, `{mail: {sender, subject, date, text}}` |
| **Output** | form of the answer | text, or JSON following a schema |
| **Model** | the role in `routing` | `routing.mail`; without an entry `main` |
| **Context** | what the model sees, as a profile | *conversation* (layers 1–7, history), *background* (fixed prefix with the house, no history), *isolated* (only a system message of its own and the input) |
| **Tools** | which tools and which other agents it can call | *none*, *read only*, *all*, or a list |
| **Rights** | the most the agent needs (see Rights) | `mail.read`: none; `conversation.voice`: all |
| **Limits** | defaults for tool rounds, time per attempt, retry | at most 3 rounds; 60 s, retry once |

## Context profiles

How each profile builds what the model sees, and how it is kept short. The parts are those of
[Context](kontext.md), section 1.

| Profile | What the model sees | History and compaction |
|---|---|---|
| **conversation** | all parts 1–6 of the composition: system message (rules, house structure, house profile), tools, example conversation, channel style, the session's history, the current turn | the session's history, compacted as in [Context](kontext.md), section 5 (`max_turns`, the context limit, `keep_turns`, more kept for a running thread) |
| **background** | the background prefix as today (system message with rules, house structure and house profile; tools as the rights allow; the background channel style), then as the current turn: time, language, the agent's instructions and the input | no session history; within a run its own rounds (the model's tool calls and their results) pile up, bounded by the limit on tool rounds; no compaction |
| **isolated** | only a system message of its own (the agent's instructions) and the input as the user message; no house, no tools, no memory | none; one request, answer as text or following the output schema |

## The call context

What comes with each call. When an agent calls another, it passes its call context on, narrower, never wider.

| Part | Meaning |
|---|---|
| **Call ID** | unique per call; plus the ID of the calling call, if there is one |
| **Origin** | who calls: Home Assistant (automation, Assist, Telegram, IMAP), Mira's timers, the Mira page, another agent |
| **Person, place, language** | for whom and where, as far as known |
| **Rights** | what this call may do (see Rights) |
| **Depth** | how many agents are above this one; 0 for an outside call |
| **Third-party text** | whether third-party text is already involved in the call (web content, a foreign plan) |
| **Mode** | synchronous with a timeout, or asynchronous with a delivery route (see Synchronous and asynchronous) |

## Rights

A small rights model, only what Mira already tells apart today:

| Right | allows |
|---|---|
| **Read the house** | read states, calendars, lists |
| **Act on the house** | Home Assistant actions that change something |
| **Remember** | write to memory |
| **Messages** | send messages to persons or the house |
| **Web** | web search and reading web pages |

- A run may do what **both** allow: the rights in the call context and the rights in the agent's description.
- When an agent calls another, it passes on in the call context at most its own effective rights. An agent that may
  not act on the house cannot do so through another one either.
- The rights add to the existing rules, they do not replace them: the guard still asks before critical actions,
  locks them after web content, and after third-party text Mira asks before keeping or passing anything on. In the
  background nobody can give that yes, so there these calls are refused.
- In an agent's run the rights decide what may change, also in the background profile: the background channel's rule
  "only read-only tools" applies to the old background turns, not to agents.
- If a called agent read third-party text, the result says so; for the caller it then counts as if it had read it
  itself.

### The right each tool needs

Derived from what a tool declares (`internal/agent/rights.go`, `RightFor`):

| Tool | Right |
|---|---|
| a tool that returns web content (`web_search`, `web_page`, MCP servers marked `web_content`) | web |
| a Home Assistant or MCP tool that only reads | read the house |
| any other Home Assistant or MCP tool | act on the house |
| `memory_save`, `memory_forget` | remember |
| `message_send`, `message_ack`, `reminder_set`, `reminder_cancel` | messages |
| `radio_play`, `sleep_timer`, `task_delete`, `task_enable` | act on the house |
| `thread_fill`, `thread_confirm`, `thread_undo` (filling or confirming may run the thread's finish, an action or a fact; taking back runs an action) | act on the house |
| every other own tool (`memory_search`, `messages_list`, `messages_read`, `reminder_list`, `calendar_events`, `skill_open`, `task_list`, `task_try`, `thread_start`, `thread_cancel`, `thread_plan`, `delegate`) | read the house |

## Calling

All ways go through the same interface: agent, input, call context; back comes the answer, or for asynchronous calls
the call ID.

- **As a tool of another agent:** if an agent is in another one's tool list, that one's model sees it as a tool:
  name, description, input schema. The call context comes from the calling run. `delegate` works like this already,
  only built in.
- **From Home Assistant:** an action, for instance at the end of an automation, with agent, input and mode;
  synchronously with an answer for the automation (`response_variable`), as `mira.assess` does today. Name:
  `mira.agent`. All agents with a fixed description can be called from Home Assistant (Decided 12).
- **From the code:** directly, as the notifier calls `notify.compose` today.

**Rights of a call from outside:** a call from Home Assistant (`mira.agent`) gets the rights the agent itself
names in its description; whoever set up the automation decided to call it. `mira.agent` can be called by whoever can
call Home Assistant actions, like `mira.assess` today. A conversation gets its rights as today.

**Fields of `mira.agent`:** `agent`, `input` (an object; for an agent without an input schema `{"input": text}`,
and without any input it gets an empty text), `mode` (`sync` / `async`), `timeout` (default 60 s),
`on_timeout` (`cancel` / `run_on`, default `cancel`), `delivery` (`event` / `message`, default `event`; for `message`
also `receiver`). `mira.agent_result` and `mira.agent_cancel` take only the call ID.

**An agent as a tool in a conversation:** at a satellite the conversation waits up to 5 seconds for the agent, as
threads do today; then the run goes on (`run_on`) and its result comes as a message to the room or the person, and
Mira says she will come back. On Telegram it waits up to 30 seconds.

**Cancelling:** the HA action `mira.agent_cancel` with the call ID cancels a running run; in a conversation "vergiss
es" ends it through the thread, as today.

**Duplicate calls from Home Assistant:** Home Assistant's own run modes handle them. Automations, like scripts, have
`mode` (default `single`: while the automation runs, a second trigger is dropped). An automation that calls
synchronously, or calls asynchronously and waits for `mira_agent_done` (`wait_for_trigger`), therefore starts no
second run. Only one that calls asynchronously and does not wait can start two runs; that is its author's choice.
Mira has no deduplication key of its own for this.

## Synchronous and asynchronous

The **caller** decides, in the mode of the call context.

- **Synchronous with a timeout:** the caller waits for the answer, at most until the timeout. What happens at the
  timeout, the caller says: cancel, or let it run on and deliver the result asynchronously. The conversation at a
  satellite does it that way (as threads do today: wait briefly, else "ich melde mich").
- **Asynchronous:** the call returns the call ID at once, the agent runs in the background.

### The run record

Every asynchronous run (and every synchronous one that runs on after the timeout) gets a record:
- call ID, agent, ID of the calling call;
- status: running, done, failed, expired (beyond its time limit of 60 minutes from the start), cancelled;
- the output following the agent's schema, or the error;
- start and end.

It is deleted 60 minutes after its end. Until then it can be queried by its call ID, also from Home Assistant
(`mira.agent_result`).

### Delivery

When the run is done, the caller is notified, on the route it gave in the call context:
1. **Home Assistant event** with call ID, agent, status and output; an automation can go on from there
   (`mira_agent_done`).
2. **Message** to a person or the house. The announcement agent (`notify.compose`) words it from the run's
   output ("So hab ich mir das gedacht"); the output itself goes into the message's details. This is the route for
   people.
3. **Continuation** of the calling agent or thread with the result. This route comes last.

The core of it exists already: the task planner runs asynchronously, its draft lies as JSON in the thread (the
thread ID is the call ID), and an announcement is made from it.

## Depth

An agent can call another, which calls another again. The depth is limited, by the setting `agents.max_depth`,
default 3. If it is exceeded, the call fails with that reason.

## Log

Every run writes one uniform line `agent done`: agent, call ID, ID of the calling call, depth, origin, mode, status,
model, duration, model calls, tokens. Through the call IDs a whole tree of calls can be followed. Lines that say what
a run achieved (such as `mail read`) stay in addition.

## Runtime

### Running

- A **synchronous** run runs in the caller's goroutine, with the caller's timeout.
- An **asynchronous** run (and a synchronous one that runs on after its timeout) is handed to the **runtime**: it
  runs each in a goroutine of its own, can cancel it, and shuts down in order.
- **Access to the models** is limited per model, not per channel as today: a local model serves one request at a
  time, a cloud model several. The runtime starts with fixed defaults for this.
- **At most 10 runs at the same time** in the runtime, a fixed default without a setting; another one waits until
  one ends. Conversations do not count towards them.
- **Local or cloud:** a model counts as local when its address is in the own network (localhost, private
  addresses); a local model serves 1 request at a time, a cloud model 4. Fixed for now; this will become more
  flexible and gets specified later.

### Jobs: code as a run

Some background work is code with model calls in between, no model loop: the task planner drafts, has Home
Assistant check the draft, and drafts again with the reason. It runs as a **job** of the runtime, named after the
thread's finish (`tasks.automation`), with the thread's ID as its call ID. It gets what every run gets: the agents
it calls (`tasks.plan`) hang under its call ID, one `agent done` line adds up their model calls and tokens, it takes
one of the 10 places, it has the time limit of 60 minutes, and ending the thread cancels it. A job is not stored;
after a restart the thread starts its work anew (it changes nothing before the user's yes).

### Several conversations at once

Several persons talk to Mira at the same time in different rooms (or on Telegram). Each turn is a run of
`conversation.voice` or `conversation.telegram` with its own call context (person, room, session), so the runs are
independent of each other:
- **Within one session** the turns run one after the other, so the history stays in order.
- **Between sessions** only the access to the model limits them: on a local model the second waits for the first,
  on a cloud model both run at once.
- Today the engine locks per channel, so all voice turns run one after the other, even with a cloud model. This
  changes with step 5.

### What a turn belongs to

Which conversation, thread and plan a turn belongs to follows from its call context, without a setting:
- **Person known** (Telegram, the Home Assistant app, or "alone at home is me"): conversation, threads and plans
  belong to the person and follow them to every satellite. Alone at home, one can simply go on talking in another
  room.
- **Person unknown** (several people at home, nobody recognized): everything stays in the room, as today. Two
  conversations in two rooms do not get in each other's way and see nothing of each other.
- **Private area:** an area in Home Assistant with the label "Mira: privater Bereich" is a hard border, like the label
  "Mira: nachfragen" on devices. What starts in it stays there, even for a known person, and nothing from outside
  comes in. This covers conversations, threads and plans. The integration creates the label (id `mira_private`,
  named in Home Assistant's language); the agent reads it from the area registry.
- If Mira later recognizes persons better (by their voice, say), the conversation follows the right person without the
  rule changing.

This rule belongs to the conversation agents and comes with step 5; it changes behavior (alone at home, the
conversation follows from room to room).

### Shared state

Memory, plans, threads and messages live in Mira's SQLite database (one connection, WAL), so a single write is safe
already. What is not safe today is "read, change, write back" (a whole thread is read, changed and written back;
two runs at once and the first one's change is lost).

- **No locks in the agents.** A change is either a single write in the database, or it checks a version number:
  write only if nothing changed since reading, else read again and retry.
- **No lock is held while a model thinks** (that takes seconds and would block everything).
- Adding a fact is a single write already. Ticking off a plan step and changing a thread get the version check.

### Surviving a restart

Asynchronous runs survive a restart of the agent and are continued.

- **Stored in the SQLite database**, next to the run record: the call context and the run's history (instruction,
  input, the model's answers, tool results).
- **After every round** the new parts are written, as rows of their own (not the whole history again). A round is
  a model call of seconds; a write takes milliseconds.
- **Before a tool runs** "started" is stored, **after it** its result.
- **On shutdown** the runtime starts no new round and stops. **On start** it loads every run with the status
  "running" and continues it from the last stored point. This works after a crash or a hard kill too, not only after
  an orderly shutdown.
- **What the run learned counts again:** besides its call context, a run stores what its guard learns while it runs
  (web content read, third-party text through a sub-agent) whenever that changes; continued, it knows it again.
- **Continuing:**
  - a model call that was running is simply made again (it has no effect outside);
  - a tool that was started but has no result: a read-only one is run again; for one that changes something the
    model gets "result unknown, the run was interrupted" and decides itself whether to look;
  - the time limits of a run count across the restart: a run that took too long is "expired" instead of continued.
- **Synchronous calls are not continued:** their caller (Home Assistant or a conversation) has no connection any
  more after the restart and got an error. Only what runs asynchronously, or ran on after its timeout, is continued;
  its result goes out on its delivery route.
- Only asynchronous runs store anything; synchronous runs write nothing extra. A synchronous call with run-on is
  stored from its start, as it may become asynchronous at its timeout.
- **The agent changed in the meantime** (an update, an edited YAML description): the run goes on with the new
  description. If the agent no longer exists, the run fails, with that reason.
- **Stored content** can hold private data (mails, web pages). It goes with the run record, 60 minutes after the
  run's end.

## The building block in the code

A package `internal/agent`:
- **`agent.Spec`:** the static description, written as values in the code.
- **`agent.CallContext`:** the call context.
- **`agent.Call(ctx, spec, input, callContext)`:** runs one run. Builds the context following the profile, calls the
  model, runs allowed tools with the guard and the effective rights, checks the output against the schema, retries
  following the limits, writes the run record (if asynchronous) and the log line, and delivers.
- A **list of the agents**, through which they are found as tools and from Home Assistant.

The conversation keeps its own flow (history, compaction, thread, confirmations); it gets an `agent.Spec` and a call
context, and the same log line.

## Switching over in steps

Steps 1 to 5 change no behavior (except `delegate`, step 4); the existing tests must stay green unchanged, and tests
per agent come in addition.

1. **Building block:** `Spec`, `CallContext`, rights, synchronous `Call`, depth, log line, access to the models
   limited per model. A run's history is a structure of its own, so it can be stored later. No user yet.
2. **Isolated agents:** `mail.read`, `mail.match`, `tasks.plan`, `tasks.assess`, `tasks.decide`, `profile.compact`.
   The mail calendar's retry moves into the limits of `mail.*`. Known consequence of "without an entry `main`":
   `mail.*` and `tasks.plan` fall back to `main` instead of `background` as today; where they should keep the
   background model, `routing.mail` / `routing.tasks` must be set (live: `mail: smart`) before this step goes live.
3. **Background without tools:** `notify.compose`.
4. **Background with tools:** `skill.task`, `delegate`. `delegate` becomes an ordinary agent as a tool. The only
   change in behavior: without `routing.delegate` it exists anyway, with `main` (see Decided, 13).
5. **Conversation:** `conversation.voice`, `conversation.telegram`.
6. **Asynchronous:** the runtime with storing and continuing, run record, query, delivery as a Home Assistant event
   and as a message; the Home Assistant action; the version check for plans and threads.
7. **Continuation** as a delivery route. Still to be specified; it waits until step 6 is built.
8. **Factory:** agents in YAML. Independent of step 7; can be built after step 6.

## Model choice

`routing` stays; its keys are the agents' roles (`voice`, `telegram`, `background`, `delegate`, `tasks`, `mail`).
Without an entry for its role, every agent takes `main`. New roles come only when someone needs them. Whether there
should be any number of named models instead of fixed roles is a separate, later topic.

## Factory: agents in YAML (optional, last)

An agent without code, with the same static description, in `mira.yaml` under `agents:` and in a skill under
`metadata.mira.agents` (same fields; placeholders from the skill's settings as everywhere):

```yaml
agents:
  rezept:
    description: Turns a recipe page into a list of steps.
    model: smart            # model name (under models, or main); without it main
    context: isolated       # isolated | background (conversation stays with the two conversation agents)
    tools: read_only        # none | read_only | a list of tools and agents
    rights: [read_house, web]  # without it only read_house
    input:                  # schema of the input
      type: object
      properties: { url: { type: string } }
    instructions: >-
      You get a recipe page and return the steps as a list …
    output:                 # optional: JSON schema of the answer
      type: object
      properties: { steps: { type: array, items: { type: string } } }
```

It is called like every agent: as a tool of another agent, from Home Assistant or from the code.

## Inventory

### The eight internal agents

| Agent | in the code today | Model | Context | Tools | Output |
|---|---|---|---|---|---|
| `conversation.voice`, `conversation.telegram` | `Engine.turn` | `routing.voice` / `telegram`, else `main` | conversation | all, with the guard | text |
| `skill.task` (schedules, briefing) | `Engine.RunTask` → `toolLoop` | `background` | background | read only | JSON message |
| `delegate` | `Engine.Delegate` → `toolLoop` | `routing.delegate`; without it there is none | background | read only | text |
| `tasks.assess`, `tasks.decide` | `tasks` through `isolatedModel` | `background` | isolated | none | JSON |
| `tasks.plan` | `tasks` through `isolatedModel` | `routing.tasks`, else background | isolated | none | JSON |
| `mail.read`, `mail.match` | `mailcal` through `isolatedModel` | `routing.mail`, else background | isolated | none | JSON, checked in code; 60 s per attempt, retry once |
| `profile.compact` | `profile` through `AssessIsolated` | `background` | isolated | none | JSON |
| `notify.compose` (announcements) | `notifier` through `Engine.Assess` | `background` | background without tools | none | JSON |

Things that stand out:
- The fallback rule is uneven: conversations fall back to `main`, everything else to `background`, and `delegate`
  does not exist without an entry.
- Time and tokens are logged in three different ways (`model round` in the conversation, `task done` /
  `delegate done` in the tool loop, `mail read` in the mail calendar), and not at all for the profile, tasks and
  announcements.
- Retrying a hanging model call exists only in the mail calendar.

### Calls and exchange today

| Agent | triggered by | route | the result goes to |
|---|---|---|---|
| `conversation.voice` | HA: a satellite's Assist pipeline | integration → `POST /v1/conversation` | back to HA (spoken); memory, threads, messages |
| `conversation.telegram` | HA: an event of HA's Telegram integration | integration (`telegram.py`) → `POST /v1/conversation` | back to HA (Telegram) |
| `skill.task` | Mira herself: a skill's schedule | internal timer → `RunTask` | a message in the queue → notifier |
| `delegate` | another agent: the conversation calls the tool `delegate` | direct call, waits for the answer | as a tool result back to the conversation |
| `tasks.assess`, `tasks.decide` | HA: an automation labelled "Mira" calls `mira.assess` / `mira.decide` | integration → `POST /v1/assess` / `/v1/decide` | back to the automation (the action's response); `assess` adds a message if wanted |
| `tasks.plan` | a thread "aufgabe" that the conversation opened and filled | the thread's finish, in the background | into the thread (database) → announced as a message → the yes comes in a later conversation |
| `mail.read`, `mail.match` | HA: event `imap_content` | integration (`mail.py`) → `POST /v1/mail` | into a calendar in HA; read by conversation and briefing |
| `profile.compact` | Mira herself: schedule (`memory.profile_schedule`); or the Mira page | internal timer / `POST /v1/ui/memory/compact` | into the profile (database); read by the conversations as context |
| `notify.compose` | the notifier, when several messages are due at once | direct call | text back to the notifier → announcement through HA |

From that:
- **Outside triggers** all come through Home Assistant (Assist, Telegram, IMAP, automations with `mira.*`) and the
  integration, as HTTP calls to the agent; plus the Mira page.
- **Inside triggers** are Mira's own timers (skills, profile) and the notifier.
- **One agent calls another directly** in two places: the conversation calls `delegate` (as a tool) and the notifier
  calls `notify.compose`. Both wait for the answer.
- **Otherwise agents exchange nothing directly, but through shared state:** the message queue, the threads, memory
  and profile (all in Mira's database) and calendars in Home Assistant.
- **No agent calls another through an HA service today.** The services `mira.assess`, `mira.decide` and
  `mira.send_message` exist, but only automations use them.

## Decided

1. **Conversation:** two agents, `conversation.voice` and `conversation.telegram`; the channels are too different.
2. **YAML agents** live in `mira.yaml` and in skills, both.
3. **Model:** without an entry for its role, every agent takes `main`.
4. **An agent is callable like a tool** (description, input schema, response): as a tool of another agent, from Home
   Assistant, from the code.
5. **Static description and call context are separate.** Rights belong in the call context; a small rights model with
   only what is needed.
6. **The depth is limited**, by a setting.
7. **The log is uniform**, through the building block.
8. **Synchronous or asynchronous is the caller's choice**, synchronous with a timeout.
9. **Asynchronous results** lie in a run record with call ID and output following the schema; the caller is
   notified. A message is one of the routes, not the only one.
10. **Names:** `mira.agent`, `mira.agent_result`, `mira.agent_cancel`, `mira_agent_done`, `agents.max_depth`.
11. **Defaults:** depth at most 3; a run record stays 60 minutes after its end.
12. **Callable from Home Assistant** are all agents with a fixed description: `delegate`, `skill.task`,
    `notify.compose` and the YAML agents. The agents whose instructions the code builds at each call (`mail.*`,
    `tasks.*`, `profile.compact`) are left out.
13. **`delegate` without an entry:** the rule "without an entry `main`" holds for it too; the tool `delegate` thus
    always exists, also without `routing.delegate` (today it does not then).
14. **The event `mira_agent_done` is fired by the integration:** Mira's own Home Assistant user may not fire events
    (`fire_event` is admin only). The agent calls an action of its own integration, allowed only for Mira's user
    (like the `mira/automation` commands today), and the integration fires the event at once.
15. **Checking the output schema:** a small check of our own for the parts of JSON Schema we use (object, required
    fields, types, `enum`, arrays), no library.
16. **The right each tool needs** is derived from what the tools already declare: Home Assistant tools that only
    read need "read the house", the others "act on the house"; tools that read web content need "web". Mira's own
    tools name their right explicitly (`memory_save`: "remember"; `message_send` and reminders: "messages"; radio and
    sleep timer: "act on the house"). The table of all tools goes into this spec with step 1.
17. **Rights from outside:** a call from Home Assistant gets the rights the agent names; `mira.agent` can be called
    by whoever can call Home Assistant actions.
18. **Cancelling:** `mira.agent_cancel` with the call ID.
19. **A new version during a run:** the run goes on with the new description; without the agent it fails.
20. **At most 10 runs at the same time**, a fixed default.
21. **No deduplication key:** Home Assistant's run modes (`single`, waiting for the result) cover duplicate calls.
22. **Model fallback consequence:** `mail.*` and `tasks.plan` take `main` without their own entry; live gets
    `routing.mail: smart` before step 2 goes live.
23. **Several conversations at once:** in order within a session, otherwise limited only by the model; conversations
    do not count towards the 10 runs.
24. **`mira.agent` fields** as described under Calling; result and cancel take only the call ID.
25. **A message as delivery** is worded by `notify.compose`; the output goes into the details.
26. **Local or cloud** by the model's address, 1 and 4 at a time; fixed for now, to be made flexible later.
27. **An agent as a tool in a conversation:** wait 5 s at a satellite, 30 s on Telegram, then run on and deliver as a
    message.
28. **YAML agents:** a model name (without it `main`); `rights` (without it only "read the house"); in skills under
    `metadata.mira.agents`; profiles `isolated` and `background`.
29. **The factory is built** after step 6, independent of the continuation.

30. **How the agent tells the integration that a run is done:** a WebSocket command of the integration that only
    Mira's user may send (`mira/agent/done`); no visible Home Assistant action, so other users and automations
    cannot forge `mira_agent_done`.
31. **What a turn belongs to:** to the person when known, otherwise to the room; an area labelled "Mira: privater
    Bereich" is a hard border. Part of step 5.
32. **No lock in the background profile:** the runtime's limit per model is the only one; an agent calling another
    cannot block itself, and `delegate` does not wait for skill jobs.
33. **In the background the rights decide** what an agent may change, not the background channel's rule; what needs
    a yes (critical actions, keeping or passing on after third-party text) is refused there, as nobody can confirm.
34. **Time limit of an asynchronous run:** 60 minutes from its start, fixed, without a setting; beyond it the run is
    "expired". `timeout` of `mira.agent` applies to synchronous calls only.
35. **Output schema and tools together:** the answer's form (`response_format`) goes along with every request of an
    agent with an output schema, also when tools are offered; no extra call to bring the answer into form. An agent
    that needs tools and an output schema takes a model that can do both.

## Open questions

1. **Continuation** (step 7): the calling run waits (stored, status "waiting") and is woken with the result; what a
   thread does with a result. To be specified once step 6 is built.
