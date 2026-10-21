# Messages – spec

Status 2026-10-16: built in 0.6.0–0.6.4 (except "recognized at the satellite", see Open). Two abstractions carry Mira's communication: [Channel](kanaele.md) and **message** (this document). Builds on [Users](benutzer.md) and [Mira in Home Assistant](ha-anbindung.md).

## Principle

The **notices** become **messages**. A message has a **sender**, a **receiver** and a **content**. Receiver, sender and the receiver's settings determine how and when it is delivered.

There is **one** term for everything Mira passes on to someone: what today is a task's notice ("Der Gasverbrauch ist ungewöhnlich hoch") will then be a message with the sender "gas task". "Sag Anna, dass es später wird" is a message with the sender Ben. No second system next to it.

Not part of it is the **broadcast** ("Sag allen, das Essen ist fertig"). It is a loudspeaker, not a delivery: now or never, to no one in particular, not stored, without receiver settings. HA brings it as a tool (`HassBroadcast`), Mira already has it via HA's MCP. At most later: name the sender and respect quiet hours.

Not for now: "to everyone individually", i.e. the same message over each person's private channel.

## The message

| Part | Values |
|---|---|
| **Sender** | a person (recognized via their channel), an area ("jemand im Wohnzimmer", anonymous at the satellite) or Mira herself: a task, a skill, an automation (`mira.send_message`), a reminder |
| **Receiver** | a person or **the house** |
| **Content** | text; plus, as today, details, importance (0–3), confidential yes/no, expiry, key against duplicates |
| **State** | waiting → delivered → heard/done, or expired |

## Receiver: the house

Like the notices today: Mira delivers in the house.
- announcement when someone enters a room (motion sensor at the satellite), if the room is set to "announce";
- otherwise the **indicator** lights up until someone asks whether there is anything new;
- following an answer ("Licht ist an. Übrigens: …");
- morning announcement, quiet hours, escalation, urgent immediately – all as before.

There are no receiver settings here; the house accepts everything.

**Own room:** A message that someone leaves at a satellite remembers the room (`origin_area`). There it does not count as waiting for `notifier.own_message_hold` (default 10 minutes, for scheduled messages from when they are due): no announcement on entering, no LED ring, no hint on the screen, no "Übrigens", no answer to "Gibt es Neues?". Other rooms get it immediately, afterwards the own room too. Urgent messages, reminders, tasks and private routes do not know this delay. It is set in the HA options ("General", minutes; 0 = off).

## Receiver: a person

**The person's settings** (on the HA person, `options["mira"]`):

| Setting | Values | Default |
|---|---|---|
| Accept messages | from everyone (also anonymous at the satellite) / from other persons / from no one | from everyone |
| Channels | the person's notify targets (companion app, Telegram chat, Signal, …), see [Channels](kanaele.md) | none |
| Alone at home, the satellites are me | on/off | off |

- "Accept" applies to messages **from humans**. What Mira sends herself (tasks, skills, reminders, automations) always arrives; otherwise one could unsubscribe from the leak sensor warning.
- "From other persons" means: the sender is recognized as an HA person via their channel. Anonymous at the satellite ("jemand im Wohnzimmer") does not count.
- If the receiver does not accept, Mira tells the sender so honestly ("Ben nimmt gerade keine Nachrichten an") and stores nothing.

**Delivery:**
- private: via the person's [channels](kanaele.md) (HA notify targets), as today for personal and confidential notices;
- at the satellite (announcement or indicator), **if the person is recognized there**. Today recognized only means: the "alone at home" rule. **What "recognized at the satellite" will mean in future we discuss separately.**

## Wording

Mira names the sender if it is a human:
- "Anna lässt dir ausrichten: Es wird später."
- "Jemand im Wohnzimmer lässt ausrichten: …" (to the house)
- Messages from Mira herself without preamble, as today.

## Creating

- In conversation: "Sag Anna, dass es später wird", "Hinterlass fürs Haus: Der Schornsteinfeger kommt um 10." For this the model calls a tool (receiver, text); the sender comes from the turn's identity, never from the model.
- From HA: the action `mira.add_notice` will then be called `mira.send_message` (receiver instead of `audience`).
- With a point in time ("Erinnere Ben morgen um 7 an den Müll", "Erinnere mich in 10 Minuten") it is a message with a **delivery time**: it waits until then and is then delivered like any other. **Reminders are fully absorbed into messages**; the separate table and the separate delivery route for reminders are dropped. A reminder to oneself is a message from me to me (it is always accepted); if asked in a room, it is spoken there, as today.

## Renaming

This time everywhere, because the concept changes:
- Code: `notices` → `messages` (table, store, notifier texts, tools such as `notices_read` → `messages_read`); `reminders` are absorbed into it, `reminder_set` becomes sending with a delivery time.
- HA: action `mira.send_message`, sensor "Pending messages" (`sensor.mira_pending_messages`).
- Web UI and guide: tab and texts "Messages".
- No data migration (everything is test).

## What Home Assistant already has

Checked against HA 2026.10, so that we do not rebuild anything:

| HA | What it is | For Mira |
|---|---|---|
| `notify.*` (companion app, Telegram bot, …) and `notify.send_message` | transports: send a message to a device or a chat | **use** as channel ([Channels](kanaele.md)) |
| `assist_satellite.announce` / `start_conversation` | speak at the satellite | **use**, as today |
| `persistent_notification` | hint in HA's bell, the same for everyone | no channel per person; at most for admin hints |
| `alert` | repeated notification as long as a state persists | nothing equivalent; our escalation stays |
| `todo` | shared lists | something else |
| Assist timer (`HassStartTimer`) | timer on the device | stays HA's business; Mira's reminders are messages |

**What HA does not have:** a message model with sender and receiver, waiting until the receiver is reachable, delivery by presence in the room, per-person settings on whom to accept from. We build that; the **transports** Mira takes from HA.

**Offer it close to HA:** The integration provides `notify` entities, one for the house (`notify.mira_haus`) and one per person (`notify.mira_anna`). Then automations send a message through Mira with HA's own action `notify.send_message`, without a Mira-specific action; the sender is then the automation. `mira.send_message` stays for more fields (importance, confidential, expiry).

Telegram runs via HA's integration `telegram_bot`; Mira's own bot is dropped ([Channels](kanaele.md)).

## Decided

- Reminders are absorbed into messages (message with delivery time).
- **No read receipt** for the sender ("Anna hat es gehört"): out of respect for the receiver's privacy.

## Open

1. **Recognized at the satellite**: when a person counts as present at a satellite (discuss separately). Until then: only the "alone at home" rule.
