# Scenarios of docs/spec/bearbeiten.md. Every scenario has a number (BE-01, …); the Go test that checks it carries
# the number in its name (TestBE01_…). @step1 is the first switch-over, @step2 the settings sections.

Feature: BE Editing models, agents and skills on Mira's page

  @step1
  Scenario: BE-01 An edit keeps the file's comments and changes only the entry
    Given a mira.yaml with comments
    When the page adds a model
    Then the comments and the other entries stay, and the file keeps its mode

  @step1
  Scenario: BE-02 What Mira would refuse is not written
    Given a model without privacy in the form
    When the page saves it
    Then the file stays as it was, and the page says why

  @step1
  Scenario: BE-03 Secrets stay out of the file, encrypted with a key
    Given a token entered on the page
    When it is saved
    Then the secrets file holds it (mode 0600, encrypted when MIRA_SECRET_KEY is set), and it holds before the environment
    And a wrong key cannot read it

  @step1
  Scenario: BE-04 A new model goes into the file, its token into the secrets
    Given the page's form for a new model with a token
    When it is saved
    Then mira.yaml has the model with ${MIRA_MODEL_<NAME>_TOKEN}, never the token, and a restart is needed

  @step1
  Scenario: BE-05 Renaming a model changes who names it; main keeps its name
    Given an agent that takes a model
    When the model is renamed
    Then the agent takes the new name, and the token moves with it
    And main cannot be renamed

  @step1
  Scenario: BE-06 A model in use is not deleted
    Given a model an agent takes
    When the page deletes it
    Then it stays, and the page names the agent; main is never deleted

  @step1
  Scenario: BE-07 An agent is checked before it is written and not deleted while used
    Given an agent Mira would refuse at start
    When the page saves it
    Then nothing is written
    And an agent another agent calls is not deleted, nor one of Mira's own

  @step1
  Scenario: BE-08 A skill's settings and an own skill's instructions
    Given an own skill with a secret setting
    When the page saves its settings and instructions
    Then the setting is in mira.yaml, the secret only as a reference, and the SKILL.md has the new instructions
    And an empty description is refused, and switching off takes the skill out of mira.yaml

  @step1
  Scenario: BE-09 Mira's own skill can be switched off
    Given Mira's own skill without an entry in mira.yaml
    When the page switches it off
    Then its entry says enabled: false, and switched on again the entry is gone

  @step1
  Scenario: BE-10 Skills are entries that are on unless they say off
    Given skills under skills, beside dirs, each with its settings
    When Mira reads the file
    Then each is on unless it says enabled: false, and the old skills.enabled is refused with a hint

  @step1
  Scenario: BE-11 A model beside the integration's main
    Given a mira.yaml without main
    When the page adds a further model
    Then it is refused without the integration's main, and written when the integration sets main up

  @step1
  Scenario: BE-12 The model form offers the providers and the server's models
    Given the page's form for a model
    When it starts and the server is known
    Then it offers Mira's providers and, last, an own server
    And the models are the ones the server's /models names, else a text field

  @step1
  Scenario: BE-13 A model is tried before it is saved
    Given the page's form for a model with a new server, model or token
    When it is saved or its model is chosen
    Then Mira tries it with a real request, and a refused key or unknown model is not saved
    And when it answers, the context field gets what the server says, else the size that holds without an entry

  @step2
  Scenario: BE-14 A settings section saved on the page goes into mira.yaml and holds at once
    Given a mira.yaml with a notifier section and a comment elsewhere
    When the page saves the notifier with another cooldown
    Then the file has the new cooldown and the comment, nothing is stored in the database
    And the running notifier has the new cooldown without a restart

  @step2
  Scenario: BE-15 A section Mira would refuse is not written
    Given a mira.yaml
    When the page saves a notifier with a morning trigger in an unknown area
    Then the page gets the reason and the file stays as it was

  @step2
  Scenario: BE-16 What Home Assistant sends is held in memory, not written
    Given a mira.yaml without persons
    When the integration sends persons and rooms
    Then Mira runs with them, the file has none of them, and nothing is stored in the database

  @step2
  Scenario: BE-17 After a restart Mira asks for the settings again
    Given Mira started and the integration has not sent its settings since
    Then the status reports an empty fingerprint
    When the integration sends them
    Then the status reports the fingerprint of the exposed list

  @step2
  Scenario: BE-18 A notifier saved while Home Assistant manages the rooms keeps the file's morning trigger
    Given the integration sent rooms with a morning trigger
    When the page saves the notifier with another cooldown
    Then the file has the new cooldown and its own morning trigger
    And Mira runs with the new cooldown and the morning trigger from Home Assistant

  @step2
  Scenario: BE-19 The page shows no source and no way back
    Given a section saved on the page
    Then the configuration the page reads has no "overridden" and there is no reset

  @step2
  Scenario: BE-20 Only what differs from the defaults goes into the file
    Given a mira.yaml with a notifier
    When the page saves the whole notifier with only the cooldown changed
    Then the file's notifier has only the cooldown
    And saved back at its defaults, the file has no notifier

  @step2
  Scenario: BE-21 The persona goes under prompt, the web search at the top
    Given a mira.yaml without a persona
    When the options save a persona and switch the web search off
    Then the file has the persona under prompt and the web search at the top
    And Mira reads the file and runs with both
    And saved back empty, the file has no prompt
