# Testable rules of docs/spec/schedule-receiver.md. Each scenario has a fixed number (PS-nn) that is never reused; its
# test carries the number in its name (TestPS01_…). The tags @stepN say in which step of the spec ("Steps") the
# scenario gets built.

Feature: PS A schedule for one person

  @step1
  Scenario: PS-01 A private message goes out over the channels at the tick
    Given a pending private message for Stefan, who has a Telegram channel
    When the notifier ticks outside quiet hours
    Then the text and the details are sent over that channel
    And the message is marked delivered there

  @step1
  Scenario: PS-02 A private message is never spoken
    Given a pending private message for Stefan, who is alone at home, and confidential_voice when_alone
    When someone enters a room with a satellite
    Then nothing is spoken
    And the message is sent over Stefan's channel
    And it does not count as waiting for the light, the screen or "Übrigens"

  @step1
  Scenario: PS-03 A private message waits for the end of quiet hours
    Given a pending private message of importance 1
    When the notifier ticks in quiet hours
    Then nothing is sent

  @step1
  Scenario: PS-04 An urgent private message is not announced
    Given a pending private message of importance 3
    When the notifier delivers urgent messages
    Then it is sent over the channels
    And no satellite announces it

  @step2
  Scenario: PS-05 A schedule with to sends its result privately to that person
    Given a skill schedule with to "Stefan", and Stefan has a channel
    When the job reports importance 1 with a spoken text and details
    Then one message is queued for Stefan with confidentiality private
    And the job ran once, not once per person

  @step2
  Scenario: PS-06 An unknown person in to fails the run
    Given a skill schedule with to "Niemand"
    When the schedule runs
    Then the run fails with no person "Niemand" with a channel of their own
    And no message is queued, not for the house either

  @step2
  Scenario: PS-07 to is filled from the settings, and excludes per_person
    Given a skill with the setting empfaenger "stefan" and a schedule with to "{{empfaenger}}"
    When the skill is loaded
    Then the schedule's to is "stefan"
    And a schedule with to and per_person: true does not start the skill

  @step2
  Scenario: PS-08 The person is found by key, name or login
    Given the person stefan named "Stefan Rinke" with the login "srinke"
    When to is "stefan", "stefan rinke" or "SRinke"
    Then the message goes to stefan
