Feature: OpenAPI (docs/spec/openapi.md)

  @step1
  Scenario: OA-01 Every route is described
    When the OpenAPI file is generated
    Then every route of the service under /v1/ is in it with its method, path, request and answer

  @step1
  Scenario: OA-02 The file is current
    Given a route's answer type gained a field
    When the tests run without the file generated again
    Then the test fails naming the difference and "go run ./cmd/openapi"

  @step1
  Scenario: OA-03 A route past the function
    Given a route registered directly on the mux
    When the tests run
    Then the test fails naming that route

  @step1
  Scenario: OA-05 The description at run time
    When a client calls GET /v1/openapi.yaml without a token
    Then the answer is the running service's description, equal to what the generator writes for its version
    And GET /v1/openapi.json gives the same as JSON

  @step1
  Scenario: OA-04 Errors and security
    When the OpenAPI file is generated
    Then every route names the bearer scheme and Mira's error body for its error statuses

  @step2
  Scenario: OA-06 A client takes an added field
    When the OpenAPI file is generated
    Then no schema says additionalProperties false, not inside a list or a map either

  @step2
  Scenario: OA-07 Every body and answer is named
    When the OpenAPI file is generated
    Then every request and answer lists its keys
    Except the open objects on purpose, each with its reason in its route's description
