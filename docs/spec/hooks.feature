# Testable rules of docs/spec/hooks.md. Each scenario has a fixed number (HK-nn) that is never reused; its Go
# test carries the number in its name (TestHK07_…). The tags @step1 and @step2 say in which step of hooks.md
# ("Steps") the scenario gets built.
#
# Groups: HK-0x order and combining · HK-1x built-in hooks (the guard before) · HK-2x the call ·
#         HK-5x errors and limits · HK-6x files and settings · HK-7x what a task may not switch ·
#         HK-8x skill actions

Feature: HK Hooks

  # --- Order and combining (hooks.md: Order, Verdicts) --------------------------------------------------

  @step1
  Scenario: HK-01 The hooks run household, then default, then system
    Given a household hook that logs its name
    When the model calls a tool
    Then the household hook runs before default/critical.star and before the system hooks
    And the system hooks run in the order rights, background, web, ask

  @step1
  Scenario: HK-02 Within a level the files run in the order of their names
    Given the household hooks "20_b.star" and "10_a.star" that each log their name
    When the model calls a tool
    Then "10_a.star" logs before "20_b.star"

  @step1
  Scenario: HK-03 The strictest verdict wins
    Given a household hook "10_a.star" that returns confirm for every call
    And a household hook "20_b.star" that returns allow for every call
    When the model calls HassTurnOn for a light in a voice turn
    Then the call is parked for the user's yes, by "household/10_a.star"

  @step1
  Scenario: HK-04 A deny ends the chain
    Given a household hook "10_a.star" that returns deny("nein")
    And a household hook "20_b.star" that logs that it was called
    When the model calls a tool
    Then the call is not run
    And the model reads "nein"
    And "20_b.star" was not called

  @step1
  Scenario: HK-05 Of two questions the first is asked
    Given a household hook "10_a.star" that returns confirm("erste")
    And a household hook "20_b.star" that returns confirm("zweite")
    When the model calls HassTurnOn for a light in a voice turn
    Then the question the model reads names "erste" and not "zweite"

  @step1
  Scenario: HK-07 The confirmed call runs once
    Given a call of HassTurnOn for the lock that was parked for a yes in the previous turn
    When the user says "Ja, bitte"
    And the model calls the same tool with the same arguments
    Then the call runs
    And a second identical call in the same turn is parked again

  @step1
  Scenario: HK-08 A deny stays a deny even for the confirmed call
    Given a call of HassTurnOn for the lock that was parked for a yes in the previous turn
    And this turn read web content
    When the user says "ja"
    And the model calls the same tool with the same arguments
    Then the call is not run

  # --- Built-in hooks: the guard before (hooks.md: The built-in hooks) ------------------------------------

  @step1
  Scenario: HK-11 A lock needs a yes
    When the model calls HassTurnOn for an entity of the domain lock in a voice turn
    Then the call is parked for the user's yes, by "default/critical.star"

  @step1
  Scenario: HK-12 The label "Mira: nachfragen" needs a yes
    Given the entity switch.pumpe has the label "Mira: nachfragen"
    When the model calls HassTurnOn for it
    Then the call is parked for the user's yes

  @step1
  Scenario: HK-13 After web content nothing critical runs
    Given this turn read a web page
    When the model calls HassTurnOn for the lock
    Then the call is not run, by "system/web.star"
    And a light can still be switched

  @step1
  Scenario: HK-14 After foreign text, keeping something asks first
    Given the turn has a plan made from a web page in its context
    When the model calls memory_save
    Then the call is parked for the user's yes, by "system/web.star"
    And the lock asks as always, by "default/critical.star"

  @step1
  Scenario: HK-15 In the background nothing changes the house
    When a background job calls HassTurnOn or memory_save
    Then the call is not run, by "system/background.star"
    And GetLiveContext runs

  @step1
  Scenario: HK-16 Nobody to ask means no
    Given an agent's run in the background
    When it calls a tool that needs a yes
    Then the call is not run, by "system/ask.star"

  @step1
  Scenario: HK-17 An agent acts only with its rights
    Given an agent's run without the right "act_on_house"
    When it calls a tool that needs the right "act_on_house"
    Then the call is not run, by "system/rights.star"
    And the model reads that this run lacks the right act_on_house

  @step1
  Scenario: HK-18 A household hook replaces a default hook by its file name
    Given a household "critical.star" that never asks
    When the model calls HassTurnOn for the lock in a voice turn
    Then the call runs

  @step1
  Scenario: HK-19 No household hook loosens a system hook
    Given a household hook "web.star" that returns allow for every call
    And this turn read a web page
    When the model calls HassTurnOn for the lock
    Then the call is not run, by "system/web.star"

  # --- The call (hooks.md: What a hook sees) --------------------------------------------------------------

  @step1
  Scenario: HK-20 A name that resolves to nothing is judged by the names it resembles
    Given the lock "Haustür"
    When the model calls HassTurnOn with name "Haustürschloss"
    Then the hooks see lock.haustuer among the candidates
    And the call is parked for the user's yes

  @step1
  Scenario: HK-21 A floor, a device class or only a domain makes every fitting entity a candidate
    When the model calls HassTurnOn with floor "EG", or only the domain "cover", or the device class "outlet" of the domain "switch"
    Then the call is parked for the user's yes
    And with floor "EG" and the domain "light" it runs

  @step1
  Scenario: HK-22 The hooks know who speaks and how
    Given a voice turn whose speaker was recognized as Anna by her voice
    When the model calls a tool
    Then the hooks see who.person "anna", who.name "Anna" and who.by "voice"

  # --- Errors and limits (hooks.md: Errors and limits) ----------------------------------------------------

  @step1
  Scenario: HK-50 A hook that fails counts as deny
    Given a household hook that raises an error
    When the model calls a tool
    Then the call is not run
    And the log has "hook failed"

  @step1
  Scenario: HK-51 A hook that runs too long counts as failed
    Given a household hook with an endless loop
    When the model calls a tool
    Then the hook is stopped at the step limit
    And the call is not run

  @step1
  Scenario: HK-52 A hook that returns no verdict counts as failed
    Given a household hook that returns "ja"
    When the model calls a tool
    Then the call is not run

  # --- Files and settings (hooks.md: Errors and limits, What happened to the guard's settings) --------------

  @step1
  Scenario: HK-60 A household file that does not load is reported and left out
    Given a household hook file with a syntax error and one without DESCRIPTION
    When Mira loads the hooks
    Then both are reported as problems
    And the other hooks run

  @step1
  Scenario: HK-61 A household critical.star needs DOMAINS and critical()
    Given a household "critical.star" with pre_tool but without DOMAINS
    When Mira loads the hooks
    Then it is reported as a problem
    And the default critical.star stays in place

  @step1
  Scenario: HK-62 The guard's old settings are no longer read
    Given a mira.yaml with security.confirm_domains
    When Mira reads it
    Then it loads
    And confirm_domains is named as no longer read

  # --- What a task may not switch (hooks.md: critical.star also says what a task may not switch) --------------

  @step1
  Scenario: HK-70 critical.star says what a task may not switch
    Given the default critical.star and the entity switch.pumpe with the label "Mira: nachfragen"
    Then a task may not switch the domains lock and alarm_control_panel
    And not switch.pumpe

  @step1
  Scenario: HK-71 A household critical.star changes what a task may not switch
    Given a household "critical.star" whose critical() also names cover.garage
    Then a task may not switch cover.garage

  # --- Skill actions (hooks.md: Skill actions) -------------------------------------------------------------

  @step2
  Scenario: HK-80 A skill's action that needs a yes is proposed first
    Given a skill whose finish is a Home Assistant action, without confirm
    And the hooks ask for a yes for that action
    When its thread has every field
    Then the action is not called but proposed
    And after the yes, which the hooks are asked about again, it is called

  @step2
  Scenario: HK-81 A skill's action the hooks refuse is not called
    Given the hooks refuse a skill's action
    When its thread has every field
    Then the thread ends with the hook's reason and nothing is called

  @step2
  Scenario: HK-82 The yes to a skill's action passes the hooks again
    Given a skill's action proposed for a yes
    And the hooks refuse it now
    When the user says yes
    Then it is not called

  @step2
  Scenario: HK-83 Taking back what the hooks would ask for is not done
    Given a skill's action that was called and has an undo
    And the hooks would ask for a yes for the undo
    When the user takes it back
    Then the undo is not called and Mira says why

  @step2
  Scenario: HK-84 A skill's action is a call of its own for the hooks
    When a skill's action lock.unlock for lock.haustuer is checked
    Then the hooks see a call named skill_action, and default/critical.star asks for a yes
    And without an entity the action's domain counts
    And in a turn that read web content system/web.star refuses it
    And an alarm on a satellite runs
