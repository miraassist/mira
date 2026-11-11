# When the satellite keeps listening – Spec

Status 2026-11-11: built (steps 1 and 2), not released: the mark in the turn's state, the fourth source in the engine, and the answers of Matter, Zigbee and the room that carry it. Scenarios: [keep-listening.feature](keep-listening.feature).

## Goal

After an answer a voice satellite either listens again (`continue_conversation`) or waits for the wake word. Today
three things decide it (`internal/conversation/engine.go`):

```go
resp.ContinueConversation = voice && !playsAfter && !stillPlaying &&
    (expectsReply(heard) || ts.ConfirmationRequested() || calledTool(added, tools.ToolKeepListening))
```

1. **A question mark** at the end of the spoken text, or of the sentence before it
   ([Streaming](streaming.md), decision 11);
2. a **confirmation** the guard asked for (the user's yes must reach this session);
3. the model called **`keep_listening`** ([Sounds and cards](satellite.md)).

The first one guesses the intent from the wording, and that is too weak. Seen 2026-11-11 at an Echo, adding a Matter
device: Mira's tool answered that the eleven digits are needed, the model said "Halte den QR-Code bereit und lies mir
die elf Ziffern daneben vor." — no question mark, so `continue_conversation` was false and the satellite stopped
listening while the user was reading the code out. The same sentence as a question ("Magst du sie mir vorlesen?")
would have worked. Whether Mira says "Wie heißt du?", "Sag mir bitte deinen Namen" or "Ohne deinen Namen komme ich
nicht weiter" makes no difference to the intent: she waits for the user's word.

**The intent is known where it arises** — in the tool that asked for the information, not in the model's wording.

## Decided (2026-11-11)

| # | Question | Decision |
|---|---|---|
| 1 | Where the intent comes from | **A tool says it, per answer.** While it runs, a tool marks the turn as "I wait for the user's word" (`tools.TurnOf(ctx).WaitForUser()`); the turn then keeps the satellite listening, however the model words the answer. Per answer, not per tool: `matter_pairing_start` asks for the digits in one case and says "plug it in" in the other. The mark lives in the turn's state (`tools.TurnState`), as the guard's confirmation does |
| 2 | The question mark | Stays as the default beside it: it catches the free answers Mira gives without a tool |
| 3 | The model's own way | `keep_listening` stays as it is. No new prompt rule: the model is not asked to call it when it wants an answer — that is what decision 1 is for |
| 4 | What beats the mark | **A finish or a cancel in the same turn:** a tool that ends the dialog says so too (`tools.TurnOf(ctx).DialogEnded()`: `thread_cancel`, a finisher such as `device_finish`), and that clears the mark |
| 5 | Which answers carry the mark | Those that ask the user for something the flow cannot go on without: `matter_pairing_start` when it asks for the digits or for the scan again, `matter_code` when the code does not fit, `zigbee_pairing_start` when it asks which network, `device_assign` when the room is unknown. Ending the dialog: `device_finish`, `thread_cancel` |
| 6 | What it does not change | The rules that end listening stay: no listening when something plays after the answer or still plays, and nothing outside a voice turn |

## What it does

1. **The mark.** `tools.TurnState` gets `WaitForUser()`, `DialogEnded()` and `WaitsForUser()`. A tool that asks the
   user for something calls the first one while it runs; one that ends the dialog calls the second. Both reach the
   turn through `tools.TurnOf(ctx)`, which every tool already has.
2. **The turn.** `ContinueConversation` takes the mark as a fourth source, in the same `||`.
3. **The end beats it** (decision 4): a turn whose thread ended, or whose call was refused, clears the mark again.
4. **Nothing else changes:** the question mark, `keep_listening`, the playing rules and the channels stay.

### Fails

| Fails | Who learns of it |
|---|---|
| A tool marks the turn, but something plays after the answer (a clip, the radio) | No listening, as today: the microphone would hear it. The flow goes on by the wake word; no line of its own |
| A tool marks the turn and the thread ends in the same turn | No listening (decision 4) |
| A tool marks a turn that is no voice turn (Telegram, an agent) | Nothing happens: the mark only reaches a satellite |

### Cost

Nothing: a flag on a tool, a bool in the turn's state, no call, no model round.

## Scenarios

`KL-01` to `KL-06` in [keep-listening.feature](keep-listening.feature).

## Steps

1. **The mark and the turn:** the field, the turn state, the fourth source, decision 4. KL-01 to KL-04.
2. **The tools of decision 5** carry it, and their specs say so. KL-05, KL-06.
