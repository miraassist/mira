# Scenarios of docs/spec/endings.md. Every scenario has a number (EN-01, …); the Go test that checks it carries the
# number in its name (TestEN01_…). @step1: all places.

Feature: EN When something ends while others still use it

  @step1
  Scenario: EN-01 A turn that ended takes no result of its agents
    Given a turn waits for its thread's agents
    When the turn ends and then the agent's result comes
    Then the turn does not take it, and it comes as a message
    And a second turn's wait on the same thread stays when the first stops waiting

  @step1
  Scenario: EN-02 A wait stopped while its video starts stops the video
    Given the wait video is being started
    When the wait is stopped before the start returned
    Then the video is stopped once the start returned

  @step1
  Scenario: EN-03 A playback stopped on purpose is no error
    Given a clip's play call is on its way
    When its run is ended
    Then no error is logged and the next turn is not told it failed

  @step1
  Scenario: EN-04 A thread's work cancelled with its thread is no failure
    Given a thread's work runs
    When the work is cancelled with its thread
    Then no failure is logged, and a turn still waiting says the thread was cancelled

  @step1
  Scenario: EN-05 A subscription made after Mira gave up is unsubscribed
    Given Home Assistant answers a subscribe only after Mira's wait ran out
    When its yes comes late
    Then Mira unsubscribes it at once

  @step1
  Scenario: EN-06 The stop waits for the running turns
    Given a turn runs
    Then waiting for the turns ends only once it ended

  @step1
  Scenario: EN-07 A Starlark tool whose caller ended says so
    Given a Starlark tool runs
    When its caller ends
    Then it is cancelled with "the tool's caller ended", not with its time limit

  @step1
  Scenario: EN-08 A run removes only its own cancel
    Given two runs under one id
    When the first ends
    Then the second can still be cancelled
