# Mira's inbox – Spec

Status 2026-11-12: steps 1 to 4 and 6 to 8 built (not released); step 8 changed the same day to knowing a mail by its hash (decision 24); of step 5 the guide's text is written, the end-to-end test is open. Step 9 (the model writes the plan, entering through the calendar's tool; decision 28) built the same day (not released). Scenarios: [inbox.feature](inbox.feature).
First source: Paperless-ngx; first kind of finding: appointments.

## Goal

1. A letter from school comes by post. Anna scans it with the Paperless app and gives it the tag "Mira".
2. A minute later, on her phone (Telegram) or at the satellite when she is recognised there: "In deiner Post: Elternbrief
   der Grundschule. Ich habe drei Termine gefunden: Elternabend am 3. Dezember um 19:30, Weihnachtsfeier am 19.
   Dezember um 15 Uhr, schulfrei am 22. Dezember. Soll ich sie eintragen?"
3. Anna: "Die ersten beiden." Mira enters them into Anna's calendar and says so. The tag "Mira" is gone from the letter.

Mira does not read the user's whole post. She has an **inbox**: what lands in it she reads, nothing else. The user
decides what goes in. Here the inbox is the tag "Mira" in Paperless. Since Paperless already collects the post, the scans
and the attachments of the mailboxes it reads, the tag reaches all of them: a mail rule or Paperless's automatic matching
can set it ("everything from the school"), the user can set it by hand. Later the inbox can also be a mail address of its
own or a photo sent over Telegram. When Mira finds something the user should act on, she asks, and acts
only after a yes. Appointments come first. Deadlines, open invoices, reminders to pay and requests to call back come
later as further kinds.

Alexa+ does the same with documents and mails forwarded to it. In a test reported by TechCrunch it found 3 of 12 dates
in a school mail. Mira reads every date out before entering anything, so the user hears what was missed.

## Decided (2026-11-11)

| # | Question | Decision |
|---|---|---|
| 1 | What Mira reads | Only her inbox: what the user puts there. How it is built does not matter (alias, own mailbox, mail filter, Paperless tag) |
| 2 | Acting | Ask first, act on the yes. Nothing goes into a calendar without the question |
| 3 | First kind | Appointments. Deadlines, invoices, reminders to pay and callbacks later. Widened 2026-11-12 (decision 28): the plan holds every thing to do the reading finds; Mira enters appointments, the rest she names |
| 4 | First source | Paperless: uploading is possible at once (app, browser, mail), and the text recognition (OCR) is done |
| 5 | The inbox in Paperless | The tag "Mira", set when uploading or later |
| 6 | Whom Mira asks | The owner of the document in Paperless, mapped to a person |
| 7 | How she asks | A thread of that person: a message over their channels, the answer from any channel |
| 8 | Read once | After reading, Mira adds the tag "Mira gelesen"; the tag "Mira" stays, so the user sees Mira's inbox in Paperless. The two tags are the inbox's state: Mira keeps nothing of her own. Reading again: remove "Mira gelesen" |
| 9 | How Mira learns of a document | A Paperless workflow sends a webhook. Polling is ruled out; the MCP server and HA's Paperless integration have no push (see Checked) |
| 10 | Where the webhook goes | Straight to the Mira service. Through HA it would only be a detour |
| 11 | The webhook's token | A token of its own, only for the inbox: everyone who may edit workflows in Paperless sees it |
| 12 | Who creates the workflow | The user by hand, after a recipe in the guide |
| 13 | Tests | Against the real Paperless with a tag of its own ("Mira-Test") |
| 14 | Photos over Telegram | Later. Mira will read them only; filing into Paperless only on request, and then the appointment comes through the Paperless way, not twice |
| 15 | Paperless as the collecting point | Paperless already reads mailboxes and files their attachments. Its own means put the tag "Mira" on what Mira should read too: a mail rule that assigns tags, the automatic matching of a tag on consumption, a workflow, or the user by hand. Mira builds no filter of her own for Paperless |
| 15a | The mail way stays | Reading a mailbox of Mira's own (an alias, a folder, a filter) through HA's IMAP integration stays a way in as well: it is cheap, the [mail calendar](mail-calendar.md) is built and runs for parcels. It comes as a later step of this spec, for whoever does not use Paperless or for mails without attachments |

## Checked (2026-11-11)

Paperless-ngx 3.3.4 (the live one), its docs and code on GitHub (docs.paperless-ngx.com refuses automated reads), the
MCP server `baruchiro/paperless-mcp` in the test environment, HA's Paperless integration.

