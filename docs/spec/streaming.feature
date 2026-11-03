# Testable rules of docs/spec/streaming.md. Each scenario has a fixed number (ST-nn) that is never reused; its test
# carries the number in its name (TestST01_…). The tags @stepN say in which step of the spec ("Steps") the scenario
# gets built.

Feature: ST Speaking while the answer is written

  @step3
  Scenario: ST-01 The model client assembles a streamed answer and its tool calls
    Given a model server that streams text in pieces and a tool call split over several pieces
    When Mira asks it with stream true
    Then the pieces of text come out in order as they arrive
    And the tool call is the same as the server's whole answer would have had

  @step4
  Scenario: ST-02 After a tool round the answer is handed on in pieces
    Given a voice turn whose model called GetLiveContext
    When the model writes its answer
    Then the first piece leaves the engine before the model has finished

  @step4
  Scenario: ST-03 An answer without a tool call is handed on in pieces too
    Given a voice turn whose model tells a story without calling a tool
    When the model writes its answer
    Then the first piece leaves the engine before the model has finished

  @step4
  Scenario: ST-04 Thinking is never spoken
    Given the model's streamed answer contains a <think> block split over several pieces
    Then no piece handed on contains any of the thinking

  @step4
  Scenario: ST-05 Übrigens comes as the last piece
    Given a message waits for the room
    When a streamed answer ends without a question
    Then the waiting message follows as the last piece, as it follows the whole answer today

  @step4
  Scenario: ST-06 A model that fails after the first piece gets no fallback
    Given the model has handed on its first piece and then fails
    Then no other model continues the answer
    And a short fixed sentence ends it

  @step4
  Scenario: ST-07 Telegram and background runs stay whole
    When a turn comes from Telegram or runs in the background
    Then its answer is handed over whole, as today

  @step5
  Scenario: ST-08 An older service ignores the field, the integration takes the whole answer
    Given a service without streaming
    When the integration asks with stream true
    Then the service answers as today and the integration passes the answer to Home Assistant whole

  @step5
  Scenario: ST-09 An older integration gets the whole answer
    When the integration asks without the field stream
    Then the service answers with one JSON object, as today

  @step5
  Scenario: ST-10 A broken connection ends the answer with a fixed sentence
    Given the connection to the service breaks after some pieces
    Then the integration ends the answer in Home Assistant with the pieces so far and a short fixed sentence

  @step6
  Scenario: ST-11 A silence of 2 seconds is bridged with a fixed sentence
    Given a voice turn whose model has handed on nothing 2 s after the request reached Mira
    Then Mira hands on "Einen kleinen Moment, bitte. " at once, so that Home Assistant starts the speech
    And the answer follows when the model has it
    And no second bridging sentence comes in the same answer

  @step6
  Scenario: ST-13 A model that speaks before its tools is not bridged
    Given a voice turn whose model writes "Ich schaue im Kalender nach. " before calling a tool
    Then that sentence is handed on as written
    And no fixed sentence comes

  @step2
  Scenario: ST-12 An answer without a tool call goes on as the model wrote it
    Given a voice turn whose model answers "Jeden Abend hat er die große Lampe eingeschaltet." without calling a tool
    Then the answer reaches the user as written
    And the model is not asked again and no tool call is forced

  @step6
  Scenario: ST-14 The fixed sentence comes only from a satellite with early_reply
    Given a voice turn whose model has handed on nothing 2 s after the request reached Mira
    When the turn comes from Home Assistant's chat (no satellite), or sessions.early_reply is false
    Then no fixed sentence is handed on
