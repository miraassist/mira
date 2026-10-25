# Speaker recognition at the satellite

Status 2026-10-25: steps 1 to 4 built (the service, matching, Mira's identify, enrolling on the card "Ich");
optional: without `speaker_id` in `mira.yaml` everything works as before and the card says how to set it up.
Scenarios: [sprecher.feature](sprecher.feature). User guide: "Wer spricht: Mira erkennt Stimmen".

## Goal

At a satellite Mira is talking to someone anonymous, unless the "alone at home" rule says who it is
([users](benutzer.md)). With speaker recognition she knows who spoke from the voice: "Erinner mich morgen an den
Müll" at the kitchen Show becomes a reminder for the person who said it, even with the whole family at home.

A recognized voice counts exactly as the "alone at home" rule does: the person's memory, their personal tasks,
reminders and notices. Nothing more. A voice is a probability, not a login.

## Not part of it

- **Authentication.** A recognized voice opens nothing that "alone at home" does not open. It never stands in for
  the HA user, and Mira does not pass it on as one.
- **Listening between requests.** Only what is said to the satellite after the wake word is looked at, the same
  audio that goes to speech recognition.
- **Mira's database.** Voice prints are biometric data and stay in the service, in one place where they can be
  seen and deleted. Mira only gets the result of a turn and keeps nothing of it, not even a history.
- **The TECHO5 firmware.** Its maintainer decided against speaker recognition on the device (TECHO5 issue #200):
  it belongs to Home Assistant's side, as a proxy for speech recognition.

## The service: `services/speaker-id`

A Wyoming speech-to-text server that Home Assistant uses like any other, in front of the real one:

```
satellite → Home Assistant (Assist pipeline) → speaker-id :10310 ──audio──→ STT (faster-whisper :10300)
                                                     │                               │
                                                     │ beside it: voice print,       │ transcript, unchanged
                                                     │ compared with the profiles    │
                                                     └──────────── HA ←──────────────┘
```

- **The transcript is the upstream's,** unchanged and not delayed: the voice print takes 50 to 100 ms on the CPU and
  runs while the STT works (about a second).
- **Model:** a speaker embedding model through sherpa-onnx (WeSpeaker ResNet34 to start with, about 25 MB; others
  of sherpa-onnx's speaker models by setting).
- **Per utterance** it finds the best and the second-best profile with their scores (cosine, 0 to 1), and
  nothing under one second of speech. **Who that is, is Mira's decision** (below), with her thresholds; the
  service's page shows a decision of its own only to help enrolling.
- **Profiles** are kept per **HA person**, by the person's id (the key of [users](benutzer.md), e.g. `anna`): several samples
  each, the profile is their mean. They are in a JSON file in the service's data directory, outside the repository
  and outside Mira.
- **No audio is kept.** The service remembers the voice prints and transcripts of the last 30 utterances in memory,
  so that they can be assigned to a person, and nothing else.
- **Enrolling** happens in Mira's page (below); the service has no page of its own. Its API is behind a token
  (`SPEAKER_TOKEN`), which only Mira has.
- **Deployment:** its own `compose.yaml`; the data directory is a setting (`SPEAKER_ID_DATA`).

## Which utterance is the turn's

The service does not know which satellite spoke: Home Assistant does not tell a speech-to-text server. It does not
have to. Home Assistant hands the transcript on to the conversation agent unchanged, so Mira asks the service for
the utterance with **the turn's text** among the last few seconds:

- `GET /api/match?text=<the turn's text>&within=10` answers with every utterance of that text no older than 10
  seconds, latest first, each with its best and second-best person and their scores; an empty list when there is
  none.
- Texts are compared after trimming, folding case and collapsing whitespace.
- Several utterances with the same text in that window are several satellites hearing the same request. With
  TECHO5's "Nearest device answers" (on by default, with a house word) only the nearest device sends its audio, so
  this is rare; it happens when that fails, with satellites without TECHO5, or with a wake word Home Assistant
  heard itself. They are the same person heard twice: the person counts only if every one of them does (below).
- A turn that did not come through the service (typed in the app, Telegram, a pipeline with another STT) finds
  nothing, which is the same as `unknown`.

## Who is speaking, in Mira

`identify` (who speaks in a turn) gets one more source, in this order at a satellite:

1. the signed-in HA user, if Home Assistant names one;
2. **the voice**, if every utterance of the turn's text names the same person, at or above the threshold and
   at least 0.05 ahead of the second-best person, and the person exists in Home Assistant;
3. the "alone at home" rule;
4. otherwise anonymous.

Anything else (below the threshold, too close to the second, satellites that disagree, nothing found), no answer,
a service that is down or slow: on to 3. The question to the service has a short
timeout (300 ms) and never holds up the turn.

- **Asked by the agent itself,** not by the HA integration: the agent has the turn's text and decides who speaks
  already, so the integration and `converse` stay as they are.
- **Setting** in `mira.yaml`, without which nothing changes:

  ```yaml
  speaker_id:
    url: http://192.168.x.y:10311   # the service's API
    threshold: 0.7                  # the score a voice needs; 0.7 if left out
  persons:
    ben:
      voice_threshold: 0.65         # a voice that scores lower than most
  ```

- **The threshold** is Mira's: one for everyone, and one for a person whose voice scores lower or higher than
  most. 0.7 to start with: with the first model, enrolled persons reached about 0.8, while a voice that was not
  enrolled (a synthetic one) still reached 0.55.

- **Log:** one line per turn at debug level with person and score, nothing stored.

## Naming the person

What Mira does for a person she only recognized can go to the wrong one, and nobody would notice. So the turn
tells the model how she knows the person, and to name them when it does something personal for them:

- **Recognized** (by the voice or by "alone at home"): "Es spricht: Ben (an der Stimme erkannt)", or "(allein zu
  Hause)", then what is the person's alone (remembering something, reminders, tasks, messages for them, "ich",
  "mir", "mein"), and: name them briefly when doing something personal like that, so a mix-up is noticed;
  otherwise not, and not in every answer. "Licht an" gets no name, "Merk dir …" does ("Hab ich mir für dich
  gemerkt, Ben").
- **Told** (signed in to the app, or writing from their messenger): the same line without the source and without
  naming.
- **No score and no "not quite sure":** what reaches the model has passed the threshold already, and a vague doubt
  would leave the model nothing to do with it. Asking back on a narrow score would be a rule of its own in the
  code, not the model's judgment.

## Enrolling in Mira's page: the card "Ich"

Every person has a card: on the start page as **"Ich"** for whoever is signed in (if Home Assistant links a person
to their login), and for admins from the list of persons, which shows a line per person with their voice and
settings and opens the same card.

- **Settings:** a person sets their own "alone at home, the satellites are me" and whose messages they take; the
  channels and the voice threshold are an admin's. Inside Home Assistant the integration keeps them on the HA
  person, as before; a person's own go through `/api/mira/settings/me`, which takes only those two fields.
- **Voice:** how many sentences are enrolled; "Stimme anlernen" and "Stimme löschen". Enrolling opens a window
  of three minutes: the card shows what the satellites hear **from that moment on**, nothing said before, and
  each line can be taken as this person's ("Das war ich"). Only those lines can be enrolled. Three to five
  sentences of a few seconds, said at a satellite with the wake word, so the profile is made through the same
  microphone and echo cancellation it is recognized through.
- **Rights:** everyone their own voice; an admin anyone's, a child's without a login too. Mira's API checks
  this (`/v1/ui/voice/{person}`), not only the page.

## Steps

1. **The service** as a proxy with its own enrollment page: built.
2. **Matching by text and persons as keys:** `/api/match`; profiles are named by the HA person's id. The service
   gets no access to Home Assistant for this: the page takes the id as typed, and Mira ignores an id Home
   Assistant does not have (SP-06). Choosing from Home Assistant's persons comes with enrolling in Mira's page
   (step 4).
3. **Mira:** the setting and the source in `identify`; scenarios SP-01 to SP-10.
4. **Enrolling in Mira's page:** the card "Ich" and the admins' list of persons (above); the service's own page
   goes, the API gets a token.
5. **A better model,** if short requests (one to two seconds, the usual case) turn out too uncertain.
