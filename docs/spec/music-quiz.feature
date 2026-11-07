# Testable rules of docs/spec/music-quiz.md. Each scenario has a fixed number (MQ-nn) that is never reused; its test
# carries the number in its name (TestMQ01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.
#
# Groups: MQ-0x the tool clip_play · MQ-1x the skill

Feature: MQ Music quiz

  # --- The tool clip_play ---------------------------------------------------------------------------------

  @step1
  Scenario: MQ-01 A clip plays after the answer, titled, as music
    Given a voice conversation at the Show, whose firmware titles its player (media_title)
    When the model calls clip_play with a preview's address, title "Musik-Quiz" and subtitle "Titel 1 von 10"
    Then the tool returns at once
    And once the satellite has said the answer, Mira titles the player and plays the address without announce
    And the satellite does not listen after the answer

  @step1
  Scenario: MQ-02 A satellite without the title action plays untitled
    Given a voice conversation at a satellite without esphome.<node>_media_title
    When the model calls clip_play
    Then the clip plays all the same, untitled

  @step1
  Scenario: MQ-03 A newer clip takes over the satellite
    Given a clip of an earlier call plays at the Show
    When the model calls clip_play again
    Then the earlier clip stops and only the new one plays after the answer

  @step1
  Scenario: MQ-04 The clip stops with its thread
    Given a clip of the music quiz's thread plays at the Show
    When the thread is cancelled
    Then the speaker is stopped

  @step1
  Scenario: MQ-05 Outside a satellite nothing plays
    Given a conversation in Telegram
    When the model calls clip_play
    Then the tool says that it plays only at a satellite
    And nothing plays

  @step1
  Scenario: MQ-06 A failed play leaves a trace and reaches the next turn
    Given playing the clip on the speaker fails
    When the model has called clip_play and the answer is said
    Then the log says "could not play a clip" with the speaker and the error
    And the thread's context of the next turn says that the last clip could not be played

  @step1
  Scenario: MQ-07 An address that is not http or https is refused
    When the model calls clip_play with "file:///etc/passwd"
    Then the tool returns an error
    And nothing plays

  @step1
  Scenario: MQ-09 No listening while a clip plays
    Given a clip of an earlier answer still plays at the satellite
    When the user asks "Wie steht es?" and Mira's answer ends with a question
    Then the satellite does not listen after the answer
    And once the clip has ended, a question keeps it listening again

  @step1
  Scenario: MQ-10 A done plan stops its clip
    Given the last song's clip of the music quiz plays
    When the model ticks the last song in the plan
    Then the threads tell once that the plan is done, and the clip stops
    And the thread stays open, so the closing question is listened to

  @step1
  Scenario: MQ-11 A song is ticked with its result
    Given the music quiz's plan with "- [ ] a-ha – Take on Me (1985)" as step 2
    When the model calls thread_plan check, step 2, note "1 Punkt"
    Then the line reads "- [x] a-ha – Take on Me (1985) → 1 Punkt" and no other line changed
    And a note with write or uncheck, or longer than 200 characters, is the tool's error and changes nothing

  # --- The skill ------------------------------------------------------------------------------------------

  @step2
  Scenario: MQ-08 A round of the music quiz
    Given a voice conversation at the fake Show
    When the user says "Lass uns ein Musik-Quiz spielen, 80er, wir raten das Jahr"
    Then Mira writes a plan with ten songs, each with artist, title and year
    And searches every song on Deezer in one round and checks every year at MusicBrainz by its ISRC in the next
    And calls clip_play for the first song without naming it
    When the user answers with a year
    Then Mira rates it, shows a card with the song and calls clip_play for the next song
