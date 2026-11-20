# Music with Music Assistant – spec

Status 2026-11-20: steps 1 and 2 built (on live, not released); step 3, the run on live, is open; step 4 (part 3) built, not on live; step 5 (part 4, radio) built, tried on the test HA; step 6 (part 5, `music_queue`) built, not on live; step 7 (part 6, `music_more`) built, not on live; step 8 (part 7, ducking) built, its setting back changed after live (decision 35), the players that lower themselves recognised by their manufacturer (decision 39), not run end to end on the test HA; step 9 (part 8, `music_seek`) built, not on live. Scenarios: [music-players.feature](music-players.feature).

## Goal

Music is played with Home Assistant's own tools `media_player__search_media` and `media_player__play_media`. They
work only with a player that can search and play from Music Assistant. The model does not know which players those
are: Home Assistant's house structure names every exposed player with its area, not the integration it comes from.
Seen on live (2026-11-20): "Spiel die Musik in der Küche" went first to "Show Küche Speaker", which is no Music
Assistant player, and failed ("No entities supported the required features"); only the second call reached the
kitchen's Music Assistant player.

Part 1: Mira tells the model which exposed players are Music Assistant's, per area, so that it picks one of them for
music. Part 2: "Nimm die Musik mit in die Küche" moves what plays, with its queue and position, to another room;
today Mira can only pause and start the artist anew elsewhere (seen on live 2026-11-20: another song). Further ideas
(grouping rooms, queueing, more of the same, favourites, announcements that lower the music) come later, each with
its own decision.

## The concept it hangs on

- **The house structure** (`internal/ha/structure`): Mira reads Home Assistant's registries (areas, devices,
  entities) and keeps a snapshot; an entity's row has its integration (`platform`). It is rebuilt when the
  registries or the exposure change ([Context](context.md), 1.3).
- **Layer 1 of the prompt**, the system message ([Context](context.md), 1): rules, Home Assistant's house structure,
  the house profile. It is the shared prefix of every channel; it changes only when the house changes.

## Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | What Mira adds | Which exposed players come from Music Assistant, per area, as a section of layer 1 after Home Assistant's house structure; with one sentence that music is played on one of them (A) |
| 2 | A default player per room | Not part of it: where an area has several Music Assistant players the model picks one, or the user names it. Asking back is not wanted |
| 3 | Not chosen | Mira rewriting a call to another player (a silent intervention); only an instruction "name the area, not a player" (fails in an area with several players) |

## What it does

- **Which players:** the entities of the domain `media_player` whose registry row has `platform: music_assistant`,
  exposed to Assist, not disabled. A player of another integration (the speaker of a satellite, a TV) and one not
  exposed are not named: the model could not play on it with Home Assistant's tools anyway.
- **The section**, in the language of the turn, after the house structure and before the house profile:

  ```
  ## Musik (Music Assistant)
  Musik spielst du mit media_player__search_media und media_player__play_media nur auf diesen Playern, mit ihrem
  Namen; andere Lautsprecher können keine Musik aus Music Assistant spielen.
  - Wohnzimmer: Onkyo, Wohnzimmer
  - Küche: Küche
  - ohne Bereich: CarPlay
  ```

  A player's name is the one Home Assistant shows (its label, as in the house structure); the areas in the order of
  their names, the players within an area too. Without any such player the section is not there.
- **When it changes:** it is part of the shared prefix and changes only with the house structure: a Music Assistant
  player added, removed, renamed, moved to another area, exposed or no longer exposed. Then the prefix hash changes as
  for any change of the house; otherwise not.

## What the model is told

The section is the instruction itself ("What the code models, the model is told"): it says which players play music
and that the others cannot.

## Part 2: taking the music along (`music_move`)

### Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 4 | Where the tool comes from | A tool of Mira's own, as `radio_play`, offered in every turn while Home Assistant has an exposed Music Assistant player (1A). Not chosen: a script of a skill `musik`, which the model would open first |
| 5 | Where the music comes from | Mira finds it herself: the Music Assistant player that plays; with several, the one in the area the user speaks in; otherwise the tool names them and the model asks (2A). Not chosen: leaving it to Music Assistant ("the first playing player") |
| 6 | Where it goes | A room or a player: a room with one Music Assistant player is that player; with several the model names one (3A), knowing them from part 1 |

### What Music Assistant does (checked on live, 2026-11-20)

`music_assistant.transfer_queue` with the target player and `source_player`, without `auto_play`:

| # | Transition | Signal | How Mira learns of it | Checked on live |
|---|---|---|---|---|
| T1 | transfer called | HTTP 200 / the action's success | the call's answer, under 2 s | 2026-11-20 18:16: Show Five (kitchen) → Dot 2 (Bad unten) |
| T2 | the target plays | the target's state `playing`, the same title, the position carried on (17 s after 17 s in the kitchen) | not waited for: the action's success is the result | as T1 |
| T3 | the source stops | the source's state `idle`, no title | not waited for | as T1 |

The target keeps its own volume. Nothing waits for a state: the tool answers when the action succeeded.

### What the tool does

`music_move(to, from?)`, described to the model as taking the music that plays along to another room or player
("Nimm die Musik mit in die Küche", "Mach die Musik im Bad weiter", "Hol die Musik her", "Verschieb die Musik
hierher", and "Lass die Musik hier weiterlaufen" when it plays in another room; added 2026-11-20 after live, where
that sentence was taken as "unpause").

