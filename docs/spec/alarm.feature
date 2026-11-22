# Scenarios of docs/spec/alarm.md. Every scenario has a number (WK-01, …); the Go test that checks it carries the
# number in its name (TestWK01_…). @step2 is the wake sound, @step3 the tool and the integration, @step4 radio and
# light.

Feature: WK Alarm

  @step2
  Scenario: WK-01 The wake sound is served like Mira's other sounds
    Given the Mira service with its sounds
    When the integration is asked for /api/mira/panel/ui/sounds/wake_rise.mp3 and wake_loop.mp3
    Then both are served without a login

  @step3
  Scenario: WK-02 A TECHO5 gets Mira's alarm, not the device's
    Given a turn at a satellite whose device has the action alarm_set
    When the model calls alarm with time 06:00 and no days
    Then an automation labelled mira_alarm is saved
    And esphome.<node>_alarm_set is not called

  @step3
  Scenario: WK-03 A satellite gets Mira's automation
    Given a turn at a satellite with the feature ANNOUNCE
    When the model calls alarm with time 06:00 and days weekdays
    Then an automation labelled mira_alarm is saved, with the time 06:00, the days weekdays, the person and the area
    And its ring sets the satellite's speaker to 0.9, then announces wake_rise and then wake_loop at that satellite

  @step3
  Scenario: WK-04 A satellite that can take no announcement gets no alarm
    Given a turn at a satellite without the feature ANNOUNCE
    When the model calls alarm with time 06:00
    Then nothing is set
    And the tool says that this satellite cannot wake

  @step3
  Scenario: WK-05 The wake word ends the ringing
    Given Mira's alarm ringing at the fake Show
    When the fake Show hears the wake word
    Then the automation's ringing run ends
    And no further round is announced

  @step3
  Scenario: WK-06 Without a reaction the ringing stops after ten minutes
    Given Mira's alarm ringing at the fake Show
    When nobody reacts for ten minutes
    Then no further round is announced

  @step3
  Scenario: WK-07 Another turn at the satellite starts nothing
    Given Mira's alarm set for 06:00 at the fake Show, not ringing
    When the fake Show hears the wake word at 05:00
    Then the automation does not run

  @step3
  Scenario: WK-08 A refusal of the integration reaches the user and the log
    Given the integration refuses the alarm's automation
    When the model calls alarm with time 06:00
    Then nothing is set
    And the tool says that the alarm could not be set
    And the log names the refusal

  @step3
  Scenario: WK-09 A once-alarm is deleted after it rang
    Given Mira's once-alarm that rang and has no running run
    When the service tidies the alarms
    Then the automation is deleted

  @step3
  Scenario: WK-10 The person's alarms are listed
    Given Anna's alarm at 06:00 in the Schlafzimmer and Ben's at 07:00 in the Kinderzimmer
    When Anna asks which alarms she has
    Then the tool lists 06:00 in the Schlafzimmer and not Ben's

  @step4
  Scenario: WK-11 An alarm with a station plays the station instead of the wake sound
    Given a turn at a satellite without alarms of its own, Music Assistant with a player in the area
    When the model calls alarm with time 06:00 and music SWR3, music_kind radio
    Then the station is searched once at setting time
    And the automation plays it on the area's Music Assistant player at 06:00

  @step4
  Scenario: WK-12 An alarm with light turns the area's lights on slowly
    When the model calls alarm with time 06:00 and light on
    Then the automation turns the area's lights on with a transition

  @step3
  Scenario: WK-13 A volume turned down the night before does not make the alarm quiet
    Given the fake Show's speaker at volume 0.1
    When Mira's alarm rings at the fake Show
    Then the speaker is at 0.9 before the first announcement

  @step3
  Scenario: WK-14 Without a satellite no alarm is set
    Given a turn from Telegram, without an asking device
    When the model calls alarm with time 06:00
    Then nothing is set
    And the tool says that an alarm is set only at a satellite, in the room where it is to ring

  @step4
  Scenario: WK-15 An alarm with a playlist, an album or an artist plays it through Music Assistant
    Given a turn at a satellite whose area has a Music Assistant player
    When the model calls alarm with time 06:00, music "Queen" and music_kind artist
    Then Music Assistant's artist Queen is searched once at setting time
    And the automation plays it on the area's Music Assistant player at 06:00

  @step4
  Scenario: WK-16 Music other than a station needs Music Assistant
    Given a turn at a satellite whose area has no Music Assistant player
    When the model calls alarm with music "Morgen-Mix" and music_kind playlist
    Then nothing is set
    And the tool says that this needs Music Assistant
