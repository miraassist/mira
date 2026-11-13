Feature: Group watch (docs/spec/group-watch.md)

  @step1
  Scenario: GW-01 A selection by device class and floor
    Given fake windows on the ground floor and on the top floor
    When the selection "windows on the top floor" is rendered
    Then only the top floor's windows are members

  @step1
  Scenario: GW-02 A labelled entity is no member
    Given the fake garden light carries the label "Mira: nicht überwachen"
    When the selection "all lights" is rendered
    Then the garden light is not a member
    And a light group is not a member, its lights are

  @step1
  Scenario: GW-03 A group helper's members
    Given a group helper with two fake windows
    When the selection is that group helper
    Then exactly its two windows are members, labelled or not

  @step2
  Scenario: GW-04 A wish becomes a group watch
    When the user says "Gib mir Bescheid, wenn ein Fenster offen ist und keiner da ist"
    And agrees to the proposal
    Then an automation with use_blueprint "mira/open_while_away.yaml" is saved as a task
    And the proposal named how many windows and doors it watches today

  @step2
  Scenario: GW-05 An exception sets the label
    When the user says "Sag Bescheid, wenn Licht ewig an ist, aber die Terrasse nicht"
    And agrees
    Then the terrace light carries the label "Mira: nicht überwachen"
    And the proposal said that it is then in no group watch

  @step2
  Scenario: GW-06 A selection without members
    When the user asks to watch the windows on a floor that has none
    Then Mira says there are none
    And nothing is saved

  @step2
  Scenario: GW-07 The label cannot be set
    Given Home Assistant refuses the entity registry's update
    When the user names an exception
    Then Mira says the exception could not be set
    And the log names the entity and Home Assistant's answer

  @step3
  Scenario: GW-08 Lights left on
    Given a watch "left_on" with 3 hours
    When a fake light is on for 3 hours
    Then an entry names the light
    And when the user hears it and says "mach's aus", the light goes off

  @step3
  Scenario: GW-09 An excluded light never makes or joins an entry
    Given a watch "left_on" and the garden light labelled
    When the garden light and the kitchen light are on for the hours
    Then the entry names the kitchen light only

  @step3
  Scenario: GW-10 Open while nobody is home
    Given a watch "open_while_away" and a fake window open
    When every fake person leaves
    Then after the delay every person gets an entry privately naming the window

  @step3
  Scenario: GW-11 Open at night
    Given a watch "open_at_night" from 23 to 6
    When a fake window is open at 23:10
    Then an entry names the window

  @step3
  Scenario: GW-12 Open too long, once per streak
    Given a watch "open_too_long" with 30 minutes
    When a fake window is open for 30 minutes and a second one opens meanwhile
    Then one entry names the window
    And no second entry comes until both were closed

  @step3
  Scenario: GW-13 Motion while nobody is home
    Given a watch "motion_while_away"
    When fake motion is detected while nobody is home
    Then every person gets an entry privately

  @step3
  Scenario: GW-14 Motion at night
    Given a watch "motion_at_night" on the garden from 23 to 6
    When fake motion in the garden at 2:00
    Then an entry names the garden's sensor

  @step3
  Scenario: GW-15 Too cold
    Given a watch "temperature_out_of_range" on the cellar below 5 °C for 30 minutes
    When a fake cellar temperature is 4 °C for 30 minutes
    Then an entry names the sensor and its value
    And an unavailable sensor makes no entry

  @step3
  Scenario: GW-16 A watch switched off says nothing
    Given a watch "motion_while_away" switched off by the household's holiday script
    When fake motion is detected while nobody is home
    Then no entry comes

  @step4
  Scenario: GW-17 Ist alles zu?
    Given the fake bathroom window is open and every other opening closed
    When the user asks "Ist alles zu?"
    Then Mira says that all is closed but the bathroom window

  @step4
  Scenario: GW-18 The check names what does not report
    Given a fake window is unavailable
    When the user asks "Ist alles zu?"
    Then Mira names it as not readable

  @step4
  Scenario: GW-19 mira.check from a script
    Given a script calls mira.check with a response variable
    And the fake bathroom window is open
    Then the response lists the bathroom window
    And an entry for the receiver names it

  @step4
  Scenario: GW-20 mira.check and nothing open
    Given every opening is closed
    When a script calls mira.check
    Then the response's list is empty
    And no entry comes

  @step4
  Scenario: GW-21 mira.check at an older service
    Given the service has no route /v1/check
    When a script calls mira.check
    Then the action fails with "Mira kann das erst nach einem Update"