1. **The Music Assistant players** are those of part 1. Without one the tool is not offered.
2. **The target** (`to`): a player's name (as the house structure has its names and aliases) or an area (its name or
   alias). An area with one Music Assistant player is that player; with several, the tool answers with their names
   and nothing is moved, so the model names one; an area or name without one: the tool says so. A player named like
   an area ("Wohnzimmer") is that player: a name wins over an area.
3. **The source** (`from`, optional; a room or a player as for the target): without it, Mira reads the states of the
   Music Assistant players (one `GetStates`, filtered to them). The players that play are the candidates; when none
   plays, those paused. One candidate: that one. Several: the one in the area of the turn's satellite; when that is
   none of them, the tool answers with their names and areas, and the model asks. None: the tool says that no music
   plays.
4. **Source and target the same**: the tool says that it plays there already; nothing is called.
5. **The move:** `music_assistant.transfer_queue` with the target as entity and `source_player`; the answer names
   both by their names and areas, for the model to say in its words.

### Its errors

| What fails | The model gets | The log gets |
|---|---|---|
| Reading the states fails | the tool's error: the players cannot be read now | one line with the error |
| Home Assistant refuses the move | the tool's error with Home Assistant's message | one line with the source, the target and the error |
| An unknown room or player, several in an area, none playing | a sentence saying so, with the names to choose from | nothing (no error) |

### Its costs

Per call: one `GetStates` filtered to the Music Assistant players (only when `from` is not given) and one
`call_service`; the turn waits for both, under 2 s on live. Nothing runs afterwards.

## Part 3: a player more, a player less (`music_group`)

### Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 7 | Fixed groups | Set up in Music Assistant's settings, not by voice. Mira only adds a player to what plays and takes one out again ("Spiel das auch im Bad", "Nimm das Bad wieder raus") |
| 8 | "Überall" | Every Music Assistant player |
| 9 | To which player the others are added | The one that plays, found as the source of `music_move` (part 2) |
| 10 | A room with several Music Assistant players | All of them are added (1A) |
| 11 | Taking out the player that plays | Mira does what Music Assistant does with `unjoin` on it (2A): the music goes on on the others, there it stops (row G3, checked on live) |
| 12 | A fixed group player plays (set up in Music Assistant, such as `werkstatt_stereo`) | Mira adds to it all the same; what Music Assistant refuses comes back as Home Assistant's error (3A). Adding to a fixed group works: tried by the maintainer, 2026-11-20 |
| 13 | Volume | Who joins keeps its own volume (4A), as the target of `music_move` |

### What Home Assistant does

`media_player.join` on the player that plays, with `group_members` the players to add; `media_player.unjoin` on a
player to take it out. Both are Home Assistant's actions; the Music Assistant players report the feature
(`GROUPING`). Which players can play together is Music Assistant's matter (an Echo with another kind of speaker, say);
what it refuses comes back as Home Assistant's error.

| # | Transition | Signal | How Mira learns of it | Checked on live |
|---|---|---|---|---|
| G1 | a player added | the join's success; the playing player's `group_members` holds it | the call's answer | 2026-11-20 19:30 on live: `join` on Show Five (playing) with Dot 2, HTTP 200; after 4 s both `playing` the same title, both list `[show_five, dot_2]` as `group_members`, each keeps its volume |
| G2 | a player taken out | the unjoin's success; it is no longer in `group_members` | the call's answer | open |
| G3 | the player that plays taken out | the leader stops (`idle`), the other member plays on alone, the group is gone (`group_members` empty) | the call's answer; the states after it | 2026-11-20 19:30 on live: `unjoin` on Show Five, HTTP 200; after 4 s Show Five `idle`, Dot 2 `playing` the same title at 40 s, both without members |
| G4 | a player added to a fixed group player | the join's success | the call's answer | 2026-11-20, tried by the maintainer: works |

### What the tool does

`music_group(add?, remove?)`, described to the model as adding rooms or players to the music that plays, or taking
them out ("Spiel das auch im Bad", "Mach die Musik überall an", "Nimm das Bad wieder raus").

1. **`add`**: rooms or players, as the target of `music_move`; "alle" (or "überall") is every Music Assistant player
   but the one that plays. A room with several players: all of them. The player that plays is found as the source
   of `music_move`; when none plays, the tool says so. Then one `media_player.join` on it with all of them.
2. **`remove`**: rooms or players; one `media_player.unjoin` per player. A player that is not in the group is left
   out of the call and named in the answer.
3. **The answer** names what was added or taken out, with the rooms.

### Its errors and costs

