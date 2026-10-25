# Scenarios of docs/spec/sprecher.md. Every scenario has a number (SP-01, …); the Go test that checks it carries the
# number in its name (TestSP01_…). @step3 is the source in Mira's identify.

Feature: SP Speaker recognition at the satellite

  @step3
  Scenario: SP-01 A recognized voice is the person
    Given several persons at home and a speaker_id service
    And the service has an accepted utterance of anna with the turn's text
    When a turn at a satellite asks who is speaking
    Then it is anna

  @step3
  Scenario: SP-02 An uncertain voice stays anonymous
    Given the service's utterance with the turn's text is uncertain
    When a turn at a satellite asks who is speaking
    Then nobody is, unless the "alone at home" rule names someone

  @step3
  Scenario: SP-03 The voice comes before "alone at home"
    Given ben is alone at home with alone_is_me on
    And the service has an accepted utterance of anna with the turn's text
    When a turn at a satellite asks who is speaking
    Then it is anna

  @step3
  Scenario: SP-04 The signed-in user comes before the voice
    Given a turn from the HA app signed in as ben
    And the service has an accepted utterance of anna with the turn's text
    When the turn asks who is speaking
    Then it is ben

  @step3
  Scenario: SP-05 A service that is down or slow holds nothing up
    Given a speaker_id service that does not answer within 300 ms
    When a turn at a satellite asks who is speaking
    Then the "alone at home" rule decides, and the turn goes on

  @step3
  Scenario: SP-06 A voice of nobody Home Assistant knows
    Given the service has an accepted utterance of a person id Home Assistant does not have
    When a turn at a satellite asks who is speaking
    Then the voice counts for nothing

  @step3
  Scenario: SP-07 Two satellites heard the same person
    Given the service has two accepted utterances of anna with the turn's text
    When a turn at a satellite asks who is speaking
    Then it is anna

  @step3
  Scenario: SP-08 Two satellites disagree
    Given the service has an accepted utterance of anna and one of ben with the turn's text
    When a turn at a satellite asks who is speaking
    Then nobody is, unless the "alone at home" rule names someone

  @step3
  Scenario: SP-09 The threshold is Mira's, per person too
    Given ben's voice scores 0.62
    When the threshold is 0.7, or 0.6, or 0.8 with voice_threshold 0.6 for ben
    Then he is nobody, ben, ben

  @step3
  Scenario: SP-10 A recognized person is named when it is personal
    Given a turn at a satellite by a person Mira recognized by the voice or the "alone at home" rule
    When the turn is put to the model
    Then the model is told how she knows them, and to name them when it does something personal for them, not in every answer
    And for a person who signed in, it is told neither
