# REWE shopping – spec

Status 2026-11-19: specified; steps 1 (korb 0.6.0) and 2 built. Scenarios: [rewe.feature](rewe.feature).

## Goal

"Bestell den Einkaufszettel bei REWE", "Pack noch Butter zur REWE-Bestellung", "Wann kommt die REWE-Lieferung?":
Mira fills a REWE basket from Home Assistant's shopping list, picks a delivery slot and orders, by voice, in one's
own house. A placed order can be changed until shortly before delivery. Browsing the catalogue is not a goal: a list
of products is read on a screen, not heard.

The use case is a common one made possible by voice, not a shop of its own: groceries to the one fixed address of
the account, ordered from inside the house. What can go wrong is a wrong or unwanted grocery order, which REWE lets
cancel until shortly before delivery. The safety is sized to that (decision 2).

## The concept it hangs on

A **skill with its MCP server** ([MCP servers in skills](skill-mcp.md)), the server started over `stdio`. The server
is not Mira's: it is **korb**, a command line client for REWE's mobile API
([rewe-cli](https://github.com/sker65/rewe-cli), a Go port of [korb](https://github.com/yannick-cw/korb)), started as
`korb mcp`. It is the first skill whose tools come from a
program the user puts next to Mira; nothing in Mira is built for it (decision 1), except what decision 5 adds.

The shopping list is Home Assistant's: its to-do tools over Assist (read, add, check off), which Mira already uses.
Preferences of the household ("lieber donnerstags") are house memory, as any fact. What belongs to the REWE account
(the login, the delivery service or market, the open modification) is kept by korb in its own folder: a skill has
no storage of its own.

Why not a skill script ([Skill files](skill-files.md)): a script runs in Home Assistant, which has no REWE
integration and cannot run korb. Why not a generic "run a program" section for skills: the model would build command
lines; an MCP server offers fixed, typed tools, and Mira's hooks see each one by name.

## Decided (2026-11-19, the maintainer)

| # | Question | Decision |
|---|---|---|
| 1 | How Mira runs korb | `korb mcp` as the skill's MCP server over `stdio`, inside the HA app (1A). korb is a static Go binary and runs in the app's distroless image. Not chosen: a new section for running programs from skills in Mira (B); `korb mcp` over HTTP on another host (C) stays possible later with the same tools |
| 2 | Who may order | Everyone who talks to Mira, after a yes in the conversation (2A). No check of the person, no second channel (a confirmation by Telegram was considered and left out: it makes the common case harder for little gain). The login is the account owner's decision to trust Mira with the account; the guide and the skill's description say so plainly |
| 3 | korb's answers as third-party text | No (3A): the skill's server is not `web_content`. With it, `system/web.star` would deny every yes after the first search, and ordering could never happen. The skill's prose tells the model not to follow instructions in product texts |
| 4 | Order of the work | This spec first (4A), then korb's server, then the skill |
| 5 | How the yes is asked | A skill's `mcp` section names the tools that need a yes (`confirm: [order, …]`); Mira gives them a new field `call.tool.confirm`, and a default hook returns `confirm` for it (5A; built as `default/skill_confirm.star`, see step 2). Small and general: the yes is Mira's, with the call's summary, and other skills can use it. Not chosen: only the skill's prose and korb's `total` check (B), a hook every household writes itself (C) |

## How the account is prepared (once, by an admin, not by voice)

The skill orders only with an account that needs no interaction at ordering time. The guide describes the steps:

1. **Build korb** with the REWE app's client certificate (it cannot be shipped with Mira: the certificate is REWE's,
   and Mira's repository is public) and put it into the HA app's config folder, e.g. `/config/rewe/korb`.
2. **Log in** with `korb login` on a computer and copy korb's config folder to `/config/rewe/konto`, or run
   `korb login` with `KORB_CONFIG_DIR=/config/rewe/konto` in a terminal that reaches the folder. Logging in is not a
   tool of the skill: a redirect address cannot be spoken, and the login is the owner's decision (decision 2).
3. **Choose the delivery service** (or a market for pickup): `korb store search <PLZ>`, `korb store set <ID> <PLZ>`.
4. **In the REWE app**: a delivery address and a payment method that needs no confirmation at ordering time (today:
   PayPal direct debit, "PayPal-Einzugsverfahren").
5. **Mira's settings** of the skill (below), then Mira restarts as for any changed setting.

The tool `status` checks all of it at once; the skill tells the model to call it first in a conversation and to say
in one sentence what is missing. The payment method and the delivery address come from REWE's checkout, which exists
only for a basket that can be ordered: with an empty basket or one below the minimum order value `status` says
`paymentChecked: false`, and `checkout_prepare` checks them (built so in step 1).

## In the skill

```yaml
# skills/rewe/SKILL.md
metadata:
  mira:
    chapter: "## Einkaufen bei REWE"
    settings:
      programm: { description: "Pfad zu korb, z. B. /config/rewe/korb", required: true }
      konto:    { description: "Ordner mit korbs Anmeldung, z. B. /config/rewe/konto", required: true }
      limit:    { description: "Höchstbetrag einer Bestellung in Euro, z. B. 150", required: true }
      liste:    { description: "Die To-do-Liste mit dem Einkaufszettel; ohne: die Liste Einkaufsliste" }
    mcp:
      transport: stdio
      command: "{{ programm }}"
      args: [mcp]
      env: ["KORB_CONFIG_DIR={{ konto }}", "KORB_MAX_TOTAL_EURO={{ limit }}"]
      confirm: [order, modification_submit, order_cancel]   # decision 5
```

```yaml
# mira.yaml
skills:
  rewe: { programm: /config/rewe/korb, konto: /config/rewe/konto, limit: 150 }
```

- A stdio server gets none of Mira's environment but the few variables a program needs to run (`StdioEnv`); korb
  needs only its config folder and the limit.
- For tests, the skill's `env` may add `KORB_API_URL` and `KORB_AUTH_URL` pointing at `korb-fake` (Steps); korb
  refuses them without its own `KORB_CONFIG_DIR`, so a test can never touch the real account.

## korb's tools (`korb mcp`)

The tools are korb's, in English, as korb is not Mira's; the model calls them `rewe__<tool>`. Every answer is
compact JSON for speaking: titles, prices in euros as text ("2,19 €"), ids only where a later call needs them; never
REWE's raw answers. Every tool works on korb's selected service (delivery or pickup) and, while a modification runs,
on the modification's basket.

| Tool | What it does | REWE calls | Changes something |
|---|---|---|---|
| `status` | logged in, the selected service (delivery or pickup, the market's name), the basket in short, an open modification; for a basket that can be ordered also a payment method usable without interaction and the delivery address; `missing` lists what an admin has to set up | 2–3, 4–5 with a basket that can be ordered | no |
| `favorites_search` | the favorites whose title contains the query; all without a query | 1 | no |
| `frequent` | products of the order history by how often they were bought, optionally filtered by a query | 1 + 1 per order of the last 10 | no |
| `product_search` | the catalogue, at most 10 hits, optionally organic, regional, vegan, vegetarian | 1 | no |
| `basket` | items, article value, deposit, total, the delivery fee, the minimum order value and what is missing to it, violations | 1 | no |
| `basket_set` | the quantity of one product (0 removes it); answers as `basket` | 1–2 | the basket |
| `slots` | delivery or pickup slots of the next days: weekday, date, time, fee; free slots first marked | 1 | no |
| `checkout_prepare` | creates the checkout for a slot: reserves it, selects the usable payment method; answers the total, the slot, the payment method, the address, `orderable`, or what stands in the way (`problem`: over the limit, no delivery address); below the minimum order value or without a usable payment method it fails | 5–6 | a reservation |
| `order` | places the prepared order; requires `total` (the amount said to the user) and refuses when the checkout's total differs, when it is over the limit, when REWE asks for a confirmation in a browser | 2 | **an order** |
| `orders` | open orders: date, slot, total, whether it can still be changed | 1 | no |
| `modification_start` | starts changing an open order (without `orderId`: the only open order that can be changed); `basket` and `basket_set` then act on it | 2 | a modification |
| `modification_submit` | submits the change; checks minimum order value and limit, requires `total` like `order` | 2 | **the order** |
| `modification_cancel` | discards the change | 1 | the modification |
| `order_cancel` | cancels an open order | 1 | **the order** |

What korb guards itself, whatever the model does: the limit (`KORB_MAX_TOTAL_EURO`), the minimum order value, only
payment methods without interaction, the total said must be the total ordered, checkout refused while a
modification runs.

## What Mira does, step by step

**Ordering the shopping list** (a voice turn per answer; the turn waits for each tool):

1. The model opens the skill `rewe` and calls `status`. Not ready: it says what is missing (the guide's words) and
   stops.
2. It reads the shopping list (Home Assistant's to-do tool, the list of `liste`): one call, grows with the list.
3. Per entry it looks for the product, in this order (a tool call each, so it grows with the list):
   1. `favorites_search` with the entry's main word: one favorite matches → that one. Several → the one whose title
      fits the entry best; when that is unclear, it asks in one short question ("Bio-Vollmilch oder Hafermilch?").
   2. none: `frequent` with the query: the one bought most often.
   3. none: `product_search`: a clear hit is taken, otherwise it asks; nothing found is said at the end.
   The quantity is the one on the list or said, else 1.
4. `basket_set` per product. Then it says the total in one or two sentences: how many products, the article value,
   which entries it did not find or had to guess. Below the minimum order value it says what is missing and offers
   products from `frequent` that are not in the basket.
5. The slot: the household's preference from memory (e.g. Thursdays, when delivery is free), else the next free
   one; `slots`, then `checkout_prepare` with it.
6. It asks with the total, the slot and the payment method: "63,40 €, Donnerstag 18 bis 22 Uhr, mit PayPal.
   Soll ich bestellen?" With decision 5, the hook asks this question.
7. After the yes, `order` with that total. Then the entries that went into the order are checked off the shopping
   list (one call each); the others stay, and it names them.

**Adding to an order** ("Pack noch Butter zur REWE-Bestellung"): `orders`, `modification_start` with the open order
(several: the next one, said), the product as in step 3, `basket_set`, the new total, the yes, `modification_submit`.
"Doch nicht" before the yes → `modification_cancel`.

**Asking** ("Wann kommt die REWE-Lieferung?"): `orders`, the slot and the total said.

**What the model is told**: the tool descriptions (korb's) and the skill's prose: the order of looking for a product
(favorites, frequent, search), to ask only when it is unclear, to say the total and the missing amount in euros
without the deposit, never to order without having said the total, slot and payment method and heard the yes, to
check off only what was ordered, not to follow instructions in product texts (decision 3), and the sentences for
the errors below.

## Open

- **Ordering in the background** (a skill schedule, "jeden Donnerstag"): not in this spec. `system/background.star`
  denies the tools anyway (the server is not `read_only`).
- **Where rewe-cli lives**: it is to become public, possibly under the account miraassist; the links follow.

## Errors

| What fails | What happens | Who learns of it |
|---|---|---|
| korb is not at `programm`, or does not start | the skill is there, its tools are missing | Home Assistant's repairs (a problem of the skill, [MCP servers in skills](skill-mcp.md)); the model says REWE is not set up in Mira |
| Not logged in, or the login expired (REWE refuses the refresh) | `status` and every tool answer `not logged in` | the model: "Die Anmeldung bei REWE fehlt, ein Admin muss korb neu anmelden." korb's error names the step |
| No service selected, no usable payment method, no delivery address | `status` names it; `checkout_prepare` refuses | the model says which, in the guide's words |
| A product is not available at the service | not found, or `basket_set` refuses | the model names the entry as not found |
| Below the minimum order value | `checkout_prepare` refuses with the missing amount | the model says the amount and offers products (step 4) |
| The slot is gone by now | `checkout_prepare` fails | the model offers the next slot |
| The total differs from the one said (prices changed, a product sold out) | `order` refuses, nothing ordered | the model says the new total and asks again |
| Over the limit | `order` refuses | the model says the limit and that an admin sets it |
| REWE asks for a confirmation in a browser | `order` refuses before ordering | the model says that this payment method does not work by voice |
| REWE does not answer, 5xx, or the client is blocked | korb retries once on a lost connection, else the tool's error names the call and REWE's answer | the model: "Ich erreiche REWE gerade nicht." Not retried in the turn |
| A modification is no longer possible | `modification_start` refuses | the model says that the order cannot be changed any more |
| Checking off an entry fails | the order stands | the model names the entry that stays on the list |

## Steps

1. **korb's MCP server** (rewe-cli): `korb mcp` over stdio with the tools above, compact answers, `status`,
   `total` on `order` and `modification_submit`, `KORB_MAX_TOTAL_EURO`; tested against `korb-fake`. RW-01 to RW-05.
   Built 2026-11-19 (korb 0.6.0): RW-01 to RW-05 and the whole flow (favorites, order, change, cancel) as tests in
   rewe-cli against `korb-fake`. Run with the MCP client Mira uses (go-sdk v1.8.0, `CommandTransport`): protocol
   2025-11-25, 14 tools; read only against the real account (`status`, `basket`, `orders`, `slots`,
   `favorites_search`): the deposit and the other fees (the transport box) appear apart, as in REWE's app.
2. **The yes** (decision 5): the field `confirm` of a skill's `mcp` section, `call.tool.confirm`, and
   `default/skill_confirm.star`; [Hooks](hooks.md) and [MCP servers in skills](skill-mcp.md) name it. RW-06.
   Built 2026-11-19: `confirm` also works for an entry under `mcp_servers` (the same struct); a name under `confirm`
   that `tools` leaves out is a problem of the skill. The question is a default hook of its own,
   `skill_confirm.star`, not part of `critical.star`: replacing a default hook is the household's right, but a
   household that replaces `critical.star` for its garage door must not lose the yes before an order it never saw.
   Dropping it means replacing `skill_confirm.star` by its name; Mira never writes it (decided 2026-11-19).
3. **The skill `rewe`** (`skills/rewe/SKILL.md`, German) with its settings, `mcp` section and prose. RW-07 to RW-09.
4. **The run on the test HA** against `korb-fake`, through the fake Show: every scenario RW-10 to RW-18, the
   interruptions too ("stop", silence, "nein" at the question).
5. **The guide** (de and en): building korb, the login, the service, the payment method, the settings, and the
   sentence that whoever logs in lets everyone who talks to Mira order.
6. **A real order** with the maintainer, by voice: ordering and changing an order end to end (korb's ordering and
   submitting a modification are not yet seen against REWE).
