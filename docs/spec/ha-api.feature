Feature: Home Assistant through Mira's interface (docs/spec/ha-api.md)

  @step1
  Scenario: HE-01 Reading a helper
    Given Home Assistant has input_select.angehoerige_zustand with state "ok"
    When a token holder calls GET /v1/ha/states/input_select.angehoerige_zustand
    Then the answer is 200 with Home Assistant's state object, state "ok" and its attributes

  @step1
  Scenario: HE-02 Setting a helper by an action
    When a token holder calls POST /v1/ha/services/input_select/select_option with entity_id and option "pause"
    Then Mira calls the action input_select.select_option with that data once
    And the answer is 200

  @step1
  Scenario: HE-03 An action's response
    When a token holder calls an action that returns a response, with return_response
    Then the answer holds Home Assistant's response as it came

  @step1
  Scenario: HE-04 An entity Home Assistant does not have
    When a token holder reads input_boolean.gibtsnicht
    Then the answer is 404 naming input_boolean.gibtsnicht

  @step1
  Scenario: HE-05 An action Home Assistant refuses
    When a token holder calls input_select.select_option with an option the select does not have
    Then the answer is 400 with Home Assistant's message and the action's name
    And the log names the action and the message

  @step1
  Scenario: HE-06 Home Assistant cannot be reached
    Given Home Assistant does not answer
    When a token holder reads an entity
    Then the answer is 502 naming the failure
    And Mira did not try again

  @step1
  Scenario: HE-07 Without a token
    When a request to /v1/ha/states/x.y has no token
    Then the answer is 401

  @step1
  Scenario: HE-08 A partner's call is shown
    When the Partner service sets a helper with its partner token and a partner user
    Then the call shows on Mira's page with the token's name and the partner user