As `music_move`: reading the states fails, Home Assistant refuses (the tool's error with its message, a log line), an
unknown room or player, none plays (a sentence). Per call one `GetStates` filtered to the Music Assistant players and
one `join`, or one `unjoin` per player taken out; the turn waits for them.

## Part 4: radio through Music Assistant

### Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 14 | How radio plays where there are Music Assistant players | As music: Home Assistant's `media_player__search_media` and `media_player__play_media` on a Music Assistant player. Mira's own `radio_play` ([Radio](radio.md)) is offered only while Home Assistant has no exposed Music Assistant player. Reason: what can be taken along or grouped must not depend on whether music or a station plays; nobody can be told why one moves and the other does not |
| 15 | Not chosen | Keeping `radio_play` beside Music Assistant (radio stays on the satellite's own speaker, which Music Assistant does not know: no `music_move`, no `music_group`); `radio_play` playing on the Music Assistant player (the station's name for the screen does not reach the device, see below) |

### What Music Assistant does (checked on the test HA, 2026-11-20)

| # | Transition | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|
| R1 | "Spiel SWR1" at the Show, with its Music Assistant player exposed | the model calls `search_media` and `play_media` on the Music Assistant player; it plays the station (`media_content_id` `radiobrowser://radio/…`, `media_album_name` the station) | the tools' answers | 2026-11-20 on the test HA, said by the maintainer: it plays |
| R2 | the station on the device's screen | Music Assistant streams to a TECHO5 device over Sendspin, not `play_media`: the device gets title, artist, album and a picture; for a station the album is its name, the artist is empty | — (the device's matter) | 2026-11-20: the device's log `sendspin now playing … album=SWR3`; the screen shows "Music Assistant" on top and the station's logo as the background |
| R3 | `esphome.<node>_media_station` before Music Assistant plays | the device takes the name for the next `play_media`, which does not come over Sendspin | — | 2026-11-20: logged by the device (`the next play_media is a station`), no effect on the screen |

So the station's name on a TECHO5 device's screen is a matter of its firmware (showing the album as the station when
no artist comes); Mira calls nothing for it on this path.

### What changes

1. **`radio_play`** is offered only while there is no exposed Music Assistant player (the same condition as
   `music_move`, negated). A house without Music Assistant keeps it as before, with the station's name on the device.
