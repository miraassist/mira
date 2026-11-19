# The chat's history – Spec

Status 2026-11-19: decided (decisions 1 to 7); steps 1 to 4 built and run on the test HA (not released); step 5 (compaction) decided, not built. Builds on
[The thread's history](thread-history.md), [Channels](channels.md) (Telegram via Home Assistant) and
[Context](context.md) (History). Terms: [Glossary](../glossary.md), Conversation. Scenarios:
[telegram-history.feature](telegram-history.feature).

## Goal

A Telegram chat is one long conversation: the user sees every line of it, Mira's and their own, also what Mira sent
by herself. Mira should see the same. Today she does not, as soon as a thread is involved.

Seen 2026-11-19 at the test Mira (Mira's conversation API, channel `telegram`, person Anna, one chat):

1. "Merk dir für dieses Gespräch: meine Lieblingszahl ist 37." – in the chat's own history `telegram:<chat>`.
2. "Ich möchte einen Termin anlegen." – the turn starts a thread and is moved into the thread's history
   `telegram:thread:<id>` ([The thread's history](thread-history.md), a turn that comes into the thread late); the
   chat is linked to the thread.
3. "Welche Lieblingszahl habe ich dir vorhin genannt?" – runs in the thread's history (the chat is linked) and does
   not see turn 1. Mira: "Du hast mir in diesem Chat bisher keine Lieblingszahl genannt."

When the thread ends, its turns are taken out of the chat for good: in a second run (a thread started and cancelled)
the chat's history began with the thread's summary; the dialog itself was gone.

And what Mira sends into the chat by herself (a reminder, a private message, a thread's result) is in no history at
all: Telegram has no asking device, so the context of what was just delivered (`messageContext`, keyed by the
device) is empty there. "Was hast du mir eben geschickt?" cannot be answered.

The cause: the thread's history is built for the turns at a satellite, which are short and whose speaker may change.
In Telegram there is no doubt about the chat, its person or its place, and the chat lives without a time limit.

## Decided (2026-11-19)

| # | Question | Decision |
|---|---|---|
| 1 | Which history a Telegram turn runs in | **Always the chat's own history**, also when the turn belongs to a thread. The thread stays bound to the chat as today (its turns run in the chat, the chat is linked to it, the thread's heading in the turn's context); only the history does not change. Departure from [The thread's history](thread-history.md), decision 1: that decision mends a dialog split between voices; a chat is not split |
| 2 | The thread's summary for the chat | **Written too**, as for every participant: the chat is recorded as a participant, and the summary goes into the chat's history when the thread ends. It repeats what the dialog already shows; it does no harm and keeps the result after the dialog is cut |
| 3 | What Mira sends into the chat by herself | **A delivery record** in the chat's own history: what was sent, as it was sent, marked as sent by Mira on her own, so "was hast du mir eben geschickt?" has its answer |
| 4 | How the service finds a chat's history from a delivery | The chat's history is keyed by the chat's **notify entity** (`telegram:notify.telegram_anna`) instead of the chat's number. The service delivers to that entity and so knows the key without a new field. The existing Telegram histories start anew once (no data carry-over yet) |
| 5 | Group chats (several people in one chat) | **Not in this spec.** One chat is one history, which fits a group too. Who writes in a group is still not told apart: the adapter assigns a chat to one person (unchanged, named under Limits) |
| 6 | A delivery to a chat that has no history (it never wrote to Mira, or after `/neu`) | **`/neu` empties the chat's history but keeps the chat known** (its session stays, without messages), so the next delivery is recorded. A chat that never wrote to Mira has no history and gets no record (named under Limits); the service does not ask Home Assistant which notify entity is a Telegram chat |

| 7 | Compaction of a chat's history (found 2026-11-19 reading `internal/conversation/compact.go`, not seen on the test HA) | **Three changes**, below under Compaction: the running thread is kept from the turn it came into this chat, not from its creation; at the context limit it is not kept; a cut the thread prevents is logged |

**Unchanged:** who may write to Mira. Home Assistant's Telegram integration lets only its allowed chats through, and
the adapter takes only chats that are a person's channel ([Channels](channels.md)); the adapter still recognises the
chat by its number. Decision 4 changes only the name the history is stored under, after that check.

## Building blocks

- **The own history** ([Agents](agents.md), What a turn belongs to; [Context](context.md), History): the chat's history
  is one, under its key `telegram:<conversation>`. Compaction, `/neu`, no time limit (`sessions.telegram_ttl`):
  unchanged.
- **The thread's link to a conversation** (`thread_conversations`, Assignment 3b) and **participants**
  (`threads.participants`): kept as they are; for a chat they decide the thread of a turn and who gets the summary,
  not the history.
- **The record in an own history** (`Engine.Record`, built for the thread's summary): a user message that marks the
  record and an answer that closes it, one turn. The delivery record uses it.
- **The one place a private delivery goes out** (`Notifier.sendTo` in `internal/notifier`): it returns the channel that
  worked; the delivery record is written there.

## What it does

### The chat's key (decision 4)

The adapter (`custom_components/mira/telegram.py`) sends the chat's notify entity as `conversation_id`, for a turn and
for `/neu` (`reset_conversation`). The service keeps its rule: the key is `telegram:<conversation id>`. Nothing else
in the service depends on the chat's number.

### Which history a Telegram turn runs in (decisions 1, 2)

The order at a turn's start ([The thread's history](thread-history.md), Which history a turn runs in) stays, with one
difference for the channel `telegram`:

1. Who speaks: the person the adapter names, as today.
2. The own history: the chat's, `telegram:<notify entity>`.
3. The thread by 3b (the chat is linked to an open thread): found, the turn **belongs to the thread** (its heading in
   the turn's context, its tools), but **runs in the chat's history**; the chat's key is recorded as a participant.
4. A turn that starts a thread or takes up a waiting one is **not moved**: it stays in the chat's history; the chat
   is linked to the thread and recorded as a participant, as for every late turn.
5. No thread's history (`telegram:thread:<id>`) is created for the channel `telegram`.

When the thread ends, its summary goes into the chat's history like into every participant's (decision 2), after the
turn's answer when a tool of the turn ended it, at once otherwise ([The thread's history](thread-history.md), The
summary when a thread ends).

A thread of a person that began at a satellite and is continued in Telegram (Assignment 3c) is unchanged: the voice
turns ran in the thread's history `voice:thread:<id>`, the Telegram turn runs in the chat's history; no continuation
across channels, as today.

**The model is told** (the same step): the heading of the current thread in the turn's context today says "the
history above is this thread's, even where different people spoke in it". On Telegram it says that the history above
is this chat's and the thread is part of it: "## Laufender Vorgang (diese Unterhaltung gehört dazu; der Verlauf oben ist
dieser Chat, der Vorgang ist ein Teil davon)" / "## Current thread (this conversation belongs to it; the history above
is this chat, the thread is part of it)".

