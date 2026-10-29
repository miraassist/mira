# Scenarios of docs/spec/telefon.md. Every scenario has a number (TE-01, …); the Go test that checks it carries the
# number in its name (TestTE01_…). @step1 is the whole feature.

Feature: TE Phone calls

  @step1
  Scenario: TE-01 A contact is called from the satellite being spoken at
    Given the phone book has "Oma Erika" with one mobile number
    And a voice conversation at the Show, whose phone is ready
    When the model calls phone_call with "Oma"
    Then Home Assistant calls esphome.show_phone_call with that number
    And the tool tells the model whom it calls, without the number

  @step1
  Scenario: TE-02 Several contacts fit
    Given the phone book has "Oma Erika" and "Oma Hilde"
    When the model calls phone_call with "Oma"
    Then nothing is dialed, and the tool names both, so the model asks

  @step1
  Scenario: TE-03 Several numbers
    Given a contact with a home and a mobile number, neither the main one
    When the model calls phone_call without a kind
    Then nothing is dialed, and the tool names the kinds
    And with the kind "Handy" the mobile number is dialed

  @step1
  Scenario: TE-04 The main number counts as clear
    Given a contact with a home and a mobile number, the mobile one marked as main
    When the model calls phone_call without a kind
    Then the mobile number is dialed

  @step1
  Scenario: TE-05 A room in the house goes over the intercom
    Given a TECHO5 device in the kitchen
    When the model calls phone_call with "die Küche"
    Then Home Assistant calls esphome.show_intercom_call with the kitchen's device

  @step1
  Scenario: TE-06 A number is dialed as it is
    When the model calls phone_call with "0721 123456"
    Then that number is dialed

  @step1
  Scenario: TE-07 Only in a voice conversation
    Given a conversation on Telegram, or a background run
    When phone_call is called
    Then nothing is dialed, and the tool says calls go only from a satellite

  @step1
  Scenario: TE-08 A satellite without a phone, or not signed in at the registrar
    Given a satellite without a phone_call action, or whose phone sensor says Connecting, Login refused or Cannot sign in
    When phone_call is called with a contact
    Then nothing is dialed, and the tool says why (no phone, not yet signed in, login refused, box not reachable)
    And an intercom call goes through all the same

  @step1
  Scenario: TE-09 Without the skill no names, but rooms and numbers
    Given the skill telefon is off
    When phone_call is called with a name, a room and a number
    Then the name is not found (the phone book is off), the room and the number are called

  @step1
  Scenario: TE-10 The phone book is read from the FRITZ!Box before every call
    Given Mira FRITZ!Box answers get_phonebook
    When the phone book is needed twice
    Then it is read twice, so a changed number counts at once
    And when the box does not answer, the contacts read last stand in, and the page says why

  @step1
  Scenario: TE-11 Test connection reads the phone book
    Given the skill telefon in the editor
    When Test connection is pressed
    Then the page says how many contacts the phone book has, or why it could not be read

  @step1
  Scenario: TE-13 Fax numbers are not dialed
    Given a contact whose only number is a fax number
    When the model calls phone_call with the contact
    Then nothing is dialed, and the tool says the contact has no number to call

  @step1
  Scenario: TE-14 The page shows the phone book, not the contacts
    Given the skill telefon is on and read its book
    When the Skills page shows it
    Then it says which books it reads, when, and links to the FRITZ!Box, without any contact

  @step1
  Scenario: TE-15 The name goes with the call where the device shows it
    Given a satellite with phone_call_named
    When the model calls phone_call with a contact
    Then Home Assistant calls phone_call_named with the number and the contact's name
    And a number without a contact goes with phone_call as before

  @step1
  Scenario: TE-16 Without Mira FRITZ!Box the skill is a problem
    Given the skill telefon is on and Home Assistant has no Mira FRITZ!Box
    When the phone book is needed
    Then nothing is dialed, and the tool says the phone book could not be read
    And the skill shows the problem that the integration is missing, until it is there