2. **The section of part 1** says that music and radio stations are played on these players ("Musik und Radiosender
   spielst du …"); it is the instruction for the model, no other text changes.

### What is lost against `radio_play`, where Music Assistant plays

- The search once more without the space ("SWR 3"): Music Assistant's search is the matter of Home Assistant's tool.
  Whether it finds "SWR 3" is to be checked on live.
- The station's name through `media_station` (row R3); see above.

### Its errors and costs

Nothing of Mira's own: the search and the play are Home Assistant's tools, their errors come back in the turn as for
music. No call of its own; the condition reads the house structure.

## Part 5: queueing (`music_queue`)

"Spiel danach Bohemian Rhapsody", "Setz das auf die Liste": Home Assistant's own tools cannot do it.
`media_player__play_media` has only `media_content_id`, `media_content_type` and the player (name, area or floor),
no enqueue: every play replaces the queue (checked on the test HA, 2026-11-20, Home Assistant 2026.11.1).

### Decided (2026-11-20, the maintainer)

| # | Question | Decision |
|---|---|---|
| 16 | Where the tool comes from | A tool of Mira's own, as `music_move`, offered while Home Assistant has an exposed Music Assistant player; the call is Music Assistant's `music_assistant.play_media` with `enqueue`, which Home Assistant's Assist tools lack |
| 17 | What is queued | A hit of `media_player__search_media`: the model searches first, on the player that plays, and passes the hit's `media_content_id` (a Music Assistant URI) and its `media_content_type`. Mira searches nothing herself |
| 18 | Where | The player that plays (or is paused), found as the source of `music_move` (part 2); a room or player the user names, as `music_move`'s `from` |
| 19 | When | "next" (after the current item: "Spiel danach …", "Als Nächstes …") or "end", the default ("Setz das auf die Liste", "Häng … hinten an", "Reih … ein"); Music Assistant's `enqueue` `next` and `add` |
| 20 | Nothing plays | No queue to add to: the tool says so, and the model plays the hit with `media_player__play_media` instead (a sentence, no error) |
| 21 | A radio station plays (it does not end by itself) | Queued all the same (A); the answer says that the item comes after the station, that is only when the user says "weiter" ("next"). A station is known by the player's `media_content_id`: a Music Assistant URI with `://radio/` in it (seen on the test HA: `library://radio/1`, `radiobrowser://radio/…`) |

### What Music Assistant does (checked on the test HA, 2026-11-20)

| # | Transition | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|
| Q1 | the model searches on a Music Assistant player | `media_player__search_media` returns Music Assistant URIs as `media_content_id` (`deezer--…://track/4091937401`, also artists and albums) | the tool's answer, in the turn | 2026-11-20 on the test HA |
| Q2 | an item added at the end | `music_assistant.play_media` with the player as `entity_id`, `media_id` the URI, `enqueue: add`: HTTP 200; `music_assistant.get_queue` shows the queue's items going from 1 to 2 | the call's answer; nothing is waited for | 2026-11-20 on the test HA |
| Q3 | adding does not start playback | the player stays as it was (`idle` in the check) | — | 2026-11-20 on the test HA, as Q2 |
| Q4 | an item added after the current one | as Q2 with `enqueue: next` | the call's answer | open: `next` is Music Assistant's mode for it, not checked separately |
| Q5 | a station plays | the player's `media_content_id` is `library://radio/…` or `radiobrowser://radio/…` | the states read of step 2 below | 2026-11-20 on the test HA (the URIs seen) |

Interrupting and cancelling: there is nothing to interrupt, the call is one action and nothing is waited for. What is
queued is taken out in Music Assistant, not by voice.

### What the tool does

`music_queue(media_content_id, media_content_type?, when?, player?)`, described to the model (in German and English)
as putting a song, an album, an artist or a playlist into the queue of the music that plays without interrupting it,
with the example sentences of decision 19; it says to search first with `media_player__search_media` on the player
that plays and to pass the hit, and that `media_player__play_media` replaces what plays.

1. **The arguments**: without `media_content_id`, or with `when` neither "next" nor "end", the tool's error names
   the arguments; nothing is read or called.
2. **The player**: `player` given, it is found as the target of `music_move` (a player's name or an area); an unknown
   one: the tool says so with the players. The candidates are that area's players (or the one player), without
   `player` all Music Assistant players. One `GetStates`, filtered to the candidates, with their attributes. Among
   them the one that plays, else the one paused, as the source of `music_move`: with several, the one in the turn's
   area; otherwise the tool names them and the model asks, then calls again with `player`. None: the sentence of
   decision 20.
3. **The call**: `music_assistant.play_media` on the player with `media_id`, `enqueue` (`next` or `add`) and, when
   given, `media_type` (the hit's `media_content_type`).
4. **The answer** names the player with its area and whether the item comes next or at the end, for the model to say
   in its words; when the player's `media_content_id` is a station (decision 21), it adds that the item comes only
   when the user says "weiter".

### What the code models, the model is told

The tool's description is the instruction: when to use it instead of `media_player__play_media`, and that the hit of
the search is what it takes. The station case is told in the answer of that call. The section of part 1 does not
change: it names the players, which is all `music_queue` needs from it.

### Its errors

| What fails | The model gets | The log gets |
|---|---|---|
| No `media_content_id`, an unknown `when` | the tool's error with the arguments | nothing (the caller logs the tool's error) |
| Reading the states fails | the tool's error: the players cannot be read now | one line with the error |
| Home Assistant refuses (an unknown URI, the player unavailable) | the tool's error with Home Assistant's message | one line with the player, the media id, the enqueue mode and the error |
| An unknown room or player, several playing, none playing | a sentence saying so, with the names to choose from or the hint to play instead | nothing (no error) |

### Its costs

Per call one `GetStates` filtered to the candidates and one `call_service`; the turn waits for both. Nothing runs
afterwards, nothing is waited for (adding does not change the player's state, row Q3). The model's search before it
is Home Assistant's tool and its cost.

## Part 6: more of the same (`music_more`)

"Spiel mehr davon", "Mehr in der Art", "Spiel Ähnliches", "Spiel was wie Queen": Music Assistant appends similar
music when `music_assistant.play_media` is called with `radio_mode`. Home Assistant's own
`media_player__play_media` has no `radio_mode` (only `media_content_id`, `media_content_type` and the player by name,
area or floor; checked on the test HA, 2026-11-20, Home Assistant 2026.11.1), and Music Assistant offers no switch or
entity for it in Home Assistant either.

### Decided (2026-11-20)

| # | Question | Decision |
|---|---|---|
| 22 | Where the tool comes from | A tool of Mira's own, as `music_queue`, offered while Home Assistant has an exposed Music Assistant player; the call is `music_assistant.play_media` with `radio_mode` (the maintainer) |
| 23 | Similar to what | To the item that plays ("Spiel mehr davon", "Mehr in der Art", "Spiel Ähnliches"), or to a search hit when the user names one ("Spiel was wie Queen": the model searches with `media_player__search_media` first and passes the hit's `media_content_id`) (the maintainer) |
| 24 | When | After the item that plays: it plays to its end, the similar music is appended (`enqueue: add`) (the maintainer). Not chosen: replacing what plays at once |
| 25 | Where | The player that plays (or is paused), found as in `music_queue` (decision 18); a room or player the user names (`player`) |
| 26 | Nothing plays | Nothing is called: without a hit the tool says that nothing plays; with a hit it says, as `music_queue` (decision 20), to play the hit with `media_player__play_media` instead |
| 27 | A radio station plays and no hit is given | Nothing is called: the tool says that "more of this" works with songs, not with a station, and that the model may search a song or an artist the user names. A station's URI as the seed is not tried (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 28 | A station plays and a hit is given | Appended all the same, as `music_queue` (decision 21): the answer says it comes only when the user says "weiter" (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 29 | The player that plays has no `media_content_id` | Nothing is called: the tool says the player does not tell what plays, and the model asks the user what the music should be like, then calls again with a hit; a log line names the player (taken in the build, 2026-11-20; for the maintainer to confirm) |

### What Music Assistant does (checked on the test HA, 2026-11-20)

| # | Transition | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|
| M1 | similar music appended to a track | `music_assistant.play_media` on the playing player as `entity_id`, `media_id` the playing track's URI (its `media_content_id`, `deezer--…://track/4091937401`), `enqueue: add`, `radio_mode: true`: HTTP 200 | the call's answer; nothing is waited for | 2026-11-20 on the test HA: the Show playing "Queen - Bohemian Rhapsody", a queue of 1 item |
| M2 | what plays goes on | the same title plays on (8 s after the call) | — | as M1 |
| M3 | the queue grows | `music_assistant.get_queue`: 26 items, the next one "Alphaville - Big in Japan" | — (not read by Mira) | as M1 |
| M4 | an artist's or an album's URI as the seed (a hit of "Spiel was wie Queen") | as M1 | the call's answer | open |

Interrupting and cancelling: there is nothing to interrupt, the call is one action and nothing is waited for. What is
appended is taken out in Music Assistant, or skipped with "weiter"; "Stopp" stops the player as always.

### What the tool does

`music_more(media_content_id?, player?)`, described to the model (in German and English) as playing more music of the
kind after the current item, with the example sentences of decision 23; it says that without `media_content_id` the
music is like what plays, and that for a kind the user names the model searches first with
`media_player__search_media` on the player that plays and passes the hit.

1. **The arguments**: both optional; arguments that are no JSON object are the tool's error with them; nothing is
   read or called.
2. **The player**: as step 2 of `music_queue` (part 5): `player` found as the target of `music_move`, an unknown one
   answered with the players; one `GetStates` filtered to the candidates; the one that plays, else the one paused;
   with several, the one in the turn's area, else the tool names them and the model asks, then calls again with
   `player`. None: the sentence of decision 26.
3. **The seed**: the hit when given; otherwise the player's `media_content_id`. A station (`://radio/` in it, as in
   decision 21) without a hit: the sentence of decision 27. No `media_content_id`: decision 29.
4. **The call**: `music_assistant.play_media` on the player with `media_id` the seed, `enqueue: add` and
   `radio_mode: true`.
5. **The answer** names the player with its area and that similar music (like the current item, or like the hit)
   follows after the current item, for the model to say in its words; after a station, decision 28.

### What the code models, the model is told

The tool's description is the instruction: when to use it, and that a hit of the search is passed only when the user
names what the music should be like. The station, nothing-plays and no-content cases are told in the answer of that
call. The section of part 1 does not change.

### Its errors

| What fails | The model gets | The log gets |
|---|---|---|
| Arguments that are no JSON object | the tool's error with the arguments | nothing (the caller logs the tool's error) |
| Reading the states fails | the tool's error: the players cannot be read now | one line with the error |
| Home Assistant refuses (an unknown URI, the player unavailable) | the tool's error with Home Assistant's message | one line with the player, the seed and the error |
| The player that plays has no `media_content_id` | a sentence: ask the user what it should be like | one line with the player |
| An unknown room or player, several playing, none playing, a station without a hit | a sentence saying so, with the names to choose from or what to do instead | nothing (no error) |

### Its costs

Per call one `GetStates` filtered to the candidates and one `call_service`; the turn waits for both. Nothing runs
afterwards, nothing is waited for (row M2). Music Assistant finds the similar music itself (row M3). The model's
search before it, when the user names a kind, is Home Assistant's tool and its cost.

## Part 7: lowering the music while a satellite listens (ducking)

A satellite (an Echo Show with Mira's firmware) lowers its own playback while it listens. Other players in its room
(an AV receiver as a Music Assistant player, say) stay loud during the turn and its follow-up questions. Neither Home
Assistant nor Music Assistant offers this: only community blueprints on the satellite's states; Music Assistant
lowers only for its own `play_announcement`.

### The concept it hangs on

No thread, no tool: a watch of Mira's own for as long as she runs, as the persons are watched
([Home Assistant integration](ha-integration.md), Watching states), on the house structure of part 1 (the Music
Assistant players with their areas and their devices' manufacturers, the satellites' areas). It is no tool: nothing changes in what the model is told,
and no text of Mira's changes. It also hears of the model's calls of Home Assistant's volume tools, which it already
has (decision 35): through the tool registry, which tells it of every MCP call that succeeded.

### Decided (2026-11-20)

| # | Question | Decision |
|---|---|---|
| 30 | Who lowers | Mira herself, for the Music Assistant players in the satellite's area: lowered when the satellite starts listening, set back when it is idle again (the maintainer) |
| 31 | A volume command during the turn ("lauter", "leiser", "auf 30 %") | It counts as the new volume: it is not overwritten when the turn ends (the maintainer). How Mira recognises one: decision 35 |
| 32 | How far down | To a third of the player's volume, in whole percent, at least 1 %; no setting (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 33 | Which players | The Music Assistant players of part 1 (exposed) in the satellite's area that are `playing` when it starts listening; a paused or idle one, and one that starts during the turn, is not touched (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 34 | A player that plays on the satellite itself | Not lowered: its firmware lowers its own playback (the maintainer). The rule is per player: every other playing player of the area is lowered, the players of a group too. Recognised since 2026-11-20 by its device's manufacturer, in any area: decision 39 |
| 35 | Setting back | Changed 2026-11-20 after live: devices round the volume to their own steps (Mira set 0.08, the player reported 0.06), so comparing values took Mira's own volume for a command of the user and left the music quiet. Now no value is compared: when the last satellite of the area is idle, every lowered player gets its remembered volume back, unless during the turn the model called one of Home Assistant's volume tools (`HassSetVolume`, `HassSetVolumeRelative`) for it and Home Assistant carried the call out: that call is the signal of decision 31, not the value. Muting and unmuting (`HassMediaPlayerMute`, `HassMediaPlayerUnmute`) do not count: setting the volume back does not touch the mute. Which players a call reaches, kept simple: a `name` → the lowered players of that name (or alias, as the house structure resolves names, in the call's `area` if it names one); else an `area` → the lowered players of that area; else a `floor` → every lowered player (the house structure knows no floors); no target → the lowered players of the turn's area (Home Assistant takes the satellite's area). Only a call of a turn from an area that is lowered counts. A volume changed in another way (an app, a remote) is overwritten. A player stopped during the turn is set back too (the maintainer, 2026-11-20) |
| 36 | Which satellites | Every `assist_satellite` with an area (its entity's, else its device's, as the house structure finds a satellite's area); one without an area lowers nothing (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 37 | Reading the players fails when setting back | No longer applies (2026-11-20, with decision 35): setting back reads no state, so there is no read to fail |
| 38 | A Mira restart during a turn | The remembered volumes are lost: the players stay lowered until the user turns them up. A nuisance only; nothing is stored (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 39 | Players that lower themselves | Mira never lowers a Music Assistant player whose device's manufacturer in Home Assistant's device registry is `TECHO5` or `EchoLocal`, in any area, not only the satellite's own (the maintainer, 2026-11-20). These devices lower their own playback while they listen. Seen on live 2026-11-20: Mira lowering an Echo Dot 2 with the TECHO5 firmware lowered the device's volume itself, so her spoken answer on it was too quiet to understand, and the volume change showed on its LED ring and hid the "listening" animation. On live every Echo and Show with the firmware has the manufacturer `TECHO5`, one Echo Dot 2 `EchoLocal`; other players have their own (`Google Inc.`, `Samsung Electronics`, `Music Assistant` for a sync group, "Unknown manufacturer" for an AV receiver). It replaces the check by the satellite's device id, which never held (a Music Assistant player is a device of its own) and is gone. The manufacturer comes from the device registry through the house structure (part 1), read from the structure at each turn: a changed manufacturer counts from the next rebuild (the registry events), and like a changed model it is no change that fetches Home Assistant's prompt again (the structure counts as the same, MU-04); it changes nothing in what the model is told |

### The signals (checked on the test HA, 2026-11-20)

| # | Transition | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|
| D1 | a turn with a follow-up question ("Stell einen Wecker" → "Wann soll der Wecker klingeln?") | the satellite's state `idle` → `listening` (0.0 s) → `processing` (0.0) → `responding` (1.3) → `listening` (3.3, the follow-up) → `idle` (11.3, listening timed out). It does not pass `idle` between the answer and the follow-up: `listening` to `idle` spans the whole conversation | `subscribe_entities` filtered to the domain `assist_satellite` | checked on the test HA (2026-11-20), on the fake satellite, with a state subscription on that entity |
| D2 | `media_player.volume_set` on a Music Assistant player | the call answers in under 10 ms (HTTP 200); the player's `volume_level` shows the new value about 0.3 s later |
| D3 | a volume command of the turn | the model's call of `HassSetVolume` or `HassSetVolumeRelative` on Home Assistant's MCP server, carried out (no error) | the tool registry tells the ducker of every MCP call that succeeded, with the turn's area and the arguments (in the turn's goroutine, without blocking) | the call itself is Mira's own; no state of Home Assistant is involved | the call's answer; the state is not read afterwards (a device may round the value to its own steps: on live, 0.08 became 0.06) | checked on the test HA (2026-11-20): `media_player.show`, idle, 0.7 and back to 0.7 |

A turn is the time from `listening`, `processing` or `responding` to any other state (`idle`, also `unavailable`):
the follow-up question keeps the music low (row D1). Nothing is waited for: lowering and setting back follow the
satellite's state changes; no time is computed.

### What it does

1. **The watch**: at the start one subscription, `subscribe_entities` filtered to the domain `assist_satellite`
   (Home Assistant filters; no other state comes), restored after a reconnect like every subscription. Its handler
   only signals a goroutine (a channel of one; signals that come while it works fold into one); the goroutine reads
   the satellites' states the watch keeps and compares them with the last ones. A satellite seen the first time
   (Mira's start, a new satellite) is only recorded.
2. **A satellite starts a turn** (from a state outside the turn to one in it): its area from the house
   structure (decision 36). When another satellite of the area is in a turn already, it only joins it (nothing is read
   or called). Otherwise the Music Assistant players of the area, without those whose device's manufacturer is
   `TECHO5` or `EchoLocal` (decisions 34 and 39), are read once (`GetStates` filtered to them, with `volume_level`); each that is `playing` with a
   volume goes to a third (decision 32) with `media_player.volume_set`; its volume before and Mira's are remembered in
   memory. A player whose third is not below its volume (1 % or less) is left. One log line names the satellite, the
   area and the players with both volumes.
3. **A volume command during the turn** (row D3): when the model's call of `HassSetVolume` or
   `HassSetVolumeRelative` on Home Assistant's server succeeded in a turn with an area, the registry hands the call
   (the turn's area, `name`, `area`, `floor`) to the ducker: it is noted under a lock and the goroutine is signalled
   (the same channel of one); the turn does not wait. The goroutine takes the noted calls in before the next states:
   when the turn's area is lowered, the players the call reaches (decision 35) are marked to keep their volume, and
   one log line names the call's targets and the players marked. A call whose arguments are no JSON object is logged
   with its arguments and marks nothing.
4. **The last satellite of the area ends its turn** (to a state outside the turn, or removed): nothing is read; each
   remembered player not marked is set back to its volume before with `media_player.volume_set`; a marked one stays
   (decision 35). The marks end with the turn. One log line names the players set back, one those left.

### Interrupting and cancelling

| What happens | What Mira does |
|---|---|
| The wake word again during the turn | The satellite stays in the turn (`listening`): nothing is called, the music stays low |
| "Stopp", "Abbrechen", silence | The satellite goes to `idle`: the music is set back (a player the user stopped too: decision 35) |
| The follow-up question, a new turn while lowered (row D1) | The satellite does not pass `idle`: the music stays low until the last `idle` |
| A second satellite in the same room starts listening | It joins: nothing more is lowered; the music comes back when both are idle |
| A satellite in another room | Its own area, its own players |
| The satellite becomes unavailable or is removed during the turn | As `idle`: the music is set back |
| "Spiel Musik" during the turn | The player that starts is not touched (decision 33) |
| A volume command during the turn | Decisions 31 and 35: a call of Home Assistant's volume tools keeps the players it reaches; a volume changed in an app or with a remote is set back |
| Mira restarts during the turn | Decision 38 |

### Its errors

| What fails | Who learns of it | What happens |
|---|---|---|
| The subscription cannot start | the log: `background task stopped` with the task `ducking` and the error | no ducking until Mira's next start |
| Reading the players fails when a turn starts | one log line with the satellite, the area, the players and the error | nothing is lowered for this turn |
| Home Assistant refuses `volume_set` when lowering | one log line with the player, both volumes and the error | that player stays as it is; the others go down |
| The arguments of a volume tool call are no JSON object (Home Assistant took them, so not expected) | one log line with the tool, the arguments and the error | nothing is marked: the players are set back after the turn |
| Home Assistant refuses `volume_set` when setting back | one log line with the player, its volume and the error | that player stays lowered; the user turns it up |
| A satellite without an area | — (no error) | nothing is lowered |

### Its costs

One subscription for as long as Mira runs, filtered to the satellites; every state change of a satellite is a
message and a comparison in memory, without a call. Per turn in an area with Music Assistant players: one `GetStates`
filtered to them and one `volume_set` per playing player when it starts, one `volume_set` per lowered player not
marked when it ends. A volume tool call costs a note in memory and, in a lowered area, a name lookup in the house
structure; every other tool call a check of its name. The turn does not wait for any of it. An area without Music Assistant players costs
nothing. It grows with the number of Music Assistant players in the satellite's area.

## Part 8: winding within the song (`music_seek`)

"Spring dreißig Sekunden vor", "Geh eine Minute zurück", "Geh an den Anfang": Home Assistant has the action
`media_player.media_seek`, but its Assist tools offer the model no seek. Seen on live (2026-11-20): for "Spring dreißig
Sekunden vor" the model called `HassMediaNext` without a target, nothing changed on the playing Onkyo, and it answered
"Dreißig Sekunden springen kann ich nicht, ich hab stattdessen zum nächsten Titel gewechselt."

### Decided (2026-11-20)

| # | Question | Decision |
|---|---|---|
| 40 | Where the tool comes from | A tool of Mira's own, as `music_queue`, offered while Home Assistant has an exposed Music Assistant player; the call is `media_player.media_seek` (the maintainer) |
| 41 | What it takes | `seconds`, relative (positive forward, negative back: "Spring 30 Sekunden vor", "Geh eine Minute zurück", "Spul zwei Minuten vor"), or `position`, an absolute place in seconds from the start ("Geh an den Anfang" = 0, "Spring zu Minute drei" = 180); exactly one of them (the maintainer) |
| 42 | Where | The player that plays (or is paused), found as in `music_queue` (decision 18); a room or player the user names (`player`) (the maintainer) |
| 43 | The current place | Home Assistant's convention: while `playing`, `media_position` + (now − `media_position_updated_at`); while `paused`, `media_position`. Now is the code's clock (the maintainer). Without `media_position_updated_at` the position is taken as it is (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 44 | Before the start | A relative target before 0 is 0 (the maintainer) |
| 45 | Beyond the end | Nothing is called: the tool says that the song ends earlier, with its length and the current place, and the model tells the user. Not chosen: winding to just before the end (taken in the build, 2026-11-20, as the simpler of the two; for the maintainer to confirm) |
| 46 | No length known (`media_duration` missing) | The target is not limited at the end; Music Assistant decides (taken in the build, 2026-11-20; for the maintainer to confirm) |
| 47 | No position known (`media_position` missing) | A relative seek calls nothing: the tool says that the player does not tell its place; an absolute one is called (the maintainer) |
| 48 | A radio station plays (`://radio/` in `media_content_id`, decision 21) | Nothing is called: the tool says that a station cannot be wound (the maintainer) |

### What Home Assistant does (checked on live, 2026-11-20, read only)

| # | Transition | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|
| S1 | a Music Assistant player can seek | `supported_features` has SEEK (bit 2; on live 8320575) | — (not read by Mira: a refusal is Home Assistant's) | 2026-11-20 on live, read only |
| S2 | the place in the song | `media_position` (seconds), `media_position_updated_at` (the time of that position), `media_duration` (seconds); the Onkyo playing: position 0 at 20:14:50Z, duration 1052 | the states read of step 2 below | 2026-11-20 on live, read only |
| S3 | the seek | `media_player.media_seek` with `entity_id` and `seek_position` (seconds) | the call's answer; nothing is waited for | open, to be checked on live by the maintainer |
| S4 | the new place shows in the state | `media_position` and `media_position_updated_at` change | — (not read by Mira) | open, to be checked on live by the maintainer: how fast it shows |

Interrupting and cancelling: there is nothing to interrupt, the call is one action and nothing is waited for. Winding
back is another call; "Stopp" stops the player as always.

### What the tool does

`music_seek(seconds?, position?, player?)`, described to the model (in German and English) as winding within the song
that plays, with the example sentences of decision 41, and that it is the tool for this, not `HassMediaNext` (which
goes to the next song).

1. **The arguments**: no JSON object, both `seconds` and `position`, neither of them, or a `position` below 0: the
   tool's error names the arguments; nothing is read or called.
2. **The player**: as step 2 of `music_queue` (part 5): `player` found as the target of `music_move`, an unknown one
   answered with the players; one `GetStates` filtered to the candidates, with their attributes; the one that plays,
   else the one paused; with several, the one in the turn's area, else the tool names them and the model asks, then
   calls again with `player`. None: the tool says that no music plays there.
3. **The target**: a station: decision 48. `position`: the target. `seconds`: the current place (decision 43) plus
   `seconds`, no position known: decision 47; before 0: decision 44. Rounded to whole seconds. A target beyond
   `media_duration`: decision 45; without it: decision 46.
4. **The call**: `media_player.media_seek` on the player with `seek_position` the target.
5. **The answer** names the player with its area and the new place (m:ss, h:mm:ss from an hour), for the model to say
   in its words.

### What the code models, the model is told

The tool's description is the instruction: when to use it (winding within the song), the two ways of saying where
to, and that `HassMediaNext` is not for it. The station, no-position, beyond-the-end and nothing-plays cases are told
in the answer of that call. The section of part 1 does not change.

### Its errors

| What fails | The model gets | The log gets |
|---|---|---|
| Arguments that are no JSON object, both or neither of `seconds` and `position`, a `position` below 0 | the tool's error with the arguments | nothing (the caller logs the tool's error) |
| Reading the states fails | the tool's error: the players cannot be read now | one line with the error |
| Home Assistant refuses (a player that cannot seek, unavailable) | the tool's error with Home Assistant's message | one line with the player, the target position and the error |
| An unknown room or player, several playing, none playing, a station, no position known, beyond the end | a sentence saying so, with the names to choose from or the song's length | nothing (no error) |

### Its costs

Per call one `GetStates` filtered to the candidates and one `call_service`; the turn waits for both. Nothing runs
afterwards, nothing is waited for (the new place is not read, row S4).

## Errors

| What fails | Who learns of it | What happens |
|---|---|---|
| The registries cannot be read | the log, as for every rebuild (`structure rebuild failed`) | the last structure stays, with its players |
| The model still picks another player | the user, by Home Assistant's error in the turn, as today | nothing of Mira's own; the model may try again |
| Music Assistant is not installed | — | no section |

## Costs

No call of its own: the registries are read anyway. The section grows with the number of exposed Music Assistant
players (a line per area, a name per player; on live about 20 names, a few hundred characters), once in the shared
prefix.

## Steps

1. **The players in the structure and the section in layer 1** (`internal/ha/structure`, `internal/prompt`,
   `internal/conversation`, `internal/music`). Scenarios MU-01 to MU-04.
2. **`music_move`** (`internal/music`). Scenarios MU-05 to MU-09.
3. **On live**: "Spiel … in der Küche" goes to the Music Assistant player at once; "Nimm die Musik mit ins Bad"
   moves it. Left for the maintainer (it plays music in the house).
4. **`music_group`** (part 3; `internal/music`, `cmd/mira`). Scenarios MU-10 to MU-17. Built 2026-11-20; the run on live
   (adding, taking out, row G2) is left for the maintainer.
5. **Radio through Music Assistant** (part 4; `cmd/mira`, `internal/music`). Scenarios MU-18 and MU-19. Built 2026-11-20.
6. **`music_queue`** (part 5; `internal/music`, `cmd/mira`). Scenarios MU-20 to MU-26. Built 2026-11-20; the run on
   live ("Spiel danach …" while music plays, row Q4) is left for the maintainer.
7. **`music_more`** (part 6; `internal/music`, `cmd/mira`). Scenarios MU-27 to MU-33. Built 2026-11-20; the run on
   live ("Spiel mehr davon" while music plays, "Spiel was wie Queen", row M4) is left for the maintainer.
8. **Ducking** (part 7; `internal/music`, `cmd/mira`, `internal/tools`). Scenarios MU-35, MU-37 to MU-41
   (MU-34 and MU-36 retired). Built 2026-11-20; setting back changed the same day after live (decision 35: a volume
   tool call is the signal, no value is compared), and the players that lower themselves are recognised by their
   device's manufacturer (decision 39). The run end to end on the test HA (a turn with a follow-up on the fake
   satellite while a Music Assistant player of its area plays) is open.
9. **`music_seek`** (part 8; `internal/music`, `cmd/mira`). Scenarios MU-42 to MU-48. Built 2026-11-20; the run on
   live ("Spring dreißig Sekunden vor" while music plays, rows S3 and S4) is left for the maintainer.
