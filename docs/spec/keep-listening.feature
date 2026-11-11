Feature: KL When the satellite keeps listening (docs/spec/keep-listening.md)

  The intent comes from the tool that asked, not from the model's wording.

  @step1
  Scenario: KL-01 A tool that waits for the user keeps the satellite listening
    Given a voice turn whose tool asked the user for something
    When the model's answer ends without a question mark
    Then the satellite keeps listening

  @step1
  Scenario: KL-02 A question mark still keeps it listening
    Given a voice turn without such a tool
    When the answer ends with a question
    Then the satellite keeps listening

  @step1
  Scenario: KL-03 A finish in the same turn ends the listening
    Given a tool of the turn asked the user for something
    When another tool of the same turn ends the dialog
    Then the satellite does not keep listening

  @step1
  Scenario: KL-04 Nothing is awaited without a tool and without a question
    Given a voice turn whose answer states something
    Then the satellite does not keep listening

  @step2
  Scenario: KL-05 Mira's questions for a device carry the mark
    Given the tools that ask for the setup code, the network or the room
    When they answer
    Then the turn waits for the user

  @step2
  Scenario: KL-06 The answer that only reports does not carry it
    Given the tool that says the device may be plugged in now
    When it answers
    Then the turn does not wait for the user
