# Testable rules of docs/spec/hacs-default.md. Each scenario has a fixed number (HD-nn) that is never reused; its test
# carries the number in its name (test_HD01_… in pytest). The tags @stepN say in which step of the spec ("Steps") the
# scenario gets built. Steps 1 and 2 are the integration's, tested in pytest; step 3 is tested in pytest (the
# integration) and Go (the service, TestHD23_…).
#
# Groups: HD-0x the model's source · HD-1x the mail calendar's repair · HD-2x an admin's access and automations ·
# HD-3x the access the admin gives · HD-4x deleting only what Mira created. Steps 5 in pytest, 6 in Go.

Feature: HD HACS default catalog

  # --- The model's source -----------------------------------------------------------------------------------

  @step1
  Scenario: HD-01 Another integration's key is never read
    Given Home Assistant has a loaded Google Gemini integration with an API key
    When the setup shows "Where does the AI model come from?"
    Then the choices are the service's providers and "Own server" only
    And no config entry of another integration is read

  @step1
  Scenario: HD-02 A provider's key is entered for Mira
    Given the setup's model form
    When the user picks Google Gemini and enters a key
    Then the integration lists the models with that key and runs the trial run as before

  # --- The mail calendar's repair ---------------------------------------------------------------------------

  @step2
  Scenario: HD-10 A missing calendar is a repair, nothing is created
    Given an IMAP entry whose device is named "Pakete", the service reports the mail application "pakete"
    And there is no calendar "Pakete"
    When the integration polls the status
    Then the repair "mail_calendar_pakete" is raised, fixable
    And no config entry is created and nothing is exposed

  @step2
  Scenario: HD-11 Confirming the repair creates the calendar and exposes it
    Given the repair "mail_calendar_pakete"
    When the admin submits its flow
    Then a local calendar "Pakete" exists and is exposed to Assist
    And the repair is gone

  @step2
  Scenario: HD-12 A refused calendar keeps the repair
    Given the repair "mail_calendar_pakete" and local_calendar aborts the flow
    When the admin submits the repair's flow
    Then the flow shows the reason, the log has a warning with the name and the reason
    And the repair stays

  @step2
  Scenario: HD-13 The repair goes when it is no longer needed
    Given the repair "mail_calendar_pakete"
    When the poll finds a calendar "Pakete", or no IMAP entry with the device "Pakete"
    Then the repair is gone

  # --- An admin's access and automations -------------------------------------------------------------------

  @step3
  Scenario: HD-20 The setup gives Mira the owner's token, no user of her own
    Given an admin sets Mira up, the setup names the owner, and the admin submits "Start using Mira"
    Then the integration makes a long-lived token "Mira" for Home Assistant's owner and sends it to the service
    And no Home Assistant user is created

  @step3
  Scenario: HD-21 An existing install is asked for the new access
    Given an install whose service holds the token of the old user "Mira"
    When the integration starts
    Then the repair "Mira needs a new access" is raised, fixable
    When an admin submits it
    Then the service gets a token of the owner, the old token "Mira agent" is revoked and the repair is gone
    And the old user "Mira" stays

  @step3
  Scenario: HD-22 Reading automation data needs an admin
    Given a non-admin user's websocket connection
    When it sends mira/automation/list, mira/automation/get, mira/automation/validate, mira/once/list or mira/agent/done
    Then each is refused as unauthorized

  @step3
  Scenario: HD-23 A task is saved through Home Assistant's API
    Given the model saves a new task that passes Mira's checks
    Then the service calls mira/automation/validate, then POST /api/config/automation/config/<id>
    And labels the new automation "Mira"
    And the integration has written no file

  @step3
  Scenario: HD-24 Only Mira's automations are deleted
    When the model deletes an automation without the label "Mira" or "Mira: einmalig"
    Then the tool says it is not one of Mira's tasks, and nothing is deleted
    When it deletes one with the label "Mira"
    Then the service calls DELETE /api/config/automation/config/<id>

  @step3
  Scenario: HD-25 A finished timer goes when it has run
    Given a timer's automation labelled "Mira: einmalig"
    When its last_triggered is set and it is no longer running
    Then the service deletes it through Home Assistant's API
    And a failed delete leaves a log warning and is tried again at the next start

  @step3
  Scenario: HD-26 Without the new access, saving says what to do
    Given the repair "Mira needs a new access" is not confirmed
    When the model saves a task or a timer
    Then the tool's error tells to confirm the repair under Settings → Repairs
    And the log has the command and Home Assistant's answer

  @step3
  Scenario: HD-27 A revoked token is made anew
    Given Mira's token is revoked in the owner's profile
    When the integration starts, or the service reports that Home Assistant refuses its token
    Then the integration makes a new token for the owner and sends it to the service
    And the log says that the old one was revoked

  # --- The admin's access (second review) ----------------------------------------------------------------------
  # HD-20, HD-21 and HD-27 are retired with step 5, HD-24 with step 6; HD-32 (a login) was dropped before it was built.
