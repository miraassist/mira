Feature: Skill files (docs/spec/skill-files.md)

  @step1
  Scenario: SB-01 A skill's files come with it from a folder
    Given a skill folder with SKILL.md, blueprints/zu_ruhig.yaml and prompts/morgen.md
    When Mira loads the skill
    Then the skill's files hold every file of the folder, in every subfolder, as it is

  @step1
  Scenario: SB-02 The same skill from a source
    Given the source delivers the same skill as paths and texts
    When Mira loads the skill
    Then it loads as the skill from the folder, with the same files

  @step2
  Scenario: SB-03 A new skill blueprint is saved
    Given a switched-on skill "zu-ruhig" with the blueprint zu_ruhig.yaml
    And Home Assistant does not have it
    When Mira starts
    Then she saves it with the path "mira_skills/zu-ruhig/zu_ruhig" without allow_override

  @step2
  Scenario: SB-04 A blueprint in the same version is left alone
    Given Home Assistant lists the skill's blueprint with the same mira-version
    When Mira starts
    Then she lists the blueprints once and saves nothing

  @step2
  Scenario: SB-05 A compatible update is saved over the old
    Given Home Assistant has the skill's blueprint in version 1
    And the skill's blueprint is version 2 with the same inputs and a new input with a default
    When Mira starts
    Then she saves it with allow_override
    And Home Assistant reloads the automations using it itself

  @step2
  Scenario: SB-06 A removed input is refused
    Given Home Assistant has the skill's blueprint in version 1 with the inputs "window_end" and "minutes"
    And version 2 has only "minutes"
    When Mira starts
    Then she does not save it and the old version stays
    And a problem of the skill names the blueprint and the input "window_end"

  @step2
  Scenario: SB-07 A new input without a default is refused
    Given version 2 of the skill's blueprint adds an input without a default
    When Mira starts
    Then she does not save it
    And a problem of the skill names the blueprint and the input

  @step2
  Scenario: SB-08 A changed selector or a lost default is refused
    Given version 2 of the skill's blueprint changes an input's selector from number to text, narrows an entity's domains, or drops a default
    When Mira starts
    Then she does not save it
    And a problem of the skill names each input and what changed

  @step2
  Scenario: SB-09 A blueprint that cannot be used
    Given a skill blueprint without mira-version, or not an automation blueprint, or not YAML
    When Mira starts
    Then it is not saved
    And a problem of the skill names the file and the reason
    And the skill runs all the same

  @step2
  Scenario: SB-10 Home Assistant cannot list its blueprints
    Given blueprint/list fails with a lost connection every time
    When Mira starts
    Then she tries three times and saves nothing
    And a problem of each skill with blueprints says Home Assistant could not be asked

  @step2
  Scenario: SB-11 A save refused
    Given Home Assistant refuses to save one skill blueprint
    When Mira starts
    Then the log and a problem of the skill name the blueprint and Home Assistant's answer
    And the skill's other blueprints are saved

  @step3
  Scenario: SB-12 A declared automation is made
    Given the skill declares the automation "nachts" from zu_ruhig.yaml with window_end "{{ window_end }}"
    And mira.yaml sets window_end to "07:00"
    And Home Assistant does not have the automation
    When Mira starts
    Then she saves the automation "mira_skill_zu-ruhig_nachts" with use_blueprint path "mira_skills/zu-ruhig/zu_ruhig.yaml" and the input window_end "07:00"
    And it is labelled "Mira" and noted as one of her own

  @step3
  Scenario: SB-13 An automation with the same inputs is left alone
    Given Home Assistant has the automation with the same use_blueprint
    When Mira starts
    Then she reads it once and saves nothing

  @step3
  Scenario: SB-14 A changed setting makes the automation anew
    Given Home Assistant has the automation with window_end "07:00" and an alias the user gave it
    And mira.yaml now sets window_end to "08:00"
    When Mira starts
    Then she saves the automation with the input window_end "08:00"
    And the alias the user gave it stays

  @step3
  Scenario: SB-15 An empty setting leaves the input to the blueprint's default
    Given the setting window_end is empty
    And the blueprint's input window_end has a default
    When Mira starts
    Then the automation is saved without the input window_end

  @step3
  Scenario: SB-16 A required input without a value
    Given the blueprint's input window_end has no default
    And the setting window_end is empty
    When Mira starts
    Then the automation is not made
    And a problem of the skill names the automation and the input

  @step3
  Scenario: SB-17 An input the blueprint does not have
    Given the declaration fills an input "fenster" the blueprint does not have
    When Mira starts
    Then the automation is not made
    And a problem of the skill names the automation and the input

  @step3
  Scenario: SB-18 A declaration that cannot be right
    Given the declaration names a blueprint file the skill does not have, or an id twice, or an id that is not small words
    When Mira loads the skill
    Then the skill does not load
    And its problem names the automation

  @step3
  Scenario: SB-19 Nothing is deleted
    Given the skill was switched off or no longer declares the automation
    When Mira starts
    Then she neither deletes nor changes its blueprints and automations in Home Assistant

  @step3
  Scenario: SB-20 A refused blueprint's automations stay as they are
    Given an incompatible update of the skill's blueprint was refused
    When Mira starts
    Then she neither makes nor changes the automations of that blueprint

  @step3
  Scenario: SB-21 Saving the automation fails
    Given Home Assistant refuses the automation's config
    When Mira starts
    Then a problem of the skill names the automation and Home Assistant's answer
    And at the next start she tries again
