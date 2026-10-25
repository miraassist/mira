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

A few acceptance tests click through Mira's page in this HA (start page,
every page of every group, the card "Ich", what someone who is no admin sees).
They need the test Mira running and Playwright:

```sh
pip install pytest playwright && playwright install chromium
pytest tests/ui
```

Without the test HA or Playwright they are skipped.

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

## Mail calendar

A mail server of its own, GreenMail (container `mira-test-mail`, SMTP on port
3025, IMAPS on 3993, kept in memory), holds the mailbox `pakete@test.local`.

```sh
go run ./cmd/testha mail             # all samples, 30 s apart
go run ./cmd/testha mail -wait 45s 3 # only the sample starting with 3
```

The first run creates an IMAP entry in the test HA and names its device
"Pakete" (search
`UNSEEN`: GreenMail knows no `UNDELETED`), the integration then creates the
calendar "Pakete" within a minute. The samples in `testenv/mailsamples` are
made up; their dates are filled in at delivery, and each gets a new
Message-ID (HA reports a mail as new only when its id differs from the last
one's). Real mails stay out of the repo: `testenv/mails/` is ignored.

## Fake TECHO5 devices

Tests must not depend on real satellites being plugged in. The test HA therefore
has two fake devices next to the real ones (`techo5_fake`, mounted from
`testenv/techo5_fake`, enabled by `techo5_fake:` in `configuration.yaml`):

| Device | Room | Like |
|---|---|---|
| Fake Show (`fake_show`) | Schlafzimmer | an Echo Show with TECHO5: screen hint, presence |
| Fake Echo (`fake_echo`) | Bad | an Echo with TECHO5 Dot: LED ring, no screen |

What they offer comes from a recording of a real device, the **contract**
(`techo5_fake/contract/show.json`, `echo.json`): every ESPHome action with its
fields (`esphome.fake_show_alarm_set` …), and the entities under the same ids as
on the real device. The ones Mira uses behave like the device: alarms
(`alarm_set`, `alarm_delete`, `alarms_list` with the same answer), the device's
own timers (`timer_start` …), Home Assistant's timers (`HassStartTimer` runs on
them, so Home Assistant offers the timer tools for them), announcing and
starting a conversation, the speaker (`play_media`, `media_stop`), the screen
hint (`notify`), the ring, the sleep timer and sleep sounds as selects. Every
other action is accepted and only logged. They speak nothing (no speech
synthesis, so no cloud voice is paid for).

What a fake was told is in `sensor.fake_show_fake_log` / `sensor.fake_echo_fake_log`
(attribute `log`, the last 50 entries with time, kind and data: announcements
with their text, actions with their fields, media played, ring on/off).
Presence is off until set: `go run ./cmd/testha set binary_sensor.fake_show_presence on`.

The evals use the fakes: a step without `at:` is said at the Fake Show
(Schlafzimmer), and alarms are read from the fakes only.

**Contract:** `go run ./cmd/testha contract` compares the real TECHO5 devices in
the test HA with the recording and says what a new firmware changed (actions,
fields, entities), and how much of it the fakes offer. After a firmware update:
check the differences, then `go run ./cmd/testha contract record` and restart
the test HA, so the fakes offer the new actions too.

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
