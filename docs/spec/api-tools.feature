# Scenarios of docs/spec/api-tools.md. Every scenario has a number (SL-01, …); the Go test that checks it carries the
# number in its name (TestSL01_…). @step1 is loading tools.star, @step2 calling, @step3 "Ausprobieren" on the page,
# @step4 the skill strava on Starlark tools.

Feature: SL API tools in Starlark

  @step1
  Scenario: SL-01 A function with a docstring is a tool
    Given a skill with a connection whose tools.star defines suche(thema, tage = 7) with a docstring in Google's style
    When the skill loads
    Then it has the tool suche with the docstring's text as its description
    And thema is a required string and tage an optional integer with the default 7, each with its text

  @step1
  Scenario: SL-02 A function beginning with _ is a helper
    Given a tools.star with the functions suche and _video
    When the skill loads
    Then suche is a tool and _video is none

  @step1
  Scenario: SL-03 A docstring that does not fit its function is a problem of the skill
    Given a tools.star whose docstring names a parameter the function does not have, or misses one it has
    When the skill loads
    Then the skill does not start and its problem names tools.star, the line and the parameter

  @step1
  Scenario: SL-04 A tools.star that is wrong in other ways is a problem of the skill
    Given a tools.star without a docstring, or with an unknown type, or with *args, or that does not parse
    When the skill loads
    Then the skill does not start and its problem says what is wrong, with the line

  @step1
  Scenario: SL-05 An api section and tools.star go together
    Given a skill with an api section and no tools.star, or a tools.star and no api section
    When the skill loads
    Then the skill does not start and its problem says which is missing

  @step1
  Scenario: SL-06 Names under writes and confirm are tools
    Given a skill whose tools section names under writes a function tools.star does not have
    When the skill loads
    Then the skill does not start and its problem names it and the tools there are

  @step2
  Scenario: SL-07 The arguments are checked before the function runs
    Given the tool suche(thema, tage = 7)
    When the model calls it without thema, with tage "sieben", or with a parameter it does not have
    Then the call is the tool's error naming the parameter, and the function does not run

  @step2
  Scenario: SL-08 api.get builds the address
    Given a tool that calls api.get with params: a string with spaces, None, a list and a time
    When it runs
    Then the query has every value encoded, no parameter for None, the list repeated and the time in RFC 3339 in UTC

  @step2
  Scenario: SL-09 The request carries the token only below the skill's prefixes
    Given a connected skill whose api names a prefix with GET
    When a tool calls api.get below it, and another tool outside it
    Then the first request carries the connection's token and its answer gives status, ok, text and json
    And the second is refused before anything is sent, and the tool fails with the address

  @step2
  Scenario: SL-10 What fails reaches the model and the log
    Given a tool that calls fail("tage ist 1 bis 30"), and a tool that reads a key its answer lacks
    When they run
    Then the model reads the message of fail, and for the other the first line of the error
    And the log has the backtrace with tools.star and the line

  @step2
  Scenario: SL-11 A tool has limits
    Given a tool that loops for ever, and a tool whose request does not answer
    When they run
    Then the first ends at the step limit and the second at the time limit, each as the tool's error and a log line

  @step2
  Scenario: SL-19 A tool that takes too much memory or answers too much is stopped
    Given a tool that grows Mira's heap by more than its brake, and a tool that answers more than 1 MB
    When they run
    Then the first is stopped and the second's answer is refused, each as the tool's error and a log line

  @step2
  Scenario: SL-12 Only the tools under writes write
    Given a skill whose api allows POST and whose tools section names post under writes and confirm
    When the tool lies calls api.post, and the tool post calls it
    Then the call of lies fails before anything is sent
    And post is no read-only tool, asks first after third-party text and is marked to always ask

  @step2
  Scenario: SL-13 Every request and print leaves a trace
    Given a connected skill and a tool that prints and makes a request
    When it runs
    Then the log has a line with the skill, the tool, the method, the address with its query, the status, the time and
      the size, and a line with the print, and none of them the token

  @step2
  Scenario: SL-14 The tools come with their skill and its connection
    Given a skill with tools.star
    When Mira offers its tools
    Then each is the tool <skill>__<function> of its skill, given to its schedules and agents too, its answer
      third-party text
    And without a connection it is not offered

  @step2
  Scenario: SL-15 The web API tools are gone
    Given a skill with an api section and tools.star
    When Mira offers its tools
    Then there is no <skill>__api_get and no <skill>__api_write

  @step3
  Scenario: SL-16 Ausprobieren shows what went over the wire
    Given a connected skill with a Starlark tool that prints and makes a request
    When an admin runs it on the page with arguments
    Then the page gets the answer, the request with method, address, status, time, size and the start of the answer,
      and the print, in order
    And a tool that fails gives its error with the backtrace

  @step3
  Scenario: SL-17 The page shows a skill's Starlark tools
    Given a switched-on skill with tools.star
    When the page lists the skills
    Then the skill's entry has its tools with description, schema and whether they write

  @step4
  Scenario: SL-18 strava brings its tools in tools.star
    Given the skill strava
    When it loads
    Then it has the tools letzte_tour, zeitraum, serie, ausruestung and bestleistungen and no strava__api_get
    And none of them writes

  @step2
  Scenario: SL-20 JSON that nests too deep is not decoded
    Given a service that answers with two megabytes of "["
    When a tool gets the answer
    Then its json is None, its text stays, and the log says that it nests too deep
    And json.decode of such a text is the tool's error
    And a tools.star that decodes it while it loads is a problem of the skill, not a crash of Mira
    And a nesting of 1000 levels is decoded
