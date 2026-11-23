# Scenarios of docs/spec/parsing.md. Every scenario has a number (PA-01, …); the Go test that checks it carries the
# number in its name (TestPA01_…), the integration's pytest test_pa…. @step1 is what is proven, @step2 a broken
# mira.yaml, @step4 the integration.

Feature: PA Reading what comes in

  @step1
  Scenario: PA-01 Entries of Home Assistant Mira cannot read are a repair
    Given an entry of Home Assistant's registries cannot be read
    When the house structure is built
    Then the problem names the entry
    And once it reads again, the problem is gone

  @step1
  Scenario: PA-02 The found devices' list leaves out a device it cannot read
    Given one device of the registry cannot be read
    When the found devices read the registry
    Then the other devices are there and the log names the one left out

  @step1
  Scenario: PA-03 A unique_id that is a number is read
    Given an entity's unique_id is a number
    When a new device's entities are read
    Then the entity is there with the number as its unique_id

  @step1
  Scenario: PA-04 A skill of the subscription that cannot be read is left out
    Given the skill source lists a skill whose entry cannot be read
    When the skills are loaded
    Then the other skills of the subscription load
    And that skill has a problem naming why

  @step4
  Scenario: PA-05 A device's name that is a number is read by the integration
    Given a device in Home Assistant has a number as its name
    When the integration lists the mail calendars
    Then it reads the name as text and does not fail

  @step1
  Scenario: PA-06 No parse error goes without a trace
    Then every place that drops a parse error logs it or says why it cannot fail

  @step2
  Scenario: PA-07 A mira.yaml that cannot be read starts Mira limited
    Given mira.yaml cannot be read
    When the service starts
    Then it serves Home Assistant's integration, and its status carries the problem with the file, the line and the error
    And a turn gets the sentence that Mira's settings cannot be read

  @step2
  Scenario: PA-08 A corrected mira.yaml starts Mira anew
    Given Mira runs limited because mira.yaml cannot be read
    When the file is saved again and reads
    Then the service starts anew
