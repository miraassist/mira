Feature: The house structure from Home Assistant's prompt (docs/spec/context.md, 1.1 to 1.3)

  Without Home Assistant's prompt Mira does not answer; nothing replaces it.

  @step1
  Scenario: HP-01 A turn without Home Assistant's prompt answers with a fixed sentence
    Given Home Assistant's prompt was never fetched
    When someone speaks to Mira
    Then Mira answers that the description of the house is missing and Repairs say why
    And no model is called

  @step1
  Scenario: HP-02 A failed fetch is a problem until the prompt comes
    Given the fetch of Home Assistant's prompt failed and there is none
    Then GET /v1/status names the problem "prompt" of the source "ha" with the reason
    When the prompt comes
    Then the problem is gone

  @step1
  Scenario: HP-03 A background job without Home Assistant's prompt fails
    Given Home Assistant's prompt was never fetched
    When a background job runs
    Then it fails and no model is called

  @step1
  Scenario: HP-04 A short outage keeps the last prompt
    Given Home Assistant's prompt was fetched
    When a later fetch fails
    Then the turn has the prompt fetched before
    And there is no problem

  @step1
  Scenario: HP-05 There is no own structure block
    Given the house structure is built from the registries
    Then it has no text for the prompt
    And the config has no assist_prompt_server

  @step2
  Scenario: HP-06 The repair for the source ha comes and goes
    Given the Mira service reports the problem "prompt" of the source "ha"
    Then Home Assistant shows the repair that Mira does not get the description of the house
    When the problem is gone from the status
    Then the repair is gone

  @step3
  Scenario: HP-07 A scheduled job waits for Home Assistant's prompt
    Given Home Assistant's prompt was never fetched
    When a job of the scheduler starts
    Then it waits for the prompt
    When the prompt comes
    Then the job runs

  @step3
  Scenario: HP-08 A job that waited in vain runs and fails as before
    Given Home Assistant's prompt was never fetched
    When a job of the scheduler waits the whole wait and the prompt does not come
    Then the log names the wait and the job runs
    And it fails as every job without the prompt, with its last run failed

  # docs/spec/context.md, 1.3: when the house changes

  @step1
  Scenario: HU-01 A list of exposed entities from the integration rebuilds the house at once
    Given registry changes are gathered for 30 seconds
    When the integration sends a list of exposed entities
    Then the house structure is rebuilt at once from the registries

  @step1
  Scenario: HU-02 A rebuild waiting for registry changes is dropped by the one at once
    Given a registry change waits for its rebuild
    When the house structure is rebuilt at once
    Then the registries are read once
    And no rebuild follows when the wait is over

  @step1
  Scenario: HU-03 A failed rebuild keeps the last structure and the next one tries again
    Given the house structure was built
    When a rebuild at once cannot read the registries
    Then the last structure stays and the log names the failed rebuild
    When the next rebuild at once can read them
    Then the structure is the new one

  @step2
  Scenario: HU-04 A new entity reaches the Mira service at once
    Given Home Assistant exposes new entities on its own
    When a new entity is created
    Then the integration sends the list of exposed entities after a moment, with the new entity

  # docs/spec/context.md, 1.1: every asking device is compared

  @step1
  Scenario: AD-01 A companion app is an asking device
    Given the device registry has a satellite and a companion app
    Then both are asking devices
    And Home Assistant's prompt and tools are fetched as each of them

  @step1
  Scenario: AD-02 A device that can start timers where no compared one can gets its own lines
    Given every compared version ends with "This device is not able to start timers."
    When a turn comes from a device that is not compared and whose version lacks that line
    Then all versions are fetched again with it, and the turn gets its own lines
    And part 1 ends before the timer line from then on, also after the next refresh

  @step1
  Scenario: AD-03 The fetch of all versions fails on that turn
    Given a turn comes from a device that is not compared and whose version does not begin with part 1
    When fetching all versions again fails
    Then the turn is refused and the error names the failed fetch

  @step1
  Scenario: AD-04 An asking device whose version fits stays in the comparison
    Given a turn comes from a device that is not compared and whose version begins with part 1
    Then the turn gets the rest as its lines
    And its version is fetched with every refresh

  @step1
  Scenario: AD-05 A turn without its device lines says so, without Repairs
    Given the asking device's lines cannot be had
    When its turn comes
    Then Mira says that she lacks what Home Assistant says about this device
    And the sentence does not mention Repairs