| Signal or fact | How it was checked | Result |
|---|---|---|
| Workflow trigger "Document Added" with filter "Any Tags" | scan from the Paperless app with the test tag, workflow with a webhook to a receiver | one webhook, 0.5 s after the document was added; none from "Updated" besides |
| Trigger "Document Updated", tag set in the document | uploaded in the browser without the tag (no webhook), then tag set and saved | exactly one webhook |
| Trigger "Document Updated", tag set for several in the list | two documents selected, tag set | one webhook per document, one second apart |
| Webhook content | parameters `doc_id`, `title`, `owner` (`{{doc_id}}`, `{{doc_title}}`, `{{owner_username}}`), "send as JSON" | `{"doc_id":"5444","title":"…","owner":"stefan"}`, header `Authorization: Bearer …` arrived |
| Webhook delivery | Paperless code (`documents/workflows/webhooks.py`) | a background task with a 5 s timeout; on an HTTP error status up to 3 tries with growing waits, on a connection error none |
| No push elsewhere | README and tool list of the MCP server; HA's integration docs | MCP: request and answer only. HA: counting sensors, polled every 120 s, no events |
| MCP tools | `tools/list` on the test server | `get_document_content(id)`, `get_document(id)` (owner as a number, no text), `bulk_edit_documents(documents, method: add_tag or remove_tag, tag)`, `list_tags(name__iexact)`, `query_documents(tag)` |
| Mira's Paperless user | API with Mira's token | user `mira`, no admin; `/api/users/` is refused (403) |
| Marking read with Mira's token | `bulk_edit_documents` add_tag and remove_tag on a test document, before and after the rights | refused (403) while the document gave Mira only view rights (`user_can_change: false`), although `mira` has the global `change_document`; with an edit right for `mira` on the document: both `OK`, the tags as expected. A tag's own rights do not cover the documents carrying it (Paperless docs, Permissions) |
| Edit right assigned by the workflow, in time for the webhook | all of Mira's rights on a document removed by hand; the workflow (tag "Mira") assigns edit rights to `mira`, then sends the webhook; the receiver asks Paperless for `user_can_change` at once, after 1 s and after 3 s | `true` at all three, the first 86 ms after the webhook came: the right is saved before the webhook goes out |
| Filter "Hat keines dieser Tags: Mira gelesen" | Mira's token adds "Mira gelesen" to a document with "Mira" (`bulk_edit_documents`), waits 15 s; then removes it | adding: no webhook; removing: one webhook 116 ms later. Marking read does not bring the document back, and removing "Mira gelesen" reads it again |
| Users for catching up | `/api/users/` with Mira's token after the right was given; the MCP server's tools | 200 over the REST API, but the MCP server has no tool for users and refuses a filter by user name: Mira learns the owners from the webhooks (decision 18) |

## Building blocks

The inbox adds a way in. What happens after it uses what exists:

- **Isolated reading with a fixed schema** from the [mail calendar](mail-calendar.md), section 3: a model call without
  house, tools, memory or history; the skill's instructions; a schema; a check in code. Here the schema has a list of
  findings instead of one entry.
- **A thread with a plan** ([Threads](threads.md), Plans), bound to the person. **A model writes the plan** (step 9,
  decision 28): the reading `inbox.read` writes it, as a helper agent writes the plan in `kochen` and the conversation's
  model in `vokabeln`. Mira's code only checks it and starts the thread with it. The answer comes from whichever channel
  the person uses (Assignment, rule 3c).
- **The calendar's building block and its tool `calendar_create`** ([Calendar](calendar.md), Adding): the same as the
  skill `termin` and the mail calendar enter through.
- **Messages of a thread** ([Messages](messages.md)): the question is a message of the thread to the person, delivered by
  the person's settings (quiet hours, channels). When the thread ends, its message is withdrawn.
- **The person's calendar by convention** ([Calendar](calendar.md), Personal calendars): named like the person or their
  login, or the default the person's memory names.
- **The skill `paperless` and its MCP server** ([Skill MCP](skill-mcp.md)): address and token are there already.
- **Retries** ([Retries](retry.md)) through `internal/retry`.

Departures, each with its reason:

- **The core calls the MCP server itself**, not a model: reading a document's text and removing a tag are fixed steps.
  The skill's `read_only` and its `tools` list stay as they are for the conversation and the agents. The core uses
  only the tools named in step 2 and step 4.
- **The answering machine's plan is written by code, the inbox's by a model** (decision 28): the answering machine's
  plan is hard facts from an interface (caller, time, length); a mail or letter is free text in which a model finds
  what to do, and the same model words the steps.

