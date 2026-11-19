# Scenario ids PG-nn; tests are named by the id (TestPG01_…). @step1 is korb's
# setup tools (tested in rewe-cli against korb-fake), @step3 the program and the
# skill's folder, @step4 the setup on the page, @step5 rewe switching over.
Feature: PG Programs for skills (docs/spec/skill-programs.md)

  @step3
  Scenario: PG-01 Mira gets the skill's program for her architecture
    Given a skill with a program whose address has {{ arch }} and a sum for Mira's architecture
    When the skill is switched on
    Then Mira downloads the program once, checks its sum, keeps it under programs/ and starts it as the skill's server

  @step3
  Scenario: PG-02 A wrong sum is never started
    Given the address serves a file whose sum differs from the skill's
    When the skill is switched on
    Then nothing is written under the program's name and nothing is started
    And the skill has a problem naming the address and both sums

  @step3
  Scenario: PG-03 A program that is there is not downloaded again
    Given the program is under programs/ with the right sum
    When Mira starts
    Then she downloads nothing and starts it

  @step3
  Scenario: PG-04 A new version comes with the skill only
    Given the skill's file names a new version with a new sum
    When the skills are loaded anew
    Then Mira downloads the new program and starts it
    And the old program is deleted at the next start

  @step3
  Scenario: PG-05 A failed download is said and tried again later
    Given the address does not answer
    When the skill is switched on
    Then the skill's tools are missing and its entry says why
    And Mira tries again after 10 minutes, not in a loop

  @step3
  Scenario: PG-06 Not for this architecture
    Given a skill with a program without a sum for Mira's architecture
    When the skills are loaded
    Then it is a problem of the skill naming the architecture

  @step3
  Scenario: PG-07 One's own program wins
    Given the setting programm names a file
    When the skill is switched on
    Then Mira starts that file and downloads nothing

  @step3
  Scenario: PG-08 The skill's folder
    When a skill with a stdio server is loaded
    Then skill-data/<skill>/ exists with mode 0700 and {{ data }} is its path
    And it is still there after a restart and after switching the skill off and on

  @step4
  Scenario: PG-09 Setup tools never reach a model
    Given a skill whose setup lists login_start
    When the model opens the skill
    Then login_start is in no tool list
    And a call of it by the model is refused as an unknown tool
    And a setup tool that tools names as well (status) is offered to the model too
    And a skill that lists a setup tool under confirm has a problem

  @step4
  Scenario: PG-10 The page builds the form from the tool's schema
    Given a skill whose setup lists store_search with a required string zip
    When an admin opens the skill's entry
    Then the section Einrichten has the button with the skill's label and a required text field zip
    And someone who is no admin does not see the section

  @step4
  Scenario: PG-11 Calling a setup tool
    When an admin fills zip and clicks the button
    Then the service calls store_search on the skill's server and the page shows the answer, addresses as links
    And the log names the admin, the skill, the tool and the outcome, without the arguments

  @step1
  Scenario: PG-12 The REWE login without a terminal
    Given korb is not logged in
    When login_start is called
    Then it answers an address to sign in and a hint how to copy the redirect address
    And login_complete with that redirect address logs korb in

  @step1
  Scenario: PG-13 The delivery service without a terminal
    When store_search is called with a postcode and store_set with the delivery service's id
    Then korb's status names the delivery service
