Feature: Bringing a Wi-Fi device into the Wi-Fi by voice (docs/spec/wifi-onboarding.md)

  @step2
  Scenario: WL-01 Without a free Wi-Fi the function is off
    Given the machine's only Wi-Fi interface is connected
    When Mira starts
    Then the Wi-Fi tools are not offered and the log names the reason

  @step2
  Scenario: WL-02 The scan finds devices in setup mode by the name of their Wi-Fi
    Given Wi-Fi networks "ShellyPlus1-A8032AB12345", "tasmota-1A2B3C-4567", "WLED-AP" and "Nachbar" are seen
    When wifi_devices runs
    Then it returns a Shelly Gen2, a Tasmota and a WLED, and not "Nachbar"

  @step2
  Scenario: WL-03 The home Wi-Fi only on 5 GHz stops the hand-over
    Given the home Wi-Fi is seen only on 5 GHz
    When the hand-over is asked for
    Then nothing is joined and Mira says why

  @step2
  Scenario: WL-04 Each family gets the home Wi-Fi its way
    When the hand-over runs for a Shelly Gen1, a Shelly Gen2, a Tasmota, an ESPHome device and a WLED
    Then each gets its request with the home Wi-Fi's name and password

  @step2
  Scenario: WL-05 The device's Wi-Fi never becomes the default route
    When Mira joins a device's Wi-Fi
    Then the profile never becomes the default route and does not connect by itself

  @step2
  Scenario: WL-06 Mira always leaves the device's Wi-Fi
    Given the device refuses the hand-over
    Then Mira leaves its Wi-Fi and deletes the profile
    And says the device did not take the Wi-Fi

  @step2
  Scenario: WL-07 The password is never said nor logged
    When a hand-over runs and fails
    Then neither the log nor a tool result holds the password

  @step3
  Scenario: WL-08 After the hand-over the found device is taken in
    Given a Shelly took the home Wi-Fi
    When Home Assistant discovers it
    Then Mira asks at the satellite whether to add it, then for the room

  @step3
  Scenario: WL-09 Not discovered after two minutes
    Given the device took the hand-over
    When Home Assistant does not discover it within two minutes
    Then Mira says it did not turn up, most often a wrong password

  @step3
  Scenario: WL-10 Nothing in setup mode
    When wifi_devices finds no device
    Then Mira says how to put the common ones into setup mode
