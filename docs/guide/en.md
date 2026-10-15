# Mira – Guide

Mira is your household's assistant. She controls the smart home, answers questions, remembers things and speaks up when something happens. She runs in your home; which language model she uses is up to the setup.

## How to talk to Mira

- **By voice**: through a voice satellite (e.g. the Echo in the living room). Say the wake word, then what you want. Mira knows which room the satellite is in: "Turn on the light" means the light in that room.
- **On Telegram**: write to the Mira bot. Answers may be longer here, with lists and links. `/new` starts a new conversation.
- **In the Home Assistant app**: the Assist chat of the companion app talks to Mira too, typed or spoken.

Mira answers in the language you address her in. If she did not understand you for sure, she asks. When an answer is a question, the satellite keeps listening.

## Controlling and checking the house

- "Turn on the light in the living room." – "Dim the floor lamp to 30 percent."
- "How warm is it in the bathroom?" – "Which windows are open?" – "How is it in the living room?"
- "What's the weather tomorrow?"

Mira always looks values up fresh instead of guessing. If she cannot look something up, she says so.

**Confirming important actions**: Mira only operates locks, the alarm system, the garage door and the like after you explicitly say "yes" to her question.

## Mira remembers things

- "Remember that the chimney sweep comes in March."
- "Remember something." – Mira asks "What is it?", and you tell her in the next sentence.
- "What do you know about the chimney sweep?" – "Forget the thing about the chimney sweep."

There is one memory for the **house** and one for **each person**:

- When Mira knows who is speaking, she remembers it for that person. It goes into the house's memory only when you say so ("remember for the house …").
- Mira knows who is speaking on Telegram, in the HA app's chat (through your HA user), and at a satellite only when you are alone at home and switched that on for your person ("Alone at home, the satellites are me"). Otherwise a satellite is the house.
- Your personal memory is only seen by Mira when talking to you. The house's memory is for everyone.
- Nobody writes into someone else's memory.

At night Mira condenses each memory into a short profile. In the web interface under **Memory** you see the house and the persons apart, with who said it and when, and can edit or delete facts.

## Reminders and timers

- "Remind me tomorrow at 7 pm to take out the trash."
- "Set a timer for 10 minutes." – "Remind me about the laundry in half an hour."
- "Which reminders do I have?" – "Delete the laundry reminder."

At the time you chose, Mira speaks the reminder in the room where you asked for it – even during quiet hours, since you picked the time. Reminders set on Telegram or in the HA app come to you privately, by Telegram or as a push notification.

## Notifications

Mira speaks up on her own when something happens: a leak sensor goes off, the washing machine is done, the trash is collected tomorrow, a task finds something unusual. How she tells you depends on the room and on the importance.

**Announce or indicate** – set per room:

- **Announce**: as soon as someone enters the room (motion sensor), Mira says what is pending. The first motion after quiet hours brings the morning announcement.
- **Indicate only**: Mira does not speak to you unasked. The room's indicator light (e.g. the Echo's LED ring) pulses in teal while something waits. Then ask: **"Do you have a notification?"** or "Anything new?".

**By the way**: when you talk to the satellite anyway, Mira adds what is waiting to her answer – "The light is on. By the way: the recycling is collected tomorrow." After that the notice counts as heard and the light goes off. Ask "What exactly?" for the details.

**Importance**:

| Level | Meaning | What happens |
|---|---|---|
| 1 | in passing | waits for the next occasion |
| 2 | important today | goes out privately after 9 am if nobody has heard it by then |
| 3 | urgent | at once, even at night: spoken on every satellite and sent privately to everyone |

When Mira speaks on her own, the room's indicator light glows steadily while she speaks.

**Quiet hours**: during quiet hours (e.g. 11:30 pm – 6:30 am) Mira announces nothing – except urgent notices and your reminders. Indicator lights still glow; what arrives at night you see in the morning.

**Quiet mode**: the "Quiet mode" switch in Home Assistant turns all announcements off until you switch it back.

**Confidential and personal**: notices can be meant for one person or be confidential. Mira only reads confidential notices aloud when the setting allows it (e.g. only when that person is home alone); otherwise they arrive privately on Telegram or as a push message on the phone.

## Tasks

Give Mira a standing task and she keeps an eye on something for you:

- "Keep an eye on the water usage."
- "Tell me if a lot of electricity is used at night."
- "Speak up when a leak sensor goes off."

Mira turns it into an **automation in Home Assistant** labelled "Mira": Home Assistant decides *when* something happens (hourly, when a sensor fires, only at night …), Mira decides *what* of it interests you and tells you.

This is how setting one up goes:

