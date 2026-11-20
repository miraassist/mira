# Music with Music Assistant – spec

Status 2026-11-20: steps 1 and 2 built (on live, not released); step 3, the run on live, is open; part 3 decided, not built. Scenarios: [music-players.feature](music-players.feature).

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

### What Home Assistant does

`media_player.join` on the player that plays, with `group_members` the players to add; `media_player.unjoin` on a
player to take it out. Both are Home Assistant's actions; the Music Assistant players report the feature
(`GROUPING`). Which players can play together is Music Assistant's matter (an Echo with another kind of speaker, say);
what it refuses comes back as Home Assistant's error.

| # | Transition | Signal | How Mira learns of it | Checked on live |
|---|---|---|---|---|
| G1 | a player added | the join's success; the playing player's `group_members` holds it | the call's answer | open, the maintainer tests it |
| G2 | a player taken out | the unjoin's success; it is no longer in `group_members` | the call's answer | open |

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
4. **`music_group`** (part 3). Scenarios to be written with it.
