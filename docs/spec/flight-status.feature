Feature: Flight status (docs/spec/flight-status.md)

  @step1
  Scenario: FS-01 The skill declares its landing automation
    When the skill flug loads
    Then it declares the automation "gelandet" from blueprints/gelandet.yaml and has no setting

  @step1
  Scenario: FS-02 A landing is a message and the flight is no longer tracked
    Given the blueprint gelandet.yaml
    Then it triggers on flightradar24_tracked_landed
    And it calls mira.send_message and then text.set_value on text.flightradar24_remove_from_track

  @step1
  Scenario: FS-03 The model is told the script and its answer
    Then the skill names the tool flug__abfrage (script.mira_skill_flug_abfrage), the delay verspaetung_minuten, its actions verfolgen, stand and vergessen, the fields of its answer,
      that a number has no date and that there is no gate

  @step3
  Scenario: FS-04 The skill brings its script, which gives the delay
    Given the skill flug
    Then it declares the script "abfrage" from its script blueprint flug.yaml, with a description for the model
    And the script's answer has verspaetung_minuten, the real (once landed) or expected against the scheduled arrival
    And its times of today read "heute 20:15", other days weekday and date ("Do 19.11. 06:40")

  @step3
  Scenario: FS-05 A flight that has landed already is not tracked
    Given Flightradar24 shows the flight landed
    When the model tracks it with aktion verfolgen
    Then the script takes it out of tracking again and answers schon_gelandet true and verfolgt false
    And the skill tells the model to say it landed, with the arrival, the terminal and the delay, and that no message comes

  @step2
  Scenario: FS-10 Asking when a tracked flight lands
    Given a flight in the air is tracked
    When the user asks "Wann landet der Flug SN 2587?"
    Then Mira names the estimated arrival from the script's answer

  @step2
  Scenario: FS-11 Tracking a flight by voice
    When the user says "Behalt bitte den Flug LX 970 im Auge"
    Then the flight is in sensor.flightradar24_additional_tracked and Mira says she will report the landing

  @step2
  Scenario: FS-12 A number Flightradar24 does not know
    When the user says "Behalt den Flug XQ 9999 im Auge"
    Then nothing is tracked and Mira asks whether the number is right

  @step2
  Scenario: FS-13 Forgetting a flight
    When the user says "Den Flug LH 123 kannst du vergessen", and then "Und LH 456 auch"
    Then neither is tracked any more
    And each time Mira says that she no longer keeps an eye on it

  @step2
  Scenario: FS-14 The landing is reported and the tracking ends
    Given a tracked flight lands
    Then the house gets a message "Der Flug … ist gelandet"
    And the flight is no longer tracked
