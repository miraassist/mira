# Testable rules of docs/spec/hooks-schreiben.md. Each scenario has a fixed number (HW-nn) that is never reused; its
# Go test carries the number in its name (TestHW07_…). The tags @step1 … @step3 say in which step of the spec
# ("Steps") the scenario gets built.
#
# Groups: HW-0x ACTIVE and reloading · HW-1x who may · HW-2x the tools · HW-3x writing a rule

Feature: HW Writing hooks on request

  # --- ACTIVE and reloading ------------------------------------------------------------------------------

  @step1
  Scenario: HW-01 A hook with ACTIVE = False is loaded but not called
    Given a household hook with ACTIVE = False that denies every call
    When the model calls a tool
    Then the call runs
    And the page shows the hook as off

  @step1
  Scenario: HW-02 A hook without ACTIVE is active
    Given a household hook without the line ACTIVE that denies every call
    When the model calls a tool
    Then the call is not run

  @step1
  Scenario: HW-03 A changed file applies without a restart
    Given Mira is running with no household hooks
    When a hook file that denies every call is written into hooks/
    Then after a moment the next tool call is not run
    And when the file is deleted, the next one runs again

  @step1
  Scenario: HW-04 A broken file while running is reported, the others apply
    Given a household hook that logs its name
    When a second file with a syntax error is written into hooks/
    Then it is reported as a problem
    And the first hook still runs

  # --- Who may (spec: Who may) ------------------------------------------------------------------------------

  @step2
  Scenario: HW-11 Only a known person may change rules
    Given an unknown voice at a satellite
    When its turn calls hook_switch
    Then the call is not run, by "system/self.star"
    And for Ben, known by his voice, the call is parked for his yes

  @step2
  Scenario: HW-12 Changing a rule always asks
    Given Anna, known by her voice, in a voice turn
    When the model calls hook_delete
    Then the call is parked for her yes

  @step2
  Scenario: HW-13 No rule changes after web content
    Given Anna, known, in a turn that read a web page
    When the model calls hook_switch
    Then the call is not run, by "system/self.star"

  @step2
  Scenario: HW-14 A system hook and critical.star are never written, switched or deleted
    When a known person's turn calls hook_delete for "web.star" or hook_switch for "critical.star"
    Then the call is not run

  # --- The tools (spec: The skill regel) ---------------------------------------------------------------------

  @step2
  Scenario: HW-20 hook_list names the rules
    Given a household rule written by Mira for Anna, and one that is off
    When the model calls hook_list
    Then it reads both, with description, on or off, and who asked

  @step2
  Scenario: HW-21 hook_switch changes only the line ACTIVE
    Given a household rule that is on
    When a known person says yes to hook_switch(name, off)
    Then the file has ACTIVE = False and is otherwise unchanged

  # --- Writing a rule (spec: The agent, Checking, Saving) ------------------------------------------------------

  @step3
  Scenario: HW-30 A rule from a wish
    Given Anna, known by her voice, at a satellite
    When she says "Merk dir eine Regel: Wenn du nicht weißt, wer spricht, schließt du nichts auf."
    Then Mira proposes the rule in one sentence and asks whether to switch it on
    And after "Ja" the file is in hooks/ with Anna's words in its head and ACTIVE = True
    And Mira says that the rule is active and not tested

  @step3
  Scenario: HW-31 A rule that does not load is not proposed
    Given the agent returns code with a syntax error, or says it cannot write the rule
    Then the check refuses it, nothing is proposed and no file is written
    And Mira asks the user how they mean the rule

  @step3
  Scenario: HW-32 The agent never writes a system hook's file or critical.star
    Given the agent returns the file name "web.star" or "critical.star"
    Then the check refuses it
    And a name that is taken gets a number
