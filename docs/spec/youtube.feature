# Scenarios of docs/spec/youtube.md. Every scenario has a number (YT-01, …); the Go test that checks it carries the
# number in its name (TestYT01_…). @step2 is the skill (Go), @step3 the run end to end on the test HA.

Feature: YT YouTube research

  @step2
  Scenario: YT-01 The skill only reads
    Given the skill youtube with client id and secret
    When Mira loads it
    Then its connection asks only for the scope youtube.readonly
    And its api section allows GET below https://www.googleapis.com/youtube/v3/ and nothing else
    And none of its tools writes

  @step2
  Scenario: YT-02 Google's login gives a refresh token
    Given the skill youtube
    When the integration asks for its login
    Then the extra parameters are access_type offline and prompt consent, with PKCE

  @step2
  Scenario: YT-03 A search is one search and one look at the details
    Given the tool youtube__suche with a topic and 7 days
    When it runs
    Then it makes one search with publishedAfter 7 days ago, the order asked for and only the ids, then one request
      for the details of all of them
    And it returns title, channel, date, views, minutes and the address of each video

  @step2
  Scenario: YT-04 A channel's uploads cost no search
    Given the tool youtube__kanal with a handle without @
    When it runs
    Then it asks for the channel by @handle, then its uploads, then their details, and makes no search
    And a handle YouTube does not know is a hint, not an error

  @step2
  Scenario: YT-05 The quota used up is a sentence
    Given YouTube answers 403 with the reason quotaExceeded
    When a tool runs
    Then its error says that the daily quota is used up

  @step2
  Scenario: YT-06 The daily list goes to the person named
    Given the skill youtube with a scheduled job from its template liste, with themen and empfaenger
    When its schedule is loaded
    Then it runs at its time and its message goes to the person, and its task names the topics
    And without themen the skill's first job is not made

  @step3
  Scenario: YT-07 Trends are YouTube's charts
    Given the skill youtube, connected
    When the user asks "Was ist gerade angesagt auf YouTube?"
    Then Mira calls youtube__charts and names two or three videos

  @step3
  Scenario: YT-08 A topic is one search
    Given the skill youtube, connected
    When the user asks "Was gibt's Neues zu Home Assistant auf YouTube?"
    Then Mira calls youtube__suche once and answers from it

  @step3
  Scenario: YT-09 The daily list for the topics
    Given the skill youtube, connected, with three topics and a person with a channel
    When the schedule runs
    Then the person gets a private message with one search per topic and the videos that fit, or nothing when none
