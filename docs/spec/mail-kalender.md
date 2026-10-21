# Mail calendar – spec

Status 2026-10-20: decided, built (0.6.57). First application: [Pakete](pakete.md).

## Target

Many mails announce something with a time: a parcel arrives tomorrow between 10 and 12, the dentist confirms
Tuesday 9:30, the railway reports a platform change, the pharmacy has the medication ready. Mira reads such mails,
pulls out what and when, and puts it into a **calendar**. There one sees it in the normal calendar view,
Mira answers questions from it, and the briefing reads it too. Newer mails change the entry, a cancellation deletes
it, Mira clears away what is done.

The **core** is general: mail in, read it in isolation, maintain the entry. **What** to get from a mail and
how to answer questions about it is said by one **skill** per application (`pakete`, later e.g. `termine`, `reisen`).

## Principle

- **Do not rebuild what HA offers:** Mails come in through Home Assistant's IMAP integration.
- **Convention before setting:** The name of the IMAP device determines application, calendar and owner.
- **Mails are foreign text:** Reading happens in isolation; only checked data comes out, never instructions.

## 1. The IMAP entry and the name of its device

- The user sets up the **IMAP integration** in HA. An entry has a folder (default: inbox) and a
  **search** in IMAP syntax (e.g. `OR FROM dhl.de FROM hermesworld.com`); both can be combined. Mira relies on
  the search; a separate folder via a rule in the mail program is possible if the search is not enough.
- One entry per application, because each application has its own senders. One can create several entries on the same
  mailbox.
- **The name of the device** that HA creates for each IMAP entry (default "IMAP (mail address)") follows the
  convention **`<application>`** or **`<application> <login>`**. Only the device name counts, not the name of the
  entry: HA shows the device first after setup, and it is what one renames. Two names that
  both count would be a trap (entry and device look almost the same in HA).
  - Examples:
  - "Pakete": application `pakete`, belongs to the **house**.
  - "Pakete stefan": application `pakete`, belongs to the person with the login name `stefan` (or who is called that, as with the
    [calendar](kalender.md)).
  - The application is the name of a mail skill (case does not matter). Entries whose device name matches
    no mail skill, Mira does not read.
- **The calendar is named like the device:** "Pakete", "Pakete stefan". If it is missing, the integration creates a local
  calendar of this name and exposes it to Assist.
- Different names via a setting: later, when someone needs them.

## 2. The path of a mail

```
IMAP entry with device "Pakete" (search)
  → HA: event imap_content (new mail)
  → Mira integration: device name matches a mail skill? fetch full text (imap.fetch)
  → agent POST /v1/mail: device name, sender, subject, date, text
  → agent: isolated reading (skill instructions, fixed schema), check fields
  → agent: create, change or delete the entry in the calendar if the mail is newer
```

- Mira's own HA user may not subscribe to `imap_content` (admins only); the **integration** listens to it. Which
  applications exist, the agent reports in `/v1/status` (`mail_apps`); other entries the integration does not
  pass on.
- Events on deleting a mail (`initial: false`) it ignores.
- The agent processes the mails of one calendar one after the other.
- The agent turns HTML mails into text before the reading.
- **Missed mails** (if two mails arrive at once with polling instead of push, HA probably reports only the last one):
  not fetched afterwards for now; observe in testing.

## 3. Reading: isolated, fixed schema

- An **isolated** model call: no house, no tools, no memory, no history. It gets the
  **mail instructions of the skill** and the mail, marked as data.
- **Model:** what `mira.yaml` names for `mail.read` and `mail.match` under `agents`; without it, `main`
  (docs/spec/modelle.md). A call that
  fails or has no answer after 60 s is repeated once (a cloud model stays without an answer now and then). A cloud model sees
  the mails (names, addresses, orders, for appointments also health data); the guide recommends a local one.
- **Schema** (JSON):

  | Field | Meaning |
  |---|---|
  | `relevant` | whether the mail announces something for this calendar; otherwise it is discarded |
  | `title` | title of the entry, short ("Paket von Amazon: Druckerpatronen") |
  | `keys` | all keys of the matter (tracking, order, booking number); empty if none |
  | `status` | `scheduled` (announced, confirmed, changed), `cancelled` (cancelled), `done` (done, delivered) |
  | `day` | day (YYYY-MM-DD); empty if the mail names none |
  | `start`, `end` | time or time window (HH:MM), empty for all-day |
  | `location` | place, short |
  | `details` | details, one "label: value" per line; never codes, PINs or passwords |

