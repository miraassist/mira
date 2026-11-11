Feature: Retries (docs/spec/retry.md)

  @step1
  Scenario: RY-01 A fatal error is not tried again
    Given a call that fails with an error marked fatal
    When it runs under a policy of 5 attempts
    Then it is called once and its error is returned

  @step1
  Scenario: RY-02 A retryable error is tried up to the bound
    Given a call that always fails with a retryable error
    When it runs under a policy of 3 attempts
    Then it is called 3 times and the last error is returned

  @step1
  Scenario: RY-03 An error nobody classified counts as retryable
    Given a call that fails with a plain error, then succeeds
    When it runs under a policy of 3 attempts
    Then it is called twice and succeeds

  @step1
  Scenario: RY-04 Waits grow as the policy says
    Given policies exponential, linear and fixed, from 1 s up to 10 s
    Then the waits are 1, 2, 4, 8, 10 s; 1, 2, 3, 4, 5 s; and 1, 1, 1 s

  @step1
  Scenario: RY-05 A named wait is kept, or the call fails at once
    Given a call that fails with "wait 2 s"
    When the caller can wait 10 s
    Then the next attempt comes after 2 s
    But when the caller can wait only 1 s, the call fails at once naming the wait

  @step1
  Scenario: RY-06 Every further attempt leaves a warning
    Given a call that fails once with a retryable error
    When it runs
    Then the log has one warning with the attempt, the bound and the wait

  @step2
  Scenario: RY-10 A person without a channel is given up at once
    Given a private message to a person without a channel
    When the notifier delivers
    Then the message is failed after one attempt
    And the log has one error with fatal=true

  @step2
  Scenario: RY-11 A channel that is down is tried again later, not every minute
    Given a private message whose channel fails with home_assistant_error
    When the notifier ticks every minute for 10 minutes
    Then it was tried at 0, 1, 3 and 7 minutes

  @step2
  Scenario: RY-12 After 10 attempts the message is given up
    Given a private message whose channel always fails
    When it was tried 10 times
    Then the message is failed and the log says "private delivery given up"

  @step2
  Scenario: RY-13 Home Assistant's not_found is fatal
    Given a private message whose channel is an action that does not exist
    When the notifier delivers
    Then the message is failed after one attempt

  @step2
  Scenario: RY-14 The next attempt survives a restart
    Given a private message that failed once with a retryable error
    When the notifier starts anew
    Then it is not tried again before its retry time

  @step3
  Scenario: RY-20 A reminder that cannot be delivered for good fails at once
    Given a due reminder for a person without a channel and no satellite
    When it is delivered
    Then it is failed after one attempt

  @step3
  Scenario: RY-21 A reminder is tried again after 15, 30, 45 s
    Given a due reminder whose delivery fails with a retryable error
    Then its attempts come 15, 30 and 45 s apart and it is failed after the fifth

  @step4
  Scenario: RY-30 The model's named wait is kept
    Given the model answers 429 with Retry-After 1
    When Mira calls it
    Then the second attempt comes after 1 s and succeeds

  @step4
  Scenario: RY-31 Google's retryDelay is read from the body
    Given the model answers 429 with a RetryInfo retryDelay of "1s"
    When Mira calls it
    Then the second attempt comes after 1 s

  @step4
  Scenario: RY-32 A named wait longer than the caller's deadline fails at once
    Given the model answers 429 with Retry-After 30
    When the caller can wait 5 s
    Then the call fails at once naming the 30 s

  @step4
  Scenario: RY-33 An agent tries again only a model that hung
    Given an agent with one retry
    When the model client returns a 400
    Then the agent does not call it again

  @step4
  Scenario: RY-34 An agent tries again a model that hung
    Given an agent with one retry and an attempt timeout
    When the first call hangs past the timeout
    Then the agent calls the model a second time

  @step5
  Scenario: RY-40 An MCP server that refuses the token is not tried again
    Given an MCP server that answers 401
    When Run's retry comes
    Then it is not connected again until its connection changes
    And the log has one error

  @step5
  Scenario: RY-41 An unreachable MCP server is tried with growing waits
    Given an MCP server that is unreachable
    Then its attempts come 30 s, 60 s, 120 s apart, at most 30 min

  @step5
  Scenario: RY-42 A reconnected MCP server starts the waits anew
    Given an MCP server that was unreachable for a while
    When it is connected again and lost later
    Then its next attempt comes after 30 s

  @step5
  Scenario: RY-43 Home Assistant's connection waits grow up to the longest
    Given Home Assistant is unreachable
    Then the reconnects come with doubling waits up to ReconnectMax and never stop

  @step6
  Scenario: RY-50 Setup does not wait on a refused token
    Given the new service refuses the token
    When setup waits for it to start
    Then setup fails at once

  @step6
  Scenario: RY-51 Setup waits for a service that restarts
    Given the new service is unreachable for 3 s
    When setup waits for it to start
    Then setup goes on once it answers

  @step6
  Scenario: RY-52 A failed answer of the Supervisor while joining is logged
    Given the Supervisor's answer fails while Mira waits for the device's Wi-Fi
    Then the log has a warning once
