# Testable rules of docs/spec/inbox.md. Each scenario has a fixed number (IN-nn) that is never reused; its test
# carries the number in its name (TestIN01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: IN Mira's inbox in Paperless

  @step1
  Scenario: IN-01 A webhook is taken at once and read afterwards
    Given the inbox token is configured
    When Paperless posts {"doc_id": "5444", "title": "Elternbrief", "owner": "anna"} with that token
    Then the answer is 202 {"queued": true} before any reading starts
    And document 5444 is queued for reading

  @step1
  Scenario: IN-02 Only the inbox token opens the inbox
    Given the inbox token and the service's API token are different
    When a webhook comes with the API token, with a wrong token, or without one
    Then the answer is 401 and nothing is queued
    And the inbox token opens no other endpoint of the service

  @step1
  Scenario: IN-03 Without an inbox token the inbox is off
    Given no inbox token is configured
    When a webhook comes
    Then the answer is 404
    And the log says once per start that the inbox is off

  @step1
  Scenario: IN-04 A broken webhook names its field
    Given the inbox token is configured
    When a webhook comes with "doc_id": "abc", or without "owner"
    Then the answer is 400 naming the field
    And a log line carries the start of the body

  @step1
  Scenario: IN-05 A document is read once at a time
    Given document 5444 is queued, or its thread is still open
    When its webhook comes again
    Then it is not queued a second time
    And a log line says why

  @step2
  Scenario: IN-06 The owner becomes the person by convention
    Given the HA person "Anna" with the login "anna"
    When the webhook names the owner "anna"
    Then the document is read for Anna

  @step2
  Scenario: IN-07 The reading takes the findings that pass the check
    Given the text of a school letter with three dates, and the model answers three appointments
    And one of them has the day "2031-13-40"
    When the document is read
    Then two findings are kept and the broken one is dropped with a log line

  @step2
  Scenario: IN-08 A long text is cut and the cut is said
    Given a document text of 50,000 characters
    When it is read
    Then the reading gets the first 20,000 characters
    And the message to the person says that only the beginning was read
    And the log names the length

  @step2
  Scenario: IN-09 At most 20 findings
    Given the model answers 25 appointments that pass the check
    When the document is read
    Then the plan has 20 steps
    And the message says that there were 5 more

  @step2
  Scenario: IN-10 An unreachable MCP server is tried again, then left to the catch-up
    Given the skill's MCP server times out on every call
    When a document is read
    Then the text is asked for 3 times, waiting 2 s, 4 s and 8 s, with a log line per try
    And on giving up the document stays unread and a log line says the next start takes it

  @step2
  Scenario: IN-11 A refused document is said to the person
    Given Paperless answers 403 for the document's text
    When the document is read
    Then there is no retry
    And the person gets a message that Mira cannot reach the document
    And the document stays unread

  @step2
  Scenario: IN-12 An answer that is no JSON names its start
    Given the model answers "Gerne! Hier sind die Termine: …"
    When the document is read
    Then the log line carries the first 200 characters of the answer
    And the person gets a message that the document could not be read

  @step3
  Scenario: IN-13 Findings become a plan and a question to the person
    Given two findings in the letter "Elternbrief" for Anna
    When the reading is done
    Then a thread of the inbox's target is started for Anna, bound to her
    And its plan has one open step per finding
    And a message of the thread asks Anna whether to enter them

  @step3
  Scenario: IN-14 Entering takes the checked data, not the model's words
    Given the plan's step 1 is "Elternabend, 3.12. 19:30, Aula"
    When the model calls inbox_enter with steps [1]
    Then the calendar entry has exactly that title, day, time and place
    And step 1 is ticked with the note "eingetragen"

  @step3
  Scenario: IN-15 Some steps, and a change
    Given a plan with three steps
    When the model calls inbox_enter with steps [1, 2] and for step 2 the start "20:00"
    Then two entries are made, the second at 20:00 after the same check as the reading
    And step 3 stays open

  @step3
  Scenario: IN-16 Without a person or a calendar there is only the message
    Given the owner "scanner" maps to no person, or Anna has no calendar by convention
    When a document with two findings has been read
    Then no thread is started
    And a message to the house, or to Anna, names both findings in full and asks what to do with them
    And the document is marked read

  @step3
  Scenario: IN-17 A failed entry leaves its step open
    Given HA refuses the entry of step 2
    When the model calls inbox_enter with steps [1, 2]
    Then step 1 is entered and ticked, step 2 stays open
    And the tool's answer names the failure of step 2

  @step3
  Scenario: IN-18 An unanswered question ends without entries
    Given the inbox thread got no answer
    When its deadline of 3 days passes
    Then nothing is entered, its message is withdrawn, and a log line says so

  @step4
  Scenario: IN-19 After the reading the document is marked read, the tag "Mira" stays
    Given the tag "Mira gelesen" has the id 621
    When document 5444 has been read, with or without findings
    Then Mira calls bulk_edit_documents with documents [5444], method add_tag, tag 621
    And the tag "Mira" is not touched

  @step4
  Scenario: IN-20 A document that cannot be marked read is said
    Given Paperless refuses adding the tag (403)
    When the document has been read
    Then the person gets a message to set the tag "Mira gelesen" on it
    And the thread runs anyway

  @step4
  Scenario: IN-21 At start the unread documents are caught up
    Given a webhook named the owner "anna" of a document whose owner number is 3
    And three documents of owner 3 carry "Mira" without "Mira gelesen" while Mira was not running, and two carry both
    When the Mira service starts and the skill's MCP server is connected
    Then the three are queued once for "anna", as if their webhooks had come, and the two are not
    And no further query follows until the next start

  @step4
  Scenario: IN-22 A missing tag is named
    Given Paperless has no tag "Mira gelesen", or Mira may not see it
    When the Mira service starts
    Then a log line names the tag once
    And webhooks are still taken

  @step4
  Scenario: IN-23 A document of an owner not learned yet stays unread
    Given no webhook ever named the owner of the unread document 5450 (owner number 9)
    When the Mira service catches up
    Then 5450 is not queued
    And a log line names the document and the owner number

  @step6
  Scenario: IN-24 A mail of the inbox is answered at once
    Given the IMAP device "Postfach anna" and a new mail in its folder
    When the integration posts it to /v1/mail with imap_entry and uid
    Then the answer is 202 {"queued": true} before any reading starts

  @step6
  Scenario: IN-25 Parcel mails stay as they are
    Given a mail of the IMAP device "Pakete"
    When the integration posts it to /v1/mail
    Then the answer waits for the reading and says what the mail did, as before

  @step6
  Scenario: IN-26 The integration sends imap_entry and uid
    Given the service names "postfach" in inbox_apps, and an imap_content event with initial true for the device
      "Postfach anna"
    When the integration passes the mail on
    Then the body carries the entry's id as imap_entry and the event's uid
    And no repair asks for a calendar "Postfach anna"

  @step6
  Scenario: IN-27 An older integration without the new fields
    Given a mail of "Postfach anna" posted without imap_entry and uid
    When the service reads it
    Then it is read and asked as any other
    And it is known again by its hash under the device's name

  @step7
  Scenario: IN-28 The device name names the person
    Given the IMAP device "Postfach anna" and the person Anna with the login "anna"
    When a mail with an appointment is read
    Then Anna's inbox thread is started with the subject as the title

  @step7
  Scenario: IN-29 Without a login the mail is the house's
    Given the IMAP device "Postfach"
    When a mail with an appointment is read
    Then no thread is started and the house gets a message with the findings and the question

  @step7
  Scenario: IN-30 A mail is read once
    Given a mail of entry E is queued, or its thread is still open
    When the same mail comes again, on any uid
    Then it is not queued a second time, with a log line

  # IN-31 and IN-32 (marking a mail seen with imap.seen) were dropped on 2026-11-12, before a release: HA's uid is the
  # mail's position in its folder (decision 24). Their ids are not used again.

  @step8
  Scenario: IN-33 A mail read before is not read again
    Given a mail of entry E has been read, at uid 4
    When a later mail is moved out of the folder and HA reports the first one again, now at uid 5
    Then it is not queued, and a log line names the entry, the hash and the subject

  @step8
  Scenario: IN-34 A new mail on an old mail's uid is read
    Given a mail of entry E at uid 3 has been read
    When another mail comes at uid 3
    Then it is read and asked about

  @step8
  Scenario: IN-35 Mails alike but for the date are two mails
    Given a mail of entry E has been read
    When a mail with the same sender, subject and text but another date comes
    Then it is read
    And the same mail with its date in another time zone is not

  @step8
  Scenario: IN-36 A mail whose reading failed is not kept
    Given the reading of a mail fails
    When the same mail comes again
    Then it is queued

  @step8
  Scenario: IN-37 Mails read that cannot be looked up or kept
    Given Mira's database refuses to look up or keep the mails read
    When a mail comes
    Then it is read and asked about anyway
    And a log line names the entry, the hash and the error, and says that the mail may be read a second time

  @step8
  Scenario: IN-38 The last 100 mails per entry are kept
    Given 101 mails of entry E and one of entry F have been read
    When Mira starts again
    Then the 100 latest of E and the one of F are known, the first of E is not
