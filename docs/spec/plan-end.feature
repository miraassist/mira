# Testable rules of docs/spec/plan-end.md. Each scenario has a fixed number (PE-nn) that is never reused; its test
# carries the number in its name (TestPE01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: PE Ending a plan on the page

  @step1
  Scenario: PE-01 A running plan is ended from the page
    Given a running plan "Quiz"
    When the page calls DELETE /v1/ui/plans/<its id> with the token
    Then the thread is ended with CancelThread
    And the answer is {"ended": true}

  @step1
  Scenario: PE-02 Only a running plan is ended
    Given an open thread that is not a plan, and an id that is no thread
    When the page calls DELETE /v1/ui/plans/<id> for either
    Then the answer is 404 "no such plan"
    And no thread is ended

  @step1
  Scenario: PE-03 Everyone who sees the plans may end one
    Given a Home Assistant user without admin rights on the page
    When they end a running plan
    Then the plan is ended

  @step1
  Scenario: PE-04 A failure is said, not swallowed
    Given reading the running plans fails, or ending the thread fails
    When the page ends a plan
    Then the answer is 500 with the error
    And the service logs "could not end a plan on the page"

  @step1
  Scenario: PE-05 Without the token nothing is ended
    When DELETE /v1/ui/plans/<id> comes without the token
    Then the answer is 401
    And no thread is ended

  @step2
  Scenario: PE-06 A user without admin rights ends a plan through Home Assistant
    Given a Home Assistant user without admin rights on the Mira page
    When the page calls DELETE v1/ui/plans/<id> through the integration's proxy
    Then the proxy passes it on with the user's headers
    But other calls on the plans, such as POST v1/ui/plans/<id> or DELETE v1/ui/plans, stay refused