# None of these numbers is used again.

  @step5
  Scenario: HD-30 The setup makes no token on its own
    Given an admin sets Mira up and chooses "Start using Mira"
    Then the form "Access for Mira" asks for a token
    And the integration makes no token

  @step5
  Scenario: HD-31 A pasted token is checked
    When the admin pastes a token Home Assistant does not know, one that is not long-lived, or one of a non-admin
    Then the form shows invalid_token, token_not_long_lived or token_not_admin, and nothing is sent to the service
    When the admin pastes a long-lived token of an active admin
    Then the service gets that token, and the entry keeps only its id

  @step5
  Scenario: HD-33 A revoked token raises a repair, nothing is made anew
    Given Mira's token is revoked, or its user is deactivated or no longer an admin
    When the status poll runs
    Then the repair "Mira has no access any more" is raised, fixable
    And no token is made

  @step5
  Scenario: HD-34 The repair takes a new access the same way
    Given the repair "Mira has no access any more"
    When an admin submits a valid token in it
    Then the service gets the token with POST /v1/setup and the repair is gone
    When the admin submits something the checks refuse
    Then the repair's form shows the same error as the setup, and the repair stays

  @step5
  Scenario: HD-35 The repair goes when the token is valid again
    Given the repair "Mira has no access any more" because the token's user is no longer an admin
    When the user is an admin again and the status poll runs
    Then the repair is gone

  @step5
  Scenario: HD-36 Without access, what Home Assistant refuses fails as before
    Given the token's user is no longer an admin
    When the model saves a task or a timer
    Then the tool says Mira needs an access again under Settings → Repairs
    And the repair "Mira has no access any more" is there once, not twice

  @step5
  Scenario: HD-37 The token the integration made is revoked at the update
    Given an install whose entry keeps the owner's token "Mira (Integration)" the integration made
    When the integration starts after the update
    Then that token is revoked, the log says so, and the repair "Mira has no access any more" is raised

  @step5
  Scenario: HD-38 Removing Mira revokes nothing
    Given Mira works with a token the admin pasted
    When the integration is removed
    Then the token stays in the admin's profile

  # --- Deleting only what Mira created ---------------------------------------------------------------------------

  @step6
  Scenario: HD-40 A new task or timer is recorded as Mira's
    When the service saves a new task or a timer and Home Assistant accepts it
    Then its id is in the table own_automations with its kind
    When the service saves a change to a household task
    Then no row is written

  @step6
  Scenario: HD-41 Only automations with a row are deleted
    Given an automation labelled "Mira" without a row in own_automations
    When the model deletes it
    Then the tool says it is not one of Mira's tasks, and nothing is deleted
    When the model deletes a task with a row
    Then the service calls DELETE /api/config/automation/config/<id> and the row goes

  @step6
  Scenario: HD-42 A finished timer without a row stays
    Given a finished timer labelled "Mira: einmalig" without a row
    When the service starts or the timer's state changes
    Then it is not deleted, and the start's log names it once

  @step6
  Scenario: HD-43 A failed record leaves a trace
    Given Home Assistant accepted a new task but the row cannot be written
    Then the log warns with the id and the database error
    And the tool says the task is saved

  @step6
  Scenario: HD-44 Tasks and timers from before are taken over once
    Given automations labelled "Mira" and "Mira: einmalig" and an empty table own_automations
    When the service starts
    Then each gets a row, and the log says how many
    And a failure leaves the table empty, warns, and is tried again at the next start
