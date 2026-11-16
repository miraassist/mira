Feature: Skill source (docs/spec/skill-source.md)

  @step1
  Scenario: SQ-01 Skills loaded from the source
    Given skills.source names a source and a valid key
    And the source holds the skill "angehoerige"
    And mira.yaml switches "angehoerige" on
    When Mira starts
    Then she asks the source once with the key and her version
    And the skill "angehoerige" is loaded like a skill from a folder

  @step1
  Scenario: SQ-02 A source's skill not switched on
    Given the source holds the skill "angehoerige"
    And mira.yaml has no entry for it
    When Mira starts
    Then the skill is known but does not run

  @step1
  Scenario: SQ-03 The source cannot be reached
    Given the source does not answer
    When Mira starts
    Then she tries three times and starts without the source's skills
    And a repair says the skills from the subscription could not be loaded

  @step1
  Scenario: SQ-04 The key is refused
    Given the source answers 401
    When Mira starts
    Then she does not try again and starts without the source's skills
    And a repair says the subscription has ended or the key is wrong

  @step1
  Scenario: SQ-05 Not a skill source
    Given the address answers 404 or with something that is not the expected JSON
    When Mira starts
    Then she does not try again
    And a repair names the address

  @step1
  Scenario: SQ-06 A path that leaves the skill's folder
    Given the source's skill has a file "../x"
    When Mira starts
    Then that skill is left out and the others load
    And a problem names the skill and the path

  @step1
  Scenario: SQ-07 A name taken twice
    Given a folder in skills.dirs holds a skill "angehoerige"
    And the source holds a skill "angehoerige" too
    When Mira starts
    Then the folder's skill is loaded and the source's is left out
    And a problem names both

  @step1
  Scenario: SQ-08 No copy on disk
    Given Mira loaded skills from the source
    Then no file and no row of the database holds their text

  @step2
  Scenario: SQ-09 The history keeps a placeholder
    When the model opens a source's skill
    Then the model gets the skill's text
    And the turn's history in the database holds only the placeholder
    And the next model call of the turn carries the text again

  @step2
  Scenario: SQ-10 The page shows no text
    When Mira's page lists a source's skill
    Then it shows name, description, settings and problems, not the text
    And it offers no editing

  @step2
  Scenario: SQ-11 An error without the text
    Given a source's SKILL.md that does not load
    When Mira starts
    Then the problem names the skill, the source's address, the file and the line
    And neither the problem nor the log holds the text

  @step3
  Scenario: SQ-12 The card "Abo"
    When the user enters the address and the key on the tab "Verbindungen"
    Then mira.yaml holds the address and the secrets file the key
    And the card says it takes effect after a restart
