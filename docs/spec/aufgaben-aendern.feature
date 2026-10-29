# Testable rules of docs/spec/aufgaben-aendern.md. Each scenario has a fixed number (AE-nn) that is never reused; its
# Go test carries the number in its name (TestAE07_…). The tags @step1 … @step3 say in which step of the spec
# ("Steps") the scenario gets built.
#
# Groups: AE-0x the integration · AE-1x finding · AE-2x drafting and proposal · AE-3x saving and who may

Feature: AE Changing tasks in the conversation

  # --- The integration ------------------------------------------------------------------------------------

  @step1
  Scenario: AE-01 Mira reads a task the editor saved
    Given a household task in automations.yaml
    When Mira asks for its configuration
    Then she gets it, with its triggers, conditions and actions

  @step1
  Scenario: AE-02 A task from a YAML package is not editable
    Given a task defined in a YAML package
    When Mira asks for its configuration
    Then the answer is "not editable"

  @step1
  Scenario: AE-03 Saving a household task keeps its labels
    Given a household task with the label "Licht"
    When Mira saves it changed
    Then it has the label "Licht" and not "Mira"

  @step1
  Scenario: AE-04 The list with all names every task
    Given one of Mira's tasks and one of the household's
    When Mira lists with all
    Then both come, the household's with editable true
    And without all only Mira's comes

  # --- Finding --------------------------------------------------------------------------------------------

  @step2
  Scenario: AE-10 A task is found by its name
    Given a task "Abendlicht Wohnzimmer"
    When the field aufgabe is "das Abendlicht im Wohnzimmer"
    Then the field holds its entity id

  @step2
  Scenario: AE-11 Two matching tasks are asked about
    Given tasks "Abendlicht Wohnzimmer" and "Abendlicht Küche"
    When the field aufgabe is "das Abendlicht"
    Then the thread asks which one, naming both

  @step2
  Scenario: AE-12 A task Mira cannot change ends the thread
    Given a task from a YAML package
    When it is the one found
    Then the thread ends, saying she cannot change it and why

  # --- Drafting and proposal ------------------------------------------------------------------------------

  @step2
  Scenario: AE-20 The proposal says the condition before and after
    Given a task that switches on at sunset
    When the change is "erst 30 Minuten danach"
    Then the proposal names the old and the new time
    And asks whether to change it so

  @step2
  Scenario: AE-21 A new action is named by the code
    Given a task that switches the living room light
    When the draft also switches the radio
    Then the proposal says that it newly switches the radio

  @step2
  Scenario: AE-22 A new critical action is refused
    Given a task that switches a light
    When the draft also unlocks the front door
    Then nothing is proposed, and the thread says why

  @step2
  Scenario: AE-23 An action that was there stays allowed
    Given a household task that unlocks the front door at 7:00
    When the change is "erst um 7:30"
    Then a proposal comes with the new time

  @step2
  Scenario: AE-24 A draft that changes nothing ends the thread
    Given a task
    When the draft equals it
    Then the thread ends, saying that nothing changes

  # --- Saving and who may ---------------------------------------------------------------------------------

  @step2
  Scenario: AE-30 After the yes the task is saved under its id
    Given a proposal for a household task
    When the user says yes
    Then the integration saves it under the same id, keeping its labels
    And the log says "task edited"

  @step2
  Scenario: AE-31 After web content a change is refused
    Given a turn that read a web page
    When the user says yes to a change of a task that unlocks the front door
    Then nothing is saved, and Mira says why

  @step2
  Scenario: AE-32 An unknown voice cannot change a task
    Given nobody Mira knows speaks
    When a change of a task that switches a light is confirmed
    Then nothing is saved, and Mira says she does not know who is speaking

  @step2
  Scenario: AE-33 An older integration says it needs an update
    Given an integration that does not know mira/automation/get
    When Mira reads a task
    Then the thread ends, saying the integration needs an update

  # --- The skill ------------------------------------------------------------------------------------------

  @step3
  Scenario: AE-40 The skill aufgabe-aendern starts with its building blocks
    Given the shipped skills
    When Mira starts
    Then the target aufgabe-aendern has the check tasks.find and the finish tasks.edit
