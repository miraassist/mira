# Testable rules of docs/spec/fake-geraete.md. Each scenario has a fixed number (FG-nn) that is never reused; its
# test carries the number in its name (test_FG07_…). The tags @step1 … @step3 say in which step of the spec
# ("Steps") the scenario gets built.
#
# Groups: FG-0x the speaker · FG-1x the satellite

Feature: FG Fake satellites that behave like the real ones

  # --- The speaker -----------------------------------------------------------------------------------------

  @step1
  Scenario: FG-01 Music plays in the background
    When the Fake Show's speaker gets play_media without announce
    Then the speaker is "playing"
    And the satellite stays "idle"

  @step1
  Scenario: FG-02 A playback ends after the media's length
    When the Fake Show's speaker gets play_media with a test file of 2 seconds
    Then the speaker is "playing"
    And after the file's length it is "idle"

  @step1
  Scenario: FG-07 A stream plays until it is stopped
    When the Fake Show's speaker gets play_media with media that has no length
    Then the speaker stays "playing" until it gets media_stop

  @step1
  Scenario: FG-03 Stopping pauses
    Given the Fake Show's speaker plays
    When it gets media_stop
    Then the speaker is "paused"

  @step1
  Scenario: FG-04 A replacing playback reports no change
    Given the Fake Show's speaker plays
    When it gets another play_media
    Then no change of the speaker's state is reported

  @step1
  Scenario: FG-05 An announced playback plays in the foreground
    When the Fake Show's speaker gets play_media with announce and a test file
    Then the speaker is "playing" and the satellite "responding"
    And after the file's length both are "idle"

  @step1
  Scenario: FG-06 The title names the next playback once
    Given esphome.fake_show_media_title was called with "Nachricht von Oma"
    When the speaker gets play_media
    Then its media_title is "Nachricht von Oma"
    And when the playback has ended it has no media_title
    And the next play_media has no media_title

  # --- The satellite ---------------------------------------------------------------------------------------

  @step2
  Scenario: FG-10 A plain announcement ends idle
    When the Fake Show gets assist_satellite.announce
    Then the satellite goes "responding", then "idle"
    And the call returns after "idle"

  @step2
  Scenario: FG-11 Starting a conversation has a short idle before listening
    When the Fake Show gets assist_satellite.start_conversation
    Then the satellite goes "responding", "idle", "listening", in that order

  @step2
  Scenario: FG-12 The wake word lowers music
    Given the Fake Show's speaker plays music
    When a test calls techo5_fake.wake_word for the Fake Show
    Then the satellite is "listening"
    And the speaker's volume is lowered
    And when the satellite is "idle" again, the volume is as before and the music still plays

  @step2
  Scenario: FG-13 The wake word does not lower an announced playback
    Given the Fake Show's speaker plays with announce
    When a test calls techo5_fake.wake_word for the Fake Show
    Then the satellite is "listening"
    And the speaker's volume is unchanged

  @step2
  Scenario: FG-14 A device without a screen shows nothing
    When the Fake Echo gets notify
    Then it is logged and nothing else changes

  @step4
  Scenario: FG-15 A voice turn reports the states of a voice turn
    When a test calls techo5_fake.say for the Fake Show with "Wie spät ist es?"
    Then the satellite goes "listening", "processing", "responding", "idle", in that order
    And the action answers with the agent's text

  @step4
  Scenario: FG-16 Background music is lowered for the whole voice turn
    Given the Fake Show's speaker plays music
    When a test calls techo5_fake.say for the Fake Show
    Then the speaker's volume is lowered until the satellite is "idle" again

  @step4
  Scenario: FG-17 A longer text takes longer to speak
    When the Fake Show announces a sentence of 15 characters and then one of 90
    Then the second keeps it "responding" longer than the first
