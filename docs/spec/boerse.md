# Share prices (skill `boerse`) – Spec

Status 2026-11-23: decided (decisions 1 and 2 by the maintainer, 3 to 6 taken while writing); steps 1 and 2 built.
Scenarios: [boerse.feature](boerse.feature) (BO-01 to BO-06).

## Goal

"Wie steht die SAP-Aktie?", "Wo steht der DAX?", "Was macht Siemens heute?": Mira says the price, the change since
yesterday's close and the time of the price. Without a skill she searched the web and found no price (checked
2026-11-23 on the test Mira: three searches and two pages for SAP, "keine aktuellen Kursdaten"; for the DAX a news
sentence without a number): finance pages load their prices by script, and search hits are news.

## Building blocks

A skill with Starlark tools ([API tools](api-tools.md)), as `youtube`, here without a login (its decision 8, changed
for this skill). Not chosen: a price of Home Assistant, which only knows the papers a user set up as sensors; Mira
reads those already.

## Decisions

### By the maintainer (2026-11-23)

| # | Question | Decision |
|---|---|---|
| 1 | The source | Yahoo Finance without a key (1A): its search and its chart answer, free, Xetra and the indices, prices up to 15 minutes late. Unofficial: it may change or refuse; then the tool's error says so, and the skill's text has Mira say it |
| 2 | A web API without a login, Mira's name | Allowed (2A): [API tools](api-tools.md), decisions 8 and 16 |

### Taken while writing (2026-11-23, following from 1 and 2)

| # | Question | Decision |
|---|---|---|
| 3 | Which listing of a share | The search's first hit of the kinds share, fund or index on Xetra ("GER"); without one on Xetra, the first hit of those kinds. The others go along as "alternativen", so the model can ask when the name is not clear |
| 4 | The indices | DAX, MDAX, TecDAX, SDAX, Euro Stoxx 50, Dow Jones, S&P 500 and Nasdaq by their names, without a search |
| 5 | What the tool returns | name, symbol, exchange, price, currency, yesterday's close, the change in percent, the day's high and low, the time of the price in the exchange's time zone, and the alternatives |
| 6 | Advice | None: the skill's text says Mira gives prices, no advice to buy or sell |

## What it does, step by step

1. The model calls `boerse__kurs(wert)` with the name, the symbol or the ISIN the user said.
2. An index by its name → its symbol; else one request to Yahoo's search (at most 6 hits), and the listing by
   decision 3.
3. One request to Yahoo's chart for that symbol: the price and the rest of decision 5.
4. The model says the price with two decimals, the currency, the change since yesterday and the time; while the
   exchange is open, that the price may be 15 minutes late.

**Fails:** no hit → the tool's error "Zu „…“ finde ich kein Wertpapier", and Mira asks for the name or the ISIN.
Yahoo refuses or is not reached (a status of 400 or more, no answer within 20 s) → the tool's error with the status
and the answer's start, a log line of the tool; Mira says she cannot reach the prices now. An answer without a price →
the tool's error naming the symbol.

## What it costs

Two requests per question (the search and the chart), one for an index; each at most 20 s; the turn waits for them.
No key, nothing stored.

## Steps

1. **A web API without a login and Mira's name** ([API tools](api-tools.md), decisions 8 and 16): the `api` section
   under `metadata.mira`, the sending shared by both ways, `User-Agent: Mira/<version>`. BO-01, BO-02.
2. **The skill `boerse`**: `skills/boerse` with `kurs`. BO-03 to BO-06.
