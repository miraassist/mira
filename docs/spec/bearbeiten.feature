# Scenarios of docs/spec/bearbeiten.md. Every scenario has a number (BE-01, …); the Go test that checks it carries
# the number in its name (TestBE01_…). @step1 is the whole feature.

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
