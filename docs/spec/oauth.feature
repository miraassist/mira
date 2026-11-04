# Scenarios of docs/spec/oauth.md. Every scenario has a number (CO-01, …); the Go or pytest test that checks it
# carries the number in its name (TestCO01_…). @step2 is the Mira service, @step3 the login and the page, @step4 the
# web API tool.

Feature: CO Connections (OAuth)

  @step2
  Scenario: CO-01 A skill's oauth section brings client id and secret as settings
    Given a skill with an oauth section and no settings
    When the page shows the skill's settings
    Then it offers client_id and client_secret, the secret as secret

  @step2
  Scenario: CO-02 A mistake in the oauth section is a problem of the skill
    Given a skill whose oauth section has a token_url that is not https
    When Mira starts
    Then the skill does not start and the problem names the field

  @step2
  Scenario: CO-03 The login data goes to the integration only
    Given a skill with an oauth section, client id and secret set
    When the integration asks GET /v1/oauth/<skill> with the bearer token
    Then it gets addresses, scopes, PKCE, extra parameters, client id and secret
    And without the bearer token it gets nothing

  @step2
  Scenario: CO-04 Tokens handed over connect the skill's MCP server
    Given a skill with an oauth section and an mcp section with token oauth, not connected
    When the integration puts the tokens to /v1/oauth/<skill>/token
    Then they are stored encrypted in Mira's database
    And Mira connects to the skill's server with the access token

  @step2
  Scenario: CO-05 A token that ran out is renewed before the call
    Given a connection whose access token has run out
    When the skill's MCP server is called
    Then Mira renews it at token_url first and the call carries the new token
    And a new refresh token from the service replaces the stored one

  @step2
  Scenario: CO-06 A 401 renews once and repeats once
    Given a connection whose access token the service refuses
    When the skill's MCP server is called
    Then Mira renews once and repeats the request once

  @step2
  Scenario: CO-07 A refused refresh token makes the connection expired
    Given a connection whose refresh token the service refuses
    When the token is renewed
    Then the tokens are deleted, the state is expired and Home Assistant shows a repair
    And when the skill is opened Mira says that the connection must be made again

  @step2
  Scenario: CO-08 The tokens reach no model and no log
    Given a connected skill
    When a conversation uses its tools and the log is written
    Then neither the access token, the refresh token nor the secret is in a prompt or in the log

  @step3
  Scenario: CO-09 Connect opens the service's login with Home Assistant's redirect
    Given the page inside Home Assistant and a skill with client id and secret
    When the user clicks Connect
    Then a window opens at the skill's authorize_url
    And its redirect address is https://my.home-assistant.io/redirect/oauth with a state signed by Home Assistant

  @step3
  Scenario: CO-10 The redirect hands the tokens to Mira and creates no config entry
    Given a login started for a skill
    When the service redirects with a code
    Then the integration exchanges it at token_url and puts the tokens to the Mira service
    And Home Assistant has no new config entry and the page shows connected

  @step3
  Scenario: CO-11 Refusing at the service stores nothing
    Given a login started for a skill
    When the service redirects with an error
    Then nothing is stored and the page shows not connected

  @step3
  Scenario: CO-12 Disconnect deletes the tokens
    Given a connected skill
    When the user clicks Disconnect
    Then the tokens are gone from Mira's database and the skill's server is disconnected

  @step3
  Scenario: CO-13 Outside Home Assistant there is no Connect
    Given Mira's page opened on its own address
    When the Connections page is shown
    Then it offers no Connect and says to open it in Home Assistant

  @step3
  Scenario: CO-14 An older integration or service
    Given an integration without the connection view, or a Mira service without /v1/oauth
    When the Connections page is shown
    Then it says which one needs an update, and nothing else breaks

  @step4
  Scenario: CO-15 The API tool calls only below the skill's prefixes
    Given a connected skill whose api names https://api.example/2/ with GET
    When the model calls the API tool with a path below it
    Then the request carries the token and the answer is third-party text

  @step4
  Scenario: CO-16 No token outside the prefixes
    Given a connected skill whose api names https://api.example/2/
    When the model calls an address outside it, or the service redirects to another host
    Then no request with the token is made

  @step4
  Scenario: CO-17 A method the skill does not name is refused
    Given a connected skill whose api allows only GET
    When the model calls the API tool with POST
    Then the call is refused with that reason

  @step4
  Scenario: CO-18 A method the skill names runs as the skill says
    Given a connected skill whose api allows POST and whose instructions say to post without asking
    When the model calls the API tool with POST
    Then the request is made without a question of Mira's own

  @step4
  Scenario: CO-19 Writing after third-party text asks first
    Given a connected skill whose api allows POST and whose instructions say to post without asking
    And a turn in which the model has read third-party text
    When the model calls the API tool with POST
    Then Mira asks the user first and calls it only after the yes
