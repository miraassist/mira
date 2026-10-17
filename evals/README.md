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
