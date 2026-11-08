# Evals: what Mira is checked with

Scenarios end to end with a real model and the test Home Assistant, without speech
(docs/spec/evals.md). 3 runs per scenario; core scenarios pass at 3 of 3, the others
at 2 of 3. A step over the answer timeout (voice 15 s) counts as not passed.

Run: `go run ./cmd/testha eval --setup evals/setups/<setup>.yaml`

Each setup shows its last run; earlier runs are in git's history.
Models tried only by hand (without an eval run): [models.md](models.md).

| Setup | Models | Date | Mira | Passed | Report |
|---|---|---|---|---|---|
| chatgpt-luna | main: gpt-6-luna | 2026-11-08 07:07 | 0.18.0 | 9 of 12 | [chatgpt-luna.md](results/chatgpt-luna.md) |
| chatgpt-luna | main: gpt-6-luna (ChatGPT subscription through LiteLLM) | 2026-11-08 07:07 | 0.18.0 | 9 von 12 | [chatgpt-luna.md](results/chatgpt-luna.md) |
| deepseek-flash | main: deepseek/deepseek-v4-flash | 2026-11-05 22:08 | 0.15.2 | 8 of 12 | [deepseek-flash.md](results/deepseek-flash.md) |
| gemini-flash | main: gemini-flash-latest | 2026-11-05 22:20 | 0.16.0 | 12 of 12 | [gemini-flash.md](results/gemini-flash.md) |
| strata-qwen36 | main: qwen3.6-35b-a3b | 2026-10-22 09:11 | 0.6.58 | 1 of 12 | [strata-qwen36.md](results/strata-qwen36.md) |
| strata-qwen38-q2 | main: qwen3.8-flash-next-q2_0 | 2026-10-22 16:02 | 0.6.59 | 4 of 12 | [strata-qwen38-q2.md](results/strata-qwen38-q2.md) |
