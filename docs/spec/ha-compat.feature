Feature: Compatibility with Home Assistant (docs/spec/ha-compat.md)

  testha compat checks what Mira relies on against the running test Home Assistant.

  @step1
  Scenario: HC-01 A missing tool fails the check and names it
    Given Home Assistant's MCP server serves GetLiveContext and HassTurnOn only
    When the tools are checked
    Then the check fails and names GetDateTime and HassTurnOff

  @step1
  Scenario: HC-02 A missing prompt fails the check
    Given Home Assistant's MCP server serves no prompt
    When the prompt is checked
    Then the check fails with the reason

  @step1
  Scenario: HC-03 A missing action fails the check and names it
    Given Home Assistant has no action assist_satellite.start_conversation
    When the actions are checked
    Then the check fails and names it

  @step1
  Scenario: HC-04 A warning of the integration in Home Assistant's log fails the check
    Given Home Assistant's log has a deprecation warning from custom_components.mira
    When the log is checked
    Then the check fails and shows the line
