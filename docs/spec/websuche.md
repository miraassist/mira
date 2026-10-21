# Web search – spec

Status 2026-10-18: decided and built in 0.6.29, fallback to Bing in 0.6.30, page start with the results in 0.6.31. See also the security rules in the [system docs](../system.md).

## Target

Looking things up on the internet is part of what distinguishes Mira from a voice control: opening hours, results, news, "Wie lange kocht ein Ei?". So far this only worked with a separate MCP server for search. It ran as a Node program next to Mira and not at all in the container, because the image has no Node.

From now on Mira brings search herself. It is **on**, without setup and without a key. It can be switched off, for instance when another search is added as an MCP server.

## The tools

- **`web_search`**: a search query; answer: up to 5 results with title, address and snippet. Plus the **page start** (about 1,500 characters of main text) of the first readable result.
- **`web_page`**: an address; answer: title and main text of the page, shortened to a manageable length. With this Mira reads a result more closely when the snippet is not enough.

`web_search` reads the page start itself, because a result's snippet often does not contain the answer and small models do not look further on their own. It tries the first two results (at most 6 seconds together) and takes the first one that can be read and has at least 200 characters of text. Pages that build their content only via script are excluded this way. If the model wants more, it reads a result in full with `web_page`.

## How searching works

- **Source:** DuckDuckGo's HTML results page (`html.duckduckgo.com`), a simple HTTP fetch.
  - No key and no account needed.
  - Region by Mira's language (`de-de`, `us-en`).
- **Fallback:** If DuckDuckGo shows a bot check (it sends it as HTTP 202, already after a few searches in a row from one address) or does not answer, Mira asks Bing via its results page (`bing.com/search`, also without a key). Bing's wrapped links are unwrapped.
  - Mojeek and Brave Search were tried: both block simple fetches (captcha, HTTP 429).
  - If neither works, the tool explicitly tells the model that it could not look it up and should not guess from memory.
- **Deliberately without a browser:** Bing or Brave via a remote-controlled browser (as in the previous MCP server) are slow, make the image hundreds of MB larger and break with every change to the pages.
- **Page text:**
  - Mira loads the page (at most 2 MB, only HTML or text) and leaves out scripts, navigation, headers and footers, forms and the like.
  - If there is an `<article>` or `<main>` with enough text, she takes that, otherwise the whole content.
- **Limits:**
  - Search and page have 10 seconds each.
  - Only `http` and `https`.
  - `web_page` rejects addresses in the own network (private, loopback, link-local), also after a redirect. A web page should not get Mira to query devices in the home network.
- **Limit of this solution:** These are fetches of results pages without an official interface. They can change or block. More effort is deliberately not planned here; whoever wants a more reliable search adds one with a key as an MCP server (for instance Brave Search) and switches off the built-in one.
- **Limits of the page start:** Up-to-the-day information (match results, news) is often not on the first readable page or further down. A small model (tested: qwen3-30b-a3b) then mixes up details or guesses from memory; a stricter tool description did not change that. A stronger model (tested: Gemini Flash) answers honestly when nothing is there.

## Security

Both tools have the same two markings as the previous MCP server for search:
- **Read only:** They change nothing. Therefore background work may use them too, for instance assessing tasks or hard questions handed off to a stronger model.
- **Third-party web text:** What they return can contain hidden instructions.
  - After they are called, critical actions are blocked for the rest of the request: locks, alarm panel and everything that must ask first.
  - Remembering something, sending a message or creating a task Mira carries out in the same request only after the user's yes (`AskAfterWeb`).
  - This is governed by the code in the guard, not by the prompt.

## Switching off

- **Where:** In the integration's options under **General**, switch **Web search**, default: on.
- **How:**
  - The integration sends the switch as config section `web_search` to the agent (`PUT /v1/ui/config`), like the personality.
  - The agent stores it in its database. The tools disappear or appear immediately, without a restart.
- **`mira.yaml`:** `web_search: {enabled: false}` changes the default. What is set in HA takes precedence.
- **Without the built-in search:** Another search is added as an MCP server (`mcp_servers`, with `read_only` and `web_content`). Using both at the same time works, but rarely makes sense.

## Later, if needed

A source with an official interface, for instance the Brave Search API (key, free quota) or an own SearXNG server. That would then be a choice next to the switch. It will only be built when DuckDuckGo is not enough in everyday use.
