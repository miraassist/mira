# Scenarios of docs/spec/strava.md. Every scenario has a number (SV-01, …); the Go test that checks it carries the
# number in its name (TestSV01_…). @step1 is the skill file (Go), @step2 the run end to end on the test HA, @step4 the
# tools in Starlark (Go).

Feature: SV Strava

  @step1
  Scenario: SV-01 The skill brings Strava's login
    Given the skill strava with a client id and a secret
    When the skills load
    Then its login is Strava's authorize and token address, the scopes read, activity:read_all and profile:read_all,
      no PKCE and the secret in the body
    And it is ready to connect

  @step1
  Scenario: SV-02 Only reading below Strava's web API
    Given the skill strava connected
    When Mira offers its tools
    Then they are its Starlark tools, all reading, and there is no strava__api_get
    And an address outside https://www.strava.com/api/v3/, or a method other than GET, is refused without a request

  @step1
  Scenario: SV-03 Without a connection there is no Strava tool
    Given the skill strava not connected
    When Mira offers its tools
    Then none of its tools is offered

  @step1
  Scenario: SV-04 No Strava data into memory
    Given the skill strava
    When its instructions are read
    Then they forbid saving activities, distances or times with memory_save

  @step1
  Scenario: SV-05 The best efforts come from the activities
    Given a connected account with activities and stats
    When strava__bestleistungen runs
    Then the longest ride and the biggest climb come from the last 100 activities and the totals from the stats
    And no field biggest_ride_distance or biggest_climb_elevation_gain is used

  @step4
  Scenario: SV-06 The week and the month are counted by the tool
    Given activities this week, earlier this month and last month
    When strava__zeitraum runs with woche and with monat
    Then the week counts from Monday in Mira's time zone and the month from the first, per sport: number, kilometres,
      metres of climbing, minutes

  @step4
  Scenario: SV-07 The series counts weeks in a row
    Given activities this week and in earlier weeks with a gap
    When strava__serie runs
    Then it counts the weeks in a row up to this one, from last week when this one has none

  @step4
  Scenario: SV-08 Strava's errors become sentences
    Given Strava answers 401 with activity:read_permission, 429 or 5xx
    When a tool reads
    Then the tool's error is the sentence the skill says, and no activity is a hint, not an error

  @step2
  Scenario: SV-10 The last tour
    Given a connected account with activities
    When someone asks "Wie war meine letzte Tour?"
    Then Mira says what, when, how far, how long and the climbing of the newest activity

  @step2
  Scenario: SV-11 No activities yet
    Given a connected account without activities
    When someone asks for the last tour, the week or the series
    Then Mira says that there is no activity on Strava yet and invents nothing

  @step2
  Scenario: SV-12 The week and the month
    Given a connected account with activities
    When someone asks how much they rode this week or this month
    Then Mira counts the activities since Monday or the first of the month and says number, kilometres and climbing

  @step2
  Scenario: SV-13 The series
    Given a connected account with activities in the last weeks
    When someone asks how long their series is
    Then Mira says the weeks in a row with at least one activity

  @step2
  Scenario: SV-14 The bike's kilometres
    Given a connected account with a bike
    When someone asks how much the bike has done, or when the chain is due
    Then Mira says the bike's kilometres and that Strava knows no parts

  @step2
  Scenario: SV-15 The best efforts
    Given a connected account
    When someone asks for their best efforts
    Then Mira says the longest ride and the biggest climb among the last 100 activities, and the totals of the year
      and of all time

  @step2
  Scenario: SV-16 Not connected
    Given the skill strava without a connection
    When someone asks for the last tour
    Then Mira says that Strava is not connected and that this is done on the page Connections
