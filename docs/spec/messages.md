# Messages – spec

Status 2026-11-13: built in 0.6.0–0.6.4 (except "recognized at the satellite", see Open); step 1 (who sees which open messages) built 2026-11-11; kinds of messages specified 2026-11-12: steps 2 to 5 built 2026-11-12; steps 1 to 5 released in 0.23.0; step 6 (the announcement knows the kinds) built 2026-11-14, not released; the kind `agent_request` built 2026-11-14 with [Remote agents](remote-agents.md), step 3. Decision 33 (kinds `appointment` and `todo` instead of `postfach`) decided 2026-11-15, built with the inbox's step 10. Scenarios: [messages.feature](messages.feature). Two abstractions carry Mira's communication: [Channel](channels.md) and **message** (this document). Builds on [Users](users.md) and [Mira in Home Assistant](ha-integration.md).

## Principle

The **notices** become **messages**. A message has a **sender**, a **receiver** and a **content**. Receiver, sender and the receiver's settings determine how and when it is delivered.

There is **one** term for everything Mira passes on to someone: what today is a task's notice ("Der Gasverbrauch ist ungewöhnlich hoch") will then be a message with the sender "gas task". "Sag Anna, dass es später wird" is a message with the sender Ben. No second system next to it.

Not part of it is the **broadcast** ("Sag allen, das Essen ist fertig"). It is a loudspeaker, not a delivery: now or never, to no one in particular, not stored, without receiver settings. HA brings it as a tool (`HassBroadcast`), Mira already has it via HA's MCP. At most later: name the sender and respect quiet hours.

"To everyone individually", the same message over each person's private channel, only as the receiver `persons` of `mira.send_message` ([Group watch](group-watch.md), decision 20): a private message of the house, sent over the channels of every person that has one.

## The message

| Part | Values |
|---|---|
| **Sender** | a person (recognized via their channel), an area ("jemand im Wohnzimmer", anonymous at the satellite) or Mira herself: a task, a skill, an automation (`mira.send_message`), a reminder |
| **Receiver** | a person or **the house** |
| **Content** | text; plus, as today, details, importance (0–3), confidential yes/no, expiry, key against duplicates |
| **State** | waiting → delivered → heard/done, or expired |

## Receiver: the house

Like the notices today: Mira delivers in the house.
- announcement when someone enters a room (motion sensor at the satellite), if the room is set to "announce";
- otherwise the **indicator** lights up until someone asks whether there is anything new;
- following an answer ("Licht ist an. Übrigens: …");
- morning announcement, quiet hours, escalation, urgent immediately – all as before.

There are no receiver settings here; the house accepts everything.

**Own room:** A message that someone leaves at a satellite remembers the room (`origin_area`). There it does not count as waiting for `notifier.own_message_hold` (default 10 minutes, for scheduled messages from when they are due): no announcement on entering, no LED ring, no hint on the screen, no "Übrigens", no answer to "Gibt es Neues?". Other rooms get it immediately, afterwards the own room too. Urgent messages, reminders, tasks and private routes do not know this delay. It is set in the HA options ("General", minutes; 0 = off).

## Receiver: a person

**The person's settings** (on the HA person, `options["mira"]`):

| Setting | Values | Default |
|---|---|---|
| Accept messages | from everyone (also anonymous at the satellite) / from other persons / from no one | from everyone |
| Channels | the person's notify targets (companion app, Telegram chat, Signal, …), see [Channels](channels.md) | none |
| Alone at home, the satellites are me | on/off | off |

- "Accept" applies to messages **from humans**. What Mira sends herself (tasks, skills, reminders, automations) always arrives; otherwise one could unsubscribe from the leak sensor warning.
- "From other persons" means: the sender is recognized as an HA person via their channel. Anonymous at the satellite ("jemand im Wohnzimmer") does not count.
- If the receiver does not accept, Mira tells the sender so honestly ("Ben nimmt gerade keine Nachrichten an") and stores nothing.

**Delivery:**
- private: via the person's [channels](channels.md) (HA notify targets), as today for personal and confidential messages;
- at the satellite (announcement or indicator), **if the person is recognized there**. Today recognized only means: the "alone at home" rule. **What "recognized at the satellite" will mean in future we discuss separately.**
- **private** (confidentiality `private`, since 2026-11-09): only over the person's channels, at once outside quiet
  hours, never spoken or shown; for a schedule's list for one person ([A schedule for one person](schedule-receiver.md)).
