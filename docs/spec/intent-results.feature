# Scenarios of docs/spec/intent-results.md. Every scenario has a number (IR-01, …); the Go test that checks it carries
# the number in its name (TestIR01_…). @step1 is the MCP host.

Feature: IR Home Assistant's intent answers where nothing succeeded

  @step1
  Scenario: IR-01 An area named and every entity failed is an error
    Given a tool of the MCP server ha answers action_done with success only the area and failed the Dot 2
    When the tool's result reaches Mira
    Then it is an error and its text is Home Assistant's answer unchanged

  @step1
  Scenario: IR-02 A partial success stays a success
    Given a tool of ha answers action_done with an entity in success and another in failed
    When the tool's result reaches Mira
    Then it is no error

  @step1
  Scenario: IR-03 An answer that is no such intent answer is left as it is
    Given a tool of ha answers without failed, with a query_answer, or with a text that is no JSON
    When the tool's result reaches Mira
    Then it is no error, and an error Home Assistant raised itself stays an error

  @step1
  Scenario: IR-04 Other servers are not touched
    Given a tool of another MCP server answers action_done with success only an area and failed an entity
    When the tool's result reaches Mira
    Then it is no error

  @step1
  Scenario: IR-05 A marked answer leaves a log line
    Given a tool of ha answers with success only the area and failed the Dot 2
    When the tool's result reaches Mira
    Then the log has "home assistant acted on nothing" with the tool and media_player.dot_2
