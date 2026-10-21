# Language – spec

Status 2026-10-16: built in 0.6.14. Applies to all channels. See also [Context](kontext.md) and [Messages](nachrichten.md).

## Principle

Mira speaks the language that is spoken. In the house, it is not "the house" that has a language, but each conversation. There is no separate setting for the language.

The language determines Mira's own texts: her rules and the channel style in the prompt, fixed sentences ("Übrigens: …", hints), announcements and what she writes on her own. That she answers in the language in which she is addressed is governed, as before, by a line in her rules.

## Which language when

| When | Language |
|---|---|
| **Conversation** (satellite, chat of the HA app) | the language HA sends with the request (that of the Assist pipeline) |
| **Announcement at a satellite** | the language last spoken at this satellite |
| **Message to a person** (channels, escalation, Telegram) | the language this person last spoke |
| **everything without satellite and person** (condensing the house profile, judgments of tasks) | the language last spoken at all |
| **never spoken yet** (first start) | HA's server language (Settings → System → General) |

- **Telegram** has no pipeline. A conversation there takes the person's last spoken language.
- **The person is only known if they are unambiguous**: via Telegram, in the chat of the HA app, at the satellite by the "alone at home" rule. No further recognition logic is added.
- The agent reads the server language via HA's WebSocket (`get_config`), without admin rights.

## Last spoken: persistent

Sessions expire and are deleted. The last spoken language is therefore **stored separately** and survives that:
- per satellite,
- per person,
- overall.

Every turn with a language from HA updates the three entries, as far as satellite and person are known.

## What changes

- `prompt.language` no longer exists. Mira runs only with HA; there is no need to set a language.
- The prompt has its own fixed beginning per language. With a local model, a switch between languages breaks the cache; with one language in the house nothing changes.
- What is stored stays in the language in which it was created: facts, message texts. The model rephrases an announcement anyway, in the language of the announcement.

## What only the model reads

Not everything follows the language of the conversation. What only the model reads is built once at startup, in the language last spoken at that time (otherwise the server language): the descriptions of Mira's tools, the texts they return to the model, the guard for questions back, Mira's own structure block, the finishes of threads. What the human hears or reads, on the other hand, follows the conversation, the room or the person: the prompt (rules, channel style, examples, hints in the turn), fixed sentences, announcements, reminders, the text on the screen.

**Open for mixed-language households:** For the model, a German tool description in an English conversation is normally not a problem. It may be, however, that in a household in which two languages are spoken it does interfere, for example when a small local model then mixes words from tool results into the answer. Then tools, guard and structure block would also have to be kept per language.

## Deliberately not

- **The language from a person's HA profile**: it is in HA's user data, which Mira's HA user may not read for others. What someone last spoke is closer anyway.
- **Looking up the pipeline per satellite**: the language last spoken there is enough.
- **A setting for it.**
