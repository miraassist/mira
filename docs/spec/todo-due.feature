Feature: To-dos with a due date
  docs/spec/todo-due.md

  @step1
  Scenario: TD-01 Background runs get HA's to-do tool under its new name
    Given Home Assistant serves its to-do tool as todo__get_items
    When a background run lists its tools with the default security settings
    Then ha__todo__get_items is among them

  @step2
  Scenario: TD-02 A to-do with a due date goes onto the person's own list
    Given the exposed lists "Anna" and "Einkauf", both able to hold a due date
    When todo.create gets the item "Rechnung Stadtwerke bezahlen" and the due date "2026-11-30" for Anna
    Then Home Assistant's todo.add_item is called once for todo.anna with the item and due_date 2026-11-30
    And the answer names the item, the list and the due date in words

  @step2
  Scenario: TD-03 The own list by the login name
    Given the exposed list "stefan" and the person "Stefan Rinke" with the login stefan
    When todo.create gets an item for Stefan without a list
    Then the item goes onto todo.stefan

  @step2
  Scenario: TD-04 Another person's list is refused
    Given the exposed lists "Anna" and "Ben"
    When todo.create gets the list "Ben" for Anna
    Then nothing is entered
    And the answer names the lists Anna may use, and a log line too

  @step2
  Scenario: TD-05 Without an own list and several lists, Mira asks
    Given the exposed lists "Einkauf" and "Haushalt" and no list named like Ben
    When todo.create gets an item for Ben without a list
    Then nothing is entered
    And the answer asks which, naming "Einkauf" and "Haushalt"

  @step2
  Scenario: TD-06 Fields that do not fit enter nothing
    When todo.create gets, one call each, an empty item, an item of 81 characters, and the due date "2026-13-40"
    Then nothing is entered by any of them
    And the answer names the field and its value, and a log line too

  @step2
  Scenario: TD-07 Home Assistant refuses
    Given Home Assistant answers todo.add_item with an error
    When todo.create gets an item for Anna
    Then the answer says it could not be entered
    And a log line names the list, the item and the error
    And the call is not repeated

  @step2
  Scenario: TD-18 A list that cannot hold a due date is not taken for one
    Given the exposed lists "Anna", unable to hold a due date, and "Haushalt", able to
    When todo.create gets an item with a due date and the list "Anna" for Anna
    Then nothing is entered
    And the answer names "Haushalt" as a list that can hold a due date, and a log line too

  @step2
  Scenario: TD-08 The skill todo finishes through the building block
    Given the skill todo
    Then its finish is the action todo.create

  # TD-09 and TD-10 dropped 2026-11-15 (no tool todo_create; the inbox's to-dos are in inbox.feature, step 10).

  @step4
  Scenario: TD-11 todo_due names overdue and today's to-dos
    Given today is 2026-11-15 and Anna's list holds open to-dos due 2026-11-12, 2026-11-15 and 2026-11-20, and one without a due date
    When todo_due is called for "Anna"
    Then it names the to-do of 2026-11-12 as overdue and the one of 2026-11-15 as today
    And neither the one of 2026-11-20 nor the one without a due date

  @step4
  Scenario: TD-17 A to-do overdue for more than 30 days is not named
    Given today is 2026-11-15 and Anna's list holds open to-dos due 2026-10-15 and 2026-10-16
    When todo_due is called for "Anna"
    Then it names the one of 2026-10-16 as overdue and not the one of 2026-10-15

  @step4
  Scenario: TD-12 Ticked to-dos are not named
    Given Anna's list holds a completed to-do due yesterday
    When todo_due is called for "Anna"
    Then it names nothing due

  @step4
  Scenario: TD-13 At most 20, the rest counted
    Given Anna's list holds 23 overdue to-dos
    When todo_due is called for "Anna"
    Then it names 20 of them and "und 3 weitere"

  @step4
  Scenario: TD-14 A list that cannot be read
    Given Home Assistant answers todo.get_items with an error
    When todo_due is called for "Anna"
    Then the answer says the list could not be read
    And a log line names the list and the error

  @step4
  Scenario: TD-15 todo_due is offered to background runs
    When a background run lists its tools
    Then todo_due is among them

  @step4
  Scenario: TD-16 The personal briefing gets the person's list
    Given the persons Anna, with the exposed list "Anna", and Ben, without one
    When the schedule persoenlich runs
    Then Anna's task names the list "Anna" in place of {{person_list}}
    And Ben's task names "-"