## What it does

### 1. The webhook (step 1)

`POST /v1/inbox/paperless` on the Mira service, body JSON `{"doc_id": "…", "title": "…", "owner": "…"}`.

- **Token:** `Authorization: Bearer <inbox token>`. Only this token is accepted here, and it is accepted nowhere else.
  It is a secret setting of the skill `paperless`, `inbox_token` (decision 16).
- **The answer comes at once** (`202`, `{"queued": true}`): Paperless waits at most 5 s. The work runs after it.
- **Errors, all answered without work:**
  - no inbox token configured: `404`, a log line once per start (the inbox is off);
  - a wrong token: `401`;
  - `doc_id` not a positive number, or `owner` empty: `400` with the field named; a log line with the body (cut to 200
    characters).

  Paperless tries an error status three more times, then gives up; its log shows it.
- **Duplicates:** a document already queued or being read is not queued again (log line, debug). A document whose
  thread is still open gets no second thread: the webhook is dropped with a log line. A second "Updated" while the tag
  is still there is therefore harmless.
- **Order:** one document at a time, in the order they come.

Cost: nothing but taking the request; the queue is in memory. If Mira stops before a document is read, the tag is still
on it, and the catch-up (step 4) finds it.

### 2. Reading (step 2)

Step 9 changes the reading's answer (3 and 4 below) and asking (section 3): see section 7.

For one queued document:

1. **The person:** the owner's Paperless user name is mapped to an HA person by the convention of
   [Calendar](calendar.md): the person's login or their name. Without a match: only a message to the house
   (decision 22).
2. **The text:** `get_document_content(id)` through the skill's MCP server. The text from the OCR of Paperless; Mira reads
   no file and no image.
   - At most **20,000 characters** go to the reading (a school letter has about 3,000). A longer text is cut there; the
     message says "Ich habe nur den Anfang gelesen", and the log names the length. This is a cut that changes what Mira
     sees, named here on purpose.
   - An empty text (OCR found nothing): no reading; the person gets the message "In <title> konnte ich keinen Text
     lesen." It is marked read (step 4).
3. **The reading:** an isolated model call, as in the mail calendar, with the agent name `inbox.read` (`mira.yaml`,
   `agents`; without it `main`, [Models](models.md)). It gets the skill's instructions for the inbox, the date of today
   and of the document (`created`), and the text marked as data. The answer is JSON:

   | Field | Meaning |
   |---|---|
   | `summary` | what the document is, short ("Elternbrief der Grundschule") |
   | `findings` | a list; empty when there is nothing to do |
   | `findings[].kind` | `appointment` (more kinds later) |
   | `findings[].title` | short ("Elternabend") |
   | `findings[].day`, `start`, `end` | as in the mail calendar: `YYYY-MM-DD`, `HH:MM` or empty |
   | `findings[].location` | short, or empty |
   | `findings[].for` | for whom, if the document says so ("Klasse 2b", "Ben"), or empty |

4. **The check in code**, before anything is taken: `day` valid and not before the document's date minus 7 days nor after
   it plus 2 years; times valid, `end` after `start` (else without `end`); title 80, location 100, `for` 60 characters;
   at most **20 findings** (more: the first 20, and the message says how many more there were). A finding of a kind
   Mira does not know yet is dropped (log line with the kind). A past appointment is kept and marked as past in the
   plan; the person decides.
5. The document's text is third-party: the plan it becomes is third-party ([Threads](threads.md), Third-party text).

**Errors:**

| What fails | Classified | What happens | Who learns of it |
|---|---|---|---|
| MCP server unreachable, timeout, 5xx | retryable | 3 tries, waits 2 s, 4 s, 8 s; then given up | log line per try (what, which document, next wait); on giving up the document stays unread, the catch-up at the next start takes it, and a log line says so |
| Paperless refuses (401, 403) | fatal | no retry | log line with the document and the answer; the person gets "Ich komme an <title> in Paperless nicht heran." It stays unread |
| Document gone (404) | fatal | dropped | log line |
| Model: no answer in 60 s, or an error the model client retries | as for the mail calendar | once more, then given up | log line; the person gets "Ich konnte <title> gerade nicht lesen, ich versuche es beim nächsten Start noch einmal." It stays unread |
| Model answer is no valid JSON | fatal for this document | dropped as unreadable | log line with the start of the answer (200 characters); the same sentence to the person |

Cost per document: 1 MCP call for the text, 1 model call (up to 20,000 characters in, a few hundred out). Nothing of it
is in a voice turn.

### 3. Asking (step 3)

