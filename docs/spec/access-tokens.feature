Feature: Access tokens
  More than one named token for the Mira service's HTTP interface; the partner token is the first kind
  (docs/spec/access-tokens.md).

  @step1
  Scenario: AT-01 A partner token is made and works
    Given Mira's page makes a partner token named "Elektro Meier"
    When a caller sends it with GET /v1/status
    Then the answer is 200

  @step1
  Scenario: AT-02 The token is shown only once and stored as a hash
    Given Mira's page makes a partner token
    Then the answer carries the token
    And the database holds its hash and first characters, not the token

  @step1
  Scenario: AT-03 whoami names the caller
    Given a partner token named "Elektro Meier"
    When it calls GET /v1/whoami
    Then the answer is name "Elektro Meier", kind "partner"
    And with the integration's token the answer is kind "integration"

  @step1
  Scenario: AT-04 A deleted token is refused at once
    Given a partner token that worked
    When it is deleted on Mira's page
    Then its next call is refused with 401

  @step1
  Scenario: AT-05 X-Partner-User counts only with a partner token
    Given a call with the integration's token and the header X-Partner-User "anna"
    Then the handler sees no partner user

  @step1
  Scenario: AT-06 A second token with the same name is refused
    Given a partner token named "Elektro Meier"
    When the page makes another named "Elektro Meier" or "elektro MEIER"
    Then the answer is 409 and nothing is stored

  @step1
  Scenario: AT-13 A partner token is refused at the setup and at backup and restore
    Given a partner token
    When it calls /v1/pair, /v1/unpair, /v1/prove, /v1/setup, /v1/backup or /v1/restore
    Then the answer is 403 and the handler is not called
    And the integration's token still gets through

  @step2
  Scenario: AT-07 A partner call is recorded with its partner user
    Given a partner token named "Elektro Meier"
    When it calls with X-Partner-User "anna"
    Then the partner activity has a row with the token's name, "anna", the method, the path and the status
    And the log has "partner call" with the same
    And a partner token's GET /v1/status is kept neither as a row nor as a log line

  @step2
  Scenario: AT-08 The call runs although its record fails
    Given the partner activity cannot be written
    When a partner token calls
    Then the call is answered
    And the log has "could not record a partner call"

  @step2
  Scenario: AT-09 Old partner activity is deleted at the nightly cleanup
    Given partner activity older than the time kept
    When the nightly cleanup runs
    Then those rows are gone and newer ones stay

  @step3
  Scenario: AT-10 The page makes a token and shows it once in its dialog
    Given an admin on Mira's page
    When they make a new token
    Then the page shows the token once in its own dialog, read-only, with "Kopieren und schließen"

  @step3
  Scenario: AT-11 Deleting asks in the page's dialog
    Given a partner token on the page
    When "Löschen" is clicked
    Then the page asks in its own dialog before deleting

  @step3
  Scenario: AT-12 The page shows what the partner did
    Given partner activity of a token
    Then the page lists it newest first with time, partner user and what was done

  @step3
  Scenario: AT-14 A partner sees no memory
    Given a fact of the house
    When a partner token reads or forgets the memory on Mira's page
    Then the answer is 403 and the fact stays (since decision 23)

  @step1
  Scenario: AT-15 A partner token cannot manage access
    Given a partner token
    When it lists, makes or deletes tokens or reads their activity
    Then the answer is 403 and nothing changes

  @step5
  Scenario: AT-16 A partner token reads only who it is and the state
    Given a partner token
    When it calls GET /v1/whoami or GET /v1/status
    Then the answer is 200
    When it calls anything else, such as an action through /v1/ha, a conversation, a connection or a page's setting
    Then the answer is 403 and no handler is called
    And the partner activity keeps the refused call
    And the integration's token still gets through there