### The delivery record (decision 3)

When `Notifier.sendTo` has sent a text over a channel that has a chat's history (`telegram:<that notify entity>`
exists), the notifier writes a delivery record into that history with `Engine.Record`, after the send succeeded:

- the user message: the header "Von Mira selbst in diesen Chat geschickt, ohne dass jemand gefragt hat (Nachricht
  <id>, Art <Art>). Was sie geschickt hat, steht als ihre Antwort darunter; die nächste Nachricht kann sich darauf
  beziehen." / "Sent into this chat by Mira on her own, without being asked (message <id>, kind <kind>). What she sent
  is her answer below; the next message may refer to it." In the receiving person's language;
- the answer: the text exactly as it was sent (`Spoken` and `Details`).

So the history reads like the chat: Mira's line where the user saw it. It counts as a turn for compaction. A turn
running in the chat at that moment is not waited for (as with the summary: `Engine.Record` takes no lock); the record
lands after it.

Whether a channel has a chat's history is a read of `sessions` by the key (one indexed read per delivery). A companion
app or another notify target never has one, so nothing is written for it.

**The model is told** by the record's own header (above); the cached instruction does not change.

### Compaction (decision 7)

The rules of [Context](context.md), section 5, hold for the chat's history: whole turns, in jumps from `max_turns` (50)
to `keep_turns` (30), at the latest at 90 % of the model's context window; delivery records and thread summaries
count as turns. The running thread stays whole. Since step 2 a chat's history holds the turns of its threads, and
three things do not fit a history without a time limit:

1. **From when the running thread is kept.** Today: from the first turn after the thread was created. A thread that
   waits for days (an appointment waiting for a yes) while the chat goes on, or one created before the chat's oldest
   turn (started at a satellite), keeps everything since then, and the history grows past `max_turns`. New: in a
   chat's history the thread is kept from **the turn in which it came into this chat**, the last turn that began at
   or before the chat's link to the thread (`thread_conversations.linked_at`). Linking the chat to the same thread
   again (every turn of the thread does) keeps the time; only a link to another thread sets it anew. Other
   histories as today.
