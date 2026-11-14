Feature: Remote agents (docs/spec/remote-agents.md)

  @step1
  Scenario: RM-01 A skill with a remote agent
    Given a skill describes an agent with remote url and token from its settings
    When Mira loads the skills
    Then she reads the agent card at /.well-known/agent-card.json
    And the agent has the card's description, or the skill's when it gives one

  @step1
  Scenario: RM-02 A card without push and without streaming
    Given the agent card offers neither push notifications nor streaming
    When Mira loads the skills
    Then the skill has a problem naming the agent
    And the skill stays, without that agent

  @step1
  Scenario: RM-03 A remote agent with a model field
    Given a skill's remote agent also sets a model
    When Mira loads the skills
    Then the skill has a problem and is left out

  @step1
  Scenario: RM-18 A remote agent that asks for OAuth2
    Given the agent card asks for OAuth2
    And the skill has an oauth section and its connection is made
    When Mira sends work to the remote agent
    Then the request carries the connection's token

  @step1
  Scenario: RM-19 OAuth2 without a connection in the skill
    Given the agent card asks for OAuth2
    And the skill has no oauth section
    When Mira loads the skills
    Then the skill has a problem naming the scheme

  @step1
  Scenario: RM-20 Only push, and no address for remote agents
    Given the agent card offers push but no streaming
    And server.url is not set
    When Mira loads the skills
    Then the skill has a problem saying Mira's address for remote agents is missing

  @step2
  Scenario: RM-21 The stream first
    Given the agent card offers push and streaming
    And server.url is set
    When the field is due
    Then Mira reads the stream and sends no push configuration

  @step1
  Scenario: RM-04 The card cannot be read
    Given the remote agent is down when Mira starts
    Then the log names the URL and the error
    And Mira reads the card again later and the agent becomes ready

  @step2
  Scenario: RM-05 A field filled by a remote agent, by push
    Given a skill's target names a remote agent for a field
    And the remote agent offers push notifications but no stream
    And server.url is set
    When the field is due
    Then Mira sends one message with a push configuration and a token of the call
    And when the remote agent pushes the completed task, the field holds the artifacts' text

  @step2
  Scenario: RM-06 A field filled by a stream
    Given the remote agent offers streaming but no push
    When the field is due
    Then Mira reads the stream until the task is completed
    And the field holds the artifacts' text

  @step2
  Scenario: RM-07 The remote agent fails
    When the remote agent reports the task failed with a status text
    Then the log names the agent, the task and the status text
    And the thread asks its owner for that field

  @step2
  Scenario: RM-08 A push with a wrong token
    When a push comes for a call with a wrong token
    Then Mira answers 401
    And the log names the call and the sender
    And the thread does not change

  @step2
  Scenario: RM-09 A push twice
    When the same completed task is pushed twice
    Then the field is filled once

  @step2
  Scenario: RM-10 The thread is cancelled
    Given a remote agent works for a thread
    When the owner cancels the thread
    Then Mira sends CancelTask for the task

  @step2
  Scenario: RM-11 Login asked during the work
    When the remote agent reports auth required
    Then the call ends as failed, naming the remote agent's status text
    And the thread asks its owner for that field

  @step3
  Scenario: RM-12 An agent request
    When the remote agent reports input required with "Wie groß ist die Wohnfläche?"
    Then the thread's owner gets a message of the kind agent_request with the thread
    And its entry names the agent by its card's name, "Hermes fragt: Wie groß ist die Wohnfläche?"
    And when the owner answers "140 Quadratmeter", Mira sends it with the same task id
    And the remote agent goes on working

  @step3
  Scenario: RM-13 The answer cannot be sent
    Given an agent request waits
    And the remote agent refuses the answer
    When the owner answers
    Then Mira says she could not pass the answer on
    And the message stays and the log names the agent and the refusal

  @step3
  Scenario: RM-22 agent_answer at a satellite waits for the remote agent
    Given a remote agent asked back in a thread started at a satellite
    When the owner answers at the satellite and the answer is passed on with agent_answer
    Then the turn waits up to 10 seconds for the remote agent
    And its result, with the finish's answer, is said in the same answer and comes as no message
    And a second question that comes in that time is said in the same answer, the satellite listens again
    And that second question is answered only in the next turn

  @step4
  Scenario: RM-14 A restart during the work
    Given a remote agent works for a thread
    And the Mira service restarts while the remote agent completes the task
    When the service is up again
    Then Mira asks the task's state once
    And the field holds the artifacts' text

  @step5
  Scenario: RM-15 mira.agent starts the skill's thread
    When a script calls mira.agent with skill "recherche", an input and a person as receiver
    Then the skill's thread starts for that person with the input as the request
    And the response holds the thread's id

  @step5
  Scenario: RM-16 mira.agent with a skill without a target
    When a script calls mira.agent with a skill that has no target
    Then the action fails naming the reason

  @step5
  Scenario: RM-17 mira.agent with skill at an older service
    Given the service does not know the field skill
    When a script calls mira.agent with skill
    Then the action fails with "Mira kann das erst nach einem Update"
