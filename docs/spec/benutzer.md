# Users: house and persons – spec

Status 2026-10-16. Also applies to the [tasks](aufgaben.md).

Built (agent 0.5.0, integration 0.4.0): identity, "alone at home" rule,
memory and profile per owner with author. Built (0.5.7): persons
come from Home Assistant. Open: tasks, notices, reminders and
histories per owner, messages to others.

## Principle

One Mira, two kinds of scopes:

- the **house scope**, shared by everyone;
- one **personal scope** per person.

Memory, tasks, notices, reminders and conversation histories have an **owner**: `house` or a person.

There are no separate agents. The house knowledge (structure, devices) stays one.

| | House | Person |
|---|---|---|
| Memory (facts, profile) | household facts | own facts, own profile |
| Tasks | "Achte auf den Wasserverbrauch" | "Sag **mir** Bescheid, wenn …" |
| Notices | announcement in the room, indicator, "Übrigens" | privately via the person's channels, at the satellite only under the "alone at home" rule |
| Reminders | in the room | on the private channel |
| speaks on its own in the house | yes | only under the "alone at home" rule |

Roles (for instance children without locks) and approvals via phone do not exist for now.

## Persons come from Home Assistant

With Home Assistant, **every HA person is automatically a person for Mira**. Mira creates no persons of her own and keeps nothing in sync.

- **The anchor is the HA person, not the HA user.** The person is the household member with presence (home/away), name and picture; the user is only a login. There are persons without a login (children, grandma at the satellite) and logins without a person (Mira's own HA user, service accounts). HA links the two itself via the person's `user_id` attribute, and by default creates a person when a user is created.
- **The key** is the internal ID of the HA person (`attributes.id` of the `person.*` entity, e.g. `anna_schmidt`). It stays when the entity is renamed. Memory, notices, reminders and sessions hang on this key.
- **Mira reads the persons from the states** (`person.*`, with `id`, `user_id`, name, presence). For this her HA user needs no admin rights. She watches them (`subscribe_entities` filtered to the domain `person`): Home Assistant reports a new, renamed or removed person and every change of presence at once; nothing is read again and again ([Watching states](ha-anbindung.md#principle)).
- **Records are created on demand:** a person's memory, profile and so on exist as soon as there is something to store. There is nothing to create.
- **What only Mira needs are optional settings on the person:** Telegram ID, push service (companion app), "Alone at home, the satellites are me". In the integration this is called "Mira settings for a person": you pick an HA person and set what you need. Whoever needs none of it sets nothing.
- **An HA user without a person** (rare) is no one in particular for Mira: in the panel they see the house memory and a hint, in the app chat they count as anonymous.
- **In `mira.yaml`** under `persons:` there are at most settings per HA person (key = person ID; an older entry with `entity:` is matched via the entity). Persons themselves always come from Home Assistant; Mira only runs with HA.

## Identity: who is speaking

The identity comes from the channel:

| Channel | Identity |
|---|---|
| Telegram | person via the Telegram ID (as today) |
| HA app, Assist chat, Mira in the HA sidebar | person via the logged-in HA user. The integration passes on the user ID, Mira maps it via the HA person's `user_id` attribute. |
| Satellite | anonymous, i.e. house scope. Exception: the "alone at home" rule |

To check when building: satellite conversations arrive without a user.

## "Alone at home" rule (switchable per person)

If a person's switch is on and, according to their person entity, they are the only person at home, **the satellites count as this person**:
- Mira then reads and writes their memory;
- reads out their confidential notices and may bring them up on her own;
- creates their personal tasks.

If no one or more than one person is at home, the satellites are anonymous.

Limit: guests without a person entity do not count. If Anna is at home with a guest, she counts as alone. That is the price of convenience, which is why the switch is per person and off by default.

The previous global setting for confidential notices (`confidential_voice: when_alone`) is absorbed into this switch.

## Memory

- **House:** all channels read it, including the anonymous satellite. Everyone may write.
- **Person X:** only X reads and writes. For this X must be identified via their channel, or the "alone at home" rule applies.
- No one writes into someone else's memory. "Merk dir für Ben …" becomes a message to Ben (see below). Ben then decides himself whether to have it remembered.
- In conversation the model sees the house memory and, for a known person, their memory. The nightly compaction runs per owner.
- A known person always remembers for themselves; into the house only if they say so explicitly.
- Every fact has an **author**: the person, otherwise the satellite's area ("jemand im Wohnzimmer"). The web UI shows author and time.
- The house profile is in layer 5 (shared prompt start), the personal profile in layer 7 of the turn, so that the cache stays the same for everyone.

## Messages to others

Own spec: [Messages](nachrichten.md). In short: the notices become messages with sender and receiver (a person or the house); each person sets from whom they accept messages. No one writes into someone else's memory: "Merk dir für Ben …" becomes a message to Ben.

## Tasks

- A task has an owner (`owner: house | <person>`).
  - Created in one's own channel ("sag **mir** Bescheid") it belongs to the person by default.
  - Created at the anonymous satellite it belongs to the house.
- `mira.assess` and `mira.decide` get `owner` (default `house`):
  - It determines which memory goes along (house, for persons also their own).
  - It determines how delivery happens (house: room; person: privately or under the "alone at home" rule).
- `audience` stays for house tasks that should reach only one person.

## Permissions

Mira keeps switching with her own HA user. Whether a command is allowed is decided by her protection, as today. A distinction by person does not exist for now.

## Data model (sketch)

- `owner` (house or person ID from HA) on: facts, profile (one per owner), tasks, reminders, sessions.
- Notices: `owner`/`audience` as before, plus `sender`.
- Persons: the HA person (ID, entity, name, user) plus Mira's settings `telegram_user_id`, `notify`, `alone_is_me` and later `accept_messages` (all, persons, none).
- So far everything is test: data is not migrated. Old keys (for instance `anna` instead of `anna_schmidt`) are simply left behind.
