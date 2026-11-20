# Scenarios of docs/spec/scheduled-jobs.md. Every scenario has a number (SJ-01, …); the Go test that checks it carries
# the number in its name (TestSJ01_…). @step1 is the service, @step2 the page, @step3 the shipped skills.

Feature: SJ Scheduled jobs

  @step1
  Scenario: SJ-01 A skill's first job runs while mira.yaml has no jobs
    Given a skill with the job template liste and the first job morgens at 7:00 with themen "A"
    And no jobs under the skill in mira.yaml
    When the skill loads
    Then it has the schedule morgens at 7:00 from the template liste, its task with themen "A"

  @step1
  Scenario: SJ-02 The jobs in mira.yaml are the ones that run
    Given the skill with jobs in mira.yaml: morgens at 7:00 with themen "A", abends at 18:00 with themen "B", switched off
    When the skill loads
    Then it has the schedules morgens and abends with their own times, topics and switch, and not the first job
    And with jobs: {} it has no schedule

  @step1
  Scenario: SJ-03 A job's parameters fill its task and its recipient
    Given a template whose task names {{themen}} and a setting, and whose to is {{empfaenger}}
    When a job has themen and an empty empfaenger
    Then the task has the topics and the setting's value, and the job is for the house

  @step1
  Scenario: SJ-04 A job that is wrong does not run, the skill does
    Given a job naming a template the skill does not have, and a job without its required parameter
    When the skill loads
    Then the skill starts with its other jobs, the two do not run, and each has a problem naming the job

  @step1
  Scenario: SJ-05 A first job without a required value is not made
    Given a template whose first job has no value for a required parameter
    When the skill loads without jobs in mira.yaml
    Then the skill has no schedule from it, and no problem

  @step1
  Scenario: SJ-06 {{since}} is the job's last run
    Given a job whose task says {{since}}, with a last run at 07:00 yesterday
    When it runs
    Then its task says that time, and at its first run the template's window before now

  @step1
  Scenario: SJ-07 schedules in a SKILL.md still work
    Given a skill of before with schedules morgen and abend in its SKILL.md
    When the skill loads
    Then it has the jobs morgen and abend with their times and tasks, as before
    And a SKILL.md with both schedules and jobs is a problem of the skill

  @step1
  Scenario: SJ-08 schedules in mira.yaml change the job of that name
    Given a skill of before and in mira.yaml schedules: abend: {cron: "0 19 * * *", enabled: false}
    When the skill loads
    Then the job abend runs at 19:00 and is switched off
    And a name the skill does not have is a problem of the skill, as before

  @step2
  Scenario: SJ-10 The skill's entry shows its templates and jobs
    Given a switched-on skill with a template and two jobs
    When the page lists the skills
    Then the skill's entry has the template with its parameters and the jobs with name, template, time, switch,
      parameters and problem

  @step2
  Scenario: SJ-11 Saving and deleting a job writes mira.yaml
    Given a skill with jobs
    When an admin saves a new job with a template, a time and parameters, and deletes another
    Then mira.yaml has the new job under jobs with its time and parameters, and not the deleted one
    And deleting the last job leaves jobs: {}

  @step2
  Scenario: SJ-12 Writing the skill's entry moves schedules to jobs
    Given a skill of before with schedules: abend: {cron: "0 19 * * *"} in mira.yaml
    When the page writes the skill's entry
    Then mira.yaml has jobs with morgen and abend, abend at 19:00, and no schedules, and the log says so

  @step2
  Scenario: SJ-13 Switching a skill on writes its first jobs
    Given a skill that is off, with a first job that has its values and one that lacks a required value
    When an admin switches it on on the page
    Then mira.yaml has the first job that has its values under jobs, and not the other

  @step2
  Scenario: SJ-14 The page Zeitplan changes a job's time in jobs
    Given a skill with jobs in mira.yaml
    When an admin changes a job's time on the page Zeitplan
    Then mira.yaml has the time under jobs.<name>.cron and the job runs at it at once

  @step3
  Scenario: SJ-20 The shipped skills bring job templates
    Given the shipped skills abfall, briefing, warnungen and youtube
    When they load without jobs in mira.yaml
    Then each has its job templates and first jobs, with the times of before, and none has schedules
    And briefing's settings kalender and themen are still settings

  @step3
  Scenario: SJ-21 youtube's list searches since the job's last run
    Given the job template liste of youtube
    When its task is read
    Then it tells to search since {{since}}, and youtube__suche takes seit