- **read out on request** (`messages_read`, "Was gibt's?"): the model says the messages in its own words, but dates
  and times, places, amounts, deadlines and names exactly as they stand, none left out or summed up, also with several
  appointments (decided 2026-11-12, after the inbox's three proposals were read out as "am 19., 20. und 23. November"
  without their times).

## Who sees which open messages

The tool `messages_list` ("Welche Nachrichten sind offen?", and for quitting one with `message_ack`) shows the same
choice as reading out (`messages_read`), where the asker is (2026-11-11; before, it listed the 20 newest open
messages of the whole house with their text to everyone, private ones of other persons included):

- **At a satellite** (a voice turn with its room): the open messages that would be spoken in that room by the
  rules of the announcements: the house's; a person's only while the routes allow it (not while that person is away
  with a person entity; a confidential one as `notifier.confidential_voice` says); **never a private one**; not one
  held in its own room (`notifier.own_message_hold`). Open means waiting or heard, not yet done or expired.
- **In a person's own chat** (Telegram): the house's and the person's own, confidential and private ones included
  (the chat is private).
- **Elsewhere** (a turn with neither a room nor a person, an agent's run): the house's only.
- Reminders are not listed (`reminder_list` lists them); the importance below `notifier.min_importance` neither, as
  for reading out. At most 20, the most important first.

What it does and costs: one query of Mira's database; at a satellite, when any message is open, the person
entities' states once (Home Assistant's states, as for an announcement). The turn waits for both.

| Fails | Who learns of it |
|---|---|
| The database cannot be read | The tool's error; Mira says she cannot see the messages now |
| A person entity's state cannot be read | That person counts as not alone and away-unknown, as for an announcement: their confidential messages are not listed (errs towards privacy); no line of its own (the announcement's rule) |

## Wording

Mira names the sender if it is a human:
- "Anna lässt dir ausrichten: Es wird später."
- "Jemand im Wohnzimmer lässt ausrichten: …" (to the house)
- Messages from Mira herself without preamble, as today.

## Creating

- In conversation: "Sag Anna, dass es später wird", "Hinterlass fürs Haus: Der Schornsteinfeger kommt um 10." For this the model calls a tool (receiver, text); the sender comes from the turn's identity, never from the model.
- From HA: the action `mira.add_notice` will then be called `mira.send_message` (receiver instead of `audience`).
- With a point in time ("Erinnere Ben morgen um 7 an den Müll", "Erinnere mich in 10 Minuten") it is a message with a **delivery time**: it waits until then and is then delivered like any other. **Reminders are fully absorbed into messages**; the separate table and the separate delivery route for reminders are dropped. A reminder to oneself is a message from me to me (it is always accepted); if asked in a room, it is spoken there, as today.

## Renaming

This time everywhere, because the concept changes:
- Code: `notices` → `messages` (table, store, notifier texts, tools such as `notices_read` → `messages_read`); `reminders` are absorbed into it, `reminder_set` becomes sending with a delivery time.
- HA: action `mira.send_message`, sensor "Pending messages" (`sensor.mira_pending_messages`).
- Web UI and guide: tab and texts "Messages".
- No data migration (everything is test).

## What Home Assistant already has

Checked against HA 2026.10, so that we do not rebuild anything:

| HA | What it is | For Mira |
|---|---|---|
| `notify.*` (companion app, Telegram bot, …) and `notify.send_message` | transports: send a message to a device or a chat | **use** as channel ([Channels](channels.md)) |
| `assist_satellite.announce` / `start_conversation` | speak at the satellite | **use**, as today |
| `persistent_notification` | hint in HA's bell, the same for everyone | no channel per person; at most for admin hints |
| `alert` | repeated notification as long as a state persists | nothing equivalent; our escalation stays |
| `todo` | shared lists | something else |
| Assist timer (`HassStartTimer`) | timer on the device | stays HA's business; Mira's reminders are messages |

**What HA does not have:** a message model with sender and receiver, waiting until the receiver is reachable, delivery by presence in the room, per-person settings on whom to accept from. We build that; the **transports** Mira takes from HA.

**Offer it close to HA:** The integration provides `notify` entities, one for the house (`notify.mira_haus`) and one per person (`notify.mira_anna`). Then automations send a message through Mira with HA's own action `notify.send_message`, without a Mira-specific action; the sender is then the automation. `mira.send_message` stays for more fields (importance, confidential, expiry).

