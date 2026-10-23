# Scenarios of docs/spec/skill-mcp.md. Every scenario has a number (SM-01, …); the Go test that checks it carries the
# number in its name (TestSM01_…). @step1 is the server in the skill, @step2 its agents.

Feature: SM MCP servers in skills

  @step1
  Scenario: SM-01 A skill's server is connected with the skill's settings
    Given a skill with an mcp section whose url and token come from its settings, the token secret
    And mira.yaml sets them under skills.enabled
    When Mira starts
    Then she connects to the server with that url and token
    And the token is in no prompt

  @step1
  Scenario: SM-02 A skill's tools are in no conversation before the skill is opened
    Given a skill with an mcp section
    When a conversation starts
    Then none of the server's tools is offered to the model

  @step1
  Scenario: SM-03 Opening the skill brings its tools for the rest of the conversation
    Given a conversation in which the model opens the skill
    When the next turns of the same conversation run
    Then the skill's tools are offered in each of them

  @step1
  Scenario: SM-04 Only the tools the skill names
    Given a skill whose server has 44 tools and whose tools list names 2
    When the skill is opened
    Then exactly those 2 are offered

  @step1
  Scenario: SM-05 Another conversation does not get them
    Given a conversation in the kitchen that opened the skill
    When someone talks in the living room
    Then the skill's tools are not offered there

  @step1
  Scenario: SM-06 After the session expired the tools are gone
    Given a conversation that opened the skill
    When its session expires and a new one starts
    Then the skill's tools are not offered

  @step1
  Scenario: SM-07 What the server returns is third-party text
    Given a skill whose mcp section has web_content true
    When the model calls one of its tools
    Then the turn counts as having read third-party text

  @step1
  Scenario: SM-08 The skill's schedules get its tools
    Given a skill with an mcp section and a schedule
    When the schedule runs
    Then the skill's tools are offered to skill.task

  @step1
  Scenario: SM-09 A server that cannot be reached does not stop Mira
    Given a skill whose server cannot be reached
    When Mira starts
    Then she starts, the skill is there without its tools, and the problem is shown

  @step1
  Scenario: SM-10 A tool the server does not have is a problem
    Given a skill whose tools list names a tool the server does not have
    When Mira connects
    Then the problem names the tool, and the other tools are offered

  @step1
  Scenario: SM-11 A broken mcp section is a problem of the skill
    Given a skill whose mcp section has no url
    When Mira loads it
    Then the skill does not start, and the problem says what is missing

  @step2
  Scenario: SM-12 A skill's agent works with the skill's tools
    Given a skill with an mcp section and an agent whose tools list names two of its tools
    When the agent runs
    Then those tools are offered to it and it can call them
    And an agent of mira.yaml naming the same tools does not get them

  @step2
  Scenario: SM-13 With conversation false only the skill's agents and schedules get the tools
    Given a skill whose mcp section has conversation false
    When a conversation opens the skill
    Then the server's tools are not offered to the conversation, nor can it call them
    But the skill's agent and its schedule get them
