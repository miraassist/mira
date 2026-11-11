Feature: Messages
  Who sees which open messages (docs/spec/messages.md).

  @step1
  Scenario: MS-01 A private message is never listed at a satellite
    Given a private message to Anna, who is home alone
    When messages_list runs at the kitchen's satellite
    Then the message is not listed
    And in Anna's own chat it is listed

  @step1
  Scenario: MS-02 A person's message is not listed at a satellite while the person is away
    Given a message to Ben, who is away
    When messages_list runs at the kitchen's satellite
    Then it is not listed
    And once Ben is home it is

  @step1
  Scenario: MS-03 A confidential message is listed at a satellite only as the setting allows
    Given a confidential message to Anna, confidential_voice "when_alone", Anna and Ben home
    When messages_list runs at the kitchen's satellite
    Then it is not listed
    And with Anna home alone it is

  @step1
  Scenario: MS-04 A message heard but not done is still listed
    Given a message to the house that was read out
    When messages_list runs at the kitchen's satellite
    Then it is listed as heard

  @step1
  Scenario: MS-05 A person's own chat lists the house's and the person's own
    Given messages to the house, to Anna (private) and to Ben
    When messages_list runs in Anna's chat
    Then the house's and Anna's are listed, Ben's not

  @step1
  Scenario: MS-06 Without a room or a person only the house's are listed
    Given messages to the house and to Anna
    When messages_list runs in an agent's run
    Then only the house's is listed
    And a reminder is not listed