Telegram runs via HA's integration `telegram_bot`; Mira's own bot is dropped ([Channels](channels.md)).

## Decided

- Reminders are absorbed into messages (message with delivery time).
- **No read receipt** for the sender ("Anna hat es gehört"): out of respect for the receiver's privacy.

- **`messages_list` shows what the asker may hear** (2026-11-11, see Who sees which open messages).

## Kinds of messages (decided 2026-11-12)

**Why.** "Hab ich neue Nachrichten?" is the question when the ring glows. Today three things answer to "Nachrichten":
the answering machine (its skill's description names exactly this sentence), the news of the world (skill
`nachrichten`) and Mira's own messages (`messages_read`, which calls them "Benachrichtigungen"). In the eval
`postfach-vorlesen` (2026-11-12) Flash Lite took the answering machine (`voicemail_list`) and said "Keine neuen
Nachrichten" while the inbox's question waited; Flash took `messages_read`. The model followed what it was given: the
abstraction is the fault, not the model.

**The idea.** What waits is always **a message**: it lights the ring and shows on the screen. A message has a **kind**,
and the kind says what happens with it, so the model never has to guess from the text. What came into a message are
its **entries**; an entry may lead to a **thread**, and the user takes up one thread after the other.

### The entities

**Message** (exists; changed): what waits for one receiver.

| Field | Meaning |
|---|---|
| `kind` | required, see Kinds |
| `receiver` | a person or the house |
| `state` | waiting → delivered → heard/done, or expired |
| `entries` | one or more (below) |

Text, sender, importance, confidentiality, expiry, the key against duplicates and the thread **move from the message
to its entries**. The delivery fields (`deliver_at`, the room asked in, retries) stay with the message.

**Entry** (new): one thing that came into a message: a mail, a recording, a found device, a task's text.

| Field | Meaning |
|---|---|
| `text` | one line that says what it is ("Elternbrief der Grundschule, 2 Termine", "Oma, heute 14:12") |
| `sender` | a person, a room, or Mira (a task, a skill) |
| `importance`, `confidentiality`, `details` | as a message has them today |
| `expires` | for an entry without a thread; one with a thread ends with its thread |
| `key` | against duplicates: an entry with the same key replaces the one before (a task that updates its text) |
| `thread` | optional: the thread this entry leads to |

An entry belongs to exactly one message. A message reaches its threads through its entries, so **a message may lead
to several threads**:

```
Message  kind postfach   for Anna   unheard
  ├─ Entry "Elternbrief der Grundschule, 2 Termine"   → thread 1
  └─ Entry "Terminvorschläge der Praxis Dr. Kaya"     → thread 2

Message  kind voicemail  for the house   unheard
  ├─ Entry "Oma, heute 14:12"          ┐
  ├─ Entry "Ben, heute 16:40"          ├→ the answering machine's thread
  └─ Entry "verpasster Anruf, 17:05"   ┘

Message  kind simple     for Anna   unheard
  ├─ Entry "Der Gasverbrauch ist ungewöhnlich hoch"  (the gas task)
  └─ Entry "Es wird später"                          (from Ben)
```

**Thread** (exists; changed): gets the place **the house** beside a person and a room, and an **expiry of its own**,
set by whoever starts it (the code or a model), instead of only its type's timeout.

### Kinds