Step 9 replaces the plan written by code, the fixed question and `inbox_enter`: see section 7.

- **Without a person or a calendar** (decision 22): the message with the findings and the question, nothing more; the
  document is marked read.
- **With findings, a person and their calendar:** Mira starts a thread of the skill's target (a plan target, `bound: person`) for the person. Its
  code writes the plan: the document's title and number (no link for now, decision 20), the summary, and one checkbox per finding:

  ```
  Elternbrief der Grundschule (Paperless 5444)
  - [ ] Elternabend, Mi 3.12. 19:30, Aula
  - [ ] Weihnachtsfeier, Fr 19.12. 15:00
  - [ ] schulfrei, Mo 22.12. (ganztägig)
  ```

  Then a **message of the thread** to the person: the summary, the findings in words, the question "Soll ich sie
  eintragen?". Delivered by the person's settings: at the satellite when they are recognised there, otherwise over
  their channels; quiet hours as always. Importance normal.
- **The answer** belongs to the thread by the rules of Assignment: the direct answer at the satellite, or the person's
  Telegram chat (rule 3c).
  - **All, some, none:** the model calls `inbox_enter(steps, calendar)` (decision 21): the numbers of the findings, and a
    calendar only when the person names one (one of theirs or one of the house, never another person's). Without it:
    the person's calendar by convention or the default their memory names.
  - The tool enters each finding from the checked data (`calendar.create_event` through HA: title, day, start, end or
    all-day, location, and "aus Paperless: <title>" in the description), ticks its step with the note "eingetragen",
    and returns what it entered and what failed.
  - "Keine" or "nein": `thread_cancel`.
  - A change ("der Elternabend ist um 20 Uhr"): the model passes the change with the step (`inbox_enter` takes
    `changes` per step, checked like the reading). Never by rewriting the plan.
- **The thread ends** when every step is ticked or dropped, on `thread_cancel`, or after its deadline (default for the
  target: 3 days, so the question waits over a weekend). Expired without an answer: nothing is entered, the message
  is withdrawn, a log line. The document is already marked read (step 4); asking again means removing "Mira gelesen".
- **Without findings:** no thread and no message, only a log line with the document and its summary (decision 17).

**Errors:**

| What fails | What happens | Who learns of it |
|---|---|---|
| The thread cannot be started (database) | the document stays unread | log line; the catch-up takes it |
| The message cannot be delivered (no channel, not recognised anywhere) | as for every message: it waits ([Messages](messages.md)) | the person when they are reached; the Plans tab shows the thread |
| An entry fails (calendar unknown, HA refuses) | that step stays open, the others are entered | the tool's answer names it; the model says it |

Cost of `inbox_enter` in the voice turn: 1 HA call per finding entered (at most 20), one after the other; the turn
waits for them.

### 4. Marking read and catching up (step 4)

- **Marking read:** once the reading has an answer (with or without findings, also an empty text), Mira adds the tag
  "Mira gelesen": `bulk_edit_documents(documents: [id], method: add_tag, tag: <id of "Mira gelesen">)`. The tag
  "Mira" stays. The ids of both tags come from `list_tags(name__iexact: …)`, once per start, kept. Mira's Paperless
  user needs to see both tags and to change the document (guide).
  - Adding "Mira gelesen" changes the document, so Paperless runs its "Updated" workflows; the workflow's filter
    "Hat keines dieser Tags: Mira gelesen" keeps the webhook from coming again.
  - Retryable errors as in step 2; on giving up and on a fatal error (403): log line, the person gets "Ich konnte <title>
    in Paperless nicht als gelesen markieren; bitte setz das Tag Mira gelesen selbst." The thread runs anyway. A
    document left unread is found again at the next start; a thread for it that is still open stops a second one
    (step 1).
  - No tag "Mira" or "Mira gelesen" in Paperless (or Mira may not see it): log line once per start naming the tag;
    webhooks are still taken, but nothing is marked read and nothing caught up.
- **Catching up:** once per start of the Mira service, when the skill's MCP server is connected: `query_documents` for
  the documents with "Mira" and without "Mira gelesen", all pages (25 per page). Every document found is queued as if
  its webhook had come. The owner comes from `get_document(id)` as a number; its user name is the one Mira learned
  from an earlier webhook (decision 18). An owner not learned yet: a log line naming the document and the number; the
  document stays unread until its tag is set again (then its webhook comes).
  - **When:** once per start, as soon as the skill paperless's MCP server is connected: at the start, or when the
    server comes later (the MCP host's signal that its tools changed). No wait by time.
  - Not a poll: it runs once per start, for the case that a webhook came while Mira was not running (Paperless does not
    retry a lost connection).
  - Errors: as in step 2; on giving up a log line, the next start tries again.

Cost at start: 2 `list_tags`, 1 `query_documents` per 25 unread documents (filters `tags__id__all`, `tags__id__none`),
1 `get_document` per unread document for its owner, then per document what step 2 costs. The learned owners are one
small entry in Mira's database.

### 5. The guide (step 5)

A recipe in the guide (German and English), next to the skill `paperless`:

1. In Paperless: create the tags "Mira" and "Mira gelesen".
2. Give Mira's Paperless user the right to see both tags (on each tag). Set the tags' matching to
   "None", so Paperless never sets "Mira gelesen" by itself.
3. Create a workflow: triggers "Dokument hinzugefügt" and "Dokument aktualisiert", each with the filters "Hat eines
   dieser Tags: Mira" and "Hat keines dieser Tags: Mira gelesen"; first an action "Zuweisung" with edit rights for
   Mira's user (the global right to change documents is not enough for a document of another owner; permissions
   assigned by a workflow are merged with the existing ones), then an action "Webhook" with the URL of the Mira service (`http://<address>:<port>/v1/inbox/paperless`),
   "Parameter verwenden" and "Als JSON senden" on, parameters `doc_id` = `{{doc_id}}`, `title` = `{{doc_title}}`,
   `owner` = `{{owner_username}}`, header `Authorization` = `Bearer <inbox token>`.
4. Try it: give a document the tag.
5. Optional, so Mira reads along by herself: in a mail rule of Paperless, assign the tag "Mira" too (for example to
   everything from the school), or set the tag's automatic matching.

If Paperless runs with `PAPERLESS_WEBHOOKS_ALLOW_INTERNAL_REQUESTS=false` or a port list
(`PAPERLESS_WEBHOOKS_ALLOWED_PORTS`), the Mira service's address must be allowed there.

### 6. The mail inbox (steps 6 to 8)

A mailbox of Mira's own, through Home Assistant's IMAP integration and the [mail calendar](mail-calendar.md)'s way in
(decision 15a): cheap, built, and running for parcels. Then it goes on as for Paperless: isolated reading, the checks,
a thread with a plan and the question, `inbox_enter`.

- **Whose:** an IMAP entry whose device is named **"Postfach <login>"** ("Postfach stefan") is that person's inbox,
  by the rule "<application> <login>" of the mail calendar ("Pakete stefan"). "Postfach" alone is the house's: only a
  message with the question (decision 22). The device name is the switch, as for every mail application.
- **What lands in it** is the entry's folder and search: the user's filter rules, a forward, moving a mail by hand.
  The guide recommends a folder "Mira" with the search **"ALL"** (decision 25).
- **Only the mail's text**, no attachments: those go through Paperless (decision 26).
- **Step by step, and what it costs:**
  1. HA's IMAP integration finds a new mail (push or its polling) and fires `imap_content` with `uid` and
     `initial: true` (nothing new: the integration's own).
  2. The Mira integration, for an entry whose device names a mail application of Mira or her inbox, calls
     `imap.fetch` (one call to the mail server) and posts the mail to `POST /v1/mail`, as today, with two new optional
     fields: `imap_entry` (the entry's id) and `uid`. The service uses `imap_entry`; `uid` is only in its log. The service names its inbox in `/v1/status` in a list of its
     own, `inbox_apps` (`["postfach"]`), beside `mail_apps`: an inbox needs no calendar, so the repair "Mira braucht
     den Kalender …" stays with `mail_apps`.
  3. The Mira service answers a mail of `postfach` **at once** (`202`, `{"queued": true}`) and reads it afterwards, in
     the inbox's queue (step 1): the integration does not wait up to five minutes as for a parcel mail. The parcel mails
     stay as they are.
  4. Before queuing, the service computes the mail's hash (SHA-256 of sender, subject, date in UTC and text, the first
     16 hex digits) and looks it up among the mails read of that IMAP entry: one read of Mira's database. A mail read
     before is dropped with a log line.
  5. The reading, the checks, asking: steps 2 and 3, with the subject as the title, the sender and the date beside
     the text. 1 model call.
  6. Kept as read: the service keeps the hash, the last 100 per IMAP entry (one read and one write of Mira's
     database; 100 hashes are about 2 KB). A reading that fails keeps nothing, so the mail is read when HA reports it
     again. Mira does not mark the mail in the mailbox; it stays where it is, as it is (decision 24).
- **Duplicates** (decision 24): a mail read before, queued or being read, or whose thread is still open, is not taken
  again. After a restart of Home Assistant, which reports the folder's last mail again (its last Message-ID starts
  empty, `imap/coordinator.py`), that mail is known and dropped.
- **The interface** ([Versions](versions.md)): `imap_entry` and `uid` are optional both ways. An older integration
  sends neither: the mail is read and known by its hash under the device's name. An older integration gets `202` instead of `200`: it takes any status below 400 as done and logs the
  answer only at debug; an older integration does not know `inbox_apps` either and passes no inbox mail on. A newer
  integration with an older service: no `inbox_apps`, so no such mail is passed on.
- **No catching up** (decision 27): HA's IMAP integration has no action that lists or searches a folder; `imap.fetch`
  needs a known `uid`, and the sensor only counts. A mail that comes while Mira is not running is lost, except the
  last one, which HA reports again after Mira's start; of two mails at once HA may report only the last. The guide
  says so; whoever cannot lose a mail sends it to Paperless.

**Errors:**

| What fails | What happens | Who learns of it |
|---|---|---|
| `imap.fetch` (the mail server) | the mail is not passed on | the integration's warning, as today |
| The service is not reachable | the mail is lost (no catching up) | the integration's warning |
| `/v1/mail` without the fields of a mail (`entry`, `text`) | `400` | the integration's warning |
| Reading, the checks, asking | as steps 2 and 3 | as there |
| Looking up the mails read (Mira's database) | not retried; the mail is read anyway and may be read a second time | log line with the entry, the hash, the subject and the error |
| Keeping the hash (Mira's database) | not retried; the thread runs; the mail may be read a second time when HA reports it again | log line with the entry, the hash, the title and the error |
| The folder changes between `imap_content` and `imap.fetch` (HA fetches by position) | another mail is fetched, or the fetch fails (500 in the trial below); a mail may be missed, none is read twice | the integration's warning on a failed fetch; open, see the TODO list |

**Checked on the test HA (2026-11-12),** GreenMail, an IMAP entry on the folder "Mira" with the search "ALL": a mail
already seen, moved from INBOX into "Mira", came as `imap_content` in the same second (`folder: Mira`, `initial:
true`, its new `uid`). HA takes the last `uid` of the search's result (`imap/coordinator.py`): with the default search
"UnSeen UnDeleted" a seen mail is not in the result and is never reported; with "ALL" every mail that lands in the
folder becomes the last one. An unseen mail appended to "Mira" came 0.1 s later (`initial: true`, `uid: 3`); `imap.seen` with entry and `uid`
answered 200, the mail's flags went from none to `\Seen`, and in the 30 s after it no `imap_content` came for any
entry.
Marking with `imap.seen` was dropped later the same day (decision 24).

**HA's "uid" is the mail's position (2026-11-12).** HA's IMAP integration searches the folder with `SEARCH` and
fetches with `FETCH` (`imap/coordinator.py`), `imap.fetch` and `imap.seen` use `FETCH` and `STORE` (`imap/services.py`),
all without `UID`: aioimaplib adds `UID` only with `by_uid=True`. The trial on GreenMail (folder "Mira", search "ALL",
three mails in it): mail A moved in came as uid 4 while its IMAP UID was 7; B as uid 5 (IMAP UID 8); B moved out made
HA report A again (uid 4, `initial: true`); B moved in again came as uid 5 while its IMAP UID was now 9. The hash of
sender, subject, date and text stayed the same for every report of the same mail (A `69110939ec19`, B
`664601f692bf`), as did a hash of the text alone. In the real test on Dovecot (2026-11-12), moving the last mail out
made Mira ask about the one before again, and a new mail on the position of a mail whose thread was open was dropped.

### 7. The model writes the plan (step 9)

Decided 2026-11-12 (decision 28). The reading's model writes the plan, the message's line and the thread's expiry; the
conversation's model works the plan with the tools that exist; appointments are entered through the calendar's tool.

**The reading's answer** (replaces the table of step 2):

| Field | Meaning |
|---|---|
| `summary` | what the document is, short ("Elternbrief der Grundschule"); for the log and the plan's head |
| `plan` | the steps, one per line, each a thing for the person to do or decide, with every fact it needs: an appointment with day, time and place; a deadline with its day; a callback with the name and the number; a choice among proposals as one step that names them. Empty when there is nothing to do |
| `line` | the message's line ("Elternbrief der Grundschule: drei Termine"), at most 120 characters |
| `expires` | `YYYY-MM-DD`: the day after which the plan is no longer of use (the last date it names, a deadline); empty for none |

The skill's instructions for the reading (`inbox.extract` in `skills/postfach/SKILL.md`) say what a step is and how it
is worded. The plan is German like every plan; dates as the plan's reader says them ("Mi 3.12. 19:30").

**The check in code** (replaces 4 of step 2): only what the code itself relies on.
- `plan`: at most **20 steps** (more: the first 20, and a last step "N weitere im Dokument"; log line), each at most 200
  characters (cut, log line).
- `line`: cut to 120 characters; empty: the summary.
- `expires`: a valid date, not before today and at most a year ahead; otherwise the target's timeout (3 days) and a log
  line with the value.
- The days in the steps are not checked by code: the calendar's building block checks what is entered.

**Asking** (replaces the plan of step 3, the fixed question and `inbox_enter`):
1. With a non-empty plan and a person: the code starts the person's thread of the target `postfach` with the plan
   (head: the summary, the source: "Paperless 5444" or "Mail von <sender>, <date>") and the expiry, as today through the
   threads' way for code ([Threads](threads.md), the same storage as `thread_plan`).
2. The message to the person is the line (until [Messages](messages.md) step 3 makes it an entry of the `postfach`
   message). At the satellite the next turn joins the thread, as today; in it the plan and the skill's prose are in the
   context.
3. **The conversation's model works the plan** by the skill's prose, with the tools that exist:
   - an appointment: `calendar_create` with the facts of the step and the source as `description`, then
     `thread_plan check` with the note "eingetragen";
   - a change ("der Elternabend ist um 20 Uhr"): the changed value goes into `calendar_create`; the plan is not rewritten;
   - a choice among proposals: the model asks which one, enters it, ticks the step;
   - a deadline: entered as an appointment for the whole day when the person wants it, not offered by itself
     (decided 2026-11-12);
   - an entry that was wrong ("nein, der war falsch") right after it was entered: through the skill
     `termin-absagen`, with its question back, as for every event (decided 2026-11-12); `calendar_create` keeps nothing
     to take back;
   - a callback, anything Mira cannot do: the model says it; the person ticks it ("hab ich erledigt") or leaves it;
   - "nichts davon", "nein": `thread_cancel`.
4. Without a person (decision 22): no thread; a message with the line and the plan's steps, to the house.

**Errors** (beside those of steps 2 and 3, which stay):

| What fails | What happens | Who learns of it |
|---|---|---|
| The answer has no `plan` field or is no JSON | as an answer that is no valid JSON (step 2) | as there |
| An entry fails | that step stays open | the tool's answer; the model says it |

**What it costs:** the reading as before (1 model call, a few hundred characters more out for the plan's wording). In
the answer's turn per appointment: `calendar_create` (1 read of the calendars, 1 call to Home Assistant) and
`thread_plan` (1 database write); the turn waits for them. One step after the other: N appointments, N of each.

**What goes away:** the findings as fields (`kind`, `title`, `day`, `start`, `end`, `location`, `for`), their check in
code, the plan written by code, the fixed question "Soll ich sie eintragen?", the tool `inbox_enter` and its texts.

## Open

- **Before step 3: a message sent into a Telegram chat must be in that chat's history.** Checked 2026-11-11 in the code:
  at a satellite the next turn gets the message just announced through the device (`messageContext` in
  `internal/conversation/layers.go`); a Telegram turn has no device, and the notifier's private delivery writes nothing
  into the chat's history. An answer in Telegram ("trag es mir ein") would not know the message. Decided: a message
  into a person's Telegram chat also goes into that chat's history, in a small spec of its own (with the question of
  compacting long chats). Decision 22 relies on it for answers in Telegram. Deferred (2026-11-11): voice comes first;
  at a satellite the answer knows the message today. Until then an answer in Telegram may not know what it refers to.

Decided 2026-11-11 from this list:

| # | Question | Decision |
|---|---|---|
| 16 | Where the inbox token lives | A secret setting of the skill `paperless` (`inbox_token`); without it the inbox is off |
| 17 | A document without findings | Only a log line: a message every time would be noise |
| 18 | The owner when catching up | Changed 2026-11-12: Mira learns each owner's number and user name from the webhooks (the webhook names the user, `get_document` its number) and keeps them. A document of an owner not learned yet stays unread with a log line until its tag is set again. Pragmatic rather than clean; no second way to Paperless past its MCP server (it has no tool for users) |
| 24 | Duplicate mails, and a mail read | Changed 2026-11-12 after the real test: HA's "uid" is the mail's position in its folder, not an IMAP UID (see Checked), so it names another mail once the folder changes. Mira knows a mail by the hash of its sender, subject, date and text, per IMAP entry, and keeps the last 100 hashes of each entry in her database; two mails alike in all four are one. A hash of the text alone was ruled out: two mails with the same text (two equal notices) would be one. Mira does not mark a mail seen any more: `imap.seen` goes by position too and could mark another mail. The mail stays in its folder as it is |
| 25 | What lands in the mail inbox | The entry's folder and search; the guide recommends a folder "Mira" with the search "ALL", so a mail moved there by hand, by a filter rule or a forward comes too, also one already seen (checked 2026-11-12) |
| 26 | Attachments of a mail | Not read; Paperless reads them |
| 27 | Catching up mails | None; the limit is named in the guide (HA's IMAP integration lists no mails) |
| 23 | Mira's code and the skill's MCP server | Decided 2026-11-12: the core uses the connection of the skill paperless's server, which the MCP host opens at the start like every server; it calls tools the skill does not offer the model (`CallServer`). The skill's tool list stays what the model and the skill's agents see ([Skill MCP](skill-mcp.md)) |
| 19 | Marking read | The tag "Mira gelesen"; the maintainer gives Mira's Paperless user the rights. The guide names them |
| 20 | A link to the document | Not for now |
| 22 | No person, or no calendar | When the owner maps to no person, or the person has no calendar by convention ([Calendar](calendar.md)): no thread, only a message with the findings and the question "Was soll ich damit machen?", to the house or to the person. The answer ("nichts", "sag es Anna", "trag es mir ein") is a normal turn after it, not the inbox's: Mira acts with her usual tools (`termin`, messages). Writing into another person's calendar needs rights, which are not built now. The message therefore carries every finding in full (title, day, time, place, the document's title and number), so the next turn has what it needs |
| 21 | How findings are entered | Replaced 2026-11-12 by decision 28. It was Mira's own tool `inbox_enter` from data checked by code, so the model would never copy a date. That mixed code and model in one flow: the code wrote the plan and checked the fields, the model only picked numbers, and a third way to enter an event grew beside the skill `termin` and the mail calendar |
| 28 | Who writes the plan, and how it is entered | 2026-11-12: agentic before code. The reading's model writes the plan, the message's line and the expiry, as in `kochen` and `vokabeln`; the code only checks what it relies on (counts, lengths, the expiry). The conversation's model works the plan with the tools that exist; appointments go through the calendar's one building block and its tool `calendar_create` ([Calendar](calendar.md), Adding), which checks the dates. The plan may hold any thing to do (deadlines, callbacks), not only appointments (decision 3 widened) |

## Steps

1. The webhook: endpoint, token, queue, duplicates. Scenarios IN-01 to IN-05.
2. Reading: person, text, isolated reading, check, errors. IN-06 to IN-12.
3. Asking: thread with plan, message, `inbox_enter`, end. IN-13 to IN-18.
4. Marking read and catching up. IN-19 to IN-23.
5. The guide's recipe and an end-to-end run against the real Paperless with the tag "Mira-Test".
6. The mail inbox's way in: `postfach` as a mail application, the answer at once, `imap_entry` and `uid` on both
   sides. IN-24 to IN-27.
7. Reading a mail with the inbox's core: person from the device name, house without a login, duplicates. IN-28 to
   IN-30.
8. Knowing a mail again by its hash (changed 2026-11-12 from marking it with `imap.seen`); the guide's recipe for the
   mailbox (folder "Mira", search "ALL", the limit). IN-33 to IN-38.
9. The model writes the plan (section 7, decision 28): the reading's new answer, the check, the thread with the
   model's plan and its expiry, the skill's prose with `calendar_create`; `inbox_enter` removed; the evals 14 to 21 on
   it. Needs the calendar's building block and tool ([Calendar](calendar.md), steps 1 and 2). IN-39 to IN-44; IN-07, IN-13 to
   IN-15 and IN-17 dropped; IN-09 and IN-16 are replaced by IN-40 and IN-42 and dropped too.

Later, each with a spec of its own: further kinds (deadline, invoice, reminder to pay, callback) and their actions;
photos over Telegram (`telegram_attachment` with caption and
`file_id`, `telegram_bot.download_file`); filing a photo into Paperless on request.

## Test

- Go tests with a fake MCP server (text, tags, errors) and the fake model, as for the mail calendar.
- End to end against the real Paperless with the tag "Mira-Test" and its workflow (they stay set up for it): a scanned
  sample letter (no real data in the repo), the message on the fake Show and in Telegram, the answer, the entries in the
  test HA's calendar, the tag gone.
