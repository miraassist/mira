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

  # Kinds of messages (decided 2026-11-12)

  @step2
  Scenario: MS-07 A message without a kind is refused
    Given a message put without a kind
    Then it is refused with the caller's error and a log line naming the caller and the text's start

  @step2
  Scenario: MS-08 messages_read names each message's kind, skill and thread
    Given a simple message to the house and a postfach message to Anna with its thread
    When Anna asks at a satellite for her messages
    Then messages_read gives both, the second with the kind postfach, the skill postfach and the thread

  @step2
  Scenario: MS-09 A simple message with several entries is worded as one text
    Given Anna's unheard simple message has three entries
    When messages_read runs
    Then the model words them as one text before it is read out, with dates and times as they stand

  @step2
  Scenario: MS-10 Different kinds are named and the user chooses
    Given a postfach message and a voicemail message wait
    When messages_read runs
    Then its instruction says to name what waits and to ask what first

  @step2
  Scenario: MS-11 A message of an ended thread is not read out
    Given a postfach message whose thread has ended
    When messages_read runs
    Then it is marked done and not read out, and a log line names the message and the thread

  @step3
  Scenario: MS-12 The first mail starts its thread and the message
    Given Anna has no unheard postfach message
    When a mail with things to do comes into her inbox
    Then a thread of Anna is started for the mail, and a postfach message to Anna has one entry leading to it

  @step3
  Scenario: MS-13 A further mail while the message is unheard
    Given Anna's postfach message is unheard with one entry
    When a further mail with things to do comes
    Then it gets a thread of its own, and the same message gets a second entry leading to it

  @step3
  Scenario: MS-14 A further mail after the message was heard
    Given Anna's postfach message was heard and its thread is still open
    When a further mail with things to do comes
    Then it gets a thread of its own and a new postfach message with one entry

  @step3
  Scenario: MS-15 A mail's thread expires when inbox.read says
    Given inbox.read gave a mail's plan an expiry at the deadline to answer, tomorrow 12:00
    When that time passes without the thread being taken up
    Then the thread expires, nothing is entered, and its entry leaves the message

  @step4
  Scenario: MS-16 A recording starts the house's thread and message
    Given no voicemail message waits and no answering machine thread is open
    When the answering machine's count of new messages rises
    Then a thread of the house starts with the new recordings in its plan and a voicemail message to the house names it

  @step4
  Scenario: MS-17 A further recording extends them
    Given the voicemail message is unheard
    When another recording comes
    Then the plan gains it, the message gets an entry, and the thread expires 5 days after this recording; there is
      still one thread and one message

  @step4
  Scenario: MS-18 Heard at the box, the thread ends and the message goes
    Given the voicemail message is unheard
    When the count of new messages goes to 0
    Then the thread ends, its entries leave the message, and the message is done and withdrawn

  @step4
  Scenario: MS-19 The ring shows the kinds of what waits
    Given a voicemail message waits and no other
    Then the ring glows in the answering machine's colour

  @step4
  Scenario: MS-20 The box cannot be read
    Given the box's list cannot be read when a recording comes
    Then no message and no thread are made, the ring still shows the count, and a log line names the error

  @step2
  Scenario: MS-21 A further simple text while the message is unheard
    Given Anna's simple message is unheard
    When a task leaves Anna a notice
    Then it is an entry of the same message; there is still one unheard simple message for Anna

  @step2
  Scenario: MS-22 An entry with the same key replaces the one before
    Given Anna's unheard simple message has an entry of the gas task with the key gas
    When the gas task leaves a notice with the key gas again
    Then the entry is replaced, not added

  @step2
  Scenario: MS-23 Two reminders stay two
    Given Anna asked for a reminder at 7:00 and one at 18:00
    Then there are two reminder messages, each announced when it is due

  @step2
  Scenario: MS-24 An urgent message is not added to a simple one
    Given the house's simple message is unheard
    When an urgent message to the house comes
    Then it is a message of its own, announced at once, and the simple one waits as before

  @step2
  Scenario: MS-25 A private message is not added to a simple one
    Given Anna's simple message is unheard
    When a private message for Anna comes
    Then it is a message of its own, sent over Anna's channels and never spoken

  @step2
  Scenario: MS-26 thread_take makes a thread the turn's own
    Given Anna has two open postfach threads, the older for the practice
    When Anna says "erst die Praxis" and the model calls thread_take with the practice's thread
    Then every thread tool in this and the following turns acts on the practice's thread until it ends

  @step2
  Scenario: MS-27 thread_take refuses a thread that is not the asker's
    Given Ben's postfach thread is open
    When thread_take is called with it in Anna's turn
    Then it is refused, and the tool names the threads open for Anna

  @step2
  Scenario: MS-28 When a thread ends, the tool names what is still open
    Given Anna's postfach message has two entries with threads
    When the first thread is done
    Then the tool's answer names the other entry as still open

  @step2
  Scenario: MS-29 The line "Wartet noch" in every turn
    Given Anna has two postfach entries and the house one device entry waiting
    When Anna speaks at a satellite
    Then the turn's context has one line naming what waits, not the threads in full

  @step2
  Scenario: MS-30 An ended thread's entries leave the message
    Given a message whose two entries lead to two threads
    When one thread expires
    Then its entry leaves the message at once; when the other ends too, the message is done and withdrawn

  @step5
  Scenario: MS-31 A found device starts the house's thread and message
    Given no device message waits
    When Home Assistant finds a device
    Then the code starts the house's thread with the device in its plan, expiring in 1 day, and a device message to
      the house has its entry

  @step5
  Scenario: MS-32 A further device extends them
    Given the device message is unheard
    When another device is found
    Then the plan gains it, the message gets an entry, and the thread expires 1 day after this one

  @step5
  Scenario: MS-33 After a day the found devices are gone
    Given the house's device thread was not taken up
    When its expiry passes
    Then the thread ends, its entries leave the message, and the message is withdrawn
