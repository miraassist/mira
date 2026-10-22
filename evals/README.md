# Evals: womit Mira geprüft ist

Szenarien Ende zu Ende mit echtem Modell und Test-Home-Assistant, ohne Sprache
(docs/spec/evals.md). Je Szenario 3 Durchläufe; Kern-Szenarien bestehen bei 3 von 3,
die übrigen bei 2 von 3. Ein Schritt über dem Antwort-Timeout (Sprache 15 s) gilt als
nicht bestanden.

Ausführen: `go run ./cmd/testha eval --setup evals/setups/<setup>.yaml`

Je Setup steht hier der letzte Lauf; frühere Läufe sind in der Git-Geschichte.

| Setup | Modelle | Stand | Mira | bestanden | Bericht |
|---|---|---|---|---|---|
| gemini-flash | main: gemini-flash-latest | 2026-10-17 11:28 | 0.6.19 | 10 von 12 | [gemini-flash.md](results/gemini-flash.md) |
| strata-qwen36 | main: qwen3.6-35b-a3b | 2026-10-22 09:11 | 0.6.58 | 1 von 12 | [strata-qwen36.md](results/strata-qwen36.md) |
| strata-qwen38-q2 | main: qwen3.8-flash-next-q2_0 | 2026-10-22 16:02 | 0.6.59 | 4 von 12 | [strata-qwen38-q2.md](results/strata-qwen38-q2.md) |
