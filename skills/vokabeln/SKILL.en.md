---
description: "Quiz and practise vocabulary or the times tables (\"Test me on my German vocabulary\", \"Practise the seven times table with me\"); wrong ones come again at the end."
---
# Practising vocabulary

You test what the user wants to learn: vocabulary, the times tables or a list of
terms. The practice is a thread with a plan: the plan holds the list, and the
context says what is up.

1. **Where the list comes from:**
   - The user says it ("Hund dog, Katze cat, …") or names a times table.
   - Or it is in what Mira knows ("this week's vocabulary"): look it up with
     `memory_search`.
   - Otherwise ask briefly, with one question, how many words it should be and
     whether easy or hard, and then make up fitting ones yourself: easy are
     everyday words (dog, house, water), hard rarer ones (umbrella, pavement,
     impatient). If the foreign language is not said, German.
2. **Direction** for vocabulary: without a wish mixed, about half from the
   user's language into the foreign one, the other half the other way round, in
   no order. If the user names a direction, only that one.
3. **Write the plan** with `thread_plan` (action `write`):
   - the heading "# Vocabulary: German" (or "# Times table: 7s");
   - a line "Right: 0 of N";
   - one checkbox per word with question and answer, in the direction you ask:
     `- [ ] dog (Answer: Hund)`, `- [ ] Regenschirm (Answer: umbrella)`;
     for a times table mixed, not in order: `- [ ] 7 times 8 (Answer: 56)`.
4. **Ask:** Read out the current question, without the answer.
5. **Judge the answer:** Also right is what only speech recognition twisted: a
   small recognition mistake ("cat" as "cut"), or a word of the other language
   written as a similar-sounding word of the language you are speaking ("Haus"
   as "house", "Hund" as "hunt"). If the answer sounds like the solution, it is
   right.
   - Right: "Right!"; tick the checkbox. If it was the first try (the line has no
     "✗" yet), count "Right" up by one.
   - Wrong: say the answer. The checkbox stays open: move the line to the end of
     the list and add "✗" if it has none yet (`- [ ] dog (Answer: Hund) ✗`). No
     new line; the list stays as long as it is, and the word comes again after
     the others.
   Then write the plan anew (`write`) and read out the next open question right
   away.
6. **End,** when all checkboxes are ticked: first play a sound with
   `sound_play`, depending on how many were right the first time: all
   `fanfare_big`, from 80 % `fanfare`, otherwise none. Then say how many were
   right the first time, and name the ones that were hard (those with "✗").
   Offer to remember them ("Remember: Ben is still practising Hund and Pferd");
   remember them only after a yes, with memory_save. Then end the thread with
   `thread_cancel` if the user does not want to go on.
