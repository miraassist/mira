# Sounds and cards on the satellite

Status 2026-10-23: built (0.6.61). Scenarios: [satellit.feature](satellit.feature).

## Goal

A skill can make Mira do more at the satellite she is talking at than speak: play a short sound (a fanfare when the
quiz went well) and put a card on its screen (the recipe's picture with the current step while cooking). Both go to
the satellite of the turn and nowhere else; where it cannot do it, nothing happens and the conversation goes on.

## The satellite of the turn

- A voice turn knows its satellite (the device that heard it). Its speaker is the `media_player` of the same device;
  its card is the ESPHome action `esphome.<device>_notify_picture` of the same device (TECHO5 firmware).
- Without a satellite (Telegram, the chat of the HA app, a background run), or when the device has no speaker or no
  card action (an Echo without TECHO5), the tool does nothing and says so; the model goes on without it.
- Rights: act on the house, as `radio_play`.

## Sounds: the tool `sound_play`

They are files in the binary, each with a name that says what it means, not where it comes from:

| Name | What it is for | Length | Source |
|---|---|---|---|
| `fanfare_big` | a big success (all right) | 4.7 s | "Tadaa.wav" by Exchanger, freesound.org/s/415504, CC0 |
| `fanfare` | a success | 1.7 s | "Tada Fanfare F" by plasterbrain, freesound.org/s/397354, CC0 |
| `too_bad` | it did not work out | 1.8 s | "jingles_NES00" from Kenney's "Music Jingles", kenney.nl, CC0 |

- The sources and licenses are in a file next to the sounds; only CC0 or public domain goes in, so the binary can
  carry them without conditions. More sounds come the same way: a file, a name by its meaning, a line in that file.
- **Argument:** the sound's name. The tool's description lists the names with what they are for; an unknown name
  plays nothing, and the tool names the sounds there are.
- **How:** Home Assistant's `media_player.play_media` with an address the satellite can fetch: Home Assistant's
  internal address (`internal_url`, else the address Mira uses for Home Assistant) and the integration's open page
  path (`/api/mira/panel/ui/sounds/<name>.mp3`); the integration passes the sound files through, as the page's own
  files. Satellites always reach Home Assistant; Mira's own address they may not know.
- **As an announcement** (`announce: true`): during a voice turn the satellite turns down everything playing in
  the background, and a sound played as media is one (TECHO5, tried 2026-10-23); an announcement plays like
  Mira's voice, at full volume, and interrupts the radio instead of replacing it.
- **Waiting:** the tool returns only when the sound is over (its length and half a second). The model speaks after
  it; a sound and speech at once do not go together, as the satellite turns a sound down while it speaks (tried on
  the Echo Show 5, 2026-10-23).

## Cards: the tool `display_card`

- **Arguments:** a title (at most 40 bytes), one line (shown on one line, about 50 characters on the Show 5; longer
  is cut with "…"), a picture (an `https://` address), and how long it stays (seconds; 0 until tapped or replaced).
- **How:** `esphome.<device>_notify_picture` with the picture full screen and the words over it, without a chime and
  without speaking them (Mira says them herself), tag `mira`. A new card replaces the one up.
- **What it is for:** something to see at a glance from across the room, never the only place a thing is said: what
  is on the card is also said, or does not matter.
- The firmware has no action to take a card down; a card goes when tapped, when its time is up or when a new one
  replaces it.

## Listening again: the tool `keep_listening`

- After an answer the satellite listens again, without the wake word, when Mira sets `continue_conversation`; until
  now only after a question or a request for a yes. Asking at the end of every cooking step sounds unnatural.
- With `keep_listening` (no arguments) the model asks for it itself, when it expects a quick word without asking
  ("weiter" after a short step). The engine sees the call and sets `continue_conversation` for that answer; it is
  called in the same round as other tools (with the card), so it costs no time.
- How long the satellite listens is its own setting (TECHO5: "Follow-up time", up to 30 seconds). Only for a quick
  word: after a long step the wake word again is fine.
- Not a rule "a plan is open, so listen": whether a quick word comes depends on the step, which only the model knows.
- Without a satellite it does nothing and says so.
- **Never after something that plays after the answer** (2026-10-29): a turn that called a tool marked "plays after"
  (`voicemail_play`) does not keep the satellite listening, whatever the answer or `keep_listening` say. The
  satellite would put the listening behind the recording, and its microphone would hear it.

## Hints on the screen wait for a quiet satellite

A hint on a screen (Mira's messages, the answering machine) never comes while its satellite is in a conversation
(listening, thinking or speaking): it would cover the turn screen, and one would not see that Mira listens or speaks
(heard 2026-10-29). It waits, watching the satellite's state (no polling), and comes once the satellite is idle.

## The skills that use them

- **quiz:** at the end, all right → `fanfare_big`, from 80 % → `fanfare`, under 50 % → `too_bad`, nothing in between.
- **vokabeln:** at the end, counting what was right the first time: all → `fanfare_big`, from 80 % → `fanfare`, else
  nothing (a round always ends with everything right, so no `too_bad`).
- **kochen:**
  - The search already gives the recipe's picture (`previewImageUrlTemplate`, with `crop-960x540` for `<format>`);
    the model puts it into the thread's `request` with the name and address.
  - After a short step (a handful of seconds' work) `keep_listening`, so "weiter" needs no wake word; not after a
    step with a waiting time.
  - With every step it reads, a card: the title "Schritt 2 von 5 · Name" (the step first: a long name is cut at the end), the line the step as a short note
    ("Teig glatt rühren · 10 Min. quellen"), the recipe's picture, until replaced.
  - At the end a last card "Guten Appetit" for a minute, which replaces the step's card; when cooking is cancelled
    likewise "Kochen beendet" for ten seconds.
  - A recipe without a picture gets no cards.
- In the quiz with several players the share counts all answers together.

## Decided

1. Mira brings her sounds in the binary; only CC0 or public domain.
2. Sounds are named by their meaning (`fanfare`, `too_bad`).
3. The sound comes before Mira's answer, and the tool waits until it is over (A); a sound after the answer (B) was
   tried and dropped: more code, and a question at the end would open the microphone into the sound.
4. Played on the satellite's own speaker, through Home Assistant's address.
5. Sounds in quiz and vokabeln as above; vokabeln without `too_bad`.
6. Cards are the picture card (full screen, one line), not the text card (three lines in big letters, no picture):
   both hold too little for a whole step, and the picture is the better sight (tried on the Show 5, 2026-10-23).
7. Sounds and cards in one spec, built together: both find the satellite of the turn the same way.
8. Listening again is the model's call, with `keep_listening`, not a question at every step and not a rule for all
   plans (2026-10-24).
