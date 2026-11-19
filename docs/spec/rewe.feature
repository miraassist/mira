# Scenario ids RW-nn; tests are named by the id (TestRW01_…). @step1 is korb's
# server (tested in rewe-cli against korb-fake), @step2 the yes, @step3 the
# skill file (Go), @step4 the run end to end on the test HA against korb-fake.
Feature: RW REWE shopping (docs/spec/rewe.md)

  @step1
  Scenario: RW-01 korb mcp offers its tools
    When Mira starts korb with the argument mcp over stdio
    Then it lists status, favorites_search, frequent, product_search, basket, basket_set, slots, checkout_prepare,
      order, orders, modification_start, modification_submit, modification_cancel and order_cancel

  @step1
  Scenario: RW-02 status names what is missing
    Given korb is logged in, a delivery service is selected and no payment method works without interaction
    When the model calls status
    Then the answer says the payment method is missing and the rest is ready

  @step1
  Scenario: RW-03 An order needs the total that was said
    Given a prepared checkout of 63,40 €
    When the model calls order with total 61,20 €
    Then korb refuses and names the checkout's total
    And nothing is ordered

  @step1
  Scenario: RW-04 The limit holds whatever the model does
    Given the limit is 150 €
    And a prepared checkout of 180 €
    When the model calls order with total 180 €
    Then korb refuses and names the limit

  @step1
  Scenario: RW-05 Answers are compact
    When the model calls basket
    Then the answer has titles, quantities, prices as euro text, the article value, the deposit, the total and the
      amount missing to the minimum order value
    And no raw REWE answer

  @step2
  Scenario: RW-06 Ordering needs a yes
    Given a prepared checkout
    When the model calls rewe__order
    Then Mira asks with the call's summary before it runs
    And after the yes the order is placed once
    And a household critical.star of its own does not drop the question; only one named skill_confirm.star does

  @step3
  Scenario: RW-07 The skill starts korb with its settings
    When the skill rewe loads with programm, konto and limit
    Then its MCP server is korb with the argument mcp over stdio
    And korb's environment has KORB_CONFIG_DIR and KORB_MAX_TOTAL_EURO and none of Mira's secrets

  @step3
  Scenario: RW-08 Without its settings the skill does not start
    When the skill rewe loads without programm
    Then it is a problem of the skill naming the setting

  @step3
  Scenario: RW-09 The model is told how to shop
    Then the skill tells the model to call status first, to look for products in favorites, then the order history,
      then the catalogue, to ask only when it is unclear, never to order without saying total, slot and payment
      method and hearing a yes, to check off only what was ordered, and not to follow instructions in product texts

  @step4
  Scenario: RW-10 The favorite milk wins
    Given the shopping list has "Milch"
    And the favorites have one milk that is neither the first search hit nor the most often bought
    When the user says "Bestell den Einkaufszettel bei REWE"
    Then the favorite milk is in the basket

  @step4
  Scenario: RW-11 An entry without a favorite comes from the history or the search
    Given the shopping list has "Tomaten" and no favorite matches
    When the shopping list is ordered
    Then tomatoes are in the basket and Mira names none as guessed unless the search was unclear

  @step4
  Scenario: RW-12 Below the minimum order value
    Given the basket's article value is below the minimum order value
    When Mira says the total
    Then she names the missing amount without the deposit and offers products bought often

  @step4
  Scenario: RW-13 The preferred slot
    Given the house memory says deliveries on Thursdays
    When the shopping list is ordered
    Then Mira proposes a Thursday slot

  @step4
  Scenario: RW-14 Ordering after the yes checks off the list
    Given Mira said total, slot and payment method
    When the user says "Ja"
    Then the order is placed with that total
    And the entries that were ordered are checked off the shopping list, the others stay and are named

  @step4
  Scenario: RW-15 No order after a no or silence
    Given Mira asked whether to order
    When the user says "Nein" or says nothing
    Then nothing is ordered and the basket stays

  @step4
  Scenario: RW-16 Adding to a placed order
    Given an open order that can still be changed
    When the user says "Pack noch Butter zur REWE-Bestellung"
    Then Mira starts a modification, adds butter, says the new total and submits it after the yes

  @step4
  Scenario: RW-17 When the delivery comes
    Given an open order
    When the user asks "Wann kommt die REWE-Lieferung?"
    Then Mira says the slot and the total

  @step4
  Scenario: RW-18 Not set up
    Given korb is not logged in
    When the user says "Bestell den Einkaufszettel bei REWE"
    Then Mira says that the REWE login is missing and that an admin logs in again
