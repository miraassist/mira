# Testable rules of docs/spec/warm-slot.md. Each scenario has a fixed number (WS-nn) that is never reused; its test
# carries the number in its name (TestWS01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: WS Always a warm slot for local models

  @step2
  Scenario: WS-01 A turn after a change of the prompt runs on the previous one in the warm slot
    Given a local llama.cpp model whose prompt is warm in slot 0
    And Home Assistant's tool list has just changed
    When a voice turn starts before the new prompt is read
    Then the turn is built on the previous prompt and sent with id_slot 0

  @step2
  Scenario: WS-02 The new prompt is read into the other slot while the model is free
    Given the prompt changed and no turn runs
    Then Mira reads the new prompt into slot 1 with max_tokens 1
    And once it is read, the next turn is built on it and sent with id_slot 1

  @step2
  Scenario: WS-03 A turn stops a running reading, which starts again afterwards
    Given the new prompt is being read into slot 1
    When a voice turn starts
    Then the reading is cancelled and the turn runs on the previous prompt in slot 0
    And once the turn has ended, the reading starts again

  @step2
  Scenario: WS-04 A newer change replaces one not yet read
    Given a change of the prompt waits to be read
    When the prompt changes again
    Then only the newest prompt is read

  @step2
  Scenario: WS-05 A model without warm_slots works as today
    Given a model without the setting warm_slots
    Then turns are sent as today (with pin_slots, if set) and the warmup works as today

  @step2
  Scenario: WS-06 warm_slots is refused for a cloud model
    Given a model with privacy cloud and warm_slots true in mira.yaml
    Then the configuration is refused with the reason

  @step2
  Scenario: WS-07 A helper runs in the spare slot and leaves the warm one alone
    Given a local llama.cpp model with warm_slots whose prompt is warm in slot 0
    When one of Mira's helpers (an agent) calls the model
    Then its request is sent with id_slot 1
    And a running reading in slot 1 is cancelled and starts again once the model is free
