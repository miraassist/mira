# The thread's history – Spec

Status 2026-11-23: decided (decisions 1 to 9); steps 1 to 3 built (released), step 5 built; the run at the Show (step 4) is open. Builds on [Threads](threads.md) (Assignment) and
[Context](context.md) (History and thread). Terms: [Glossary](../glossary.md), Conversation. Scenarios:
[thread-history.feature](thread-history.feature).

## Goal

A turn's history belongs to the recognised person today, a thread to its binding (a room, a person, a chat). The two
come apart as soon as the recognised voice changes, and the model loses the dialog it is in.

Seen 2026-11-16 at the test HA, adding a Matter motion sensor at the Show (one speaker throughout):

1. "Ich habe ein neues Matter-Gerät." Nobody recognised: history `voice:<conversation>`.
2. Mira's announcement "In welchen Raum kommt er?", the answer "ins Wohnzimmer" recognised as Anna: history
   `voice:person:anna`. Mira: "Soll ich ihn Bewegungsmelder Wohnzimmer nennen?"
3. "Ja, das passt." in the same conversation of Home Assistant (the same `conversation_id` as turn 2, seen in the
   pipeline debug), recognised as Martin: history `voice:person:martin`, empty. The thread's log held neither the
   room's answer nor the proposed name. The model called `device_finish` with "Bewegungsmelder Flur", a name nothing
   in its context gave.

One dialog ran in three histories. The cause is not the voice recognition: a conversation that serves a thread must
be bound to the thread, not to whoever is recognised.

## Decided (2026-11-16)

