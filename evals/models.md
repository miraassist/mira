# Models tried by hand

Models tried in voice turns on the test Home Assistant without an eval run (docs/spec/evals.md): what they did,
with the evidence from Mira's log, her chat history and Home Assistant's state history. A model that already fails
here gets no eval run. The eval results are in the [overview](README.md).

| Model | Provider | Tried | Speed | Tools and threads | Verdict |
|---|---|---|---|---|---|
| `gemini-flash-latest` | Google Gemini | 2026-11-05, radio, stop, music | rounds 1.5–2 s at 18:40; 3–15 s from 19:10 on, without a retry (the log has no `model call failed, trying again`) | right tool each time (`radio_play`, `HassMediaPause`, `search_media` → `play_media`) | good; its speed varies a lot, which a live demo cannot afford |
| `qwen/qwen3-30b-a3b-2507` | local, llama.cpp | 2026-11-05, radio | rounds about 1.7 s | "Spiel SWR3" answered "SWR3 läuft." without calling a tool (`rounds=0`); nothing played | not for voice turns with tools: it claims actions it did not take |
| `minimax/minimax-m3.1-flash` | OpenRouter | 2026-11-05, radio, stop, quiz | rounds about 1–2 s; "Spiel SWR3" and "Stopp" 3.5 s on the fake Show | radio and stop right; after a failed pause it alternated `GetLiveContext` and the pause for 9 rounds (37 s, up to 1,011 tokens a round); in the quiz it filled fields of a thread with a plan (an error), wrote a garbled question ("Welches Betriebssystem nutzt das Linux-Kernzeichen?"), and on the answer "Linux." wrote 1,024 tokens without a tool call until cut off ("Ich bin damit nicht ganz fertig geworden.") | not usable; no eval run (decided 2026-11-05) |
| `deepseek/deepseek-v4-flash` | OpenRouter | 2026-11-05, radio, stop, a quiz of three questions (right, wrong, "Keine Ahnung, weiter"), on the fake Show | turns 2.7–7.2 s: "Spiel SWR3" 4.6 s, "Stopp" 2.7 s, quiz turns 3.7–7.2 s | right tool each time (`radio_play`, `HassMediaPause`, `thread_start`, `thread_plan`, `sound_play`); scored and ended the quiz correctly; no error, no retry | good by voice (a whole quiz without a repeated question, 2026-11-05, with the note of streaming decision 9); eval 8 of 12 ([report](results/deepseek-flash.md)): a due to-do and a task fail (no proposal before acting), cancelling an appointment passes 2 of 3 where a core scenario needs 3 |
