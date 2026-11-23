# Testable rules of docs/spec/fake-devices.md. Each scenario has a fixed number (FG-nn) that is never reused; its
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
    Then the speaker is "playing" and the satellite stays "idle"
    And after the file's length the speaker is "idle"

  @step1
  Scenario: FG-06 The title names the next playback once
    Given esphome.fake_show_media_title was called with "Nachricht von Oma"
    When the speaker gets play_media
    Then its media_title is "Nachricht von Oma"
    And when the playback has ended it has no media_title
    And the next play_media has no media_title

  @step1
  Scenario: FG-08 The station names the next playback once
    Given esphome.fake_show_media_station was called with "SWR3"
    When the speaker gets play_media
    Then its media_channel is "SWR3"
    And when the playback has ended it has no media_channel
    And the next play_media has no media_channel

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

  @step9
  Scenario: FG-18 A question is answered by a voice turn
    When the Fake Show gets assist_satellite.ask_question with the answers "ja" and "nein"
    Then the satellite goes "responding", "idle", "listening", in that order
    When a test calls techo5_fake.say for the Fake Show with "Ja, alles gut"
    Then the action answers with the id "ja"
    And the satellite goes "idle", without "processing"

  @step9
  Scenario: FG-19 A question nobody answers
    When the Fake Show gets assist_satellite.ask_question and nobody speaks
    Then the satellite goes "idle" after the listening time
    And the action fails with "No answer received from the satellite"

  @step10
  Scenario: FG-20 The speaker plays while an answer is spoken
    When a test calls techo5_fake.say for the Fake Show and the answer has text
    Then the satellite goes "responding" and the speaker "playing"
    And when the answer is spoken the speaker goes "idle", then the satellite

  @step10
  Scenario: FG-21 The wake word cuts an answer off
    Given the Fake Show speaks an answer
    When a test calls techo5_fake.wake_word
    Then the speaker goes "idle", then the satellite "listening"
    And techo5_fake.say answers with interrupted true

  @step10
  Scenario: FG-22 An empty answer is not spoken
    When the answer of a voice turn is empty
    Then the satellite does not go "responding" and the speaker does not play

 (testenv/fritz_fake as mira_fritz)

  @step7
  Scenario: FG-30 New recordings raise the count of new messages at once
    When a test adds two recordings with mira_fritz.fake_add
    Then the sensor of new messages goes from 0 to 2 in one change

  @step7
  Scenario: FG-31 The actions answer as Mira FRITZ!Box's
    Given two new recordings
    Then get_messages with new_only lists both with id, name, date, minutes, new, media_content_id and the machine's name
    When mark_heard takes the first
    Then the count is 1 and get_messages with new_only lists only the second
    And get_calls lists no missed call and get_phonebook has no contacts

  @step7
  Scenario: FG-32 The box is emptied for the next scenario
    Given a recording
    When a test calls mira_fritz.fake_reset
    Then get_messages lists nothing and the count is 0

  @step11
  Scenario: FG-33 The test HA has registry entries of another type on purpose
    Given watch_fake is set up
    Then the device registry has a device whose identifier is a number
    And the entity registry has an entity of that device whose unique_id is a number
