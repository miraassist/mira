# Testable rules of docs/spec/radio.md. Each scenario has a fixed number (RA-nn) that is never reused; its test
# carries the number in its name (TestRA01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: RA Radio

  @step2
  Scenario: RA-01 A device that can name the station is told it before it plays
    Given the room's speaker is on a device with the action esphome.<node>_media_station
    When the user says "Spiel SWR3"
    Then Mira calls esphome.<node>_media_station with the station's name as it is on Radio Browser
    And then media_player.play_media with the station

  @step2
  Scenario: RA-02 A device without the action plays as before
    Given the room's speaker is on a device without esphome.<node>_media_station
    When the user says "Spiel SWR3"
    Then Mira calls only media_player.play_media
    And if naming the station fails, the station plays all the same

  @step3
  Scenario: RA-03 On the satellite's own speaker the confirmation is short
    Given the user speaks to the satellite in the bathroom and its own speaker plays
    When the user says "Spiel SWR3"
    Then Mira confirms briefly, without naming the speaker or the room
    And on any other speaker, in that room or another, Mira names it
