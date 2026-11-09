Feature: Devices Home Assistant found (docs/spec/found-devices.md)

  @step3
  Scenario: FD-01 A discovered device is said once as a message to the house
    Given the skill geraet-einbinden is on
    When Home Assistant discovers a Hue Bridge
    And 60 seconds pass without another find
    Then a message to the house says "Ich habe ein neues Gerät gefunden: Hue Bridge. Soll ich es einbinden?"
    And the flow is marked said

  @step3
  Scenario: FD-02 A burst of new devices is one message
    When an integration adds five devices without an area within a minute
    Then one message names the first three and "2 weitere"

  @step3
  Scenario: FD-03 Said once, never again
    Given a found device was said and nobody answered
    When Mira restarts
    Then the device is not said again
    And "Gibt's neue Geräte?" still lists it

  @step3
  Scenario: FD-04 What does not count is not said
    When a device without entities to expose is created
    Or a device gets an area within the 60 seconds
    Or a device the Zigbee dialog waits for is created
    Or Mira's own integration is discovered
    Then no message is sent

  @step3
  Scenario: FD-05 Home Assistant's backlog is not read out
    Given Home Assistant has two discovered flows open
    When the noticing runs for the first time
    Then no message is sent
    And both are marked said and listed on request

  @step4
  Scenario: FD-06 A new device is taken in: room, name, exposed
    Given a message named a new device without an area
    When the user answers "Ja", then "Küche", then "Ja"
    Then the device is in the kitchen, named by Home Assistant's name, its entities exposed
    And the thread is done

  @step4
  Scenario: FD-07 A discovered device with a confirmation only
    Given a message named a discovered flow whose step has no fields
    When the user answers "Ja"
    Then the step is submitted, the entry created, and Mira asks for the room of its device

  @step4
  Scenario: FD-08 A step that needs the button pressed
    Given the discovered flow's step fails until a button is pressed
    When the user says "Ja" before pressing it
    Then Mira says the step's text and the flow stays open
    When the user presses it and says "erledigt"
    Then the entry is created

  @step4
  Scenario: FD-09 A step with fields is left to Home Assistant
    Given the discovered flow's step asks for a password
    When the user answers "Ja"
    Then Mira says it is set up in Home Assistant under Discovered
    And nothing is submitted

  @step4
  Scenario: FD-10 Ignoring a discovered device uses Home Assistant's ignore
    When the user answers "Nein, das ist vom Nachbarn" to a discovered flow
    Then the flow is ignored in Home Assistant
    And it is not listed any more

  @step4
  Scenario: FD-11 Ignoring a new device labels it
    When the user answers "ignorieren" to a new device
    Then the device has the label "Mira: ignoriert"
    And it is not listed any more

  @step4
  Scenario: FD-12 A refused step is said, not swallowed
    Given Home Assistant refuses to submit the step
    When the user answers "Ja"
    Then Mira says Home Assistant's error
    And the log says what failed
