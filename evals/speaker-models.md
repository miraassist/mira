# Speaker models tried by hand

Speaker models for the speaker-id service (`SPEAKER_MODEL`, [Speaker id](../docs/spec/speaker-id.md)), tried with
real voices at a satellite. There is no eval run for them: the scores come from the service's log (one line per
utterance with the best person, the score and the length of speech) and from what Mira decided with the threshold.

**This is hardly objective.** One enrolled voice, one satellite, one room, a few dozen utterances, and nobody
spoke who was not enrolled, so how high a stranger scores is unknown. Scores of different models are not
comparable either: each model has its own spread, so a threshold chosen for one does not carry over. What the
table says is how a model behaved with the default threshold of 0.7.

| Model | Tried | Scores of an enrolled voice | Verdict |
|---|---|---|---|
| `wespeaker_en_voxceleb_resnet34_LM.onnx` (about 25 MB, the default) | 2026-10 | enrolled persons about 0.8; a voice that was not enrolled (a synthetic one) 0.55; a follow-up answer of 9.8 s once 0.565 (2026-10-26) | worked, but recognition was not always convincing in daily use (the maintainer's impression, no numbers kept) |
| `nemo_en_titanet_large.onnx` | 2026-11-06, an Echo Show (TECHO5), German | 0.52–0.68 with 4 enrolled sentences; 0.71–0.80 for whole requests with 6 to 9 (below) | better by impression: with 9 sentences 4 of 5 free requests passed 0.7; short answers stay below |

## TitaNet large, 2026-11-06

One person enrolled anew (profiles of another model do not fit: changing the model means enrolling again). Times
are local, scores as the service logged them; "enrolled" means the utterance was then added to the profile.

| Stage | Utterances | Length | Scores | Passed 0.7 |
|---|---|---|---|---|
| 4 sentences enrolled | 2 free requests ("Weißt du, wer ich bin?", …) and the enrolling sentences scored against the profile so far | 2.7–4.1 s | 0.628–0.684 | 0 of 5 |
| 6 sentences | "Wie spät ist es gerade?", "Wie wird das Wetter morgen?" (both then enrolled) | 3.0–3.1 s | 0.736, 0.711 | 2 of 2 |
| 6 sentences | "Alles klar." | 3.7 s, mostly silence | 0.519 | no |
| 6 sentences | a free request of 3.4 s; a long one of 5.9 s | 3.4 / 5.9 s | 0.619 / 0.801 | 1 of 2 |
| 9 sentences | three enrolling sentences (then enrolled) | 3.1–3.3 s | 0.746–0.796 | 3 of 3 |
| 9 sentences | five free requests, 17:26–17:27 | 3.1–5.0 s | 0.644, 0.731, 0.728, 0.711, 0.764 | 4 of 5 |

What the log shows:
- **More enrolled sentences raised the scores** of the same kind of request from about 0.65 (4 sentences) to about
  0.73 (9 sentences).
- **Short answers score low**: "Alles klar." 0.519. A follow-up in a running conversation does not need to pass on
  its own (the speaker stays, [Speaker id](../docs/spec/speaker-id.md)), a first request does.
- **One-word answers fall much lower**: in a vocabulary test the same person's one-word answers scored 0.366 and
  0.38 (reported by the maintainer from the service's log). The test still went well: the speaker stayed for the
  running conversation, as specified.
- **No second profile and no stranger**, so there is no margin to the second-best and no score of a voice that
  should be refused. Whether 0.7 keeps strangers out with TitaNet is not known.

## Open

- A measurable comparison needs the same recorded utterances scored by each model: several enrolled persons, short
  and long requests, and voices that are not enrolled (Phonon's 14 voices could stand in for strangers). Not built.
- The enrolling page asks for 4 of 8 fixed sentences (`enrollSentences`, `internal/ui/voice.go`), all short; a
  second round repeats some of them.
