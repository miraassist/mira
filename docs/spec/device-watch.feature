Feature: Device watch (docs/spec/device-watch.md)

  @step1
  Scenario: CW-01 A missing blueprint is saved in Home Assistant
    Given Home Assistant has none of Mira's blueprints
    When the blueprints are made ready
    Then each of the four blueprints is saved under "mira/" with blueprint/save

  @step1
  Scenario: CW-02 An unchanged blueprint is not saved again
    Given Home Assistant has every blueprint in its current version
    When the blueprints are made ready
    Then nothing is saved

  @step1
  Scenario: CW-03 A changed blueprint replaces the old one and reaches its automations
    Given Home Assistant has the blueprint "water_runs_on" in an older version
    And two automations use it
    When the blueprints are made ready
    Then the blueprint is saved with allow_override
    And exactly those two automations are reloaded

  @step1
  Scenario: CW-04 Saving a blueprint is refused
    Given Home Assistant refuses blueprint/save
    When the blueprints are made ready
    Then a log line names the blueprint's path and Home Assistant's answer
    And no watch is drafted from that blueprint

  @step2
  Scenario: CW-05 A wish that fits a blueprint becomes a task from it
    Given the house has a water meter
    When the user says "Pass auf, ob bei uns Wasser dauernd läuft"
    And agrees to the proposal
    Then an automation with use_blueprint "mira/water_runs_on.yaml" and the water meter as input is saved
    And it is labelled "Mira" and has a row of kind task

  @step2
  Scenario: CW-06 The proposal comes from the rendered automation
    Given the planner chose "water_runs_on" with a duration of 30 minutes
    When the proposal is made
    Then it says the water meter and the 30 minutes

  @step2
  Scenario: CW-07 Home Assistant refuses the inputs
    Given the planner chose a blueprint with an input Home Assistant refuses
    When the draft is checked
    Then the planner gets Home Assistant's answer once
    And if it is refused again, the tool's error names the blueprint and the answer
    And nothing is saved

  @step2
  Scenario: CW-08 A blueprint that does not exist
    Given the planner names a blueprint that is not in the catalog
    When the draft is checked
    Then the code refuses it before Home Assistant is asked
    And the planner gets the reason once

  @step2
  Scenario: CW-09 A wish without a blueprint is drafted as before
    When the user says "Sag Bescheid, wenn die Waschmaschine fertig ist"
    Then the planner drafts an automation of its own as today

  @step2
  Scenario: CW-10 A watch is deleted like a task
    Given a watch made from a blueprint
    When the user says "Pass nicht mehr aufs Wasser auf"
    Then its automation is deleted
    And the blueprint stays in Home Assistant

  @step3
  Scenario: CW-11 Water runs on
    Given a watch "water_runs_on" on the fake water meter with 30 minutes
    When the meter changes every 5 minutes for 30 minutes
    Then a simple message says that water has run for 30 minutes, with the meter's value
    And it is said only once while the water keeps running

  @step3
  Scenario: CW-12 Water never rests
    Given a watch "water_never_rests" on the fake water meter
    When the meter has no rest of an hour within the window
    Then a simple message says the meter did not rest, with its value
    And it comes again after another window without a rest

  @step3
  Scenario: CW-13 A rest starts the window anew
    Given a watch "water_never_rests" on the fake water meter
    When the meter rests for an hour before the window ends
    Then no message comes at the end of the first window

  @step3
  Scenario: CW-15 A device leaves its daily routine
    Given a watch "daily_routine" on a fake plug with the windows "07:40-11:40, 14:20-21:00"
    When the plug's power drops to 0 W at 18:36
    Then 15 minutes later a simple message says the device left its daily routine

  @step6
  Scenario: CW-16 Mira proposes the daily routine from the history
    Given the fake plug's power was above 5 W from 07:40 to 11:40 and from 14:20 to 21:00 for the last days
    When the user says "Achte aufs Aquarium"
    Then Mira proposes the two windows in words
    And the user's correction of a time goes into the windows

  @step6
  Scenario: CW-17 No daily routine in the history
    Given the plug's power shows no recurring times
    When the user asks Mira to watch its daily routine
    Then Mira says she finds no routine and asks for the times

  @step2
  Scenario: CW-21 A watch is switched off and on by voice
    Given a watch "water_never_rests"
    When the user says "Pass erstmal nicht aufs Wasser auf"
    Then its automation is switched off, not deleted
    And "Pass wieder aufs Wasser auf" switches it on

  @step4
  Scenario: CW-23 The device profile finds a cycle
    Given a power sensor on at most hours with a mean far below its peak
    And ten days of states with runs of 55 to 80 minutes every two hours
    When its device profile is computed
    Then its kind is cycle with the longest run of 80 minutes and its longest pause

  @step4
  Scenario: CW-24 The device profile finds a run with too few in the last days
    Given a power sensor with an hourly peak above 500 W now and then for months
    And one run in the last ten days
    When its device profile is computed
    Then its kind is device run and it says that runs are missing

  @step4
  Scenario: CW-25 The recorder refuses
    Given Home Assistant refuses the statistics for a sensor
    When its device profile is computed
    Then the tool's error names the sensor and Home Assistant's answer

  @step7
  Scenario: CW-26 A fridge runs without a pause
    Given a watch "duty_cycle" on a fake plug with max_on 120 minutes
    When the plug's power stays above 5 W for 120 minutes
    Then a simple message says it runs without a pause

  @step7
  Scenario: CW-27 A fridge does not run any more
    Given a watch "duty_cycle" on a fake plug with max_off 240 minutes
    When the plug's power stays at 0 W for 240 minutes
    Then a simple message says it does not run any more

  @step7
  Scenario: CW-28 A device runs too long
    Given a watch "device_run" on a fake plug with max_minutes 180
    When a run lasts 180 minutes
    Then a simple message says it runs longer than usual

  @step7
  Scenario: CW-29 A device run ends without heating
    Given a watch "device_run" on a fake plug with heat_above 1500 W
    When a run ends without a peak above 1500 W
    Then a simple message says it finished without heating

  @step8
  Scenario: CW-30 A candidate waits for its next run
    Given the dishwasher is a candidate with too few runs in the last days
    When the user says "Pass auf die Spülmaschine auf"
    Then Mira says she takes it into the watching after its next run
    And when its next run ended and the device profile has enough, the offer comes as an entry with its thread

  @step1
  Scenario: CW-31 A lost connection is tried again, a refusal is not
    Given Home Assistant answers blueprint/save first with a 503, then with success
    When the blueprints are made ready
    Then the blueprint is saved on the second try
    And when Home Assistant refuses blueprint/save, it is not tried again and the log names the path and the answer

  @step5
  Scenario: CW-32 Mira lists the candidates of the house
    Given the house has a water meter and a fake plug with a cycle
    When the user says "Gibt's Geräte, die du überwachen könntest?"
    Then Mira names the water meter and the plug with what she would watch on each

  @step5
  Scenario: CW-33 What is watched or declined is not listed
    Given the water meter is watched already and the plug was declined
    When the user asks for candidates
    Then neither is named

  @step5
  Scenario: CW-34 No candidates
    Given every candidate of the house is watched or declined
    When the user asks for candidates
    Then Mira says she finds nothing new to watch

  @step5
  Scenario: CW-35 A sensor that cannot be read
    Given the recorder refuses the statistics of one power sensor
    When the user asks for candidates
    Then the others are listed
    And the answer names the sensor that could not be read

  @step8
  Scenario: CW-36 A declined offer
    Given the offer for the dishwasher came after its run
    When the user says "Nein, lass mal"
    Then the thread ends and the dishwasher is kept as declined
    And it is not offered again
    And "Pass doch auf die Spülmaschine auf" makes the watch all the same and forgets the decline
