Feature: The thread's history (docs/spec/thread-history.md)

  A turn that belongs to a thread runs in the thread's history, whoever speaks; when the thread ends, its summary
  goes into the own history of everyone who took part.

  @step1
  Scenario: TV-01 A finisher's result ends its thread through the one place
    Given a thread whose finisher's result is done or cancelled, in the foreground and in the background
    When the result is applied
    Then the thread ends through the one place where threads end
    And its hook is told and its open messages are withdrawn

  @step1
  Scenario: TV-02 Taking back a done thread goes through the same code
    Given a done thread that can be taken back
    When it is taken back
    Then it is cancelled through the same code as every end

  @step2
  Scenario: TV-03 The answer to a thread's announcement runs in the thread's history
    Given an open thread of a room with Mira's question announced at the room's satellite
    When the answer comes, recognised as Anna
    Then the turn runs in the thread's history, not in Anna's
    And Anna's own history key is recorded as a participant

  @step2
  Scenario: TV-04 A change of voice in the same conversation stays in the thread's history
    Given a turn in a thread's history recognised as Anna, whose answer keeps listening
    When the next turn of the same conversation of Home Assistant is recognised as Martin
    Then it runs in the thread's history and sees Mira's question of the turn before
    And Martin's own history key is recorded as a participant

  @step2
  Scenario: TV-05 A person's thread is continued in the running dialog by another voice
    Given an open thread of Anna's, its question announced at a satellite
    When the answer is recognised as Martin
    Then the turn joins Anna's thread and runs in its history

  @step2
  Scenario: TV-06 A person's waiting thread is not shown to another person
    Given an open thread of Anna's waiting
    When Martin starts a new conversation
    Then his turn does not see Anna's thread

  @step2
  Scenario: TV-07 A turn that starts a thread moves into the thread's history
    Given a turn in the speaker's own history
    When it starts a thread
    Then after the turn its messages are in the thread's history and no longer in the own history
    And its conversation is linked to the thread

  @step2
  Scenario: TV-08 Mira's announcement of a thread is in the thread's history
    Given an open thread
    When Mira announces a question of the thread by herself and the answer comes
    Then the question is in the thread's history, with the turn that answers it

  @step3
  Scenario: TV-09 A done thread's summary goes into every participant's own history
    Given a thread that Anna and Martin took part in
    When a tool in Martin's turn ends it as done
    Then after the turn's answer Anna's and Martin's own histories each hold the summary
    And it names the target, the request, the end, the fields or the plan, and Mira's last sentence

  @step3
  Scenario: TV-10 An expired or cancelled thread gets its summary at once
    Given an open thread with a participant
    When it expires or is cancelled from the page
    Then the participant's own history holds the summary with that end

  @step3
  Scenario: TV-11 A summary that cannot be written leaves a trace and spares the others
    Given a thread with two participants, and the first one's history cannot be written
    When the thread ends
    Then a log line names the thread, the participant and the error
    And the second participant gets the summary

  @step3
  Scenario: TV-12 The model is told what a summary in a history is
    Given a turn in a thread's history, and later a turn after the thread ended
    Then the first is told that the history is the thread's, with several speakers
    And the second sees the record's header that says what it is and how to use it
