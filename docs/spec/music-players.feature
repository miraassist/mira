# Scenarios of docs/spec/music-players.md. Every scenario has a number (MU-01, …); the Go test that checks it carries
# the number in its name (TestMU01_…). @step1 is the players in the prefix, @step2 music_move, @step4 music_group, @step5 radio through Music Assistant,
# @step6 music_queue, @step7 music_more, @step8 ducking, @step9 music_seek, @step10 a player named like its area
# where the player that plays is looked for.

Feature: MU Music with Music Assistant

  @step1
  Scenario: MU-01 The exposed Music Assistant players are in the prefix, per area
    Given exposed players of Music Assistant in the living room, the kitchen and without an area
    When the prompt's prefix is built
    Then its system message has the section Musik after the house structure, with one line per area and the names
    And the sentence that music plays only on these players

  @step1
  Scenario: MU-02 Other players are not named
    Given the speaker of a satellite, a Music Assistant player that is not exposed and one that is disabled
    When the section is built
    Then none of them is in it

  @step1
  Scenario: MU-03 Without Music Assistant players there is no section
    Given a house without Music Assistant
    When the prompt's prefix is built
    Then its system message has no section Musik

  @step1
  Scenario: MU-04 The section changes only with the players
    Given a built house structure
    When it is built again from the same registries, and then with a Music Assistant player renamed
    Then the structure is the same the first time and differs the second time

  @step2
  Scenario: MU-05 music_move is there only with Music Assistant players
    Given a house with exposed Music Assistant players, and one without
    When Mira offers her tools
    Then music_move is offered in the first and not in the second

  @step2
  Scenario: MU-06 The source is the player that plays
    Given the kitchen's player plays and the others do not
    When music_move runs with to "Bad" and no from
    Then the music moves from the kitchen's player
    And with players playing in the kitchen and the living room it moves the one in the area of the turn's satellite
    And when that is neither, nothing moves and the answer names both

  @step2
  Scenario: MU-07 The target is a room with one player, or a player by name
    Given the bathroom with one Music Assistant player and the living room with two
    When music_move runs with to "Bad", with to "Wohnzimmer" and with to "Onkyo"
    Then the first moves to the bathroom's player, the second moves nothing and names both, the third moves to Onkyo

  @step2
  Scenario: MU-08 The move is Music Assistant's transfer_queue
    Given the kitchen's player plays
    When music_move runs with to "Bad"
    Then music_assistant.transfer_queue is called with the bathroom's player and source_player the kitchen's
    And the answer names both players with their rooms

  @step2
  Scenario: MU-09 What cannot be moved says why
    Given no player plays, or the target is the player that plays, or Home Assistant refuses the move
    When music_move runs
    Then the answer says that no music plays, that it plays there already, or Home Assistant's error, and the log has the refusal

  @step4
  Scenario: MU-10 music_group is there only with Music Assistant players
    Given a house with exposed Music Assistant players, and one without
    When Mira offers her tools
    Then music_group is offered in the first, with the examples "Spiel das auch im Bad", "Mach die Musik überall an" and "Nimm das Bad wieder raus", and not in the second

  @step4
  Scenario: MU-11 A room is added to the player that plays
    Given the kitchen's player plays
    When music_group runs with add "Bad"
    Then media_player.join is called on the kitchen's player with group_members the bathroom's
    And the answer names both with their rooms, and the log has the join
    And with players playing in the kitchen and the living room the one in the area of the turn's satellite leads
    And several rooms in one add are one join

  @step4
  Scenario: MU-12 "Überall" adds every other player
    Given the kitchen's player plays
    When music_group runs with add "überall", "alle" or "everywhere"
    Then one join on the kitchen's player adds every other Music Assistant player

  @step4
  Scenario: MU-13 A room with two players adds both; who is in the group already is left out
    Given the kitchen's player plays and the living room has two players
    When music_group runs with add "Wohnzimmer"
    Then both are added in one join
    And when one of them is in the kitchen's group already, only the other is added
    And when all are in it already, nothing is called and the answer says so

  @step4
  Scenario: MU-14 A player is taken out with unjoin; one not in a group is named
    Given the bathroom's player plays in a group with the kitchen's
    When music_group runs with remove "Bad, Onkyo"
    Then media_player.unjoin is called on the bathroom's player and not on Onkyo
    And the answer names the bathroom's player as taken out and Onkyo as in no group, and the log has the unjoin

  @step4
  Scenario: MU-15 The player that plays can be taken out
    Given the kitchen's player leads a group with the bathroom's
    When music_group runs with remove "Küche"
    Then media_player.unjoin is called on the kitchen's player

  @step4
  Scenario: MU-16 Unknown rooms and no music say so
    Given no player plays, or a room without a Music Assistant player, or players playing in two rooms, neither the turn's
    When music_group runs
    Then nothing is called and the answer says that no music plays, that the room is unknown with the players, or names both
    And a call with neither add nor remove is the tool's error

  @step4
  Scenario: MU-17 What Home Assistant refuses is the tool's error
    Given Home Assistant refuses the join or the unjoin, or the states cannot be read
    When music_group runs
    Then the tool's error has Home Assistant's message and the log has a line

  @step5
  Scenario: MU-18 radio_play is there only without Music Assistant players
    Given a house with an exposed Music Assistant player, and one without
    When Mira offers her tools
    Then radio_play is offered in the second and not in the first

  @step5
  Scenario: MU-19 The section says that radio stations play on these players too
    Given exposed Music Assistant players
    When the section is built
    Then its rule names music and radio stations

  @step6
  Scenario: MU-20 music_queue is there only with Music Assistant players
    Given a house with exposed Music Assistant players, and one without
    When Mira offers her tools
    Then music_queue is offered in the first only, its description has the example sentences in German and English
    And it tells the model to search first with media_player__search_media and that media_player__play_media replaces what plays

  @step6
  Scenario: MU-21 "next" and "end" are Music Assistant's enqueue next and add on the player that plays
    Given the kitchen's player plays
    When music_queue runs with a hit of the search and when "next", "end" or none
    Then music_assistant.play_media is called on the kitchen's player with the hit and enqueue "next", "add" and "add"
    And the states are read once, filtered to the Music Assistant players, and the answer names the player, its room and next or the end
    And a paused player is queued on as well

  @step6
  Scenario: MU-22 With music in several rooms the turn's room wins, else the tool names them
    Given players play in the kitchen and in the living room
    When music_queue runs in a turn in the kitchen, and in one in the bathroom
    Then the first is queued on the kitchen's player, the second calls nothing and names both for the model to ask

  @step6
  Scenario: MU-23 A room or a player the user names is where it is queued
    Given players play in the kitchen and in the living room
    When music_queue runs in the kitchen with player "Wohnzimmer", "Onkyo" or "Garage"
    Then the first two are queued on the living room's player that plays, reading only the named players
    And "Garage" calls nothing and the answer names the players

  @step6
  Scenario: MU-24 When nothing plays, the model is told to play the item instead
    Given no Music Assistant player plays, or none in the room named
    When music_queue runs
    Then nothing is called and the answer says that no music plays and to play the hit with media_player__play_media

  @step6
  Scenario: MU-25 After a radio station the item is queued all the same, and the answer says it comes on "weiter"
    Given the kitchen's player plays a station (its media_content_id library://radio/1 or radiobrowser://radio/…)
    When music_queue runs
    Then the hit is queued and the answer says that it comes only when the user says "weiter"
    And with a song playing the answer says nothing of a station

  @step6
  Scenario: MU-26 What fails is the tool's error and names its input
    Given Home Assistant refuses the call, the states cannot be read, the media_content_id is missing or when is unknown
    When music_queue runs
    Then the tool's error has Home Assistant's message or the arguments, nothing is queued
    And a refusal's log line names the player, the media id and the enqueue mode

  @step7
  Scenario: MU-27 music_more is there only with Music Assistant players
    Given a house with exposed Music Assistant players, and one without
    When Mira offers her tools
    Then music_more is offered in the first only, its description has the example sentences in German and English
    And it tells the model to search first with media_player__search_media when the user names what it should be like
    And both its arguments are optional

  @step7
  Scenario: MU-28 Without a hit, the seed is the item that plays, appended in radio mode
    Given the kitchen's player plays a song
    When music_more runs without media_content_id
    Then music_assistant.play_media is called on the kitchen's player with the song's media_content_id, enqueue "add" and radio_mode
    And the states are read once, filtered to the Music Assistant players, and the answer names the player, its room and that similar music follows
    And a paused player is used as well, also when the tool is called without arguments

  @step7
  Scenario: MU-29 With a hit, the hit is the seed, after a station too
    Given the kitchen's player plays a song, or a station
    When music_more runs with a hit of the search
    Then the hit is the seed and the answer says that music like the hit follows
    And after a station the answer says that it comes only when the user says "weiter"

  @step7
  Scenario: MU-30 The player is found as music_queue's
    Given players play in the kitchen and in the living room
    When music_more runs in the kitchen, in the bathroom, and in the kitchen with player "Wohnzimmer", "Onkyo" or "Garage"
    Then the kitchen's player is used, the bathroom's turn names both for the model to ask, the named ones use the living room's player
    And "Garage" calls nothing and the answer names the players

  @step7
  Scenario: MU-31 When nothing plays, nothing is called; with a hit the model is told to play it
    Given no Music Assistant player plays
    When music_more runs without a hit and with one
    Then nothing is called, the first answer says that no music plays and the second to play the hit with media_player__play_media

  @step7
  Scenario: MU-32 A station or a player that does not say what it plays calls nothing without a hit
    Given the kitchen's player plays a station (library://radio/1 or radiobrowser://radio/…), or has no media_content_id
    When music_more runs without a hit
    Then nothing is called; for the station the answer says that "more of this" works with songs, not a station
    And without a media_content_id the answer tells the model to ask the user, and a log line names the player

  @step7
  Scenario: MU-33 What fails is the tool's error and names its input
    Given Home Assistant refuses the call, the states cannot be read, or the arguments are no JSON object
    When music_more runs
    Then the tool's error has Home Assistant's message or the arguments, nothing is appended
    And a refusal's log line names the player and the seed

  # MU-34 (the players that play go down, without one of the satellite's own device) was retired on 2026-11-20
  # with decision 39: the players that lower themselves are recognised by their device's manufacturer, in any
  # area; replaced by MU-41.

  @step8
  Scenario: MU-35 The follow-up question keeps the music low; idle sets it back
    Given the kitchen's player plays at 60 %
    When the kitchen's satellite goes listening, processing, responding, listening (the follow-up), processing, responding
    Then the player is lowered once, to 20 %
    And when the satellite is idle, or unavailable, the player is set back to 60 %, also when it was stopped during the turn

  # MU-36 (a volume found changed at the end of the turn stays) was retired on 2026-11-20 with the change of
  # decision 35 after live: devices round the volume, so no value is compared any more; replaced by MU-40.

  @step8
  Scenario: MU-37 Two satellites in one room: the music comes back when both are idle
    Given two satellites in the living room
    When the first starts listening, then the second, then the first is idle
    Then the music is lowered once and stays low
    And it is set back when the second is idle too, or is removed

  @step8
  Scenario: MU-38 What fails leaves a log line and the rest goes on
    Given reading the players fails, or Home Assistant refuses volume_set for one player, or the watch cannot start
    Then a failed read when lowering lowers nothing for the turn, with a log line naming the players
    And a refused player stays as it is while the others go down and come back
    And a refused setting back leaves a log line that the player stays lowered
    And a watch that cannot start is the background task's error

  @step8
  Scenario: MU-39 The subscription's handler only signals
    Given ducking runs on a watch of the satellites
    When many state changes come at once on the connection's read loop
    Then the handler never blocks, and the goroutine lowers and sets back from the satellite's states

  @step8
  Scenario: MU-40 A volume tool call of the turn keeps the players it reaches; any other volume is set back
    Given in the living room the Onkyo (70 %), the Sonos (50 %) and a Cast group (90 %) were lowered while a satellite there listens
    When the satellite is idle and the Onkyo reports 6 % (the device rounded Mira's 23 %), with no volume tool call
    Then all three are set back to their volumes before, without reading their states
    And when during the turn Home Assistant carried out HassSetVolume or HassSetVolumeRelative without a target, none is set back, with a log line
    And a call naming "Onkyo" keeps only the Onkyo; one naming the living room keeps all three; one naming the kitchen keeps none of them; one naming a floor keeps all three
    And a call from a turn in an area that is not lowered, or without an area, keeps nothing
    And a call that Home Assistant answered with an error, a mute, or a tool of another server keeps nothing
    And the marks end with the turn: the next turn sets the players back again
    And noting a call never blocks the turn

  @step8
  Scenario: MU-41 While a satellite listens, the players that play in its area go down to a third, but not those that lower themselves
    Given in the living room the Onkyo plays at 70 %, the Sonos is paused, and two players whose devices' manufacturer is TECHO5 and EchoLocal play
    And Mira has seen the satellites' states once
    When the living room's satellite starts listening
    Then the living room's players are read once, without the TECHO5 and EchoLocal players
    And only the Onkyo is set to 23 % with media_player.volume_set, and a log line names it with both volumes
    And a satellite without an area lowers nothing and reads nothing
    And the manufacturer is the one the device registry gives the player's device

  @step9
  Scenario: MU-42 music_seek is there only with Music Assistant players, and says it is not HassMediaNext
    Given a house with exposed Music Assistant players, and one without
    When Mira offers her tools
    Then music_seek is offered only in the first, with seconds, position and player, none of them required
    And its description in German and English has the example sentences and says that HassMediaNext is not for winding

  @step9
  Scenario: MU-43 Relative: from the current place by the clock while playing, from the position while paused
    Given the kitchen's player plays at 1:00, reported 10 s ago, in a song of 5:00
    When music_seek runs with seconds 30
    Then media_player.media_seek is called with seek_position 100 and the answer names 1:40 and the player with its room
    And with seconds -120 the target is 0
    And when the player is paused at 1:00 with the same report, seconds 30 gives 90

  @step9
  Scenario: MU-44 Absolute: the position is the target, also without a known place or length
    Given the kitchen's player plays
    When music_seek runs with position 0, or 180 without media_position and media_duration
    Then media_seek is called with 0, or with 180

  @step9
  Scenario: MU-45 Beyond the end, a station or no known place: nothing is called
    Given the kitchen's player plays at 4:50 in a song of 5:00, or a station, or does not report media_position
    When music_seek runs with seconds 30, or position 400
    Then nothing is called, and the answer says that the song ends at 5:00, that a station cannot be wound, or that the player does not tell its place
    And a station is not wound with position either

  @step9
  Scenario: MU-46 The player is found as music_queue's
    Given players play in the kitchen and in the living room
    When music_seek runs in the kitchen, in the bathroom, and in the kitchen with player "Wohnzimmer", "Onkyo" or "Garage"
    Then the first seeks on the kitchen's player, the second calls nothing and names both
    And "Wohnzimmer" and "Onkyo" seek on the Onkyo, "Garage" calls nothing and names the players
    And when nothing plays nothing is called and the answer says so

  @step9
  Scenario: MU-47 Exactly one of seconds and position
    Given the kitchen's player plays
    When music_seek runs with both, with neither, with a position below 0, or with arguments that are no JSON object
    Then the tool's error names the arguments and nothing is read or called

  @step9
  Scenario: MU-48 What fails is the tool's error and names its input
    Given Home Assistant refuses the seek, or the states cannot be read
    When music_seek runs
    Then the tool's error has Home Assistant's message or the read's error, with a log line
    And a refusal's log line names the player and the target position

  @step10
  Scenario: MU-49 Where the player that plays is looked for, a player named like its area stands for the area too
    Given the living room has an idle player named "Wohnzimmer" and the Onkyo, which plays
    When music_seek, music_queue or music_more runs with player "Wohnzimmer", or music_move with from "Wohnzimmer"
    Then the Onkyo is wound, gets the item or more of the same, or its music moves
    And when the player named "Wohnzimmer" plays, it is the one taken
    And music_move with to "Wohnzimmer" still moves to the player named "Wohnzimmer"
