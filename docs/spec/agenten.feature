# Testable rules of docs/spec/agenten.md. Each scenario has a fixed number (AG-nn) that is never reused; its Go
# test carries the number in its name (TestAG07_…). The tags @step1 … @step8 say in which step of the switch-over
# (agenten.md, "Switching over in steps") the scenario gets built.
#
# Groups: AG-0x rights · AG-1x depth · AG-2x synchronous · AG-3x asynchronous and run record ·
#         AG-4x delivery · AG-5x log · AG-6x calls from Home Assistant and as a tool · AG-7x model ·
#         AG-8x/9x runtime · AG-Ax YAML agents · AG-Bx what a turn belongs to

Feature: AG Agents

  # --- Rights (agenten.md: Rights) -------------------------------------------------------------------

  @step1
  Scenario: AG-01 Effective is what both call context and agent allow
    Given an agent whose description names the rights "read the house" and "web"
    And a call context with the rights "read the house" and "act on the house"
    When the agent runs with this call context
    Then its effective rights are only "read the house"

  @step1
  Scenario: AG-02 Without the right "act on the house" an acting action is refused
    Given a run without the effective right "act on the house"
    When the model calls an action that changes something in Home Assistant
    Then the action is not run
    And the model gets as the result that this run may not act on the house

  @step1
  Scenario: AG-03 A sub-agent gets at most its caller's effective rights
    Given a run with the effective rights "read the house" and "web"
    And a second agent whose description names all rights
    When the run calls the second agent
    Then that one's call context holds at most "read the house" and "web"

  @step1
  Scenario: AG-04 Who may not act on the house cannot do so through another agent either
    Given a run without the effective right "act on the house"
    And a second agent that can call actions in Home Assistant
    When the run calls the second agent and that one's model calls an acting action
    Then the action is not run

  @step1
  Scenario: AG-05 Rights do not replace the guard
    Given a run with the effective right "act on the house"
    When the model calls a critical action (such as opening a lock)
    Then the guard does not just run it: in a conversation it asks first, as before; in the background, where nobody
      can confirm, it refuses

  @step1
  Scenario: AG-06 A sub-agent's third-party text counts for the caller
    Given a run without any third-party text involved yet
    When it calls a sub-agent that reads a web page
    And the run then wants to keep or pass something on (remember, message)
    Then it counts as after web content in its own run: in a conversation Mira asks first; in the background it is
      refused

  @step1
  Scenario: AG-07 After web content critical actions stay locked
    Given a run that has read web content
    When the model then calls a critical action
    Then it is not run, not even with a yes

  @step3
  Scenario: AG-08 In the background the rights decide what may change
    Given a background run with the effective right "act on the house"
    When the model calls an action that changes something and is not critical
    Then it runs; the background channel's rule "only read-only tools" does not apply to agents

  # --- Depth (agenten.md: Depth) ---------------------------------------------------------------------

  @step1
  Scenario: AG-10 An outside call has depth 0, each sub-agent one more
    When Home Assistant calls an agent
    Then its call context has depth 0
    And an agent it calls has depth 1

  @step1
  Scenario: AG-11 A call beyond the allowed depth fails
    Given the setting agents.max_depth is not set
    And a run with depth 3
    When it calls another agent
    Then the call fails
    And the reason is that the allowed depth is reached

  @step1
  Scenario: AG-12 The allowed depth is a setting
    Given agents.max_depth is 1
    And a run with depth 1
    When it calls another agent
    Then the call fails

  # --- Synchronous (agenten.md: Synchronous and asynchronous) ----------------------------------------

  @step1
  Scenario: AG-20 Synchronously the caller gets the answer
    When an agent is called synchronously with a timeout
    And it is done before the timeout
    Then the caller gets its answer

  @step1
  Scenario: AG-21 Timeout, and the caller wants to cancel
    Given a synchronous call whose caller wants to cancel at the timeout
    When the agent is not done by the timeout
    Then the run is cancelled
    And the caller gets that the time is up

  @step6
  Scenario: AG-22 Timeout, and the caller lets it run on
    Given a synchronous call whose caller wants it to run on at the timeout
    When the agent is not done by the timeout
    Then the caller gets the call ID
    And the run goes on and is delivered like an asynchronous one

  @step1
  Scenario: AG-23 An output that does not follow the schema is no result
    Given an agent with a schema for its output
    When the model gives an answer that does not follow the schema
    Then the run counts as failed

  @step1
  Scenario: AG-24 A hanging model call is retried following the limits
    Given an agent with the limits "60 s per attempt, retry once"
    When the first model call has no answer after 60 s
    Then it is retried once
    And the run goes on with the answer of the second attempt

  # --- Asynchronous and run record (agenten.md: The run record) --------------------------------------

  @step6
  Scenario: AG-30 Asynchronously the call ID comes at once
    When an agent is called asynchronously
    Then the caller gets a call ID at once
    And the run record has the status "running"

  @step6
  Scenario: AG-31 A finished run holds its output
    Given an asynchronous run of an agent with a schema for its output
    When the run is done
    Then the run record has the status "done"
    And it holds the output following the schema

  @step6
  Scenario: AG-32 A failed run holds its error
    Given an asynchronous run
    When the run fails
    Then the run record has the status "failed"
    And it holds the error

  @step6
  Scenario: AG-33 A run beyond its time limits has expired
    Given an asynchronous run
    When it exceeds its time limits
    Then the run record has the status "expired"

  @step6
  Scenario: AG-34 The run record knows its caller
    When a run calls another agent asynchronously
    Then that one's run record holds the call ID of the calling run

  @step6
  Scenario: AG-35 A run can be queried by its call ID
    Given an asynchronous run with its call ID
    When the caller asks with the call ID
    Then it gets the run record with the status and, if done, the output

  @step6
  Scenario: AG-36 60 minutes after its end a run record is gone
    Given a run that was done 61 minutes ago
    When someone asks with its call ID
    Then the run no longer exists

  # --- Delivery (agenten.md: Delivery) ---------------------------------------------------------------

  @step6
  Scenario: AG-40 Delivery as a Home Assistant event
    Given an asynchronous call with the delivery route "HA event"
    When the run is done
    Then Home Assistant fires the event mira_agent_done
    And it holds call ID, agent, status and output

  @step6
  Scenario: AG-41 Delivery as a message
    Given an asynchronous call with the delivery route "message to a person"
    When the run is done
    Then the person gets a message that the conversation worded from the result

  # --- Continuation: agents fill a thread (agenten.md: Continuation) --------------------------------

  @step7
  Scenario: AG-42 An agent's result fills a field of the thread
    Given a thread whose target has a field an agent fills
    When the agent is done
    Then its result is in the field, checked as if the user had given it
    And the log says which agent filled it

  @step7
  Scenario: AG-43 An agent starts once its input is there
    Given a field whose agent takes another field as input
    When the user gives that other field
    Then the agent is started with it, in the thread's call context

  @step7
  Scenario: AG-44 Several agents run at once, the last result finishes
    Given a thread with two fields filled by agents and the other fields given
    When both agents run
    Then they run at the same time
    And when the second result is there, the finish runs and its answer comes as a message of the thread

  @step7
  Scenario: AG-45 A failed agent makes the thread ask the user
    Given a field an agent fills
    When the agent fails
    Then the log says so
    And the thread asks the user for that field, as a message of the thread

  @step7
  Scenario: AG-46 Ending the thread cancels its agents
    Given a thread with an agent still running
    When the user cancels the thread
    Then the agent's run is cancelled

  @step7
  Scenario: AG-47 An agent writes a plan
    Given a plan target that names an agent
    When the thread starts and the agent is done
    Then its result is the thread's plan, marked foreign if the agent read third-party text
    And the user gets a message that the plan is there

  @step7
  Scenario: AG-48 While an agent works the model does not ask for its field
    Given a field an agent is filling
    When the model sees the thread
    Then the field shows that an agent fills it

  @step7
  Scenario: AG-49 An unknown agent in a target is a problem of the skill
    Given a skill whose target names an agent that does not exist
    When Mira starts
    Then the skill is left out, and the problem names the agent

  # --- Log (agenten.md: Log) -------------------------------------------------------------------------

  @step1
  Scenario: AG-50 Every run writes a line "agent done"
    When a run ends, however it ends
    Then the log has a line "agent done"
    And it names agent, call ID, depth, origin, mode, status, model, duration, model calls and tokens

  @step1
  Scenario: AG-51 Sub-agents can be matched to their caller
    When a run calls another agent
    Then the called run's line "agent done" names the calling run's call ID and its depth

  # --- Calls from Home Assistant and as a tool (agenten.md: Calling) ---------------------------------

  @step4
  Scenario: AG-60 An agent in the tool list is a tool
    Given an agent with a second agent in its tool list
    When its model is called
    Then it sees the second agent as a tool, with name, description and input schema

  @step4
  Scenario: AG-61 An agent not in the tool list is no tool
    Given an agent without a second agent in its tool list
    When its model is called
    Then it does not see the second agent

  @step6
  Scenario: AG-62 From Home Assistant synchronously with an answer
    When an automation calls the action mira.agent synchronously with an agent and its input
    Then the automation gets the agent's answer in its response_variable

  @step6
  Scenario: AG-63 From Home Assistant asynchronously
    When an automation calls the action mira.agent asynchronously
    Then it gets the call ID
    And the result comes as the event mira_agent_done

  @step6
  Scenario: AG-64 Query a run from Home Assistant
    Given a call ID of a run
    When an automation calls the action mira.agent_result with this call ID
    Then it gets the run record

  @step6
  Scenario: AG-65 All agents can be called from Home Assistant
    Given any agent with a fixed description, internal or from YAML
    When an automation calls mira.agent with its name and a fitting input
    Then the agent runs

  @step6
  Scenario: AG-66 An unknown agent
    When an automation calls mira.agent with a name that does not exist
    Then the call fails with that reason

  # --- Model (agenten.md: Model choice) --------------------------------------------------------------

  @step1
  Scenario: AG-70 Without an entry for its role an agent takes main
    Given routing has nothing for an agent's role
    When the agent runs
    Then it takes the model main

  @step1
  Scenario: AG-71 With an entry an agent takes the model entered
    Given routing has the model smart for an agent's role
    When the agent runs
    Then it takes the model smart

  @step4
  Scenario: AG-72 The delegate agent exists without an entry too
    Given routing has nothing for delegate
    When the conversation is called
    Then it has the tool delegate
    And the delegate agent takes the model main

  # --- Runtime (agenten.md: Runtime) -----------------------------------------------------------------

  @step1
  Scenario: AG-80 A local model serves one run at a time
    Given two runs that use the same local model
    When both want to call the model at the same moment
    Then the second waits until the first call is answered

  @step1
  Scenario: AG-81 A cloud model serves several runs at once
    Given two runs that use the same cloud model
    When both call the model at the same moment
    Then both calls run at the same time

  @step6
  Scenario: AG-82 An asynchronous run runs in the runtime
    When an agent is called asynchronously
    Then the runtime runs it in the background
    And the runtime can cancel it

  @step6
  Scenario: AG-83 Two runs change the same plan at the same time
    Given a plan with the steps 1 and 2 open
    When one run ticks off step 1 and another step 2 at the same time
    Then both steps are ticked off

  @step6
  Scenario: AG-84 After every round the new parts are stored
    Given an asynchronous run
    When a round ends
    Then the model's answer and the tool results of that round are in the database

  @step6
  Scenario: AG-85 A run is continued after a restart
    Given an asynchronous run that stored two rounds
    When the agent is restarted
    Then the run goes on after the second round

  @step6
  Scenario: AG-86 An interrupted model call is made again
    Given an asynchronous run that was waiting for the model when the agent stopped
    When the agent starts again
    Then the run makes that model call again

  @step6
  Scenario: AG-87 An interrupted read-only tool is run again
    Given an asynchronous run with a read-only tool started but without a stored result
    When the agent starts again
    Then the tool is run again

  @step6
  Scenario: AG-88 An interrupted tool that changes something is not repeated
    Given an asynchronous run with an acting tool started but without a stored result
    When the agent starts again
    Then the tool is not run again
    And the model gets that its result is unknown because the run was interrupted

  @step6
  Scenario: AG-89 Synchronous calls are not continued
    Given a synchronous run without run-on that was running when the agent stopped
    When the agent starts again
    Then the run is not continued

  @step6
  Scenario: AG-90 The time limits count across a restart
    Given an asynchronous run whose time limit passed while the agent was stopped
    When the agent starts again
    Then the run record has the status "expired"
    And the run is not continued

  @step6
  Scenario: AG-91 On shutdown no new round starts
    Given an asynchronous run between two rounds
    When the agent shuts down
    Then no new round starts
    And everything up to then is stored

  @step6
  Scenario: AG-67 A call from Home Assistant gets the agent's own rights
    Given an agent whose description names the rights "read the house" and "messages"
    When an automation calls it with mira.agent
    Then the run's effective rights are "read the house" and "messages"

  @step6
  Scenario: AG-68 Cancelling a run from Home Assistant
    Given an asynchronous run with its call ID
    When an automation calls mira.agent_cancel with this call ID
    Then the run is cancelled
    And no further round starts

  @step6
  Scenario: AG-92 At most 10 runs at the same time
    Given 10 asynchronous runs are running
    When an eleventh is called
    Then it waits until one of the 10 ends

  @step6
  Scenario: AG-93 After a restart a run goes on with the agent as it is now
    Given an asynchronous run of an agent whose description changed while the agent was stopped
    When the agent starts again
    Then the run goes on with the new description

  @step6
  Scenario: AG-94 After a restart a run of an agent that no longer exists fails
    Given an asynchronous run of an agent that no longer exists after the restart
    When the agent starts again
    Then the run record has the status "failed"
    And the reason is that the agent no longer exists

  @step5
  Scenario: AG-95 Two conversations in different rooms run at the same time on a cloud model
    Given two satellites in different rooms and a cloud model for conversations
    When two persons ask at the same moment
    Then both turns run at the same time

  @step5
  Scenario: AG-96 Two turns of the same session run one after the other
    Given a session with a turn still running
    When a second turn of the same session arrives
    Then it starts after the first one has ended

  @step6
  Scenario: AG-101 After a restart a run knows again what it had read
    Given an asynchronous run that read third-party text, itself or through a sub-agent, before the agent stopped
    When the agent starts again and the run goes on
    Then it counts as having read third-party text, as before the restart
    And what keeps or passes something on is refused, as it would have been

  @step6
  Scenario: AG-97 Conversations do not count towards the 10 runs
    Given 10 asynchronous runs are running
    When a person talks to Mira
    Then the conversation runs without waiting for them

  @step6
  Scenario: AG-98 The defaults of mira.agent
    When an automation calls mira.agent with only agent and input
    Then the call is synchronous with a timeout of 60 seconds
    And at the timeout it is cancelled

  @step6
  Scenario: AG-99 An agent as a tool at a satellite answers late
    Given a voice conversation that calls an agent as a tool
    When the agent is not done after 5 seconds
    Then Mira tells the user she will come back
    And the run goes on and its result comes as a message to the room or the person

  @step6
  Scenario: AG-100 A result delivered as a message is worded by the announcement agent
    Given an asynchronous call with the delivery route "message"
    When the run is done
    Then the message's spoken text comes from notify.compose
    And the run's output is in the message's details

  @step8
  Scenario: AG-A1 A YAML agent in mira.yaml can be called
    Given an agent described in mira.yaml under agents
    When an automation calls mira.agent with its name and a fitting input
    Then the agent runs with the instructions, input schema and output schema from the YAML

  @step8
  Scenario: AG-A2 A YAML agent in a skill can be called
    Given an agent described in a skill under metadata.mira.agents
    When it is called
    Then it runs, with the skill's settings filled into its placeholders

  @step8
  Scenario: AG-A3 A YAML agent without a model takes main
    Given a YAML agent without model
    When it runs
    Then it takes the model main

  @step8
  Scenario: AG-A4 A YAML agent without rights may only read the house
    Given a YAML agent without rights
    When it runs
    Then its effective rights are at most "read the house"

  @step8
  Scenario: AG-A5 A YAML agent cannot use the conversation profile
    Given a YAML agent with context conversation
    When Mira loads it
    Then it is not started, and the problem names the profile

  # --- Run view (agenten.md: Run view) -----------------------------------------------------------------

  @step9
  Scenario: AG-C1 The stored runs are listed
    Given two asynchronous runs, one done and one running
    When an admin opens the tab Agenten on Mira's page
    Then both are listed, the running one first, with agent, status, start, duration, model, model calls and tokens

  @step9
  Scenario: AG-C2 A run shows its rounds
    Given a done run that called a tool and read a web page
    When the admin opens it
    Then its input, call context, the model's answers with the tool calls, the results, the change of what its guard
      knows and its output are shown in order

  @step9
  Scenario: AG-C3 Only admins see the tab
    Given a user of Home Assistant who is no admin
    When they open Mira's page
    Then there is no tab Agenten, and the runs cannot be read

  @step5
  Scenario: AG-B1 A known person's conversation follows them to another satellite
    Given a person known to Mira (alone at home) talking at the satellite in the kitchen
    When the same person goes on at the satellite in the living room
    Then the turn belongs to the same conversation, with its threads and plans

  @step5
  Scenario: AG-B2 Without a known person the conversation stays in its room
    Given several persons at home and nobody recognized
    And a conversation at the satellite in the kitchen
    When someone talks at the satellite in the living room
    Then that turn belongs to a conversation of its own
    And it sees nothing of the kitchen's conversation, threads or plans

  @step5
  Scenario: AG-B3 A private area is a hard border
    Given the area "Gästezimmer" has the label "Mira: privater Bereich"
    And a known person starts a plan there
    When the same person talks at a satellite in another room
    Then the plan of the private area is not there

  @step5
  Scenario: AG-B4 Nothing comes into a private area from outside
    Given a known person with a running plan from the kitchen
    And the area "Gästezimmer" has the label "Mira: privater Bereich"
    When the person talks at the satellite in the Gästezimmer
    Then the kitchen's plan is not there
