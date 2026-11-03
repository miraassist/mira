# Testable rules of docs/spec/history.md. Each scenario has a fixed number (VL-nn) that is never reused; its test
# carries the number in its name (TestVL01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: VL Questions about the past

  @step1
  Scenario: VL-01 A window's day becomes periods with durations and totals
    Given binary_sensor.fenster_bad was closed until 07:12, open until 07:40, closed until 18:05, open until 18:20
    When the model calls state_history for binary_sensor.fenster_bad with period "today"
    Then the answer lists the four periods with their durations
    And it says "offen 43 min, 2-mal" for the whole day

  @step1
  Scenario: VL-02 The state at the start of the period counts from the start
    Given the light was switched on yesterday at 22:00 and off today at 01:30
    When the model asks for "today"
    Then the first period is "an" from 00:00 to 01:30

  @step1
  Scenario: VL-03 A long history is shortened, the totals are not
    Given a motion sensor changed 80 times today
    When the model asks for "today"
    Then the answer lists the first 3 and the last 6 periods and says how many are left out
    And the totals and counts cover all 80 changes
    And the answer is shorter than 1,000 characters

  @step1
  Scenario: VL-04 The last period ends with the live state
    Given the recorder has not yet written a switching from a second ago
    When the model asks for "today"
    Then the last period is the live state, since the time it changed

  @step1
  Scenario: VL-05 An entity not exposed to Assist is answered like an unknown one
    Given light.secret exists but is not exposed to Assist
    When the model calls state_history for light.secret
    Then the answer is the same as for an entity that does not exist
    And Home Assistant is not asked

  @step1
  Scenario: VL-06 A period beyond 31 days or in the future is refused with the reason
    When the model asks with from 40 days ago
    Then the tool refuses and names the longest period
    And Home Assistant is not asked

  @step1
  Scenario: VL-07 A history that begins later than asked says so
    Given the recorder keeps the history of the last 10 days
    When the model asks for "last_31_days"
    Then the answer says from which day the history is there
    And it does not count the days before as one state

  @step1
  Scenario: VL-08 The fixed periods follow Home Assistant's time zone
    Given Home Assistant's time zone is Europe/Berlin and it is 01:00 on the day after a clock change
    When the model asks for "yesterday"
    Then the period runs from midnight to midnight local time, 25 hours long

  @step2
  Scenario: VL-09 A power meter with a threshold becomes running periods
    Given sensor.waschmaschine_leistung (state_class measurement) drew about 500 W from 09:05 to 10:45 yesterday
    When the model calls state_history with period "yesterday" and above 5
    Then the answer is "über 5 W 9:05–10:45 (1 h 40 min)", from the 5-minute statistics

  @step2
  Scenario: VL-10 A measurement without a threshold gives minimum, maximum and mean per day
    When the model asks for a temperature sensor over "last_7_days"
    Then the answer has one line per day with minimum, maximum and mean

  @step2
  Scenario: VL-11 Older measurements come hourly, and the answer says so
    Given the 5-minute statistics reach back 10 days
    When the model asks for a power meter over "last_31_days" with above 5
    Then the last 10 days are periods to 5 minutes
    And the days before are whole hours whose maximum lay above 5 W, with the duration as an upper bound
    And the answer names from when on it is to 5 minutes and that it is hourly before

  @step2
  Scenario: VL-12 A number without statistics is not read from the raw history
    Given a sensor with numbers but without a state_class
    When the model calls state_history for it
    Then the answer says that Home Assistant keeps no statistics for it
