# Testable rules of docs/spec/anrufbeantworter.md. Each scenario has a fixed number (AB-nn) that is never reused; its
# Go test carries the number in its name (TestAB07_…). The tags @step1 and @step2 say in which step of the spec
# ("Steps") the scenario gets built.
#
# Groups: AB-0x reading and playing · AB-1x showing

Feature: AB The answering machine at the satellite

  # --- Reading and playing --------------------------------------------------------------------------------

  @step1
  Scenario: AB-01 The list names messages and missed calls apart
    Given two new messages and a missed call from Ben, twice
    When the model calls voicemail_list
    Then it gets the messages with from whom, when and minutes, and Ben's missed calls with how often
    And the missed calls are marked as seen

  @step1
  Scenario: AB-02 A message plays at the satellite of the turn and counts as heard
    Given a voice conversation at the Show
    When the model calls voicemail_play with one message
    Then the Show's speaker plays the message's media_content_id
    And the message is marked as heard

  @step1
  Scenario: AB-03 The screen shows what plays
    Given a voice conversation at the Show, which has an overlay
    When a message from Oma plays
    Then the overlay says "Nachricht von Oma" with day and time while it plays, and goes when the Show says it ended

  @step1
  Scenario: AB-04 Playing only at a satellite
    Given a conversation in Telegram
    When the model calls voicemail_play
    Then nothing plays, and the tool says it plays only at a satellite

  @step1
  Scenario: AB-05 Without Mira FRITZ!Box the skill is a problem
    Given the skill anrufbeantworter is on and Home Assistant has no Mira FRITZ!Box
    When Mira starts
    Then the skill shows the problem that the integration is missing

  @step1
  Scenario: AB-06 Of many messages Mira names the newest three
    Given nine messages
    When the model calls voicemail_list
    Then it gets all nine with their ids, and is told to name only the newest three and how many more there are

  @step1
  Scenario: AB-07 With several answering machines each message names its own
    Given messages on "Anrufbeantworter privat" and "Anrufbeantworter business"
    When the model calls voicemail_list
    Then each message names its answering machine
    And with one answering machine none does

  @step1
  Scenario: AB-08 Several messages play one after the other
    Given a voice conversation at the Show, and messages from Oma and from Ben
    When the user says "Ich will eine nach der anderen hören"
    Then the model calls voicemail_play once with both, in that order
    And Mira's answer names the first, and before the second Mira says "Nächste: von Ben, gestern 09:05." on the turn screen
    And each counts as heard once it has played to its end

  @step1
  Scenario: AB-09 Stopping stops the rest
    Given two messages playing one after the other
    When someone says "Stopp" during the first, or answers the announcement of the second
    Then what was not heard to the end does not play on and stays new
    And a wake word alone only lowers the recording while the satellite listens

  # --- Showing --------------------------------------------------------------------------------------------

  @step2
  Scenario: AB-10 The ring shows the answering machine in its colour
    Given no message of Mira waits and one message is on the answering machine
    When the sensor changes
    Then every ring glows amber

  @step2
  Scenario: AB-11 Mira's messages rank first on the ring
    Given a message of Mira waits and a missed call is new
    When the indicators update
    Then the ring glows in Mira's colour

  @step2
  Scenario: AB-12 The screen counts both
    Given two messages on the answering machine and one missed call
    When the indicators update
    Then a screen shows "2 Nachrichten auf dem Anrufbeantworter · 1 verpasster Anruf"

  # --- On Mira's page -------------------------------------------------------------------------------------

  @step3
  Scenario: AB-20 The start page names what waits on the answering machine
    Given two messages on the answering machine and one missed call
    When someone opens Mira's start page
    Then the card for every day says "2 neue Nachrichten auf dem Anrufbeantworter · 1 verpasster Anruf"
    And the line links to the FRITZ!Box's web interface, in a new tab

  @step3
  Scenario: AB-21 Nothing waits, nothing shows
    Given no new message on the answering machine and no new missed call
    When someone opens Mira's start page or the messages page
    Then neither names the answering machine

  @step3
  Scenario: AB-22 The messages page teases it
    Given one message on the answering machine
    When someone opens the messages page
    Then a line above Mira's messages says "1 neue Nachricht auf dem Anrufbeantworter", linking to the FRITZ!Box

  # --- Listening as a thread ----------------------------------------------------------------------------------

  @step4
  Scenario: AB-30 Asking for messages starts the thread with the new messages as its plan
    Given Oma's and Ben's messages are new and Papa's is heard
    When the model calls voicemail_list
    Then a thread of the target anrufbeantworter runs in the room
    And its plan lists Oma and Ben newest first with their ids, not Papa's
    And no model round was needed to write it
    And asked again once Oma's is heard, the plan keeps Oma's ticked and Ben's open

  @step4
  Scenario: AB-31 Asking again keeps the one thread
    Given the thread runs and a new message came meanwhile
    When the model calls voicemail_list again
    Then the same thread's plan lists the new message too, and there is no second thread

  @step4
  Scenario: AB-32 What played to its end is ticked
    Given the thread runs with Oma and Ben open
    When Oma's message has played to its end
    Then it is marked as heard at the box and ticked in the plan, and the plan's current is Ben

  @step4
  Scenario: AB-33 A turn after "Nächste …" joins the thread without becoming the person
    Given Anna started the thread at the Show and two messages play one after the other
    And Mira says "Nächste: von Ben, gestern 09:05." there, as a plain announcement that nothing listens after
    When someone the voice does not recognise says the wake word and "Lösch die und nächste"
    Then the turn belongs to the thread, with its plan and the skill's tools
    And it is no turn of Anna's: no history of hers, no person

  @step4
  Scenario: AB-34 "weiter" while one plays ends that run and plays all the rest
    Given four messages playing one after the other, the second playing
    When the wake word and "weiter" make the model call voicemail_play with the open ones after it
    Then the run that played the second ends before anything new plays, and the second is not ticked
    And the third and the fourth play

  @step4
  Scenario: AB-35 The playing satellite gets no new hint until it ends
    Given the Show plays two messages and the count of new messages drops after the first
    When the screens update
    Then the other screens show the new count at once
    And the Show shows it only once the playing has ended

  @step4
  Scenario: AB-36 A turn that plays after the answer does not listen
    Given the model calls voicemail_play and also asks to keep listening, or asks a question
    When the turn ends
    Then the satellite does not keep listening

  @step4
  Scenario: AB-37 A hint waits while the satellite is in a conversation
    Given the Show listens
    When the count of new messages changes
    Then no hint covers the turn screen
    And it comes once the Show is idle

  @step4
  Scenario: AB-38 A short plan shows every open step
    Given the listening plan holds four messages, one heard
    When a turn sees the thread
    Then its context names all three open ones with their ids, not only the next

  @step4
  Scenario: AB-39 Ending the thread stops its recordings
    Given four messages playing one after the other, the second playing
    When the wake word and "abbrechen" make the model cancel the thread
    Then the recording stops at once, nothing more plays, and the second stays new

  @step4
  Scenario: AB-40 Deleting the one that plays goes on with the next
    Given two messages playing one after the other, the first playing
    When the model deletes it
    Then it stops, is deleted and ticked, and the second plays
