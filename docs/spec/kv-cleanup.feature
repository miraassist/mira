Feature: Cleaning up the key-value table (docs/spec/kv-cleanup.md)

  @step1
  Scenario: KV-01 Dead keys are deleted
    Given a database with "ui_config:persona", "ui_managed_by_ha", "telegram.offset" and "feed_seen:modelle"
    When the service starts
    Then none of them is in kv
    And every other key is still there

  @step1
  Scenario: KV-02 Every key the code writes is described
    Given the keys and prefixes the code writes into kv
    Then each of them is in the table of kv in docs/database.md

  @step2
  Scenario: KV-03 The schedules' last runs move to their table
    Given a database with "schedule:briefing:morgen" and "schedule:briefing:persoenlich:anna"
    When the service starts
    Then schedule_runs has a row for briefing/morgen of the house and one for briefing/persoenlich of anna
    And no key "schedule:" is left in kv
    And the page shows the same last runs as before

  @step2
  Scenario: KV-04 A mail read stays read
    Given a database whose "inbox_mails" holds two hashes for one IMAP entry
    When the service starts
    And the same two mails come again
    Then neither is read again

  @step2
  Scenario: KV-05 A device said stays said, a gone one is forgotten
    Given a database whose "found.said" holds a device Home Assistant still has and one it no longer has
    When the service starts and the noticing starts
    Then the first device is not said again
    And found_said has no row for the second

  @step2
  Scenario: KV-06 The languages move to their table
    Given a database with "language.area.kuche" = "de" and "language.last" = "en"
    When the service starts
    Then a turn in the kitchen starts in German
    And no key "language." is left in kv

  @step2
  Scenario: KV-07 A value that does not parse stops the migration
    Given a database whose "found.said" is not JSON
    When the service starts
    Then it does not start
    And the log names the migration, the key and the value's start

  @step1
  Scenario: KV-12 The setup's value covers mira.yaml, and the log says so
    Given mira.yaml has ha.url "http://a.example:8123"
    And the setup sent the address "http://b.example:8123"
    When the service starts
    Then it uses "http://b.example:8123"
    And a log line "setup covers mira.yaml" names "ha.url"

  @step1
  Scenario: KV-13 A covered token is named, never its value
    Given mira.yaml has an ha.token and the setup sent a different one
    When the service starts
    Then a log line names "ha.token"
    And neither token is in the log
