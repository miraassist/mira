---
description: "Play a quiz, alone or with several players, with points (\"Let's play a quiz\", \"Ask me questions about animals\"); then it goes on with the answer, \"next\" or \"What's the score?\"."
random:
  difficulty: [easy, medium, hard]
  topic:
    Animals: [mammals, birds, sea animals, insects, reptiles, dinosaurs, pets, animal records]
    Geography: [capitals, rivers and lakes, mountains, countries and flags, islands, Europe, deserts and climate]
    Nature: [plants, trees, weather, seasons, volcanoes and earthquakes, rocks and crystals]
    Space: [planets, stars, the moon, space travel, the sun]
    History: [Stone Age, antiquity, knights and castles, explorers, inventions, 20th century]
    Science: [the human body, chemistry at home, physics at home, numbers and maths]
    Sport: [football, Olympic Games, winter sports, ball games, records]
    Music: [instruments, composers, pop music, children's songs]
    Art and literature: [painters, fairy tales, children's books, myths and legends]
    Film and TV: [cartoons, children's series, film classics, film characters]
    Food and drink: [fruit and vegetables, dishes from around the world, baking, where food comes from]
    Language: [sayings, word meanings, loanwords, languages of the world]
    Technology: [vehicles, computers, buildings, household devices]
  question: [who, what, where, when, how many, which is bigger or faster, true or false, odd one out]
---
# Quiz

You play a quiz with the user, alone or with several players. The quiz is a
thread with a plan: the plan holds the questions and the points, and the context
says which question is up.

1. **Clarify briefly first**, with one question, only what has not been said: the
   topic (or "mixed") and who is playing (names). With only one player you need no
   names. If what Mira knows says how old the players are, fit the questions to them.
2. **Draw the questions:** call `random_pick` with skill `quiz`, `count` 10.
   Under `fixed` give what is settled: `topic`, if one was named (also one that is
   not in the choice; for "mixed" leave it out, then it is drawn); `difficulty`,
   if said. Each question follows its line: topic, difficulty and the form of
   the question (`question`), and fits the players' age (1); if it is not known,
   for adults. If a form does not fit the topic,
   take the closest one.
3. **Write the plan** with `thread_plan` (action `write`), the questions in the
   order of the lines:
   - the heading "# Quiz: topic";
   - a line "Points: Anna 0, Ben 0" (alone: "Points: 0");
   - ten questions as checkboxes, each with its answer:
     `- [ ] What is the capital of France? (Answer: Paris)`.
   Questions with a short, clear answer; no trick questions. With several players
   they take turns: put the name before the question ("Anna: …").
4. **Questions:** Read out the current question, without the answer.
5. **Judge the answer:** If it is right (also in other words or with a small
   mistake), say "Right!"; otherwise say the right answer. Then write the plan anew
   (`write`): the points updated, this question ticked off, the ones already ticked
   off stay ticked off. Then read out the next question right away.
   - "next" or "I don't know": say the answer, no point, next question.
   - "What's the score?": the points from the plan.
6. **End:** After the last question, first play a sound with `sound_play`,
   depending on the share of right answers (with several players, all together):
   all right `fanfare_big`, from 80 % `fanfare`, below 50 % `too_bad`, in between
   none. Then the points, with several players who won, and whether another round
   is wanted (then draw anew as in 2 and a new plan). If nobody wants to play on, end
   the thread with `thread_cancel`.
