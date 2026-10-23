# Scenarios of docs/spec/satellit.md. Every scenario has a number (SA-01, …); the Go test that checks it carries the
# number in its name (TestSA01_…). @step1 is the whole feature.

Feature: SA Sounds and cards on the satellite

  @step1
  Scenario: SA-01 A sound plays on the satellite's speaker
    Given a conversation at a satellite whose device has a speaker
    When the model calls sound_play with fanfare
    Then Home Assistant plays the sound on that speaker, from Home Assistant's address

  @step1
  Scenario: SA-02 The tool returns when the sound is over
    Given a sound of 1.7 seconds
    When the model calls sound_play with it
    Then the tool returns no earlier than the sound's length

  @step1
  Scenario: SA-03 Without a satellite no sound and no card
    Given a conversation on Telegram
    When the model calls sound_play or display_card
    Then nothing plays or shows, and the tool says there is no satellite

  @step1
  Scenario: SA-04 An unknown sound
    Given a name that is no sound of Mira's
    When the model calls sound_play with it
    Then nothing plays, and the tool names the sounds there are

  @step1
  Scenario: SA-05 The sound files are served without a token
    Given Mira's page
    When a satellite fetches /ui/sounds/fanfare.mp3
    Then it gets the MP3, and an unknown name gets 404

  @step1
  Scenario: SA-06 A card on the satellite's screen
    Given a conversation at a satellite with the TECHO5 firmware
    When the model calls display_card with a title, a line and a picture
    Then Home Assistant calls that device's notify_picture, without chime and without speaking

  @step1
  Scenario: SA-07 A satellite without cards
    Given a conversation at a satellite whose device has no notify_picture
    When the model calls display_card
    Then nothing shows, and the tool says this satellite has no screen for cards

  @step1
  Scenario: SA-08 keep_listening makes the satellite listen again without a question
    Given a voice turn whose answer is no question
    When the model called keep_listening in it
    Then the answer asks the satellite to keep listening, and the next answer without the call does not
