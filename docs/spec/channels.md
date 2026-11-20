# Channels – spec

Status 2026-10-16: built in 0.6.2–0.6.3. Two abstractions carry Mira's communication: **channel** (this document) and **message** ([Messages](messages.md)). Builds on [Mira in Home Assistant](ha-integration.md): what HA can do, Mira does not rebuild.

## Principle

A **channel** is a way through which Mira reaches or hears someone. Each channel can either
- **only send**: it is enough to deliver a message; or
- **hold a conversation**: it sends and receives, so also turns.

The service knows only this model: "incoming text from channel X, conversation Y, person Z" and "answer/message to channel X, target Y". Which events, actions or APIs are behind it, it does not know.

## The channels

| Channel | Direction | From where | Person |
|---|---|---|---|
| **notify** – every HA notify target (Companion app, Telegram chat, Signal, Slack, Threema, …) | send only | HA's `notify`, Mira builds nothing | the person the target is assigned to |
| **Satellite** | conversation (voice); delivery as announcement or display | HA's Assist pipeline and `assist_satellite` | anonymous; recognized only by the "alone at home" rule (more open) |
| **HA chat** (Assist in the app, Mira in the side menu) | conversation | HA's conversation agent | via the logged-in HA user |
| **Telegram** | conversation and send | **adapter on HA's integration `telegram_bot`** | via the chat assigned to the person |

The HA chat is not suitable for delivery: Mira cannot write anything into it on her own.

## Telegram via Home Assistant

- Telegram is set up once in HA (integration `telegram_bot`: bot token, allowed chats). Mira runs **no bot of her own anymore**; Mira's own Telegram logic (`internal/channels/telegram`, `telegram:` in `mira.yaml`) is dropped entirely, also without a replacement for installations without HA.
- An **adapter in the Mira integration** (a self-contained piece in the HA process) connects both:
  - incoming: listens to `telegram_text` (chat ID, sender, text), assigns the chat to a person and passes the text to Mira as a turn (channel `telegram`, conversation = the chat's notify entity, [The chat's history](telegram-history.md));
  - outgoing: sends Mira's answer back with HA's Telegram actions (formatting, splitting long answers), shows "typing …" meanwhile (`send_chat_action`).
- **Only chats that belong to a person** talk to Mira. The adapter ignores other chats (even ones allowed in HA); the safeguard is thus HA's list of allowed chats plus the assignment to a person.
- The dependency on HA's event names and structure sits only in this adapter and its tests. `telegram_text` is a public, documented interface; HA announces changes as a "breaking change".
- Later: **voice messages** (`telegram_attachment`, download, speech recognition, as a turn). Noted, not now.

### Telegram's Markdown in a message Mira sends (2026-11-20)

HA's `telegram_bot` sends with its parse mode, `markdown` (Telegram's first Markdown), and Telegram refuses a whole
message whose Markdown does not parse: `Can't parse entities: can't find end of the entity …`. A single `_` does it, as
in a YouTube address (`watch?v=wSFJwZ9_fGg`) or an X account (`@max_muster`); seen on live 2026-11-20 with the list of
the skill youtube, and checked on the test HA the same day (the text with `_` refused, the same with `\_` sent).

Decided (2026-11-20, the maintainer): **the Markdown stays, Mira makes it parse**. Before a message goes to a channel
that is a notify entity of the platform `telegram_bot` (its entry in the entity registry, `config/entity_registry/get`,
one call per channel and delivery), Mira escapes with a backslash what would open an entity that never ends:

- in an address (`http://…`, `https://…`) every `_` and `*`;
- a `_` or `*` between two letters or digits (`max_muster`);
- then, per sign (`_`, `*`, `` ` ``), when the rest still has an odd number, all of that sign: the text stays readable,
  that sign loses its formatting;
- a `[` that no `](` follows.

The target of a link, the `(address)` of `[text](address)`, is left as it is and not counted: Telegram takes it
unparsed (checked on the test HA 2026-11-20: `[Video](…wSFJwZ9_fGg)` sent).

Balanced Markdown (`_kursiv_`, `*fett*`, `` `code` ``, `[Text](Adresse)`) stays as it is. A channel that is no
Telegram entity (the companion app, a legacy `notify.<name>` service) gets the text unchanged. When the entity's
platform cannot be read, the text goes unchanged and the log says so (`could not read the channel's platform`); a
refusal of Telegram then ends the delivery as below.

A refusal `Can't parse entities` is **fatal** ([Retries](retry.md), row 1): the same text fails every time, so the
delivery ends at once as failed, with its log line, instead of ten attempts.

## Channels of a person

On the HA person (`options["mira"]`, see [ha-anbindung](ha-integration.md)):
- **Channels**: a list of notify targets that belong to this person, e.g. `notify.mobile_app_anna` and the Telegram chat `notify.telegram_anna`. Replaces the previous fields "Telegram ID" and "push service".
- If a Telegram chat is among them, it is at the same time Anna's **conversation channel** via Telegram: this is how the adapter recognizes that an incoming chat is Anna.
- Delivery happens privately via the person's channels (all, or the first one that works – to be decided when building), at the satellite only if she is recognized there.

## Idea for Home Assistant

HA has only half the abstraction for messengers: `notify` for sending; receiving is a separate event per integration (Telegram complete, Matrix only commands, Signal/Discord/Slack/Threema/XMPP not at all). A proposal to HA (PR or architecture discussion) for a common receiving path – for example "incoming message" as a uniform event or a conversation platform for messengers – would make adapters like ours unnecessary. Just an idea, noted.
