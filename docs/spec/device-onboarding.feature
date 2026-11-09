# Testable rules of docs/spec/device-onboarding.md. Each scenario has a fixed number (DV-nn) that is never reused; its
# test carries the number in its name (TestDV01_…). The tags @stepN say in which step of the spec ("Steps") the
# scenario gets built.

Feature: DV Adding a Zigbee device by voice

  @step3
  Scenario: DV-01 Pairing opens through Zigbee2MQTT
    Given a Zigbee2MQTT bridge in Home Assistant
    When the thread calls zigbee_pairing_start
    Then permit_join is published with time 180
    And the tool returns once the bridge answers ok

  @step3
  Scenario: DV-02 No bridge, no pairing
    Given no Zigbee2MQTT bridge in Home Assistant
    When the thread calls zigbee_pairing_start
    Then the tool returns that no Zigbee2MQTT was found
    And nothing is published

  @step3
  Scenario: DV-03 The new device is announced at the thread's satellite
    Given pairing is open for a thread at the Show
    When the interview succeeds and Home Assistant creates the device with that IEEE address
    Then pairing is closed
    And the device is written into the thread
    And the thread's question is spoken at the Show with start_conversation

  @step3
  Scenario: DV-04 The answer names the room
    Given the thread asked for the room of the new device
    When the answer at the Show is "Wohnzimmer"
    Then the device's area is the living room

  @step3
  Scenario: DV-05 Finishing renames and exposes
    Given the device is in the living room
    When the thread calls device_finish with "Temperatursensor Wohnzimmer"
    Then the rename is published with homeassistant_rename true
    And every entity of the device but configuration and diagnostic ones is exposed to Assist
    And the thread is done

  @step3
  Scenario: DV-06 Nothing joins
    Given pairing is open for a thread at the Show
    When Zigbee2MQTT ends pairing without a device
    Then Mira says at the Show that she saw no device and asks whether to try again

  @step3
  Scenario: DV-07 The interview fails
    Given pairing is open for a thread at the Show
    When the interview of a device fails
    Then Mira says so at the Show
    And pairing stays open

  @step3
  Scenario: DV-08 A known device paired again
    Given a device with that IEEE address is already in Home Assistant
    When it announces itself while pairing is open
    Then Mira says it is known, with its name and room
    And the thread is done

  @step3
  Scenario: DV-09 Cancelling closes pairing
    Given pairing is open for a thread at the Show
    When the user says "abbrechen"
    Then pairing is closed and the thread is cancelled

  @step3
  Scenario: DV-10 A refused change is said, not swallowed
    Given Home Assistant refuses the area update
    When the thread calls device_assign
    Then the tool returns Home Assistant's error
    And the log says what failed
