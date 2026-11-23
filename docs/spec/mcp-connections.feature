# Scenarios of docs/spec/mcp-connections.md. Every scenario has a number (MX-01, …); the Go test that checks it carries
# the number in its name (TestMX01_…). @step1 is the transport, @step2 the loop per server.

Feature: MX MCP servers: transport and session

  @step1
  Scenario: MX-01 The transport knows nothing of the host
    Then the package of the transport imports neither the host, the config nor the prompts

  @step2
  Scenario: MX-02 A reconnect does not cut a call on the old connection
    Given a call runs on a server's connection
    When the server's connection is replaced
    Then the call finishes on the old connection
    And its tool ran once
    And the old connection is closed once the call ended

  @step2
  Scenario: MX-03 Two calls that fail on a lost session connect once
    Given the server forgot Mira's session
    When two calls fail on it at the same time
    Then Mira connects once, and both calls are repeated on the new connection

  @step2
  Scenario: MX-04 A call on a connection Mira closed is not repeated
    Given a call runs on a server's connection
    When the Mira service stops and closes it
    Then the call is not repeated: Mira does not connect again for it
    And its tool ran once

  @step2
  Scenario: MX-05 A change of the server's connection lets running calls finish
    Given a call runs on a server's connection
    When the server's connection changes (Reconnect)
    Then the call finishes, its tool ran once
    And the next call goes to the new connection
