Feature: The house structure from Home Assistant's prompt (docs/spec/context.md, 1.2)

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