| Kind | What it is | Thread | Taken up |
|---|---|---|---|
| `simple` | texts: a task's text, "Sag Anna …" (the sender named), a device's fault | none | worded by the model and read out |
| `reminder` | due at a time ("Erinnere mich in 10 Minuten") | none | announced when due, in the room it was asked in, or sent privately |
| `urgent` | importance 3 | none | announced at once, also in quiet hours, escalated until heard |
| `private` | only for the person's channels | none | sent over the person's channels, never spoken or shown |
| `confidential` | for one person, spoken only under the conditions of `notifier.confidential_voice` | none | spoken when those hold, else over the person's channels |
| `reply` | what comes back to a request: a thread's question or proposal, the result of a helper that ran on after the turn | the thread it comes from, if any | said at once where it was asked, else sent privately; an answer to it goes on in that thread |
| `postfach` | a mail or document with things to do ([Mira's inbox](inbox.md)) | **one per mail**, of the person; plan and expiry by `inbox.read` | the skill `postfach`. Replaced by `appointment` and `todo` with the inbox's step 10 (decision 33) |
| `appointment` | something that takes place, to be entered into a calendar; from Mira's inbox now, from other sources later (decision 33) | **one per appointment**, of the person: a thread of the skill `termin` started by code with its fields and its proposals (one or several to choose one) | the skill `termin` |
| `todo` | something to do by a day, to be put on the person's to-do list; from Mira's inbox now (a deadline, a payment, a callback), from other sources later (decision 33) | **one per to-do**, of the person: a thread of the skill `todo` started by code with its fields | the skill `todo` |
| `voicemail` | new recordings and missed calls on the answering machine | **one**, of the house; plan by the code | the skill `anrufbeantworter` |
| `device` | a device Home Assistant found ([Found devices](found-devices.md)) | **one per device**, of the house until taken up in a room (decisions 27, 28); plan by the code | the skill `geraet-einbinden` |
| `agent_request` | a remote agent's question for missing input ([Remote agents](remote-agents.md), D) | the thread whose field or plan the remote agent fills; **one entry per question**, keyed by its call, so a second question of the same call replaces the first while that is open | gathered and announced like the kinds that wait, worded by the model with the others (no skill gives it an instruction; its entry "Hermes fragt: …" names the agent), sent privately to a person who cannot hear it; the answer goes back with `agent_answer`; done once answered, gone when its thread ends |

**Decided:**

| # | Question | Decision |
|---|---|---|
| 1 | Intent of a message | A field `kind`, always set by whoever puts the message, `simple` included. A message without a kind is refused (the caller's error, a log line); nothing is read into a missing field |
| 2 | The answering machine | Becomes a message (kind `voicemail`); that is the core of the concept |
| 3 | One per kind and receiver | Per kind and receiver at most **one unheard message**. What comes while it is unheard becomes an entry of it, so everything is said in one go, however it came. This holds for `simple` too. Whoever wants something kept apart needs a kind of its own |
| 4 | Heard, then more comes | A new message, with the new entries |
| 5 | Whose | A message, its entries' threads and the receiver are one: a person's inbox is that person's (Anna's and Ben's apart); the answering machine and found devices are the house's, their message and their thread too |
| 6 | Which kinds | A kind of its own wherever adding to one message per receiver does not work or should not be: a reminder (two due at 7 and at 18 would be one; one due tomorrow would be read out today), urgent (it would carry yesterday's notices into the night, or wait with them; its escalation follows this one message), private (another way, never spoken), confidential (spoken only under conditions). A message from a person stays `simple`, the sender on its entry; missed calls go into the `voicemail` message (the same box, the same skill, the same go) |
| 7 | A thread per mail | A mail's plan is complex in itself (appointments, a deadline, a call back): each mail keeps a thread of its own. The answering machine and found devices are alike facts worked through in one go: one thread, extended by the code |
| 8 | Asking | The model always asks for all messages first (`messages_read`). Only `simple`: the model words them as one text (`notify.compose`, as for an announcement today) and reads it out. More than that: Mira says what waits, entry by entry where they lead to threads of their own, and asks what first ("Ein Elternbrief mit zwei Terminen, Terminvorschläge der Praxis und drei Nachrichten auf dem Anrufbeantworter. Womit soll ich anfangen?"). The answer chooses the thread |
| 9 | Taking up a thread | A new tool `thread_take(id)` makes a thread the turn's own (the one every thread tool then acts on). The ids come with `messages_read` and with the threads in the context. One at a time: the chosen thread is worked through; nobody jumps between threads |
| 10 | What is still waiting | When a thread ends, the tool that ended it names the open entries of the same receiver ("Noch offen: Terminvorschläge der Praxis"), and the model offers the next. In every turn of the person (or at a satellite, of the house) one line says what waits ("Wartet noch: 2 Mails, 1 neues Gerät") |
| 11 | Expiry | A thread has its own expiry, set by whoever starts it. A mail's by `inbox.read` with its plan (until the deadline to answer, until the first appointment, else 24 hours); the answering machine's by the code: 5 days from when Mira starts or extends the thread (whoever has not listened by then looks at the box); a found device's by the code: 1 day from when Mira starts or extends it. Changed 2026-11-12: an expiry counts from when Mira starts or extends the thread, never from a date the thing carries (a recording's, a mail's, a document's); the dates `inbox.read` names are deadlines in the content, not its date. Taking up a thread extends it by its type's timeout, as today |
| 12 | An ended thread's entries | When a thread ends (done, cancelled or expired), its entries leave their message at once; a message without entries is done and withdrawn. The ring shows only what still waits |
| 13 | A thread of the house | Threads get the place "the house". A thread of the house is the same wherever it is taken up. Should it later move to the room it is taken up in, that is a transition of its own, named in a spec, with a log line; never a side effect |
| 14 | Who writes an entry's text | Who writes the plan: `inbox.read` for a mail, the code for a recording or a found device (facts of the box, of Home Assistant), the sender for `simple`. The conversation's model words "what first?" from these lines |
| 15 | Messages waiting at the update | The messages waiting in the old form are dropped when the update starts (one log line with their number); the release notes say so. No migration into entries (decided 2026-11-12) |
| 16 | What comes back at once | A kind `reply` (decided 2026-11-12): a thread's question or proposal, the result of a helper that ran on after its turn (Agents, decision 27). It does not wait, so it is not gathered; it is a message all the same: stored, announced (an answer to it finds its thread through the announcement), sent privately when it cannot be said, listed on the page. Its entry names the thread it comes from; a helper's result has none (the run has ended when it is said; a question about it has the message's text and details) |
| 17 | The kind from fields that say it | Where a caller has fields that say it (a task's verdict, a skill's schedule, `mira.send_message`, `notify.send_message`), the kind comes from them by one rule: private → `private`, confidential → `confidential`, importance 3 → `urgent`, else `simple` (decided 2026-11-12). This translates fields that are there; it reads nothing into a missing one |
| 18 | Rules per entry in a gathered message (decided 2026-11-12) | The importance threshold (`notifier.min_importance`) holds per entry: an entry below it counts as not there: it is neither announced nor read out, nor does it light the ring (as a message below it today). The own room's hold (`own_message_hold`) holds per entry: in the room it came from, the entry stays out of what is said for the hold; with none left, the message does not count there. "At most three per announcement" counts messages: a gathered message is one, with all its entries, worded as one text. Done (`message_ack`) is per entry: `messages_list` shows the entries with their ids, `message_ack` takes one out; the last one ends the message |
| 19 | Missed calls (decided 2026-11-12, step 4) | Stay as they are: no entry and no step of a plan, since there is nothing to work through. The ring shows them red by the box's sensor, and `messages_read` names them in one line ([Voicemail](voicemail.md), decision 6). Decision 6's "missed calls go into the `voicemail` message" is withdrawn |
| 20 | The answering machine's thread taken up (decided 2026-11-12, step 4) | Its type's timeout becomes one day (was 15 minutes): taken up and left, it stays a day, while the recordings are still new at the box. Not taken up, it keeps the 5 days from when Mira last started or extended it (decision 11) |
| 21 | The box's count goes to 0 (decided 2026-11-12, step 4) | Everything is heard: the thread's plan counts as done (every step ticked), so it stays open the 5 minutes a done plan stays ("nochmal" still works), and its entries leave the message at once |
| 22 | The ring's colour by kind (decided 2026-11-12, step 4) | Only `voicemail` waiting: amber, as before; anything else waiting: Mira's colour. The box's sensor stays the way to amber where no message is (the box's list could not be read) and the only way to red (missed calls) |
| 23 | The answering machine's thread is the house's (decided 2026-11-12, step 4) | The code starts it when no turn exists, so no room is its own; a turn in any room continues the same plan. `voicemail_list` finds or starts the house's thread and takes it up for the turn; "Nächste …" still joins the next turn at that satellite to it |
| 24 | The answering machine announced on entry (decided 2026-11-12, step 4) | As every message: where a room announces on entry, the `voicemail` message is said there ("Auf dem Anrufbeantworter: Oma, heute 14:12"); elsewhere the ring and the screen show it |
| 25 | New recordings when Mira starts (decided 2026-11-12, step 4) | Taken as a rise: when the watch on the box's sensor starts and its count is above 0, Mira reads the list and keeps the thread and the message as for a recording that came. Reading again adds nothing twice (the entries are keyed by the recording) |
| 26 | Taking up never shortens (decided 2026-11-12, step 5) | A thread's expiry only grows: taking it up, or any step in it, gives the later of its expiry and its type's timeout from now. Only a done plan ends sooner (its `done_after`). A mail's thread taken up keeps 3 days, the answering machine's its 5 days, a found device's its day |
| 27 | A thread of the house taken up in a room (decided 2026-11-12, step 5; decision 13's transition) | A target says where its threads belong: `bound: house` stays the house's wherever it is taken up (the answering machine, decision 23); `bound: room` started by the code at the house moves to the room of the turn that takes it up (`thread_take`, or a tool of its skill that takes it), with its satellite, and a log line names the move. It is the room's from then on |
| 28 | A found device's thread (decided 2026-11-12, step 5) | One thread per found device, as one per mail (decision 7): the list lives in the `device` message, one entry per device, and each entry leads to the device's own thread of `geraet-einbinden` with its plan (add, room, name). The code starts it at the house when the device is found (decision 27 moves it into the room that takes it up); `device_adopt` takes it; `device_ignore` ends it, and its entry leaves |
| 29 | What the model knows when it words a message (decided 2026-11-14, step 6) | Seen live 2026-11-14: three found devices were announced as "Neues Gerät gefunden: …" and nothing more; nobody answered, because nothing was offered. Step 5 made found devices a kind with entries and threads, but the wording still got only the spoken lines, and a single entry was read out as it is. What the code models, the model is told (CLAUDE.md, design rules): every announcement is worded by the model, also with a single entry, `simple` included (its entries summed up as one text). It gets each message: its kind, its entries, the threads they lead to (id, skill, plan), and for a kind with a skill (`voicemail`, `postfach`, `device`) the kind's instruction (decision 30) |
| 30 | Where a kind's instruction lives (decided 2026-11-14, step 6) | In the skill that takes the kind up, which also describes how its thread goes on: a section `## Ansage` of its `SKILL.md` (the skills stay German). Mira reads the skills at her start; the section is taken from there when a message of the kind is announced or read, and the skill is not opened for it. The three: `geraet-einbinden` names the found devices by their names (never their ids) and offers to add them, one ("Soll ich es einbinden?") or several (all, or which ones); `anrufbeantworter` says who called and when and offers to play the recordings; `postfach` says what each mail or document asks for and offers to go through it. A skill that takes up a kind without the section is a problem of the skill (as a skill's other problems, a repair); a message of its kind is not announced until it is fixed |
| 31 | When the wording fails (decided 2026-11-14, step 6) | The wording is a model call like any other, and its errors are handled as those of every model call: no answer (the model unreachable, the key refused, a server error after the retries of [Retries](retry.md), longer than 20 s) and an answer that cannot be used (no JSON, a message left out) are errors. Mira then says the sentence of a failed model, as in a turn ("Mein KI-Modell lehnt meinen Schlüssel ab. …", "Ich erreiche mein KI-Modell gerade nicht."): at an announcement at the satellite, at `messages_read` as the tool's error. The messages and their threads stay waiting; the next occasion announces them anew. No fixed form in the code does the model's job instead (before: the entries read out one after the other). Replaces the fixed form of an announcement ([Fallbacks](fallbacks.md), row 14) |
| 33 | Kinds by meaning, not by source (decided 2026-11-15) | A kind says what is to happen with its entries, not where they came from. The inbox's `postfach` said the source; its announcement could only offer to "go through" a mail. It gives way to `appointment` (into a calendar) and `todo` (onto the person's to-do list), one entry and one thread per thing, the source named in the entry's line ([Mira's inbox](inbox.md), section 8, decision 29). Decision 7's thread per mail changes to a thread per thing. Each kind's instruction (`## Ansage` of `termin` and `todo`) names what will be done ("Soll ich ihn eintragen?", "Soll ich das auf deine Liste setzen?"), never "durchgehen". How several of them are offered at once: [Mira's inbox](inbox.md), decision 31 (offered together, "ja" confirms them one after another). **Which skill takes a kind** is said by the skill itself, `takes: <kind>` in its frontmatter (`termin` takes `appointment`, `todo` takes `todo`; `anrufbeantworter` `voicemail` and `geraet-einbinden` `device` move there from the code's fixed map `store.KindSkill`), so a later skill brings its kind without a change of code. Among the switched-on skills a kind has at most one taker: two switched-on skills taking the same kind are a problem (a repair on Mira's page naming both, saying to switch one off), and until it is fixed no skill takes the kind: the inbox's reading is not offered it, and its messages are not announced (as a kind whose skill lacks `## Ansage`, decision 30). Two such skills with only one switched on are fine |
| 32 | Several kinds at once, and the afterword (decided 2026-11-14, step 6) | One rule for the announcement, `messages_read` and the afterword ("Übrigens: …" added to an answer), given to the model with the messages: with one kind whose entries lead to threads, it follows that kind's instruction and offers what it says ("Soll ich sie einbinden?"); with several such kinds (a mail with an appointment and two new devices), it names each briefly as its instruction says and asks what to start with ("Ein Elternbrief mit einem Termin und zwei neue Geräte. Womit soll ich anfangen?"), and the answer chooses the thread as in decision 8; messages without a thread are just said. The afterword is worded by the model too (before: the messages' texts after "Übrigens"); the answer it is added to waits for it, about 1 to 3 s more in that turn. When its wording fails, the sentence of a failed model is added instead (decision 31), and the messages stay waiting |
| 33 | A remote agent asks back (2026-11-14, [Remote agents](remote-agents.md), decision 5) | A kind of its own, `agent_request`, not a `reply`: a reply does not wait, while a question of a remote agent waits for its owner like a mail. It gathers per receiver like the kinds that wait and counts in the line "Wartet noch"; its entry leads to the remote agent's thread |

