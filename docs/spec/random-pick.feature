# Testable rules of docs/spec/random-pick.md. Each scenario has a fixed number (RP-nn) that is never reused; its test
# carries the number in its name (TestRP01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.
#
# Groups: RP-0x the axes · RP-1x the tool · RP-2x the skills

Feature: RP Random pick

  # --- The axes -------------------------------------------------------------------------------------------

  @step1
  Scenario: RP-01 A skill's axes are read in their order
    Given a skill with metadata.mira.random: a list axis "schwierigkeit" and a map axis "thema"
    When the skill loads
    Then it has both axes in the order of the file, the map axis with its sub-values

  @step1
  Scenario: RP-02 Broken axes are a problem of the skill
    Given a skill whose random has an empty list, a list in a list, or a map value without sub-values
    When the skill loads
    Then the skill is left out and named as a problem with the reason

  @step1
  Scenario: RP-03 A translation brings its own axes
    Given the skill quiz with German axes and SKILL.en.md with English ones
    When random_pick runs in an English turn
    Then the lines use the English axes and values

  @step1
  Scenario: RP-04 A broken translation of the axes is a problem of the skill
    Given SKILL.en.md whose random has an empty list
    When the skill loads
    Then the skill is left out and named as a problem with the reason

  # --- The tool -------------------------------------------------------------------------------------------

  @step1
  Scenario: RP-10 Only the free axes are drawn
    Given the skill quiz
    When random_pick is called with fixed alter "Erwachsene" and schwierigkeit "schwer", count 3
    Then every line has alter "Erwachsene" and schwierigkeit "schwer", and a drawn thema and frageart

  @step1
  Scenario: RP-11 A value comes again only after all were drawn
    Given an axis with four values
    When random_pick is called with count 8
    Then the first four lines have four different values, and so have the last four

  @step1
  Scenario: RP-12 A fixed map value draws only its sub-value
    Given the map axis thema with "Tiere" and its subtopics
    When random_pick is called with fixed thema "Tiere"
    Then every line has thema "Tiere / " and one of its subtopics

  @step1
  Scenario: RP-13 A fixed sub-value or an unknown value is kept as given
    When random_pick is called with fixed thema "Vögel", and again with thema "Harry Potter"
    Then the lines have thema "Tiere / Vögel", and "Harry Potter" with no sub-value

  @step1
  Scenario: RP-14 Wrong calls are told to the model
    When random_pick is called for a skill that does not exist, a skill without axes, an axis the skill does not have,
      or count 0 or 31
    Then the tool fails with a sentence that names what was wrong (and the skill's axes, for a wrong axis)

  @step1
  Scenario: RP-15 The tool is there only with a skill that has axes
    Given enabled skills of which none has axes
    Then there is no tool random_pick

  # --- The skills -----------------------------------------------------------------------------------------

  @step2
  Scenario: RP-20 The quiz draws its questions
    Given the skill quiz
    Then it has the axes schwierigkeit, thema and frageart, in German and in English (not the age: it is the
      players', not drawn)
    And its guide calls random_pick before it writes the plan

  @step2
  Scenario: RP-21 Vocabulary Mira makes up draws its word field
    Given the skill vokabeln
    Then it has the axes niveau and wortfeld, in German and in English, drawn once for the whole round
    And its guide calls random_pick only for a list Mira makes up herself

  @step2
  Scenario: RP-22 The music quiz draws its songs' years
    Given the skill musikquiz
    Then it has the axes zeit (decade to years) and herkunft, in German and in English
    And its guide calls random_pick before it chooses the songs

  @step2
  Scenario: RP-23 A bedtime story draws its hero
    Given the skill geschichte
    Then it has the axes held, ort, problem and gegenstand
    And its guide calls random_pick before the story