| # | Question | Decision |
|---|---|---|
| 1 | Whose is the history of a turn in a thread | **The thread's.** Every turn that belongs to a thread runs in the thread's history, whoever speaks. The recognised person still decides who speaks (the person's memory and profile, rights, "Es spricht"); not which history the model sees |
| 2 | A change of speaker while a thread runs | **Ignored, for now:** whoever continues the running dialog continues the thread (Assignment 3a and 3b), also a person's thread. This drops the exception of 3a ("not when the voice is recognised as another person"). Whether a speaker change should ever count is left for later |
| 3 | A person's thread waiting (Assignment 3c) | Unchanged: shown to that person only. Decision 2 holds for the running dialog, not for offering a waiting thread to others |
| 4 | The result in one's own history | When a thread ends, Mira's code writes a **summary of the thread** into the own history of every participant, so one can refer to it later ("wie hast du ihn genannt?") and Mira knows what she just did. No model writes it |
| 5 | For which threads | **Every kind**, by one generic rule at the one place where a thread ends: a device, an appointment, a to-do, the answering machine, a plan |
| 6 | Turns that come into a thread late | A turn that starts a thread or takes up a waiting one (3c, the model's choice) began in the speaker's own history; after the turn, it moves into the thread's history whole |
| 7 | How long the thread's history lives | As long as the thread is open, not the voice conversation's five minutes |
| 8 | A rule that makes Mira ask back when an answer refers to something not in her context | **Not built.** It would mend a broken history after the fact; with decision 1 there is no such break |
| 9 | A thread that ends while a turn still runs in it (2026-11-23, by the maintainer) | Ending is not dropping. A thread that ends (done, cancelled, expired, by a tool, an agent's result, the page) is ended at once; its summary and the drop of its history wait until no turn holds it any more. Every turn that belongs to the thread holds it for its duration; the last one to let go writes the summary and drops the history. Seen on the test Mira (2026-11-23, also in 0.30.1): a remote agent's result ended the thread while the turn that had answered its question still ran in the thread's history; the history was dropped under it, the turn could not store its answer ("FOREIGN KEY constraint failed"), and the satellite said "Something went wrong" |

## Building blocks

- **A turn's history** ([Context](context.md), History and thread; table `sessions` with its `chat_messages`): the
  thread's history is one more of them, under its own key. Compaction, the turn as the unit, the cache: unchanged.
- **The thread's link to a conversation** (`sessions.thread_id`, Assignment 3b): kept; what is new is that the link
  is made by Home Assistant's conversation, so it holds when the recognised voice changes.
- **The one place where a thread ends** (`Manager.end` in `internal/threads`, with its hook `Ended`): the summary is
  written there. Departure: two ways of ending a thread pass by this place today and are brought to it (below).

## What it does

### Which history a turn runs in

Before the model runs, the code knows for two of the Assignment rules that the turn belongs to a thread:

- **3a, the direct answer to an announcement of the thread**: the device and the messages announced on it moments ago
  (as today).
- **3b, the same conversation**: Home Assistant's `conversation_id` of the turn (voice) or the chat (Telegram) is
  linked to an open thread. Home Assistant keeps the `conversation_id` across a follow-up that keeps listening (seen
  2026-11-16, turns 2 and 3 above); the answer to an announcement starts a new one, which 3a links.

The order at the start of a turn becomes:

1. Who speaks (voice recognition, a follow-up, the channel), as today.
2. The speaker's **own history**, by the rule of [Agents](agents.md) (What a turn belongs to): the person's, else the
   conversation's, always the private area's.
3. **The thread by 3a or 3b**, from the device, the announced messages and the conversation. Found: the turn runs in
   the **thread's history** (key `<channel>:thread:<thread id>`), and the speaker's own history key is recorded in
   the thread as a **participant**. Not found: the turn runs in its own history.
4. The context as today, with the history chosen in 3.

A private area stays a hard border: a thread started there has its history there; no turn from outside joins it.
No continuation across channels: the thread's history has the thread's channel.

Telegram departs from this (decided 2026-11-19, [The chat's history](telegram-history.md)): a Telegram turn of a
thread runs in the chat's history; the chat is linked and a participant as here, but no thread's history is made.

### A turn that comes into the thread late

A turn that starts a thread (`thread_start`, a tool that starts one by code such as `matter_pairing_start` or
`voicemail_list`) or takes up a waiting one (3c: `thread_fill`, `thread_confirm`, `thread_plan`, a skill's tool on it)
ran in its own history. When it is done:

1. Its messages (the user's, the tool calls and results, the answer) move from the own history into the thread's
   history: added there, taken out of the own history, in one transaction. The turn counts whole or not at all.
2. Its conversation is linked to the thread, so the next turn of the conversation is 3b.
3. The speaker's own history key is recorded as a participant.

A turn that touched two threads moves into the one it took up last (an assumption that it is rare; logged).

### Mira's own sentences in the thread

What Mira says by herself for a thread (its announcements: "Ich hab's: … In welchen Raum kommt er?", a background
result) reaches the thread's history with the turn that answers it: that turn is the direct answer (3a), and what was
just announced at its satellite is part of its context, which is kept with the turn in the history. So every later
turn of the thread finds the question before the answer. It also stays in the thread's log, as today. (Found while
building, 2026-11-16: no message of its own is written; a history holds whole turns, each from a user's message on.)

### The summary when a thread ends

Every way a thread ends goes through `Manager.end`: done, cancelled, expired, cancelled from the page. There Mira's
code writes the summary:

- what the thread was: its target's name and the request,
- how it ended: done, cancelled, expired, taken back,
- what it holds: the filled fields, or for a plan its steps with their state,
- Mira's last sentence in the thread's history.

Example: "Vorgang „Neues Matter-Gerät“ erledigt. Bitte: Ich habe ein neues Matter-Gerät. Plan: Gerät anmelden ✓,
Raum ✓, Name ✓. Zuletzt gesagt: Bewegungsmelder Wohnzimmer ist fertig."

It goes into the own history of **every participant**, as one turn of its own: a user message that marks it as
Mira's record of a thread, not something said (the header "Ergebnis eines Vorgangs, festgehalten von Mira, nicht
gesagt. Darauf kannst du dich beziehen wie auf deine eigenen Worte; sag es nicht ungefragt noch einmal:"), with the
summary, and Mira's short answer "Festgehalten.", so the history keeps its turns of a user's message and an answer.
It counts as a turn for compaction. An own history lives by its channel's time as before (voice: five minutes without
a turn); the record keeps it alive for that time or starts it anew, so it is there for the next turns. Then the
thread's history is dropped: it lived as long as its thread (decision 7).

**When** (decision 9): every turn that belongs to a thread holds it while it runs (`Manager.Hold`). A thread that
ends while it is held is ended at once, whoever ends it (a tool of the turn, an agent's result, the expiry, the
page); its summary is written and its history dropped when the last turn holding it has stored its answer, so the
last sentence is in the summary and no turn loses its history. A thread ended by a tool of a turn that does not
belong to it (another thread's turn, a turn in an own history) gets its summary after that turn's answer, as before.
A thread nobody holds gets it at once. **Fails:** nothing new: a holder that never lets go cannot happen (the turn
lets go in a deferred step, also when it fails or is ended by its caller).

**The model is told** (the same step), where it reads it, so the cached instruction stays as it is: the heading of the
current thread in the turn's context says that the history above is this thread's, even where different people spoke
in it; the record's own header says what it is and how to use it (above).

### Ways of ending that pass by today

Two ways set a thread's end state themselves and do not reach `Manager.end` (and so not its hook `Ended`, the
withdrawal of its messages, nor the summary):

| Where | What | Then |
|---|---|---|
| `Manager.apply` (`internal/threads/threads.go`) | a finisher's result (foreground and background work: an appointment, a to-do, a task) sets `done` or `cancelled` directly | the end goes through `Manager.end`; what it does today (logging the result, saving the content) stays before it |
| `Manager.Undo` (`internal/threads/action.go`) | taking back a thread that is done sets `cancelled` | the thread has ended already and has its summary; taking it back writes a second, short record ("… zurückgenommen") into the participants' histories through the same code |

### When a step fails, who learns of it

| Fails | Who learns of it |
|---|---|
| The thread by 3a/3b cannot be read (the database) | The turn runs in its own history; log `could not read the turn's thread; the turn runs in its own history` with the conversation and the error. The model sees the waiting threads as today (3c) |
| Moving a late turn into the thread's history fails | The turn stays in the own history (the transaction rolls back); log `could not move the turn into the thread's history` with the thread, the session and the error. The next turn of the conversation is still linked (step 2) and finds the thread's history without this turn |
| Linking the conversation fails | Log `could not link the conversation to its thread` with both; the next turn finds the thread by 3c, as today |
| Writing the summary into a participant's history fails | The thread has ended all the same; log `could not write the thread's summary` with the thread, the participant's key and the error. The other participants get theirs |
| A thread without participants ends (started by code, nobody spoke in it) | Nothing to write; no log line |

### Cost

No model call. Per turn: one look-up of the conversation's thread before the model (an indexed read). A late turn: one
transaction that moves its messages (a handful). A thread's end: one write per participant (usually one or two). The
thread's history is cut by the same rules as every history.

## Between the integration and the service

Nothing changes: the conversation id comes from Home Assistant as today.

## Steps

1. **Every end through one place:** `Manager.apply` and `Manager.Undo` brought to `Manager.end` (the table above),
   with their tests; nothing else changes. TV-01, TV-02. Built 2026-11-16.
2. **The thread's history:** the order at a turn's start (3a/3b before the model, the history chosen by it), the link
   by Home Assistant's conversation, participants, the late turn moved, Mira's own sentences of a thread in its
   history, the history living as long as the thread; Assignment 3a without its exception for another person
   (decision 2); AG-114 dropped. TV-03 to TV-08. Built 2026-11-16.
3. **The summary** at the end for every kind, what tells the model of it. TV-09 to TV-12.
   Built 2026-11-16.
5. **Dropped only when all are done** (decision 9): `Manager.Hold`, the conversation's turn holding its thread,
   `Manager.end` leaving the summary to the last holder. TV-13 in Go. Built 2026-11-23; run on the test Mira with
   a remote agent answering within the turn.
4. **The run end to end** at the Show: the Matter dialog above with two voices (or one recognised as two), an
   appointment entered and then asked about ("wann war der Termin nochmal?").
