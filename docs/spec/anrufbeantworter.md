# The answering machine at the satellite – Spec

Status 2026-10-29: built (steps 1 to 4), not released. Scenarios: [anrufbeantworter.feature](anrufbeantworter.feature). Builds on
the integration Mira FRITZ!Box ([miraassist/mira-fritz](https://github.com/miraassist/mira-fritz), its spec
`docs/spec/fritz.md`) and on the indicators of [Messages](nachrichten.md).

## Goal

1. The ring of the Echo in the kitchen glows in the answering machine's colour; the Show's screen says "1 Nachricht
   auf dem Anrufbeantworter".
2. Anna: "Habe ich neue Nachrichten?" Mira: "Eine auf dem Anrufbeantworter, von Oma, heute um 14:12. Und Ben hat
   zweimal angerufen, ohne etwas zu sagen. Soll ich die Nachricht abspielen?"
3. Anna: "Ja." Mira: "Von Oma, heute um 14:12:" and the satellite plays the recording. Afterwards it counts as heard,
   at the FRITZ!Box too, and the ring goes out.

## Decided (2026-10-29)

| # | Question | Decision |
|---|---|---|
| 1 | Missed calls and messages | Shown apart: two counts on a screen, two colours on a ring |
| 2 | "Lies mir die Nachricht vor" | Mira plays the recording, saying first from whom and when. No transcription: the recording stays in the house |
| 3 | Where the FRITZ!Box is read | Only through Mira FRITZ!Box; Mira keeps no access of her own |
| 4 | A ring when several kinds wait | One colour at a time, by rank: Mira's own messages, then the answering machine, then missed calls. Asking "Gibt es Neues?" gives all of them |
| 5 | The colours | Fixed, no setting: messages as today (teal), answering machine amber, missed calls red |
| 6 | Missed calls count as seen | Once Mira has said them |
| 7 | Several messages | One after the other, Mira naming each further one on the turn screen; speaking stops the rest; no limit |
| 8 | Listening to the messages | A thread with a plan, as cooking and vocabulary: the messages are the steps (step 4) |
| 9 | Who may go on with the listening | Anyone at that satellite, as anyone may hear and delete messages with the wake word; answering never makes them the person who started it |
| 10 | The screen hint while playing | Held back on the satellite that plays, shown once with the new count when the playing ends; other screens at once |

## The skill `anrufbeantworter`

A skill (German) without settings; it needs Mira FRITZ!Box and shows a problem without it, as `telefon` does. Its
description makes the model open it on "Habe ich neue Nachrichten?", "Spiel die Nachricht von Oma ab", "Wer hat
angerufen?", "Lösch die Nachricht".

Its tools (Mira's own, calling the integration's actions):

- `voicemail_list(new_only)`: the messages, newest first (id, from whom, when, minutes, new, and the answering
  machine's name where there are several: "Anrufbeantworter privat", "Anrufbeantworter business"), and the missed
  calls not seen yet (from whom, when, how often). Facts, no sentences. Of more than three messages Mira names the
  newest three and how many more there are; the tool gives all, so "die von Oma" can be played too. Telling the
  missed calls marks them as seen (`mira_fritz.mark_calls_seen`).
- `voicemail_play(ids)`: plays messages at the satellite of the turn, one after the other (`media_player.play_media`
  with each message's `media_content_id` on the satellite's speaker, as an announcement), and marks each as heard
  (`mira_fritz.mark_heard`).
  It plays **after Mira's answer**: the tool returns at once, Mira says only from whom and when, and the recording
  starts once the satellite is out of the conversation (it waits for "responding", then for it to be idle). Waiting in
  the tool would not do: a recording may last longer than Home Assistant waits for a voice turn. Only in a voice
  conversation at a satellite; in Telegram or the app the tool says it plays only at a satellite.
- **Every step by a signal, never by a computed time** ([Signals, not timing](ha-anbindung.md#principle); heard
  2026-10-29: the recording started 7 seconds after the play command, and a wait computed from its length ticked it
  before it was heard and said "Nächste …" into it):
  - **A recording plays as music** on the satellite's speaker (`media_player.play_media`, no announcement), tried at
    the Show 2026-10-29: the speaker says "playing" at once, "idle" when it played to its end (then it is heard and
    ticked), "paused" when it was stopped. A wake word lowers it (the device ducks music for a turn) and the
    satellite understands the user over it; "Stopp" pauses it through Home Assistant's own media tool, and the rest
    stays new. As an announcement it would neither duck nor stop at the wake word (the device plays announcements
    in the foreground, outside a turn), and its microphone heard the recording. The box's minutes plus half a
    minute are a safety net, not the signal.
  - **On the screen** the device shows its player for a recording it plays, today named "Radio"; a title of Mira's
    needs a change of the firmware (the play command carries only the address).
  - **"Nächste …"** only into a quiet satellite (idle), never into a recording or a listening.
  - **No listening after the answer**: a turn that plays something afterwards never keeps the satellite listening
    (a tool marked so, [satellit.md](satellit.md)); its microphone would hear the recording (heard 2026-10-29: Mira
    answered "Ich habe nichts verstanden" to her own recording).
- **Several messages** ("Ich will eine nach der anderen hören", decided 2026-10-29): one call with all of them, in the
  order wanted, however many. Mira's answer names the first; before each further one Mira says it herself
  ("Nächste: von Ben, gestern 09:05.") as a **plain announcement** (`assist_satellite.announce` with text), and the
  next recording follows at once. **Nothing listens after it** (decided 2026-10-29): an answer is the wake word's, at
  any time, and a wake word lowers the recording. Listening after it ended the run on every answer and on every
  silence, and the model summed the messages up again. Each counts as heard once it has played to its end.
  - "Stopp" (wake word, then "Stopp") pauses the recording through Home Assistant's own media tool; the rest does not
    play, and what was not heard to the end stays new.
  - "Weiter", "nächste" while one plays: the model calls `voicemail_play` with all open messages after the one
    playing; the skipped one stays new.
  - **A new `voicemail_play` ends the run at its satellite** before anything of it plays: a replaced recording keeps
    the speaker "playing" (tried at the Show 2026-10-29), so the old run would tick a message it did not play.
  - A plain announcement shows no turn screen on a TECHO5 (by design of its firmware); the player screen of the
    recording follows at once. A title there is a firmware change (todo).
- `voicemail_delete(id)`: deletes a message at the FRITZ!Box, only when the user asks for it.

**Words**: Mira says "Anrufbeantworter", never "Mailbox" and never the box's name: "FRITZ!Box" trips the speech
synthesis on its "!" (heard 2026-10-29), so no text meant for speaking carries it.

Before playing, Mira says from whom and when, in her words ("Von Oma, heute um 14:12:"); the tool gives the facts.

**On the screen while it plays**: a satellite with an overlay (TECHO5, the action the screen hints use) shows "Nachricht
von Oma · 29.10. 14:12" with a voicemail icon while the recording plays, and takes it away when the satellite says
it ended. Playing a recording shows nothing by itself (tried 2026-10-29: clear sound, empty screen); one must see what
is speaking. Mira never fetches a recording herself: neither its length nor anything else is needed.

## Showing what is new

Mira reads the integration's two sensors by what they are, not by their names (their entity ids follow Home
Assistant's language: `sensor.fritz_box_neue_nachrichten`): the entities of the platform `mira_fritz` whose unique
ids end in `_new_messages` and `_missed_calls`. A change of either updates the indicators at once.

- **The ring** (a satellite with an indicator light, [ha-anbindung.md](ha-anbindung.md)): lights while anything waits,
  in the colour of what ranks first (4, 5).
- **The screen** (a satellite with an overlay, TECHO5): a hint of its own beside Mira's messages, with the two counts
  ("2 Nachrichten auf dem Anrufbeantworter · 1 verpasster Anruf"); taken away when nothing waits. While a satellite
  plays messages it gets no new hint: each one heard lowers the count, and the hint would cover the card of what plays.
  It gets the new count once the playing ends (decision 10, step 4).
- **In every room**: the answering machine belongs to the house, like a message to the house. Quiet hours hold back
  nothing here: a light and a screen hint wake no one.
- **"Gibt es Neues?"** names the answering machine too, after Mira's own messages.

### On Mira's page

Mira's page shows the answering machine only while something waits there; Mira shows no FRITZ!Box connection status
(an unreachable box is Home Assistant's to show: the integration's error, its sensors unavailable).

- **Start page**: the card for every day gets a line below Mira's messages: "2 neue Nachrichten auf dem
  Anrufbeantworter · 1 verpasster Anruf". None when nothing waits.
- **Messages page**: the same line at the top, as a short teaser above Mira's messages. The page lists no recordings
  of its own.
- **The link**: the line links to the FRITZ!Box's web interface, where the messages can be played. Mira takes the
  address from the box's device in Home Assistant (`configuration_url`, which Mira FRITZ!Box already sets), so
  nothing new crosses between the integration and Mira. It is the box's start page: no link that opens the
  answering machine's page directly was found. It opens in a new tab and works only at home, where the box is
  reachable. Only a web address (http, https) becomes a link.
- The counts are the two sensors' (above), the same the ring and the screen show.

## Listening as a thread (step 4)

**Why.** Heard 2026-10-29: after "Nächste: von Ben …" the satellite listens, and the user answers "weiter" or "ja".
That answer is a turn of its own: Home Assistant gives `start_conversation` a new conversation id, and a short "ja" is
too little voice to recognise anyone. The turn has no history and no open skill, so the model listed the messages
again, summed them up again, and played one. "Bitte löschen und nächste" could not know which message is meant.

What holds across turns is the **thread** ([Threads](vorgaenge.md)): listening to the answering machine is the same
as cooking or vocabulary, a list of steps worked through, with a "current" one. It becomes a thread with a **plan**
([Plans](vorgaenge.md#plans-long-processes-step-by-step)); nothing new besides two general rules for threads (below).

### The thread

- **Target**: the skill `anrufbeantworter` gets `metadata.mira.target: {plan: true, bound: room, timeout: 15m,
  done_after: 5m}`. Bound to the room ([Threads](vorgaenge.md), Assignment): the messages play at that satellite and
  belong to the house, so whoever speaks there, recognised or not, continues the same thread.
  `done_after` is new for plans (default the 30 minutes plans keep today): how long a plan stays open once all is
  ticked. The answering machine needs no "halt, doch nicht fertig"; 5 minutes keep it out of the context of later
  turns (decided 2026-10-29).
- **Start**: `voicemail_list` starts it, or finds the one running in the room. Not the model with `thread_start`: the list is facts, the code
  writes the plan, so asking costs no extra model round (decided 2026-10-29). This is new: in `kochen` a helper agent
  writes the plan, in `vokabeln` the conversation's model. The threads get a way for Mira's own code to set a plan
  and tick a box, the same storage `thread_plan` uses.
- **The plan** is written by the code from the list it just read: the **new** messages, newest first, never the
  whole history (decided 2026-10-29: one plan held 4 messages, the other 18, depending on how the model asked).
  Asking again keeps the messages the plan held, ticked once heard, and adds those that came meanwhile:

  ```
  # Anrufbeantworter
  - [ ] Oma, heute 14:12 (id 0:13)
  - [ ] Ben, gestern 09:05 (id 0:12)
  ```

  Asking again rewrites the plan of the running thread; there is never a second one.
- **Ticking**: a message played to its end is ticked by the code (`mark_heard` at the box, then its box in the plan),
  as is a deleted one. The model never ticks here.
- **In the context** (layer 7, as for every plan): done/total, the one just heard (`previous`), the one to play next
  (`current`) and after it (`next`), with their ids. "weiter", "nochmal", "die von Oma", "lösch die und nächste" are
  the model's, with these ids, no fixed phrases.
- **End**: all ticked, then 5 minutes (`done_after`); `thread_cancel` ("Stopp, das reicht"); or 15 minutes without
  anything happening.
- Missed calls are not in the plan: they are said once and then count as seen (decision 6).

### "Nächste …" belongs to the thread

Before each further message Mira says "Nächste: von Ben, gestern 09:05." as a plain announcement (decision 7). That
announcement is **the thread's**: Mira records it as an announcement on that satellite, naming the thread. A turn there
soon after, by the wake word, joins the thread by the rule that exists ([Threads](vorgaenge.md), Assignment 3a),
without becoming the person who started it: who speaks is only ever decided by the voice (decision 9). The turn then
has the skill's tools and the plan in its context: a short plan (up to ten steps) shows **every** open message with its
id ([Threads](vorgaenge.md), In the context), so "weiter" plays all the rest, not only the next.

### Two general rules for threads (to [Threads](vorgaenge.md))

1. **An announcement may name its thread directly.** Today an answer finds its thread through a message of the
   thread that the notifier announced. "Nächste …" is no message (it would land in the messages page's history), so
   an announcement record carries the thread itself. Same window, same satellite, same rule.
2. **A turn in a thread has the thread's skill open.** A turn joined to a thread (by an answer, or by the model at
   the thread's place) gets the target skill's tools, as if it had called `skill_open`. Without it the answer
   turn sees the plan but cannot play or delete.

### What each step does and costs

| Step | Does | Costs | Turn waits? |
|---|---|---|---|
| `voicemail_list` | reads messages and new missed calls, marks the calls seen, starts or finds the thread, writes the plan | 2 actions of Mira FRITZ!Box (each a TR-064 call to the box), 1 more when calls are new; database writes | yes, well under a second |
| `voicemail_play` | checks the ids against the list, queues them | 1 action (`get_messages`) | yes, milliseconds |
| per message, after the answer | screen card, play, wait for the speaker to play and go idle, `mark_heard`, tick, card away | 2 overlays, 1 `play_media`, 1 `mark_heard`; no download by Mira | no |
| per further message | wait for a quiet satellite, record the announcement, "Nächste …" as a plain announcement (the call returns as it ends) | 1 action; nothing listens after it | no |
| watching | the speaker (started, played, stopped), the satellite (quiet) | a **state subscription** on the entity for each wait ([Watching states](ha-anbindung.md#principle)) | no |
| an answer turn | the plan window in layer 7, the skill's 3 tools | about 300 characters and 3 tools more in that turn only | — |
| every other turn at the place while the thread runs | the plan window in layer 7 | about 300 characters | — |

Nothing grows with the number of messages in a turn.

## Steps

1. **Reading and playing**: the skill and its three tools.
2. **Showing**: ring and screen, by the two sensors.
3. **On Mira's page**: the line on the start page and the messages page, with the link.
4. **Listening as a thread**: the plan, ticking, "Nächste …" as the thread's announcement, the two rules for threads,
   the screen hint held back while playing.

## Later

- Playing a message in Telegram or the app (as a voice message).
- A summary of a message, if the household wants it (a model hears the recording).
