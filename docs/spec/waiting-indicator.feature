Feature: Showing that Mira waits (docs/spec/waiting-indicator.md)

  @step3
  Scenario: WA-01 Pairing shows the wait on the Show
    Given a Zigbee thread at the Show
    When pairing opens
    Then the Show plays the waiting video

  @step3
  Scenario: WA-02 The device turns up and the video stops
    Given the waiting video plays at the Show
    When the device is there
    Then the video is stopped before Mira asks for the room

  @step3
  Scenario: WA-03 Cancelling stops the video
    Given the waiting video plays at the Show
    When the user says "abbrechen"
    Then the video is stopped

  @step3
  Scenario: WA-04 A satellite without video shows nothing
    Given a Zigbee thread at an Echo
    When pairing opens
    Then no video is played and nothing fails

  @step3
  Scenario: WA-05 The Show's video switch is turned on
    Given the Show's switch "Video" is off
    When pairing opens
    Then Mira turns the switch on, plays the video, and the log says so once

  @step3
  Scenario: WA-06 The wait shows once the satellite is idle after the answer
    Given pairing opened in a turn at the Dot
    When the satellite still answers the turn
    Then nothing is shown yet
    And when the satellite is idle the ring starts
    And a thread stopped before that shows nothing
