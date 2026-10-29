# Phone calls

Status 2026-10-24: built, released in 0.7.0. Scenarios: [telefon.feature](telefon.feature).

## Goal

"Ruf Oma an" at a satellite, and the satellite calls Oma. A TECHO5 device is a speakerphone (SIP, with the
FRITZ!Box as registrar since a small firmware change); Home Assistant's MCP offers no tool for it, and its phone book
lives on each device, so the same sentence would not work at every satellite. Mira closes both gaps: one tool that
places the call from the satellite being spoken at, and one phone book for the whole house, the FRITZ!Box's.

## What the satellite offers (TECHO5)

- `esphome.<device>_phone_call` with `number` places a call; `esphome.<device>_intercom_call` with `device` calls
  another TECHO5 in the house (the intercom with the house word, no phone line). Mira calls these actions over her
  own connection to Home Assistant, as she does for alarms and cards; no new integration is needed.
- The sensor `sensor.<device>_phone` says whether the phone is signed in (*Ready*).
- Incoming calls show the name the FRITZ!Box passes on from its phone book (tried 2026-10-24); nothing to do there.
- The device's own phone book (12 contacts, set with `phone_contacts`) is **not used**: contacts stay off the
  devices. It would only serve the device's own English "call Mom" and the Call list on its screen.

## The phone book: the skill `telefon`

- Mira reads the FRITZ!Box's phone book over TR-064 (`X_AVM-DE_OnTel`: `GetPhonebookList`, `GetPhonebook`, then the
  phone book's XML), with digest authentication. That is code in Mira, not a skill alone: `web_page` cannot speak
  SOAP with digest.
- The skill `telefon` switches it on and holds the access, like every skill (bearbeiten.md):

  ```yaml
  skills:
    telefon:
      user: mira
      password: ${MIRA_SKILL_TELEFON_PASSWORD}
      fritzbox: 192.168.178.1       # optional; the default
      telefonbuch: Telefonbuch      # optional; without it the first phone book
  ```

  Convention over configuration: only `user` and `password` are needed. `fritzbox` is the box's address (some homes
  phone over another box than their main router); without it `192.168.178.1`. Mira reaches TR-064 on its port 49000
  there; nothing more to set.

  `enabled: false` switches it off. The user is a FRITZ!Box user of its own with only the right **Telefonie**
  (FRITZ!Box: System → FRITZ!Box-Benutzer); that is enough for TR-064's phone book (tried 2026-10-24).
