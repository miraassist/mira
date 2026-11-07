# Music quiz – Spec

Status 2026-11-07: steps 0 to 3 done: the state table checked, `clip_play` built, the skill written and played by the
maintainer at the Show, its English text and the guide's section written; released in 0.18.0. Scenarios:
[music-quiz.feature](music-quiz.feature). Builds on [Threads](threads.md) (a thread with a plan, as the skill
`quiz`), on the playback of the [answering machine](voicemail.md) (plays after the answer, titled, as music) and on
[Sounds and cards](satellite.md).

## Goal

A game like "Hitster" at a satellite:

1. Anna: "Lass uns ein Musik-Quiz spielen, 80er." Mira: "Was wollt ihr raten: Interpret, Titel, das Jahr oder
   alles?" – "Das Jahr." Mira: "Titel 1 von 10:" and the satellite plays 30 seconds of a song. Its screen says
   "Musik-Quiz · Titel 1 von 10", never the song.
2. Anna, with the wake word, while it plays: "1984!" Mira: "Fast: 1983, Nena mit 99 Luftballons." A card shows the
   cover, title, artist and year. "Titel 2 von 10:" and the next song plays.
3. After ten songs: a fanfare by the share of right answers, the points, and whether another round is wanted.

## Decided (2026-11-06)

