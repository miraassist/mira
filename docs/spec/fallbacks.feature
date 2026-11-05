Feature: No silent fallbacks (docs/spec/fallbacks.md)

  Where something Mira needs fails, she does not carry on with a substitute nobody decided on.

  @step1
  Scenario: FB-01 An assessment without the person states fails
    Given the person states cannot be read from Home Assistant
    When a task asks Mira to assess
    Then the assessment fails with the error
    And no model is asked

  @step1
  Scenario: FB-02 A result for a person not known yet does not go to the house
    Given a run's result is for Anna and Anna is not among the known persons
    When the result is delivered as a message
    Then the message is for Anna, not for the house

  @step1
  Scenario: FB-03 "Gibt es Neues?" says when the answering machine could not be read
    Given the answering machine cannot be read
    When Mira tells what is new
    Then the answer says that the answering machine could not be read

  @step2
  Scenario: FB-04 A turn whose tool rounds are used up says it did not finish
    Given the model still asks for tools after its last round
    When the turn ends
    Then Mira says she did not finish, not what the model wrote alongside
    And the dropped call is not run, and the log names it

  @step2
  Scenario: FB-05 An empty answer says it did not finish
    Given the model answers nothing and nothing was said in the turn
    Then Mira says she did not finish

  @step2
  Scenario: FB-06 A satellite whose prompt was never fetched fetches it on its turn
    Given the satellite's version failed when the prompt was fetched
    When its turn comes and it still fails
    Then it is an error, not the version without a device
    When it works again
    Then the turn gets the satellite's own lines

  @step2
  Scenario: FB-07 A satellite whose tools could not be listed lists them on its turn
    Given the listing for the satellite fails when Mira connects
    When its turn comes and the listing still fails
    Then it is an error
    When it works again
    Then the turn lists them and has the satellite's tools

  @step2
  Scenario: FB-08 A mail when the calendars cannot be read is an error, not "no calendar"
    Given the calendars cannot be read
    When a mail for the calendar Pakete comes
    Then it is an error back to the integration, not "ignored"

  @step2
  Scenario: FB-09 A start_conversation that runs out of time counts as spoken only when the satellite spoke
    Given start_conversation runs into its deadline
    When the satellite was responding
    Then the message counts as spoken
    When the satellite never spoke
    Then it is not delivered by voice

  @step2
  Scenario: FB-10 A message is not spoken when the satellite's state cannot be read
    Given the satellite's state cannot be read
    When a message is to be spoken there
    Then the delivery fails and nothing is spoken

  @step2
  Scenario: FB-11 A message for a busy satellite waits until it is idle
    Given the satellite is responding
    When a message is to be spoken there
    Then it waits
    When the satellite reports idle
    Then the message is spoken

  @step3
  Scenario: FB-12 The devices are read the same under Home Assistant 2026.10 and 2026.11
    Given the device registry
    Then all device entries come out, whatever iterating it gives in this version

  @step5
  Scenario: FB-13 A retried model call is in the log
    Given the model's server answers 503 and then the answer
    When Mira asks the model
    Then the answer comes from the second attempt
    And the log has a warning for the retry with the model, the host, the attempt, the status, the attempt's time and the wait

  @step5
  Scenario: FB-14 A thread's result whose thread cannot be read again is in the log
    Given a thread's background work is done and reading the thread again fails
    Then the result is dropped as before
    And an error names the thread and says that its result is dropped (row 21)

  @step5
  Scenario: FB-15 A message that cannot be played is in the log
    Given the speaker cannot play a message of the answering machine
    Then the playback ends and the message stays new
    And an error names the message, the speaker and the reason (row 22)

  @step5
  Scenario: FB-16 A state that cannot be watched is in the log
    Given subscribing to an entity's state fails
    Then it counts as not reached, as before
    And a warning names the entity and the reason (row 23)

  @step5
  Scenario: FB-17 An entities event that cannot be read is in the log
    Given Home Assistant sends an entities event that is no valid JSON
    Then the event is dropped
    And a warning gives the reason and the start of the event (row 24)

  @step5
  Scenario: FB-18 A message state that cannot be written back is in the log
    Given a delivery failed and setting its message back to open fails
    Then the delivery's error goes to the caller as before
    And an error names the message and says that it may not come again (row 25)

  @step5
  Scenario: FB-19 Open skills that cannot be read are in the log
    Given the skills a conversation opened cannot be read
    Then the turn runs without their tools
    And a warning names the session and the reason (row 26)

  @step5
  Scenario: FB-20 An announcement that cannot be read is in the log
    Given what was just announced at the satellite cannot be read
    Then the turn runs without it; no announcement in the window stays without a line
    And a warning names the device and the reason (row 27)

  @step5
  Scenario: FB-21 Past judgements that cannot be read are in the log
    Given a task's past judgements cannot be read
    Then it is judged without them
    And a warning names the task and the reason (row 28)

  @step5
  Scenario: FB-22 A skill that cannot be read is in the log
    Given a skills folder or a SKILL.md cannot be read (not: does not exist)
    Then the skill is missing from the list
    And a warning names the folder or the skill and the reason (row 29)

  @step5
  Scenario: FB-23 A skill of a backup that is not restored is in the log
    Given a skill named in a backup's manifest is not in the archive
    Then the restore goes on without it
    And a warning names the skill (row 30)

  @step5
  Scenario: FB-24 A secrets file that cannot be read while editing is in the log
    Given the secrets file cannot be read while a model is renamed
    Then the model keeps its token under the old name
    And a warning names both names and the reason (row 31)

  @step5
  Scenario: FB-25 A kept list that cannot be read is in the log
    Given the stored list of what Assist sees is no valid JSON
    Then it counts as not sent
    And a warning gives the reason (row 32)

  @step5
  Scenario: FB-26 Stored or received JSON that cannot be read is in the log
    Given a tool call's arguments are no JSON object
    Then the hooks see no arguments
    And a warning gives the reason and the start of the arguments (row 33)

  @step5
  Scenario: FB-27 A thread saved again after a conflict is in the log
    Given a thread changed meanwhile while it is saved
    Then it is read again and saved
    And a line names the thread and the try (row 34)

  @step5
  Scenario: FB-28 A hit's page that cannot be read is in the log
    Given the page of a search hit cannot be read
    Then the next hit is taken
    And a line names the address and the reason (row 35)

  @step6
  Scenario: FB-29 An answer of Mira the integration cannot read is in the log
    Given Mira answers a restore with a body that is no JSON
    Then the integration logs a warning with the HTTP status
    And when Mira is not reachable at setup, the integration logs that her id and settings are taken over at the next start (row 36)