- **Check in code** before anything is taken over: `day` valid and at most 7 days before, at most one year after the
  date of the mail; times valid, `end` after `start` (otherwise without `end`); each key only letters, digits
  and hyphen (at most 40), at most 8; title (80) and location (100) shortened, details at most 6 lines of
  100 characters. What does not fit is dropped.
- **Without title or day there is no entry**, until a mail names the day (a shipping mail without a delivery day
  enters nothing). A cancellation needs no day: It deletes the entry it finds via a key.

## 4. The calendar entry

- With `start` (and `end`): appointment; otherwise all-day on `day`. Without `end` an appointment lasts one hour.
- **Description:** the details, below them fixed lines that Mira reads:
  - `mira-mail: <key>, <key>` (the keys; without keys `mira-mail: -`),
  - `Stand: <date of the mail>` and `Status: <status>`.
- **Keys:** Different mails about the same matter often name different numbers (the shop the order number, the
  carrier the tracking number). Before creating, Mira searches the entries of the calendar from 30 days before to one
  year after today for one that shares **one** key with the mail (case does not matter). Found:
  change it, and the keys of both are merged. The internal ID of the calendar does not help, the
  calendar assigns it.
- **Without keys:** A second isolated call gets the open entries (title, day) and says whether the mail
  concerns the same one (number) or a new one. When in doubt, new.
- **What the new mail does not name stays:** the details whose label it does not name; the location if it
  names none; the time window if it names the same day without a time; the title if the new one is only its beginning
  ("Paket von Amazon" leaves "Paket von Amazon: Druckerpatronen" standing).
- **The newest mail wins:** If the date of the mail is older than the as-of date (`Stand`) of the entry found, it is
  discarded. So it does not matter in which order mails arrive; processing twice changes nothing.
- **Status:** `cancelled` deletes the entry; `done` leaves it standing until the end of its day.
- **Sign for done:** If the skill names one (`mail.done`, for Pakete "●"), the core puts it in front of the title
  as soon as an entry is `done` ("● Paket von Amazon: Ladekabel"), and removes it again when it is open
  again. A sign instead of a word, because calendar views often show only the beginning of the title; the
  speech output skips it. It must exist in the font of the display (the Go font of the TECHO5 devices has
  no check mark, but ●).
- **Cleanup** (hourly): Entries with status `done` whose day is past, and all entries whose day is more than
  a week past, Mira deletes. Only in mail calendars and only entries with `mira-mail:`.

## 5. Mail skills

A skill becomes a mail application through a section `mail` in the header:

```yaml
metadata:
  mira:
    mail:
      extract: >-
        Anleitung für die Auswertung: was eine relevante Mail ist, wie Titel und Kennung zu bilden sind …
```

- `extract` goes as instructions to the isolated call. The schema and the check are set by the core, not the
  skill.
- `done` (optional): the sign for done entries, see section 4.
- The text of the skill is, as with every skill, the instructions for questions ("Wann kommt mein Paket?").
- Mail skills without required settings switch themselves on when their folder is there, like the built-in targets: The
  device name of the IMAP entry is the switch.

## 6. Reading along in the briefing

The personal briefing gets `{{person_login}}` and reads the calendar "Pakete {{person_login}}" too, if it
exists; the house briefing reads "Pakete" if it is in its setting `kalender`.

## 7. Test

- The test HA gets an IMAP server in the container (GreenMail) and an IMAP entry with device "Pakete";
  `go run ./cmd/testha mail` delivers the sample mails (testenv/README.md).
- Sample mails: self-written ones following the patterns of DHL, GLS, Hermes and eBay (in the repo, without real data);
  real mails only locally (`testenv/mails/`, not in Git).
- Checked: entry created; newer mail changes it; older one is discarded; cancellation deletes; pickup code does not
  appear; advertising mail is discarded.