- **What the FRITZ!Box gives** (tried 2026-10-24 on the test house's box): `GetPhonebookList` names the books
  (`0,1`: the box's own and one synced with Google, 16 and 470 contacts); `GetPhonebook` gives a book's name and an
  address of its XML (with a session id, on the box's IPv6 address), read with a plain GET. Each number has a `type`
  (`home`, `mobile`, `work`, `intern` for the box's own phones, `fax_work`, `memo`, `fixedline`, `label:…` from
  Google, or none) and `prio="1"` on the main number; almost every contact has one main number.
- **Which books:** without `telefonbuch` the first one (the box's own, `0`); with it those named (comma-separated),
  e.g. `Telefonbuch, Google …` for the synced one as well. A name in two books is one contact when the numbers match.
- **Which numbers:** fax numbers (`fax_…`) are never dialed; `intern` numbers (`**610`) are, they reach the box's
  own phones.
- **Read before every call** (about 0.4 s), so a contact just added or a number just changed counts at once (changed 2026-10-24: kept for an hour, a changed number of a known name was missed). When the box does not answer, the contacts read last stand in, and the page says why. Mira also reads it once soon after her start, for the page.
- **What the model sees:** only the entries that match what it asked for, by name and the kind of number (Handy,
  privat, geschäftlich); never the numbers and never the whole book. The tool dials, not the model.
- The skill's instructions say when to call and how to ask back; they are short, the tool does the work.
- **On Mira's page** (Skills, the skill `telefon`): that the phone book is on, which books it reads and when it last
  read them (or why it could not), with a link to the FRITZ!Box (`http://<fritzbox>`), where the contacts are kept.
  The contacts themselves are not shown.
- **Connection test:** the editor's **Test connection** works for this skill too: it reads the phone book with the
  form's settings and says how many contacts it has, or why it could not.

## The tool `phone_call`

- **Argument:** `target`, what the user said: a name ("Oma"), a room or device in the house ("die Küche"), or a
  number; optionally `kind` (Handy, privat, geschäftlich), when the user named one.
- **How the target is found**, in this order:
  1. A room or TECHO5 device in the house → `intercom_call` with that device.
  2. A contact of the phone book: names compared without case and accents; a whole word of a contact's name counts
     ("Oma" finds "Oma Erika"). One contact → its number: the one of the asked kind, else the main number (the
     FRITZ!Box's "Hauptnummer", `prio="1"`), else the only one. Since nearly every contact has a main number, asking
     back for the number is rare; the main number is also what the box's own phones dial.
  3. Digits → that number.
- **Asks back instead of dialing** when several contacts match, or a contact has several numbers and none is the
  main one or the asked kind: the tool returns the names (and kinds), the model asks, and calls again with the
  answer.
- **Dials at once** when the target is clear (decided 2026-10-24). Mira says whom she calls ("Ich rufe Oma an, auf
  dem Handy."), never the number; the call is placed from the satellite being spoken at. Calling off: the device's
  hang-up (button, screen, hang-up word).
- **Where it does not go:**
  - Only in a voice conversation, from the satellite of the turn. Not from Telegram or the app's chat, not from tasks,
    schedules or agents: a call may cost money and rings real people.
  - A satellite without a phone (no `phone_call` action): the tool says so, nothing is dialed.
  - **Before every call** Mira reads the phone's state (`sensor.<device>_phone`, i.e. whether the device is signed in
    at the SIP registrar) and dials only when it is *Ready*. Otherwise nothing is dialed, and the tool says why, so
    Mira can tell the user what to do:

    | State | Mira says, in substance |
    |---|---|
    | *Not set up* | The phone of this satellite has no login yet. |
    | *Connecting* | The phone is not signed in at the telephone box yet; try again in a moment. If it stays so, the box may not be reachable. |
    | *Login refused* | The telephone box refused the login; the IP phone's user name or password is wrong. |
    | *Cannot sign in* | The telephone box cannot be reached (address, network). |
    | *Ringing*, *Calling*, *In call* | The line is busy with a call. |
    | unavailable | The satellite itself is not reachable. |

    Intercom calls need no registrar and are not held back by it.
  - Without the skill (off or not set up) names cannot be found; rooms and numbers still work, and the tool says
    that the phone book is off.
- **Rights:** act on the house.
- **The call page** shows whom the call is to: where the satellite has `phone_call_named` (TECHO5 from 1.6.5), a
  contact is called with it and its name (at most 40 characters), and the page shows "Oma Erika (0172 …)". Elsewhere,
  and for a number, `phone_call` as before, and the page shows the number.

## Decided

1. The FRITZ!Box's phone book is the one phone book, read over TR-064, switched on and set up as the skill `telefon`
   (2026-10-24). Only user and password are needed; the address (`192.168.178.1`) and the book (the first) have
   defaults and can be set.
2. Dial at once when the target is clear; ask only when several contacts or numbers fit.
3. Rooms and devices in the house are called over the intercom (`intercom_call`) by the same tool.
4. No contacts on the devices: neither for the call page's name nor for a list of recent calls.
5. The call page shows the contact's name where the satellite has `phone_call_named` (TECHO5 1.6.5, 2026-10-25); elsewhere the number.
6. Fax numbers are never dialed; the box's internal numbers are.
7. Mira's page shows that the phone book is on, its books and a link to the FRITZ!Box, never the contacts.

## Open

- **The phone book moves** (decided 2026-10-29) into the integration Mira FRITZ!Box
  ([miraassist/mira-fritz](https://github.com/miraassist/mira-fritz), its spec `docs/spec/fritz.md`), together with
  the answering machine and missed calls. Mira then reads it with the action `mira_fritz.get_phonebook`; the skill's
  settings for the FRITZ!Box go away.