### What it does, step by step

1. **A recording comes** (the answering machine's sensor `_new_messages` rises; Mira already subscribes to it for the
   indicators). Mira reads the box's list (the action `get_messages` of `voicemail_list`). No open thread: the code
   starts the house's thread with the plan of the new recordings; else it adds them to the plan (missed calls are no
   steps, decision 19). Its expiry becomes 5 days from now (decision 11). Each new recording is an entry of the
   house's unheard `voicemail` message, or of a new one (decision 4), keyed by the recording, so reading again adds
   nothing twice. The count going to 0 (heard at the box or through Mira) makes the plan done and takes its entries
   out at once (decisions 12 and 21).
2. **A mail or document with things to do comes into a person's inbox**: `inbox.read` writes its plan, its entry's
   line and its expiry ([Mira's inbox](inbox.md), changed separately); the code starts the person's thread for it and
   puts the entry into the person's unheard `postfach` message, or a new one.
3. **A device is found** ([Found devices](found-devices.md)): the code starts the device's own thread of
   `geraet-einbinden` at the house, with its plan (add, room, name); expiry 1 day from now (decision 11). Its entry
   goes into the house's unheard `device` message, or a new one (decision 28). Taken up, the thread moves into the
   room that takes it up (decision 27).
4. **A simple text comes** (a task, a person, an automation): an entry of the receiver's unheard `simple` message,
   or a new message.
5. **The ring and the screen** show what waits by the kinds of the waiting messages (teal, answering machine amber,
   as today); the box's sensors trigger step 1 and stay the way to amber when the list could not be read, and to red
   for missed calls (decision 22).
6. **"Hab ich neue Nachrichten?", "Was gibt's?"**: the model calls `messages_read`. The tool gives each message with
   its kind and entries; the `simple` one worded as one text; an entry with a thread with the thread's id and skill;
   a kind with a skill with its instruction (decision 30). Its instruction: only `simple`: read it out; else word each
   kind as its instruction says and ask what first.
7. **The choice** ("erst die Praxis"): `thread_take(id)`; the turn has that thread with its plan and its skill's tools.
   Worked through until it ends; then the tool names what is still open (decision 10) and the model offers the next.
8. **Heard**: a message counts as heard once said (read out, announced, or named in the "what first?" answer). An
   announced message whose entries lead to exactly one thread joins the next turn at that satellite to it, as today;
   with several, the answer to "what first?" chooses (step 7).

The skill `anrufbeantworter` no longer names "Habe ich neue Nachrichten?" in its description; it keeps "Spiel die
Nachricht von Oma ab", "Wer hat angerufen?", "Lösch die Nachricht". The word for Mira's messages is "Nachrichten"
everywhere, also in `messages_read` (no "Benachrichtigungen").

### What it costs

| Step | Does | Costs | Turn waits? |
|---|---|---|---|
| a recording comes | read the box's list, find or start the thread, write or extend the plan, add the entries | 2 actions of Mira FRITZ!Box (TR-064 calls), a few database writes; per change of the sensor | no |
| a mail or document comes | as today plus the entry | 1 database read and write more | no |
| a device is found | as today plus the thread and the entry | a few database writes | no |
| a simple text comes | find the unheard message, add the entry | 1 database read more than today | no |
| `messages_read` | the waiting messages with their entries; the `simple` one worded; the kinds' instructions | one database query; one model call (`notify.compose`) when a `simple` message has more than one entry; the instructions from the skills read at the start (no file read) | yes; the wording about 1 to 3 s (as for an announcement today) |
| an announcement, an afterword | the model words it with the messages and the kinds' instructions (decisions 29, 32) | one model call (`notify.compose`) per announcement, also for a single entry (before: none for a single one); its prompt grows with the entries and the kinds' instructions (a few hundred characters each) | an announcement: no turn, the speech at the satellite comes 1 to 3 s later; an afterword: the turn's answer waits 1 to 3 s more |
| `thread_take` | links the turn to the thread, opens its skill | one database write | yes, milliseconds |
| the line "Wartet noch" | counts the waiting entries of the person and the house | one database query per turn; about 100 characters | yes, milliseconds |

### Errors

| What fails | What happens | Who learns of it |
|---|---|---|
| A message is put without a kind | refused | the caller's error; a log line with the caller and the text's start |
| The box's list cannot be read when a recording comes | no entry, no thread; the ring still shows the sensor's count; tried again at the next change of the sensor | log line with the error and the count |
| A thread cannot be started or extended (database) | no entry either: an entry of a kind with a skill without its thread would lead nowhere | log line; for the inbox as its spec says |
| The entry cannot be added | the thread stays; it is found on the Plans tab and by the line "Wartet noch" (it counts threads too) | log line with the kind and the receiver |
| The wording of an announcement fails (decision 31) | the sentence of a failed model is said at the satellite; the message stays waiting, and the next occasion announces it anew | log warning `could not word the announcement; it waits for the next occasion` with the error, the messages' ids and, for an answer that could not be used, its start |
| The wording at `messages_read` fails | the tool gives the error; Mira says the sentence of a failed model; the messages stay | the user; the log line of the failed tool call |
| A skill that takes up a kind has no section `## Ansage` | its kind is not announced; the messages wait | a problem of the skill in `GET /v1/status` (a repair in Home Assistant), with the skill and the missing section |
| `thread_take` with an id that is not open, or not the asker's | refused; the tool says which threads are open for the asker | the model, from the tool's answer |
| A thread's entries cannot be removed when it ends | the message stays with them; `messages_read` drops an entry whose thread has ended and marks it done | log line with the thread and the error |

### Open

1. **Later, an idea:** when Mira knows who listens at the satellite, a `private` message of that person could be
   spoken together with the `simple` one. The kind stays `private`; this would be a rule of its delivery.

## Steps

1. **Who sees which open messages**: `messages_list` by the asker's room or own chat, as reading out. MS-01 to
   MS-06 (built 2026-11-11).
2. **Kinds and entries**: the field `kind` (required), the entry, a message leading to several threads, threads of
   the house with an expiry of their own, `messages_read` with kinds and entries, the wording, "what first?",
   `thread_take`, the line "Wartet noch", an ended thread's entries leaving, the words (no "Benachrichtigungen", the
   answering machine's description), the kinds reminder, urgent, private and confidential. MS-07 to MS-11, MS-21 to
   MS-25, MS-26 to MS-30.
3. **The inbox as a kind**: a thread per mail, the entry, the expiry by `inbox.read` (after the inbox's plan is
   written by `inbox.read`). MS-12 to MS-15.
4. **The answering machine as a kind**: the house's thread on a new recording, extended, 5 days from now, the
   indicators by kind. MS-16 to MS-20, MS-34 (the start, decision 25).
5. **Found devices as a kind**: a thread per found device at the house, 1 day, its entry in the `device` message;
   taken up, it moves into the room (decision 27); taking up never shortens (decision 26). MS-31 to MS-33, MS-35,
   MS-36.
6. **The announcement knows the kinds** (decisions 29 to 31): the message with its kind, entries and threads to
   `notify.compose`, every announcement worded (also a single entry, `simple` included), the section `## Ansage` of `geraet-einbinden`,
   `anrufbeantworter` and `postfach`, the kinds' instructions in `messages_read`, no fixed form when the wording
   fails (the sentence of a failed model, the messages wait), the skill's problem without the section; several kinds
   at once and the afterword worded by the model (decision 32); the skill `geraet-einbinden` no longer quotes the old sentence.
   MS-37 to MS-44.

## Open

1. **Recognized at the satellite**: when a person counts as present at a satellite (discuss separately). Until then: only the "alone at home" rule.
