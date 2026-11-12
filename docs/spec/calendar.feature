Feature: Calendars – one building block for entering an event
  docs/spec/calendar.md, Adding: one building block, one tool.

  @step1
  Scenario: CA-01 An event with a time is entered
    Given Anna's calendar "Anna" is writable
    When calendar.create gets the title "Elternabend", the start "2026-12-03 19:30" and the location "Aula" for Anna
    Then Home Assistant's calendar.create_event is called once for calendar.anna with these values and the end 20:30
    And the answer names title, day, time and calendar

  @step1
  Scenario: CA-02 A whole day
    When calendar.create gets the title "schulfrei" and the day "2026-12-22" for Anna
    Then the event has the start date 2026-12-22 and the end date 2026-12-23

  @step1
  Scenario: CA-03 Dates that do not fit enter nothing
    When calendar.create gets, one call each, the start "2026-12-03 19:30" with the day "2026-12-03", the end "2026-12-03 18:00" before that start, and the start "2026-13-40 19:30"
    Then nothing is entered by any of them
    And the answer names the field and its value, and a log line too

  @step1
  Scenario: CA-04 The calendar by the one rule
    Given the writable calendars "Anna", "Ben" and "Familie"
    When calendar.create gets no calendar for Anna
    Then it enters into "Anna"
    When it gets the calendar "Familie"
    Then it enters into "Familie"

  @step1
  Scenario: CA-05 Another person's calendar is refused
    Given the writable calendars "Anna", "Ben" and "Familie"
    When calendar.create gets the calendar "Ben" for Anna
    Then nothing is entered
    And the answer names "Anna" and "Familie"

  @step1
  Scenario: CA-06 Without an own calendar and several others, it asks
    Given the writable calendars "Familie" and "Garten", and none of Anna's
    When calendar.create gets no calendar for Anna
    Then nothing is entered
    And the answer asks which one, naming "Familie" and "Garten"

  @step1
  Scenario: CA-07 Home Assistant refuses
    Given Home Assistant refuses calendar.create_event
    When calendar.create enters an event
    Then the answer says it was not entered
    And a log line names the calendar, the title and the error

  @step2
  Scenario: CA-08 The tool comes with its skills only
    Given calendar_create belongs to the skill postfach
    Then a turn without a skill is not offered calendar_create
    And a turn with postfach open is

  @step2
  Scenario: CA-09 A tool of several skills
    Given a tool of Mira's that belongs to the skills "a" and "b"
    Then it is offered in a turn with "a" open, and in one with "b" open, and not in one with only "c"
