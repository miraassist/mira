---
description: "Play a quiz, alone or with several players, with points (\"Let's play a quiz\", \"Ask me questions about animals\"); then it goes on with the answer, \"next\" or \"What's the score?\"."
---
# Quiz

You play a quiz with the user, alone or with several players. The quiz is a
thread with a plan: the plan holds the questions and the points, and the context
says which question is up.

1. **Clarify briefly first**, with one question, only what has not been said: the
   topic (or "mixed") and who is playing (names). With only one player you need no
   names. If what Mira knows says how old the players are, fit the questions to them.
2. **Write the plan** with `thread_plan` (action `write`):
   - the heading "# Quiz: topic";
   - a line "Points: Anna 0, Ben 0" (alone: "Points: 0");
   - ten questions as checkboxes, each with its answer:
     `- [ ] What is the capital of France? (Answer: Paris)`.
   Questions with a short, clear answer; no trick questions. With several players
   they take turns: put the name before the question ("Anna: …").
3. **Questions:** Read out the current question, without the answer.
4. **Judge the answer:** If it is right (also in other words or with a small
   mistake), say "Right!"; otherwise say the right answer. Then write the plan anew
   (`write`): the points updated, this question ticked off, the ones already ticked
   off stay ticked off. Then read out the next question right away.
   - "next" or "I don't know": say the answer, no point, next question.
   - "What's the score?": the points from the plan.
5. **End:** After the last question, first play a sound with `sound_play`,
   depending on the share of right answers (with several players, all together):
   all right `fanfare_big`, from 80 % `fanfare`, below 50 % `too_bad`, in between
   none. Then the points, with several players who won, and whether another round
   is wanted (then a new plan with new questions). If nobody wants to play on, end
   the thread with `thread_cancel`.
