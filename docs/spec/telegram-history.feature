Feature: The chat's history (docs/spec/telegram-history.md)

  A Telegram chat is one history: every turn of the chat runs in it, also in a thread, and what Mira sends into the
  chat by herself is recorded in it.

  @step1
  Scenario: TC-01 The adapter sends the chat's notify entity as the conversation
    Given a Telegram chat that is one of Anna's channels, with the notify entity "notify.telegram_anna"
    When a text comes from that chat
    Then the turn goes to the service with the conversation "notify.telegram_anna" and the channel "telegram"

  @step1
  Scenario: TC-02 A new conversation drops the chat's history by its notify entity
    Given Anna's Telegram chat with a history
    When Anna sends "/neu"
    Then the service is asked to reset the conversation "notify.telegram_anna" of the channel "telegram"

  @step2
  Scenario: TC-03 A turn of a thread in a chat sees the chat before the thread
    Given Anna's chat in which she said her favourite number is 37
    And a thread started in the chat afterwards and still open
    When Anna asks in the chat which number she named
    Then the turn belongs to the thread and runs in the chat's history
    And the model sees the turn with the number

  @step2
  Scenario: TC-04 A turn that starts a thread in a chat is not moved
    Given Anna's chat
    When a turn in it starts a thread
    Then the turn stays in the chat's history
    And the chat is linked to the thread and recorded as a participant
    And no thread's history is created for the channel telegram

  @step2
  Scenario: TC-05 A thread's turns stay in the chat after it ends
    Given a thread whose turns ran in Anna's chat
    When the thread ends
    Then its turns are still in the chat's history
    And the thread's summary follows them in the chat's history

  @step2
  Scenario: TC-06 The thread's heading on Telegram names the chat's history
    Given a turn in a chat that belongs to an open thread
    When its context is built
    Then the thread's heading says that the history above is the chat and the thread is part of it

  @step2
  Scenario: TC-07 A voice turn in a thread keeps the thread's history
    Given an open thread of a room whose question was announced at the room's satellite
    When the answer comes at the satellite
    Then the turn runs in the thread's history, as before

  @step3
  Scenario: TC-08 A delivery to a chat is recorded in the chat's history
    Given Anna's chat with a history under "telegram:notify.telegram_anna"
    When a reminder for Anna is delivered over "notify.telegram_anna"
    Then the chat's history ends with a record that Mira sent it on her own, with the message's id and kind
    And with the text exactly as it was sent as Mira's answer

  @step3
  Scenario: TC-09 A delivery to another channel is not recorded
    Given Anna's first channel is a companion app
    When a message for Anna is delivered over it
    Then no history is written

  @step3
  Scenario: TC-10 A failed record leaves the delivery delivered
    Given Anna's chat with a history whose writing fails
    When a message for Anna is delivered over the chat
    Then the message is delivered
    And the log says the delivery record could not be written, with the message's id and the chat's key

  @step3
  Scenario: TC-11 A failed send writes no record
    Given Anna's chat with a history
    When sending a message over the chat fails
    Then the chat's history gets no record

  @step3
  Scenario: TC-12 A new conversation keeps the chat known for deliveries
    Given Anna's chat with a history
    When Anna sends "/neu"
    Then the chat's history is empty and the chat is still known
    And the next delivery over the chat is recorded in it

  @step5
  Scenario: TC-13 A thread is kept from the turn it came into the chat
    Given a thread created before the chat's oldest turn
    And Anna's chat with 50 turns, linked to the thread in its 40th turn
    When the history is compacted by its number of turns
    Then the last 30 turns stay, the thread's turns among them

  @step5
  Scenario: TC-14 Linking the chat to the same thread again keeps the time
    Given Anna's chat linked to a thread
    When later turns of the chat belong to the same thread
    Then the time of the chat's link to the thread does not change
    And a link to another thread sets it anew

  @step5
  Scenario: TC-15 At the context limit the running thread is cut too
    Given a history whose running thread began before its last 30 turns
    When a turn uses 90 % of the model's context window
    Then the history is cut to its last 30 turns
    And the log names the thread that was cut

  @step5
  Scenario: TC-16 A cut the running thread prevents is logged
    Given Anna's chat with 50 turns, linked to a running thread in its first turn
    When the history is compacted by its number of turns
    Then nothing is cut
    And the log warns that the running thread began at the history's first turn, with the session and the thread
