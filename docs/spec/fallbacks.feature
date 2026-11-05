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