| # | Question | Decision |
|---|---|---|
| 1 | How a song is played | Deezer's 30-second preview, played by Mira on the satellite's speaker with a neutral title, through a new general tool `clip_play` (below). Not through Music Assistant: a TECHO5 device shows Music Assistant's title, artist and cover on its player screen (`nowplaying_sensors.go`, `playingCarried`), and a cut after 60 seconds would be a timer, not a signal |
| 2 | How the songs are chosen | The model plans them from its own knowledge, by genre or decade; Deezer only gives the clip. The year is the song's first release, from the model's knowledge, not Deezer's date (that is the album's, often a reissue: "Take on Me" 2007-01-29 for the track, 1985-06-12 for the album; "Billie Jean" from "Thriller 25") |
| 3 | The reveal | A card (`display_card`): the album's cover, title, artist and year |
| 4 | After the clip | The players answer with the wake word, also while the clip plays; nothing listens after it (as the answering machine) |
| 5 | Departure from "media through Music Assistant" ([Media via MA](skills-plan.md#3-music-via-music-assistant)) | Accepted: Music Assistant offers neither a hidden title nor a clip. Deezer's public API needs no account and no setting |
| 6 | Checking the year (changed 2026-11-06: before, MusicBrainz won unless one of its recordings had the model's year, which made "Take on Me" 1984) | No song database of Mira's own. Before a song plays, its year is checked at MusicBrainz by the song's ISRC (which Deezer gives with every hit): the earliest `first-release-date` of the recordings. Where it differs from the model's year by more than one year, MusicBrainz wins; within a year the model's stays (a single and its album a year apart: "Take on Me" has 1984, 2003 and 2020 at MusicBrainz, the hit is 1985, the first version 1984). Tried 2026-11-06: "Another One Bites the Dust" 1980-06-24, "Billie Jean" 1982-11-30, "99 Luftballons" 1983-01-14, all right |
| 7 | Listening while a clip plays | Never: while a clip of an earlier answer still plays at the satellite, a turn there does not keep it listening, even after a question (the engine asks `clip_play`'s player). Seen 2026-11-06: "Wie steht es?" during a clip ended with a question, and the satellite listened into the music |
| 8 | When Deezer and MusicBrainz are asked | At the start, for all songs at once: every search in one model round, every year check in the next; the plan keeps each song's Deezer id, cover and checked year. Per song only the fresh preview (`api.deezer.com/track/ID`), together with the card and the plan in one round. Seen 2026-11-06: searching and checking per song, one tool per round, took 3 to 7 rounds and 10 to 32 seconds per answer |
| 10 | Keeping a song's result | `thread_plan` `check` with a `note` ([Threads](threads.md), Tool `thread_plan`): the song ticked and "→ 1 Punkt" appended, without writing the plan anew. Measured 2026-11-06: the round that rewrote the plan took 23.5 s with Gemini (570 tokens out) |
| 9 | The last clip at the end | When a thread's plan becomes done (every step ticked), its clip stops, as when the thread ends; the thread stays open for "noch eine Runde?". So the closing question keeps the satellite listening (decision 7 no longer holds it back), and the sound plays without a clip under it. Seen 2026-11-06 at the Show: the last clip still played at the end, and "Möchtest du noch eine Runde spielen?" was not listened to |

## Building blocks

- **The concept: a thread with a plan**, as the skill `quiz`: the songs are the plan's steps, with their answers;
  points in the plan; the current step in the context. No departure.
- **Playing: as the answering machine plays a recording** (`voicemail_play`): after Mira's answer, titled on the
  player screen, as music (the wake word ducks it), the run belongs to the thread. Departure: the answering
  machine's tool plays only its recordings; a clip from the web needs a general tool, `clip_play`.
- **Reading Deezer and MusicBrainz: `web_page`**, as `fahrplan` reads its JSON. No tool of its own.
- **Reveal and end: `display_card`, `sound_play`, `thread_cancel`**, as `quiz` and `kochen` use them.

New is only `clip_play`. Everything else is the skill's prose.

## The tool `clip_play(url, title, subtitle)`

Plays a short clip at the satellite of the turn, after Mira's answer, under the title it is given. General: any
skill that plays something short (a song, an animal's call) uses it.

1. **Only in a voice conversation at a satellite.** Elsewhere (Telegram, the app) it says that it plays only at a
   satellite, and plays nothing. A satellite without a speaker: it says so.
2. **The address:** `http` or `https`, else the tool's error. Mira does not fetch it; Home Assistant passes it to the
   speaker.
3. **It returns at once** with "plays after your answer". The turn does not wait for the clip; it is marked
   "plays after" (`PlaysAfter`), so the satellite does not listen after the answer
   ([satellite.md](satellite.md), Listening again).
4. **A clip takes over the satellite:** a clip of an earlier call still playing there stops (`media_stop`), and only
   the newest plays. Replacing one that plays keeps the speaker `playing` (tried at the Show 2026-10-29), so the old
   run is ended by Mira's own call, never by the speaker's state.
5. **After the answer** (the satellite goes `responding`, then `idle`; the same wait as the answering machine):
   - where the satellite can title its player (`esphome.<node>_media_title`), Mira titles it with `title` and
     `subtitle`, source "Mira". If that fails, the clip plays all the same, untitled, and the log says
     `could not title the player` with the error;
   - `media_player.play_media` with the address, as music (no `announce`). If that fails, the log says
     `could not play a clip` with the address's host (not the whole address: it may carry a token), the speaker and
     the error; the run ends, and Mira writes "the last clip could not be played" into the thread's log. The model has already
     answered, so the user hears nothing: the next turn's model learns it from the thread's context ("the last clip
     could not be played") and says it.
6. **Watching the end** by the speaker's state, with a state subscription on the speaker (the same watch as the
   answering machine's): `idle` is played to its end, `paused` is stopped. The log says `clip played` with how it
   ended. Two safety nets, not signals: a clip that does not start playing within a minute, and a watch with no end
   after 10 minutes, end the watch, and the log says `clip watch ran out` with which. If the speaker cannot be
   watched, the log says `could not watch the clip play`. The clip has no length Mira knows, and none is computed.
7. **The run belongs to the turn's thread**, if there is one: when the thread ends (done, cancelled, expired), or when
   its plan becomes done (every step ticked; the threads tell it once, decision 9), the clip stops.

**Rights:** `ActOnHouse`, as `sound_play`. The hooks see it as every tool call.

### What the screen shows

- A TECHO5 device shows its player with the title and subtitle, no cover, no artist (tried on the fake Show and the
  Show 2026-11-06: during the clip the Show's sensors said title "Musik-Quiz", artist "Titel 1 von 10", source
  "Mira", and nothing of the song; empty again when it ended). The preview's ID3 tag is skipped by its firmware and not shown (`mp3stream.go`, `skipID3`).
- A satellite without the title action plays untitled; Home Assistant sends it only the address.
- The speaker's entity in Home Assistant carries the address (`media_content_id`), not the song; only someone reading
  Home Assistant could guess from it.

## The skill `musikquiz` (German)

`target`: `plan: true`, `bound: room`, `timeout: 1h`, as `quiz`. No settings. The guide, in prose
([skills/musikquiz/SKILL.md](../../skills/musikquiz/SKILL.md)); tools that do not wait for each other are called
together in one round:

1. **Clarify briefly**, one question, only what is not said: genre or decade (or "gemischt"), what to guess
   (`Interpret`, `Titel`, `Jahr`, `Jahrzehnt` or `alles`), who plays (names; alone, none). As many songs as asked
   for; ten without a wish.
2. **Choose the songs**: well-known songs of the genre or decade, different artists, the year of the first release;
   no live, remix or cover versions.
3. **Search and check them all at once** (decision 8):
   - one round: for each song `web_page` on `https://api.deezer.com/search?q=ARTIST%20TITLE&order=RANKING&limit=2`;
     of the hits the one whose artist and title fit and whose title has no addition ("Live", "Acoustic", "Remix", a
     year): its `id`, `preview`, `isrc` and `album.cover_xl`. No fitting hit or no preview: another song;
   - the next round: for each song `web_page` on `https://musicbrainz.org/ws/2/isrc/ISRC?fmt=json`: the earliest
     `first-release-date` of its recordings; it wins when it differs by more than a year (decision 6).
4. **Write the plan** (`thread_plan` `write`), in the same round as the first song: "# Musik-Quiz: 80er · Jahr", the
   players ("Spieler: Anna"), each song a box with answer, Deezer id and cover: `- [ ] a-ha – Take on Me (1985) · id
   664107 · Cover https://…`. No points line: each song gets its result when it is rated, and the points are counted
   from them (decided 2026-11-06, after a rewrite lost a point of a counter). Several players take turns, the name
   before the song. **Whose answer:** alone, every answer is the player's, whether the voice is recognized or not;
   with several, the answer is for whoever's turn it is in the plan (seen 2026-11-06 at the Show: two answers with an
   unknown voice, and the model did not count one of them).
5. **Play a song**: say only "Titel 3 von 10:" (with the player's name, with several), never the song, and call
   `clip_play` with its preview, title "Musik-Quiz", subtitle "Titel 3 von 10". For every song but the first, the
   preview comes fresh from `api.deezer.com/track/ID` (its address expires after about 15 minutes).
6. **Rate the answer** against the song that is "Jetzt" in the plan, never by the conversation's history: each person
   has a history of their own, and what another player answered is not in it (seen 2026-11-06 at the Show: one
   answer was taken for the son's voice, and the next answer was rated against the song before; tried again on the
   fake Show with Anna and Martin taking turns, all three rated right). Then as `quiz` does: right (also roughly, a small slip) or the right answer. A point for the artist,
   the title, the exact year or the decade; in `alles` one each for artist, title and year (±1). Then in one round:
   the card (title "Songtitel · Interpret", line the year, picture the cover from the plan, 20 seconds), the song
   ticked with its result as a note (`check`, `note` "1 Punkt" or "0 Punkte"; up to 3 in `alles`; decision 10), and
   the next song's fresh preview. Then the next song (5). (Seen 2026-11-06 before the note: a rewrite set a points
   counter back to 0, and a `check` without the result lost a point.)
7. **Words:** "weiter" or "weiß ich nicht": the answer, no point, next song. "Nochmal": the same song again, with a
   fresh preview, not rated. "Wie steht es?": the points, counted from the rated songs. "Stopp" or "Pause": Home Assistant's pause; the game stays.
   "Abbrechen", "Hör auf zu spielen": `thread_cancel`. A note in the thread that the last clip could not be played:
   Mira says it and asks whether to try again.
8. **End**: the last answer is rated exactly as in 6 (card, the last song ticked with its result), only
   without a next song; then, in the same answer, the points counted from the plan, the sound by the share of the
   possible points, the words with the points, and another round (new plan, new songs) or `thread_cancel`. (Seen
   2026-11-06 with "in the next step": the model ended the turn with "Richtig!" and left the end out.) (Seen 2026-11-06 at the Show: the guide said "after the last song
   first a sound", and the model played the sound instead of writing the plan; the last song stayed open, and the
   following "Nein" was rated as its answer.) Ticking the last song stops its clip (decision 9), so the closing question is listened to.
   When the model calls the sound in the same round as the plan, the speaker may still report the clip for a moment
   ([satellite.md](satellite.md), Sounds: the device's sensor followed the speaker by about a second, 2026-11-06), and
   the sound then plays as an announcement; nothing else changes.

### Interruptions

| What happens | Who does it | Result |
|---|---|---|
| The wake word while the clip plays | the satellite | the clip is ducked, the user is heard over it; it carries on |
| An answer while the clip plays | the model | rates, shows the card, calls `clip_play` for the next song: the clip stops, the next plays after the answer |
| "Stopp" | Home Assistant's pause | the clip stops (`paused`); the thread stays |
| A question while the clip plays ("Wie steht es?") | the engine | the answer may end with a question, but the satellite does not listen while the clip plays (decision 7) |
| "Nochmal" | the model, `clip_play` with the same song | as an answer, without rating |
| "Weiter" | the model | the answer, no point, next song |
| "Abbrechen" | `thread_cancel` | the thread ends, the clip stops |
| Silence | nobody | the clip ends (`idle`); the thread waits until it expires (1 h) |
| An unknown voice | nobody | anyone at the satellite may answer, as in `quiz`; the plan says whose turn it is, not the voice |

### When a step fails

| Step | Fails how | What happens, who learns of it |
|---|---|---|
| Deezer search (start) or fresh preview (per song) | `web_page` fails or Deezer answers an error | the model says that the song cannot be fetched right now and asks whether to go on; `web_page` logs the failure ([Web search](web-search.md)) |
| Deezer search | no fitting hit, or the hit has no preview (seen 2026-11-06: "Don't Stop Believin'" without one) | the model replaces the song in the plan by another of the same genre or decade and says nothing about it |
| MusicBrainz | `web_page` fails, MusicBrainz answers an error (it allows about one request a second; it asks for a user agent that names the program, `web_page` sends a browser's, which it answered on 2026-11-06), or the ISRC is unknown there | the plan's year stays; the model says nothing about it; `web_page` logs a failure. If MusicBrainz refuses `web_page` for good, that is discussed, not worked around |
| `clip_play` | not at a satellite | the tool says so; the model says the quiz plays only at a satellite with a speaker |
| `clip_play` | titling fails | plays untitled; log line `could not title the player` |
| `clip_play` | `play_media` fails | log line `could not play a clip`; the thread's context says it; the next turn's model says it |
| `clip_play` | no state comes in 10 minutes | the watch ends; log line `clip watch ran out` |
| `display_card` | fails | the tool's error; the model has said the answer already, the game goes on |

## State table

Transitions of one song at a TECHO5 satellite. The skill's flow is built only when every row is checked.

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 1 | answer said | the turn's answer | satellite `responding`, then `idle` | state subscription on the satellite | 2026-10-29 (answering machine, Show) |
| 2 | player titled | `esphome.<node>_media_title` | speaker attribute `media_title` | none needed (fire and forget); the state history shows it | fake Show 2026-11-06; Show 2026-10-29 (answering machine) |
| 3 | clip starts | `media_player.play_media`, Deezer preview | speaker `playing` | state subscription on the speaker | fake Show and Show 2026-11-06 |
| 4 | clip played to its end | the preview ends (30 s) | speaker `idle` | state subscription on the speaker | fake Show 2026-11-06 (30.2 s); Show 2026-11-06 (30.3 s) |
| 5 | wake word during the clip | the user | satellite `listening`, the clip ducked | not needed: the turn arrives | 2026-10-29 (answering machine, Show) |
| 6 | stopped | "Stopp": Home Assistant's pause; `thread_cancel`: `media_stop` | speaker `paused` | state subscription on the speaker | 2026-10-29 (answering machine, Show) |
| 7 | replaced | `clip_play` while one plays | none (speaker stays `playing`) | Mira's own call ends the old run | 2026-10-29 (answering machine, Show) |

## What each step does and costs

Measured on the fake Show with Gemini Flash, 2026-11-06 (four songs; a model round took 2 to 24 seconds, the same
kind of round varied that much).

| Step | Does | Costs | Turn waits? |
|---|---|---|---|
| start | clarifying question if needed; N Deezer searches in one round, N MusicBrainz checks in the next; plan and first `clip_play` in one round | 2 × N HTTP requests (Deezer about 0.1 s, MusicBrainz about 0.07 s each); about 4,400 + 500 characters per song in the turn; 4 model rounds (19.8 s for four songs) | yes |
| per answer turn | card, the song ticked with its result and the next song's fresh preview in one round; then `clip_play` with the words | 1 HTTP request (about 0.05 s), about 5,000 characters in the turn; 1 `display_card`, 1 `thread_plan` `check`; 2 to 3 model rounds (8.9 to 12.6 s measured with the note, before 7 to 32 s with the whole plan written anew) | yes |
| per song: `clip_play` | stops an older clip, queues the new one | 1 `media_stop` if one plays | yes, milliseconds |
| per song, after the answer | title, play, watch | 1 `media_title` (if offered), 1 `play_media`, 1 state subscription on the speaker; no download by Mira | no |
| every turn of the game | the plan in the context | about 180 characters per song (answer, id, cover address) | — |

What grows with the number of songs: the start (two requests and about 4,900 characters per song) and the plan in
every turn's context. A turn's tool results do not carry over to the next turn.

## Steps

0. **The real Show** (rows 3 and 4), done 2026-11-06: one Deezer preview ("Billie Jean") through Home Assistant on
   the Show's speaker: `playing` at 10:55:59.1, `idle` at 10:56:29.4; the now-playing sensors named "Musik-Quiz" and
   "Titel 1 von 10", not the song; the screen, as the maintainer saw it, neutral too.
1. **The tool `clip_play`** (built 2026-11-06), from the answering machine's playback (after the answer, title, play, watch, take
   over, end with the thread). Scenarios MQ-01 to MQ-07; no listening while a clip plays (decision 7), MQ-09; a done
   plan stops its clip (decision 9), MQ-10; a note on a ticked step (decision 10, in `thread_plan`), MQ-11.
2. **The skill `musikquiz`**, German. Scenario MQ-08 (an eval scenario: plan, search, `clip_play`, rating, card).
   Run on the fake Show 2026-11-06 with Gemini Flash: start, answers while the clip plays, "nochmal", "weiter",
   "weiß ich nicht", "Wie steht es?" during a clip (no listening), "Stopp" (paused, game stays), silence (the clip
   ends, a later answer counts), "Abbrechen" during a clip (stops), the end with the sound and "Nein danke"; an
   unknown voice in every turn (the fake has none). Again with the per-song results, through Mira's conversation API
   with Anna and unknown voices taking turns, to the end: three rated right, 3 of 3, `fanfare_big`, "Nein" ends it
   (the last answer took 33 s: card and plan, then the sound, which the turn waits for, then the words). Next: the
   maintainer at the Show.
3. **English text** of the skill (`SKILL.en.md`), and the guide: a section on the music quiz.

## Later

- Full songs (60 seconds) through Music Assistant, if TECHO5 gets a way to hide what Music Assistant plays.
- Spotify, if Deezer is not enough.
