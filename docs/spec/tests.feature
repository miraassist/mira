# Scenarios of docs/spec/tests.md. Every scenario has a number (CK-01, …); the Go test that checks it carries the
# number in its name (TestCK01_…). @step1 is the clock.

Feature: CK A clock for tests

  @step1
  Scenario: CK-01 The fake clock stands still until it is moved
    Given a fake clock at 10:00
    When a timer of 5 minutes is made
    Then the clock still says 10:00 and the timer has not fired
    When the clock is advanced by 5 minutes
    Then the timer has fired with the time 10:05

  @step1
  Scenario: CK-02 Advance fires the due timers in the order of their times
    Given timers of 3, 1 and 2 minutes on a fake clock
    When the clock is advanced by 3 minutes
    Then their AfterFuncs ran in the order 1, 2, 3 minutes

  @step1
  Scenario: CK-03 An AfterFunc has run when Advance returns
    Given an AfterFunc of 1 second on a fake clock
    When the clock is advanced by 1 second
    Then the function has run before Advance returned

  @step1
  Scenario: CK-04 A stopped timer does not fire
    Given a timer of 1 minute on a fake clock
    When it is stopped and the clock is advanced by 2 minutes
    Then Stop reported it pending and it has not fired

  @step1
  Scenario: CK-05 A reset timer fires at its new time
    Given a timer of 1 minute on a fake clock
    When it is reset to 3 minutes and the clock is advanced by 2 minutes
    Then it has not fired
    When the clock is advanced by 1 more minute
    Then it has fired

  @step1
  Scenario: CK-06 A ticker fires once for each period it passes
    Given a ticker of 1 minute on a fake clock
    When the clock is advanced by 1 minute three times
    Then three ticks arrived, at 1, 2 and 3 minutes

  @step1
  Scenario: CK-07 The test learns when the code waits on the clock
    Given a goroutine that waits on After(1 minute) of a fake clock
    When the test awaits one waiter
    Then the channel is closed once the goroutine's timer is pending
    And advancing then by 1 minute lets the goroutine go on

  @step1
  Scenario: CK-08 The real clock is the time package
    Given the real clock
    Then Now is the time package's now and an After of 0 fires

  @step1
  Scenario: CK-09 AdvanceToNext jumps to the earliest timer
    Given timers of 1 hour and 10 seconds on a fake clock
    When the clock advances to the next timer
    Then it moved 10 seconds and only that timer fired

  @step1
  Scenario: CK-10 Drive lets every wait of the code run out
    Given code that waits 1 minute, then 1 hour on a fake clock
    When the test drives it
    Then it went on at 1 minute and at 1 hour 1 minute, without real time
