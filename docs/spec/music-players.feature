# Scenarios of docs/spec/music-players.md. Every scenario has a number (MU-01, …); the Go test that checks it carries
# the number in its name (TestMU01_…). @step1 is the players in the prefix, @step2 music_move, @step4 music_group, @step5 radio through Music Assistant.

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
