Feature: Calendars – one building block for entering an event
  docs/spec/calendar.md, Adding: one building block, one tool.

  @step1
  Scenario: CA-01 An event with a time is entered
    Given Anna's calendar "Anna" is writable
    When calendar.create gets the title "Elternabend", the start "2026-12-03 19:30" and the location "Aula" for Anna
    Then Home Assistant's calendar.create_event is called once for calendar.anna with these values and the end 20:30

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

  @step3
  Scenario: CA-10 Without a conflict the event is entered without a question
    Given the calendar "Familie" has nothing on Friday between 15:00 and 16:00
    When the skill termin enters "Zahnarzt" on Friday at 15:00 in "Familie"
    Then it is entered at once, as before

  @step3
  Scenario: CA-11 A conflict asks before entering
    Given the calendar "Familie" has "Elternabend" on Friday from 15:30 to 16:30
    When the skill termin enters "Zahnarzt" on Friday at 15:00 in "Familie"
    Then nothing is entered
    And the thread proposes, naming "Elternabend" and 15:30–16:30
    And after a yes the event is entered once

  @step3
  Scenario: CA-12 Only the calendar it goes into counts
    Given the calendar "Anna" has "Training" on Friday from 15:00 to 16:00
    When the skill termin enters "Zahnarzt" on Friday at 15:00 in "Familie"
    Then it is entered at once

  @step3
  Scenario: CA-13 Whole days are no conflict
    Given the calendar "Familie" has "Geburtstag Oma" all day on Friday
    When the skill termin enters "Zahnarzt" on Friday at 15:00 in "Familie"
    Then it is entered at once
    And a new all-day event on Friday is entered without reading the calendar's events

  @step3
  Scenario: CA-14 Touching is no conflict
    Given the calendar "Familie" has "Friseur" on Friday from 14:00 to 15:00
    When the skill termin enters "Zahnarzt" on Friday from 15:00 to 16:00 in "Familie"
    Then it is entered at once

  @step3
  Scenario: CA-15 A calendar that cannot be read does not stop the entering
    Given Home Assistant answers the calendar's events with HTTP 500
    When the skill termin enters "Zahnarzt" on Friday at 15:00 in "Familie"
    Then it is entered
    And a log line names the calendar, the title and the error, and the answer says conflicts were not checked