2. **At the context limit** (the cut for 90 % of the context window) the running thread is **not** kept: the history
   is cut to `keep_turns` like any other. The thread's state stays in every turn's context (section 4 of
   [Context](context.md): the thread carries the concern); what drops out are its older turns. Otherwise a long
   thread can push the chat past the model's window, where nothing cuts any more. This holds for every history,
   voice too. The log line `history compacted` says so (`kept_for_thread` empty, the thread named as `thread_cut`).
3. **A cut the running thread prevents is logged.** Today, when the thread is kept from the history's first turn,
   the cut ends without a trace. New: a warning `history cannot be cut: the running thread began at its first turn`
   with the session, the thread, the turns and the reason.

Nothing new is told to the model: what the code models (the thread, its state in the turn's context) does not
change, only which old turns stay.

### When a step fails, who learns of it

| Fails | Who learns of it |
|---|---|
| The thread by 3b cannot be read | As today: the turn runs in the chat's history without the thread; log `could not read the turn's thread; the turn runs in its own history` |
| Linking the chat to a thread, or recording it as a participant, fails | As today: log `could not link the conversation to its thread` / `could not record a participant of the thread` with both; without the participant the chat gets no summary at the end |
| Writing the summary into the chat fails | As today: log `could not write the thread's summary` with the thread and the chat's key; the dialog is in the chat's history all the same |
| Reading whether the channel has a chat's history fails | The delivery stays delivered; log `could not read the chat's history; the delivery is not recorded` with the message id, the channel and the error |
| Writing the delivery record fails | The delivery stays delivered (the record is not part of it and is not retried); log `could not write the delivery record` with the message id, the chat's key and the error. A later "was hast du mir geschickt?" finds nothing |
| Reading the chat's link to its thread for compaction fails | The thread is kept from its creation, as before; log `could not read the chat's link to its thread; the thread is kept from its creation` with the session, the thread and the error |
| An older integration sends the chat's number as `conversation_id` | No error: the turns run in `telegram:<number>` as today, delivery records go to `telegram:<notify entity>`, which nobody reads. The record is skipped, since no such history exists |

### Cost

No model call. A Telegram turn: as today, minus moving a late turn. A compaction of a chat's history: one indexed
read of `thread_conversations` more (compactions are rare: every 20 turns or at the context limit). A thread's end: one write more (the chat as a
participant). A delivery: one indexed read of `sessions`, and for a chat one transaction of two rows. Nothing grows
with the number of items.

## Between the integration and the service

No new field. What changes is the value of `conversation_id` the adapter sends for the channel `telegram` (decision
4). An older integration with a newer service: as today, without delivery records (the table above). A newer
integration with an older service: the history is keyed by the notify entity, the rest as today.

## Limits

- **A group chat** is one history and one person: everyone in the group writes as the person the chat is assigned to
  (decision 5). Telling the sender apart is on the TODO list.
- **A chat that never wrote to Mira** gets no delivery record until its first turn (decision 6).

## Steps

1. **The chat's key:** the adapter sends the notify entity as `conversation_id`, for a turn and for `/neu`; pytest.
   TC-01, TC-02. Built 2026-11-19.
2. **The chat's history in a thread:** a Telegram turn runs in the chat's history also in a thread, a late turn is not
   moved, the chat is a participant and gets the summary, the thread's heading for Telegram (the model is told).
   TC-03 to TC-07. Built 2026-11-19.
3. **The delivery record:** written by the notifier after a successful send over a channel that has a chat's history,
   with its header (the model is told); its error paths; `/neu` on Telegram empties the history and keeps the chat
   (decision 6). TC-08 to TC-12. Built 2026-11-19.
4. **The run end to end** on the test HA, through Mira's conversation API with the channel `telegram` and a notify
   entity as the conversation: the three turns of the Goal, with the thread kept open and with it ended; a reminder
   delivered to the chat and asked about ("was hast du mir eben geschickt?"). The delivery needs a notify entity among
   Anna's channels in the test HA; if none fits, the fake is extended first. Run 2026-11-19 with the test HA's real
   bot (its chat is the Admin person's channel, so no fake was needed): the favourite number asked inside an open
   thread, the thread cancelled, a reminder recorded and asked about, `/neu` with a reminder recorded afterwards.
5. **Compaction of a chat's history** (decision 7): the running thread kept from the turn it came into the chat, the
   link's time kept for the same thread, no protection at the context limit (every history), the log line for a cut
   the thread prevents, its error path. TC-13 to TC-16. On the test HA: a chat with a waiting thread and more than
   `max_turns` turns, through Mira's conversation API with the channel `telegram`, the log's `history compacted`.
