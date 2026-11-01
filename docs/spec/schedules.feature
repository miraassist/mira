# Testable rules of docs/spec/schedules.md. Each scenario has a fixed number (ZP-nn) that is never reused; its test
# carries the number in its name (TestZP01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.
#
# Groups: ZP-0x names and mira.yaml · ZP-1x the last run · ZP-2x the page

Feature: ZP Schedules

  # --- Names and mira.yaml -----------------------------------------------------------------------------------

  @step1
  Scenario: ZP-01 An entry in mira.yaml replaces the skill's time
    Given the skill briefing has the schedule "morgen" at "30 5 * * *"
    And mira.yaml has skills.briefing.schedules.morgen.cron "45 6 * * 1-5"
    When Mira starts
    Then "morgen" runs next on the next weekday at 06:45

  @step1
  Scenario: ZP-02 A schedule switched off does not run and keeps its time
    Given mira.yaml has skills.briefing.schedules.abend.enabled false
    When the skill's time for "abend" comes
    Then the job does not run
    And the list shows "abend" off, with its time and no next run

  @step1
  Scenario: ZP-03 Without an entry the skill's own time holds
    Given mira.yaml has no entry for the schedule "morgen"
    When the skill comes with a new time for "morgen"
    Then "morgen" runs at the new time

  @step1
  Scenario: ZP-04 An entry for a schedule the skill has not is refused
    Given mira.yaml has skills.briefing.schedules.mittag
    When Mira loads mira.yaml
    Then the entry is refused with the reason, as an unknown setting

  @step1
  Scenario: ZP-05 Two schedules of a skill with one name stop the skill
    Given a skill with two schedules named "morgen"
    When Mira loads the skills
    Then the skill does not start, and the reason names the schedule

  @step1
  Scenario: ZP-06 A change holds at once
    Given Mira runs with "morgen" at 05:30
    When the page saves "morgen" for 06:45
    Then the next run of "morgen" is at 06:45, without a restart

  # --- The last run ------------------------------------------------------------------------------------------

  @step2
  Scenario: ZP-10 A run keeps its result
    When the schedule "morgen" has run and queued a message
    Then its last run has the time, "done" and the spoken text
    And a run that found nothing to say has "nothing to report"
    And a run that failed has "failed" and the error

  @step2
  Scenario: ZP-11 A per-person schedule keeps one last run per person
    When the schedule "persoenlich" has run for two persons
    Then each person has its own last run

  @step2
  Scenario: ZP-12 Mira's own schedule keeps its last run
    When the profiles have been rebuilt at night
    Then the last run of Mira's schedule says how many profiles were rebuilt

  # --- The page ----------------------------------------------------------------------------------------------

  @step3
  Scenario: ZP-20 The list has every schedule
    When the page asks for the schedules
    Then each schedule of each skill is listed, with its description, its time in words and its next run
    And Mira's own is listed last, without a switch

  @step3
  Scenario: ZP-21 The switch writes mira.yaml
    When the page switches "abend" off
    Then mira.yaml has skills.briefing.schedules.abend.enabled false
    And the schedule does not run any more

  @step3
  Scenario: ZP-22 Time and weekdays become a cron expression
    When the page saves "morgen" for Monday to Friday at 06:45
    Then mira.yaml has skills.briefing.schedules.morgen.cron "45 6 * * 1-5"

  @step3
  Scenario: ZP-23 A time the fields cannot show is not changed on the page
    Given the schedule of warnungen runs every hour
    Then the list shows it "stündlich", with a switch and without time fields
    And a PUT with a cron for it is refused

  @step3
  Scenario: ZP-24 Run now runs the job and shows its result
    When the page runs "morgen" now
    Then the answer comes at once and the schedule is "running"
    And afterwards its last run is the new one
    And a message of the same day is updated, not added

  @step3
  Scenario: ZP-25 Run now on a running schedule is refused
    Given "morgen" is running
    When the page runs "morgen" now
    Then the answer is "It is running"

  @step3
  Scenario: ZP-26 A schedule without a name is shown, not changed
    Given a user's skill with a schedule without a name
    Then the list shows it with the hint to give it a name
    And it has no switch and no time fields
