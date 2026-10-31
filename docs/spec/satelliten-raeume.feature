# Testable rules of docs/spec/satelliten-raeume.md. Each scenario has a fixed number (SR-nn) that is never reused;
# its test carries the number in its name (TestSR05_… in Go, test_SR01_… in pytest). The tags @step1 … @step4 say in
# which step of the spec ("Steps") the scenario gets built. Steps 1 and 3 are the integration's, tested in pytest.
#
# Groups: SR-0x a satellite without an area · SR-1x the integration's rooms · SR-2x the service's rooms

Feature: SR Satellites and rooms

  # --- A satellite without an area ---------------------------------------------------------------------------

  @step1
  Scenario: SR-01 A satellite without an area raises a repair
    Given an Assist satellite that is on, and neither it nor its device has an area
    When the integration starts or the registries change
    Then Home Assistant shows a repair for it that names it and links to its device

  @step1
  Scenario: SR-02 The repair goes when the satellite gets an area
    Given the repair for a satellite without an area
    When its device gets an area, or it is switched off or removed
    Then the repair is gone

  @step1
  Scenario: SR-03 The page lists a satellite without an area greyed
    Given an Assist satellite without an area
    When the page "Satellites" lists the satellites
    Then it is listed with the reason, and it has nothing to set

  @step1
  Scenario: SR-04 A satellite with an area raises nothing
    Given every Assist satellite has an area, its own or its device's
    When the registries change
    Then there is no repair of Mira for a satellite

  @step2
  Scenario: SR-05 A turn's area is the satellite entity's, else its device's
    Given a satellite whose entity has an area and whose device has none
    When a turn comes from its device
    Then the turn is in the entity's area
    And a satellite whose entity has no area is in its device's area, as before

  # --- The integration's rooms -------------------------------------------------------------------------------

  @step3
  Scenario: SR-10 A second satellite in an area makes the first the primary
    Given an area with one satellite, without a mark
    When a second satellite is given that area, even one older in the entity registry
    Then the satellite that was there is marked primary
    And the rooms sent to Mira have one room for the area, with the primary as its satellite and the other in others

  @step3
  Scenario: SR-11 The switch makes another satellite the primary
    Given a room with two satellites, the first marked primary
    When the second is saved with "Main satellite in the room" on
    Then the second is marked primary and the first is not

  @step3
  Scenario: SR-12 A room without a primary gets one again
    Given a room with three satellites whose primary is switched off
    When the registries change
    Then one of the two left is marked primary, the older one after a restart

  @step3
  Scenario: SR-13 Two satellites in an area with settings are not refused
    Given two satellites in one area, each with motion sensors
    When their settings are saved
    Then they are stored, and the room has both, each with its own sensors

  @step3
  Scenario: SR-14 A room with one satellite is sent as before
    Given an area with one satellite
    When the rooms are sent to Mira
    Then the room has no others, and the satellite carries no mark

  # --- The service's rooms -----------------------------------------------------------------------------------

  @step4
  Scenario: SR-20 A room with others becomes a list, the primary first
    Given the integration sends a room with a satellite and others
    When Mira takes the rooms
    Then the room's satellites are the primary first, then the others
    And the indicators of all of them are the room's
    And the others' motion sensors are the room's, each speaking at its own satellite

  @step4
  Scenario: SR-21 A room without others is taken as before
    Given the integration sends rooms without others
    When Mira takes the rooms
    Then each room has one satellite, its indicator and its sensors, as before

  @step4
  Scenario: SR-22 The morning announcement comes where the motion sensor fired
    Given a room with two satellites announcing on entry, each with its own motion sensor
    When the sensor of the satellite that is not primary fires
    Then Mira speaks what waits at that satellite

  @step4
  Scenario: SR-23 A sensor set on two satellites speaks at the primary
    Given a room with two satellites that share a motion sensor
    When it fires
    Then Mira speaks at the primary

  @step4
  Scenario: SR-24 A reminder comes at the satellite it was asked at
    Given a room with two satellites
    When a reminder "here" was asked at the satellite that is not primary, and it is due
    Then it is spoken at that satellite

  @step4
  Scenario: SR-25 A reminder from a satellite that left the room comes at the primary
    Given a reminder asked at a satellite that has since moved to another area
    When it is due
    Then it is spoken at the primary of the room it was asked for

  @step4
  Scenario: SR-26 A message to the room comes at the primary
    Given a room with two satellites
    When a message for the room is announced without being asked at one of them
    Then it is spoken at the primary

  @step4
  Scenario: SR-27 Every ring of the room shows what waits
    Given a room with two satellites, each with its ring
    When a message waits for the room
    Then both rings show it, and both go off when it is heard

  @step4
  Scenario: SR-28 An urgent announcement comes once per room
    Given two rooms, one with two satellites
    When an urgent message is announced everywhere
    Then it is spoken once in each room, at its primary

  @step4
  Scenario: SR-29 The answer of a thread comes at the satellite it began at
    Given a thread begun at the satellite of a room that is not primary
    When the thread tells its result later
    Then it is spoken at that satellite
