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
- "What do you know about the chimney sweep?" – "Forget the thing about the chimney sweep."

At night Mira condenses what she remembered into a short profile of the household. You can see and delete the profile and all facts in the web interface under **Memory**.

## Reminders and timers

- "Remind me tomorrow at 7 pm to take out the trash."
- "Set a timer for 10 minutes." – "Remind me about the laundry in half an hour."
- "Which reminders do I have?" – "Delete the laundry reminder."

At the time you chose, Mira speaks the reminder in the room where you asked for it – even during quiet hours, since you picked the time. Reminders set on Telegram arrive on Telegram.

## Notifications

Mira speaks up on her own when something happens: a leak sensor goes off, the washing machine is done, the trash is collected tomorrow, the watched feed has news. How she tells you depends on the room and on the importance.

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

**Quiet hours**: during quiet hours (e.g. 11:30 pm – 6:30 am) Mira announces nothing – except urgent notices and your reminders. Indicator lights still glow; what arrives at night you see in the morning.

**Quiet mode**: the "Quiet mode" switch in Home Assistant turns all announcements off until you switch it back.

**Confidential and personal**: notices can be meant for one person or be confidential. Mira only reads confidential notices aloud when the setting allows it (e.g. only when that person is home alone); otherwise they arrive privately on Telegram or as a push message on the phone.

## Standing orders

Give Mira a standing order and she keeps an eye on something for you:

- "Keep an eye on the water usage."
- "Tell me if a lot of electricity is used at night."
- "Watch the front door at night."

Mira turns it into a plan – which sensors she watches and when she speaks up. That takes about half a minute; she plans in the background and then comes back with her proposal, in the same room or on Telegram. If it suits you, say: **"Start the order"**. Only then does it run.

Then Mira keeps comparing: the usage or reading of the last hour with the same hour over the past two weeks (from Home Assistant's history), plus states and thresholds. Only when something is really out of the ordinary does she judge whether it interests you and speak up – like any other notification, and at once when there is danger (such as water running long at night). She reports the same thing at most every six hours.

- "Which orders do you have?" – "Delete the electricity order."
- In the web interface under **Orders** you see all orders with their plans and can start and delete them.

## Skills

Skills are extra abilities the setup turns on. Mira opens a skill only when a question needs it.

**Waste collection** (`abfall`): Mira knows the collection calendar.

- "When is the residual waste collected?" – "What is collected this week?"
- The evening before, Mira reminds you on her own: "The recycling is collected tomorrow."
- If the collection is today, she says so, in case the bin still has to go out.

## Hard questions

If the setup connects a stronger language model, Mira hands demanding questions to it – explanations, planning, recipes, comparisons – and sums up the result for you. The stronger model can look things up but cannot change anything in the house.

## Web interface

At `http://<mira>:8787/ui` (access token on first visit):

- **Notices**: see open and delivered notices and mark them done.
- **Orders**: what Mira watches for you, with its plan; start and delete.
- **Notifications**: quiet hours, pauses, escalation, confidential and urgent notices.
- **Watchers**: what Mira watches (sensors, feeds) and what it means.
- **Rooms**: which satellite speaks where, motion sensors, announce or indicate, indicator light.
- **People**: who gets personal notices, by Telegram or push.
- **Memory**: profile and remembered facts.
- **Guide**: this page.

If the Mira integration is set up in Home Assistant, you edit watchers, rooms and people there (Settings → Devices & services → Mira) with pickers for entities and areas; the web interface then only shows them.

## In Home Assistant

- **Device "Mira"** with the **Quiet mode** switch, the **Pending notices** sensor and the **Compact profile** button.
- Mira is a conversation agent: choose her for your satellites under Settings → Voice assistants.
- Automations can give Mira notices with the action `mira.add_notice`.

## Good to know

- Mira does not make up values: if something is missing, she says so or asks.
- Mira treats what is on the web as information, never as instructions. After a web search she performs no critical actions until you give the command again directly.
- If speech recognition gets you wrong, speak clearly and in one sentence.
