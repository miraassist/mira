# Scenarios of docs/spec/boerse.md. Every scenario has a number (BO-01, …); the Go test that checks it carries the
# number in its name (TestBO01_…). @step1 is the web API without a login, @step2 the skill.

Feature: BO Share prices

  @step1
  Scenario: BO-01 A skill without a login reaches its web API, and only that
    Given a skill without an oauth section names an api prefix
    Then its Starlark tool reaches an address below the prefix, without a token
    And an address elsewhere is refused before anything is sent

  @step1
  Scenario: BO-02 Every request of a Starlark tool names Mira
    Then the request carries the header User-Agent Mira and her version

  @step2
  Scenario: BO-03 A share by its name, on Xetra
    Given Yahoo's search finds Siemens on Xetra and in Frankfurt
    When the tool is asked for "Siemens"
    Then it gives the price of the Xetra listing with its change since yesterday and the time
    And the other hits as alternatives

  @step2
  Scenario: BO-04 An index by its name needs no search
    When the tool is asked for "DAX"
    Then it asks Yahoo's chart for ^GDAXI only

  @step2
  Scenario: BO-05 Nothing found is said
    Given Yahoo's search finds nothing for the name
    Then the tool's error says that no security was found

  @step2
  Scenario: BO-06 Yahoo refusing is the tool's error with its status
    Given Yahoo answers 429
    Then the tool's error names the status and the answer's start
