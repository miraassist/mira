# Testable rules of docs/spec/backup.md. Each scenario has a fixed number (BK-nn) that is never reused; its test
# carries the number in its name (TestBK01_… in Go, test_BK10_… in pytest). The tags @stepN say in which step of the
# spec ("Steps") the scenario gets built. Steps 2 and 3 are the integration's, tested in pytest.
#
# Groups: BK-0x the service · BK-1x into Home Assistant's backup · BK-2x restore

Feature: BK Backup and restore

  # --- The service ------------------------------------------------------------------------------------------

  @step1
  Scenario: BK-01 The snapshot holds the database, the settings and one's own skills
    Given Mira with facts in her memory, a mira.yaml, a secrets file and a skill of one's own
    When the integration asks for GET /v1/backup
    Then the archive has mira.db, mira.yaml, secrets, the skill's folder and a manifest
    And Mira's bundled skills are not in it

  @step1
  Scenario: BK-02 The snapshot is consistent while Mira writes
    Given Mira writes messages while the snapshot is made
    Then the archive's database opens and passes SQLite's integrity check

  @step1
  Scenario: BK-03 A restore replaces the data and keeps the pairing
    Given a snapshot with other facts and an older pairing
    When it is sent with POST /v1/restore
    Then Mira has the snapshot's facts after her restart
    And the integration still reaches her with its token

  @step1
  Scenario: BK-04 A snapshot of a newer Mira is refused
    Given a snapshot whose highest migration this Mira does not know
    When it is sent with POST /v1/restore
    Then it is refused with the reason, and nothing is changed

  @step1
  Scenario: BK-05 A broken archive changes nothing
    Given an archive without a manifest, or with a database that does not open
    When it is sent with POST /v1/restore
    Then it is refused with the reason, and nothing is changed

  @step1
  Scenario: BK-06 The secrets key is never in the snapshot
    Given MIRA_SECRET_KEY is set
    When the snapshot is made
    Then the archive has the encrypted secrets file and nowhere the key

  # --- Into Home Assistant's backup -------------------------------------------------------------------------

  @step2
  Scenario: BK-10 Before a backup the snapshot is laid into the config folder
    When Home Assistant starts a backup
    Then .storage/mira_backup.tar.gz holds Mira's snapshot before the backup is written
    And after the backup it is gone

  @step2
  Scenario: BK-11 Mira not reachable does not stop the backup
    Given Mira does not answer
    When Home Assistant makes a backup
    Then the backup is made without Mira
    And a repair says that Mira was not in the last backup, and why

  @step2
  Scenario: BK-12 An older Mira does not stop the backup
    Given Mira answers GET /v1/backup with 404
    When Home Assistant makes a backup
    Then the backup is made without Mira, and the repair says to update Mira

  @step2
  Scenario: BK-13 The repair goes with the next backup that has Mira
    Given the repair "Mira was not in the last backup"
    When the next backup has Mira
    Then the repair is gone

  # --- Restore ----------------------------------------------------------------------------------------------

  @step3
  Scenario: BK-20 After a restore of Home Assistant the repair offers Mira's data
    Given .storage/mira_backup.tar.gz is there when the integration starts
    When the integration is connected to Mira
    Then a repair "Restore Mira's data" names the snapshot's date and Mira's version

  @step3
  Scenario: BK-21 Yes restores Mira's data
    When the user confirms the repair
    Then the archive goes to POST /v1/restore, and the file is deleted

  @step3
  Scenario: BK-22 Ignore keeps Mira as she is
    When the user ignores the repair
    Then nothing is sent, and the file is deleted

  @step3
  Scenario: BK-23 A refused restore says why
    Given Mira refuses the archive (a newer Mira's, broken)
    When the user confirms the repair
    Then the repair's flow shows Mira's reason, and the file stays for another try
