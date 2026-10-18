# Test Home Assistant

A throwaway Home Assistant for testing Mira against a real HA without
touching the house: same version as the house (2026.10.0), demo devices in
German-named areas, simulated leak/motion/window sensors, and a non-admin user
`mira` like in the house.

```sh
go run ./cmd/testha up        # start, onboard, seed; writes testenv/.env
go run ./cmd/testha set binary_sensor.lecksensor_keller on
go run ./cmd/testha dashboard # (re)write the dashboard "Mira Simulation"
go run ./cmd/testha down      # stop, keep state
go run ./cmd/testha reset     # delete all state
```

- HA: http://localhost:8123, login `admin`, password in `testenv/.env`.
- `testenv/.env` holds `HA_TOKEN` (user `mira`, not admin), `TESTHA_ADMIN_TOKEN`
  (for driving tests) and `MIRA_API_TOKEN`.
- A test Mira runs next to the live one on port 8788:

  ```sh
  set -a; . testenv/.env; set +a
  go run ./cmd/mira -config testenv/mira.yaml run
  ```

  From inside the container HA reaches it as `http://host.docker.internal:8788`.

The container uses a bridge network on purpose: in the host network HA
discovers the real devices of the house and sets some of them up by itself.
The seed also disables any integration that came from network discovery.

A broker of its own, `mosquitto` (container `mira-test-mqtt`, port 1883 on this
machine, no password), is there for test devices that speak MQTT, such as a
Tasmota lamp: point the device at this machine's address and port 1883. The
test HA has the MQTT and Tasmota integrations on it, so the device shows up by
itself. Home Assistant's Tasmota integration needs the device's topic to
contain `%prefix%` (the default, `%prefix%/%topic%/`, does).

Entities you add to the test house yourself (a lamp, say) are hidden from Assist by `testha up`. List them in `testenv/.env` to keep them exposed and on the dashboard: `TESTHA_EXPOSE="light.lampe|Wohnzimmer|Farblampe;switch.x|Büro"` (area and name optional).

The dashboard **Mira Simulation** (sidebar, `/mira-sim`) has the switches for the
simulated sensors and for who is home, Mira's quiet mode, the Show's alarm
buttons, a card for each voice satellite (state, alarm buttons, assistant, wake word), and a view of the house with the lists and calendars. `testha up`
writes it, and `testha dashboard` writes it anew (changes made in the UI are
overwritten; make a dashboard of your own for those).

Simulated sensors are template binary sensors driven by `input_boolean.sim_*`;
`testha set` flips them. Any other entity is set directly in the state
machine, which its integration may overwrite again.

No voice satellite: announcements can be tested up to the
`assist_satellite` call, not heard.

## Two languages (planned)

The demos and the tests must run in German and in English, each one clean:
what Mira says and what the dashboard shows use that language's names. English
aliases next to German names are **not** enough ("I switched on the Stehlampe"
does not work for a recording).

So `testha up` takes a language, `TESTHA_LANG=de|en` in `testenv/.env` (default
`de`), and switches everything that is named:

- Home Assistant's `language` and country, and a second pipeline (speech
  recognition and voice in that language) that the satellites' "Assistant"
  select can pick.
- The areas, the names of the exposed devices, the lists and calendars, the
  dashboard and the simulated sensors, and the appointments and waste
  collection dates the seed writes.
- Going back to `de` restores the German house. Both must stay in step: a new
  entity gets both names.

Open: whether Mira answers cleanly in English with the skills, which are
written in German only (`skills/*/SKILL.md`). To be tried first; if not, the
skills get English texts.
