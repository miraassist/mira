# Scenarios of docs/spec/modelle.md. Every scenario has a number (MO-01, …); the Go test that checks it carries the
# number in its name (TestMO01_…). The tags @step1 … @step5 say in which step (modelle.md, "Steps") it gets built.
#
# Groups: MO-0x models · MO-1x agents name their model · MO-2x privacy · MO-3x fallback · MO-4x the integration

Feature: MO Models

  # --- Models (modelle.md: The models in mira.yaml) ---------------------------------------------------------

  @step1
  Scenario: MO-01 A model is a named entry
    Given mira.yaml has the models main and smart with endpoint, model, token and privacy
    When Mira loads it
    Then both models exist under their names, the token taken from the environment

  @step1
  Scenario: MO-02 Without main in the file, main comes from the integration
    Given mira.yaml has models but none called main
    When Mira loads it
    Then the other models load, and main has to come from the integration's setup
    And without that Mira waits for the setup and says main is missing

  @step1
  Scenario: MO-03 A model needs its privacy
    Given a model without privacy
    When Mira loads it
    Then loading fails, and the error names the model and that privacy is local or cloud

  @step1
  Scenario: MO-04 The old llm and routing are refused with what to write instead
    Given mira.yaml still has llm or routing
    When Mira loads it
    Then loading fails, and the error says to write models.main instead of llm, agents.<name>.model instead of routing

  @step1
  Scenario: MO-05 How many requests at once follows privacy unless set
    Given a local model and a cloud model without parallel, and a cloud model with parallel 2
    When requests come in at the same time
    Then the local one takes 1 at a time, the first cloud one 4, the second 2

  @step1
  Scenario: MO-06 Settings a model leaves out come from main
    Given main with max_tokens 1024 and a model smart without max_tokens
    When smart is called
    Then it is called with max_tokens 1024

  @step1
  Scenario: MO-07 Only models that want it are warmed up
    Given main local with warmup_on_start and smart cloud
    When Mira starts
    Then main is warmed for the conversation channels whose agent takes main, smart is not warmed

  @step1
  Scenario: MO-08 Slot pinning per model
    Given main with pin_slots for voice, telegram and background
    When a voice turn runs on main
    Then the request carries the voice slot; a model without pin_slots sends none

  # --- Agents name their model (modelle.md: Which agent takes which model) ---------------------------------

  @step2
  Scenario: MO-10 An agent without a model takes main
    Given Mira's own agent tasks.assess with no entry under agents
    When it runs
    Then it takes main

  @step2
  Scenario: MO-11 Mira's own agents take the model mira.yaml names
    Given agents.conversation.telegram.model is smart
    When someone writes on Telegram
    Then the turn runs on smart

  @step2
  Scenario: MO-12 Mira's own agents take only model and privacy
    Given an entry for delegate under agents with instructions
    When Mira loads it
    Then the entry is not taken, the problem names the field, and Mira starts

  @step2
  Scenario: MO-13 An unknown model of one of Mira's own agents is a problem
    Given agents.delegate.model names a model that does not exist
    When Mira loads it
    Then the problem names the model, and delegate takes main

  # --- Privacy (modelle.md: Privacy as a rule) --------------------------------------------------------------

  @step3
  Scenario: MO-20 An agent that must run locally does not start on a cloud model
    Given a YAML agent with privacy local and a model with privacy cloud
    When Mira loads it
    Then it is not started, and the problem names the model and the rule

  @step3
  Scenario: MO-21 A skill that must run locally checks its agents
    Given a mail skill with privacy local and mail.read on a cloud model
    When Mira loads it
    Then the skill is not started, and the problem names mail.read and its model

  @step3
  Scenario: MO-22 A local requirement never falls back to the cloud
    Given an agent with privacy local on a local model whose fallback is a cloud model
    When the local model fails
    Then the cloud model is not called, and the call fails

  # --- Fallback (modelle.md: Fallback) ----------------------------------------------------------------------

  @step4
  Scenario: MO-30 A failing model passes to its fallback
    Given smart with fallback main
    When smart fails after its retries
    Then the call is made on main, with main's settings
    And the log line names main and that it was a fallback from smart

  @step4
  Scenario: MO-31 A busy model is waited for, not passed by
    Given smart with fallback main and all of smart's requests taken
    When another call for smart comes
    Then it waits for smart

  @step4
  Scenario: MO-32 A circle of fallbacks does not load
    Given smart with fallback lite and lite with fallback smart
    When Mira loads it
    Then loading fails, and the error names the circle

  @step4
  Scenario: MO-33 A chain of fallbacks is followed
    Given smart with fallback lite and lite with fallback main
    When smart and lite fail
    Then the call is made on main

  # --- The integration (modelle.md: What the integration sets) ----------------------------------------------

  @step1
  Scenario: MO-40 The integration's model becomes main
    Given the integration set a model from a provider
    When Mira starts
    Then main is that model, with privacy cloud, over what mira.yaml says for main

  @step1
  Scenario: MO-41 An own server from the integration is local
    Given the integration set a model on an own server
    When Mira starts
    Then main has privacy local