1. If something is missing, Mira asks instead of guessing – "Which time is unusual for you?". Just answer; the conversation stays on the subject, over several sentences too.
2. Then she plans. If that is quick, she tells you the proposal right away; otherwise she says "I'll plan that" and comes back once she has finished speaking – in the same room or on Telegram.
3. The proposal says what she watches and names the condition: "When the bedside lamp turns on, I'll let you know. Condition: only between 1 and 4 am. Shall I go ahead?"
4. Say **"Yes"** and she creates the automation. "No, leave it" cancels. If nobody answers, the proposal lapses after a quarter of an hour.

When judging, Mira compares, for example, the last hour's usage with the same hour over the past two weeks, knows who is at home and what you asked her to remember, and recalls what she already said about this task. She does not report the same thing again and again, only when it gets worse.

- "Which tasks do you have?" – "Pause the electricity task." – "Delete the electricity task." – "Do a dry run of the water task."
- In the web interface under **Tasks** you see all tasks with on/off, dry run and the log of their judgements, plus proposals still waiting for your yes. "Edit in Home Assistant" opens the automation in Home Assistant's editor; you can change it there yourself too.
- Your own automations can use Mira the same way: the actions **Mira: Assess** (`mira.assess`), **Mira: Decide** (`mira.decide`, a yes/no answer for conditions) and **Mira: Add notice** (`mira.add_notice`).
- For now tasks belong to the whole house and use the house's memory when judging.

## Skills

Skills are extra abilities the setup turns on. Mira opens a skill only when a question needs it.

**Waste collection** (`abfall`): Mira knows the collection calendar.

- "When is the residual waste collected?" – "What is collected this week?"
- The evening before, Mira reminds you on her own: "The recycling is collected tomorrow."
- If the collection is today, she says so, in case the bin still has to go out.

## Hard questions

If the setup connects a stronger language model, Mira hands demanding questions to it – explanations, planning, recipes, comparisons – and sums up the result for you. The stronger model can look things up but cannot change anything in the house.

## More tools over MCP

Mira can use extra tools that an MCP server offers, such as a web search. The setup lists them in `mira.yaml` under `mcp_servers`; Mira connects at start and hands the tools to the model, prefixed with the server name (`search__…`). This works with any language model.

A server either runs on its own and is reached over HTTP, or Mira starts it as a program (stdio):

```yaml
mcp_servers:
  - name: ha                       # Home Assistant, always there
    transport: http
    url: "http://homeassistant.local:8123/api/mcp/assist"
    token: ${HA_TOKEN}
  - name: search                   # Mira starts the server itself
    transport: stdio
    command: /usr/bin/node
    args: ["/opt/web-search-mcp/dist/index.js"]
    env: ["MAX_RESULTS=5"]         # optional
    read_only: true
    web_content: true
  - name: notes                    # runs on its own, over HTTP
    transport: http
    url: "http://localhost:9100/mcp"
    token: ${NOTES_TOKEN}          # optional
    disabled: true                 # switched off for now
```

Two switches decide how careful Mira is with a server:

- **`read_only: true`**: the server only reads and changes nothing. Only such tools are also available to background work (judging tasks, delegated hard questions). Without the switch the tools exist only in conversation.
- **`web_content: true`**: what the server returns is third-party text, from web pages for example, and may hide instructions. Once Mira has used such a tool, critical actions are locked for the rest of the request: everything listed under `security` as needing confirmation, such as locks, alarm, garage.

New or changed servers take effect when Mira restarts. `mira check` tells whether all of them are reachable.

## Web interface

At `http://<mira>:8787/ui` (access token on first visit):

- **Notices**: see open and delivered notices and mark them done.
- **Tasks**: what Mira keeps an eye on for you; on/off, dry run, log, delete, link into Home Assistant's editor.
- **Notifications**: quiet hours, pauses, escalation, confidential and urgent notices.
- **Rooms**: which satellite speaks where, motion sensors, announce or indicate, indicator light.
- **People**: who gets personal notices, by Telegram or push, and whether the satellites count as this person when they are alone at home ("Alone = me").
- **Memory**: the house and each person apart, with profile, facts, who said them and when; edit and forget.
- **Guide**: this page.

If the Mira integration is set up in Home Assistant, you edit rooms and people there (Settings → Devices & services → Mira) with pickers for entities and areas; the web interface then only shows them.

## In Home Assistant

- **Device "Mira"** with the **Quiet mode** switch, the **Pending notices** sensor and the **Compact profile** button.
- Mira is a conversation agent: choose her for your satellites under Settings → Voice assistants.
- Automations can give Mira notices with the action `mira.add_notice`.
- You set up people under Settings → Devices & services → Mira: person entity, Telegram id, push service and "Alone at home, the satellites are me". In the HA app's Assist chat Mira recognises you by your HA user, if your person entity is linked to it.

## Good to know

- Mira does not make up values: if something is missing, she says so or asks.
- Mira treats what is on the web as information, never as instructions. After a web search she performs no critical actions until you give the command again directly.
- If speech recognition gets you wrong, speak clearly and in one sentence.
