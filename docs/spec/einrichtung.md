# Setup – spec

Status 2026-10-17: built in 0.6.22, pairing without a code, model choice and assistants in 0.6.23, pairing only after the summary in 0.6.25, overview first in 0.6.26, texts simplified in 0.6.28. See also [Mira in Home Assistant](ha-anbindung.md) and the security of the connections in the [system doc](../system.md).

## Target

Today a new user needs nine steps:
- in HA: HACS, the integration "Model Context Protocol Server", a separate user with token (for that, log in as this user), exposures;
- on the Docker machine: write `mira.yaml` and `.env`, start the container;
- in HA again: enter the same token once more, choose Mira as conversation agent.

In future there are three, and none of them requires understanding a config file or typing in a code:

1. **Install Mira via HACS**, restart HA.
2. **Start the container**, one line, without a config file:
   `docker run -d --name mira --network host -v mira-data:/var/lib/mira ghcr.io/miraassist/mira`
3. **Add Mira under "Discovered".** An overview shows what is still missing: the AI model (LLM) and the assistants. "Start using Mira" does the rest.

Exposing to Assist remains, as before, a decision of the user in HA.

## 1. The integration sets up everything on the HA side by itself

The integration runs in HA with admin rights. During setup it does what is manual work today:

- **Mira's HA user:** It creates a user "Mira", **without admin rights** and without login. For it, it generates a long-lived token. If the user already exists (an earlier setup), it takes it and renews the token.
- **HA's MCP server:** If the integration "Model Context Protocol Server" with Assist as API is missing, it sets it up.
- **Handover to the agent:** It sends it the HA address (HA's internal address) and token (`POST /v1/setup`). The agent stores both in its database and connects.
- **Removing:** If one removes the integration, it revokes the token. It leaves the user in place, because Mira's automations hang on its ID.

After that, the agent's config contains nothing about HA anymore (`ha.url`, `ha.token`, the MCP entry for HA).

## 2. Pairing instead of typing a token

- **First start** without a token in the config: The agent generates a random access token (`server.token`) and stores it in its database.
- **Overview first, pairing last.** After "Mira found" the overview comes right away. Its text shows what is chosen and what is still missing; below it the branches "Choose the AI model (LLM)", "Use Mira in" (the assistants; the preferred one is preselected) and "Start using Mira". Each branch returns to the overview. HA dialogs have no back button; the overview replaces it. "Start using Mira" appears only when nothing is missing anymore.
  - Before that nothing changes, neither on the agent nor in HA: no user, no token, no pairing. An aborted dialog leaves nothing half set up. HA keeps a discovered dialog open when closing and calls the step without input again when reopening. By this the integration recognizes the reopening and shows the overview instead of being stuck in a sub-form (such as the key of a provider chosen by mistake).
  - So that the dialog can list and try out models beforehand, the agent answers `/v1/setup/models` and `/v1/setup/probe` without a token, but only as long as it has never been paired.
  - "Start using Mira" creates user and token, switches on HA's MCP server and then sends HA address, token and model in **one** call (`POST /v1/pair`). The agent stores everything, hands out its access token and restarts, or it does nothing at all. If it goes wrong, the integration revokes the token again.
- **Without a code the first time:** An agent that has never been paired pairs with the first Home Assistant that completes "Start using Mira".
  - **Why that is enough:** An attacker in the home network would have to strike between container start and setup. He would not go unnoticed: The owner's dialog then asks for a code. The guide says: set up in your own home network, not in the guest network.
  - A code in the mDNS entry deliberately does not exist: Any device in the network could read it.
- **Removing releases:** If one removes the integration, it reports this to the agent (`POST /v1/unpair`). It makes a new token, forgets the HA access, keeps the model and waits for setup again, the next time again without a code; the summary then shows "AI model: stays …".
- **Pairing again with a code:** If the agent is still paired (the integration could not reach it when removing, or another Home Assistant has it), a **pairing code** of six digits is needed: `mira pair` on the agent's machine (`docker exec mira /app/mira pair`). It then also appears on the agent's page. The code is valid for 15 minutes and 5 attempts. The overview shows it as missing, with the branch "Enter the pairing code"; it is used at "Start using Mira", and if it is wrong, the overview says so. The agent keeps the model; it can be changed afterwards under Configure.
- **As before:** If a token is in the config (`server.token`), that one applies, without pairing. So existing installations stay unchanged.

## 3. Agent without a config file

- **AI model (LLM) in the form:** Meant is the LLM Mira thinks with; it is not called "language model", that gets confused in HA with speech recognition. The dialog only asks where it comes from, and assumes the most common case: a cloud provider, no expensive hardware.
  - **From Home Assistant:** If HA already has an AI integration (OpenAI, Google Gemini, OpenRouter, Ollama), it is at the top of the list. The integration reads address, key and model from its entry and gives them to the agent. The key is never shown, not even as a password field. If one changes it later in HA, one chooses "From Home Assistant" again under Configure → AI model. Anthropic is missing because its OpenAI-compatible interface is untested.
  - **Provider:** OpenAI, Google Gemini, OpenRouter, Mistral with a fixed address; only the key is asked.
  - **Own server:** address (as seen from the agent) and optionally a key. A category "local" with LM Studio, Ollama, llama.cpp deliberately does not exist, and the agent does not scan the network.
  - The model is chosen from the list the agent queries at this address (`/v1/models`).
  - **Trial run:** Before saving, the agent sends a real small request with a tool. It must be answered and call the tool. Errors come back with a reason and appear understandably in the form: not reachable, key rejected, model unknown or not loaded, model without tools. A local model gets loaded right away in the process.
  - It can be changed later under Configure → AI model (LLM).
- **Defaults for everything else:**
  - port 8787 on all interfaces;
  - mDNS on;
  - the database in the data folder;
  - the bundled skills;
  - language and persons from HA.
- **`mira.yaml` stays optional,** for advanced things: further models and routing, further MCP servers, skills with settings, finer points of the notifier. What the integration sets (HA access, language model) applies before the file; the rest of the file applies as before.

## 4. Mira as conversation agent

Otherwise everything would be set up, and Mira still never answers.
- The last step in the dialog shows HA's assistants (Assist pipelines), the preferred one is checked. Satellites on "preferred" follow it.
- As soon as Mira's entity exists, the integration sets it as conversation agent of these assistants and remembers which agent each had before.
- When removing, the assistants that still use Mira get their previous agent back, or HA's own if the previous one no longer exists.

## Database path

The database path has a default that fits almost all installations:
- The image sets its data folder (`MIRA_DATA=/var/lib/mira`), the default is then `/var/lib/mira/mira.db`.
- Without `MIRA_DATA` (the program directly on the machine) it stays `mira.db` in the working folder.

`store.path` only needs to be set if one wants it differently.

## Existing installations

- **With config file:** An installation with `server.token`, `ha.url` and `ha.token` in `mira.yaml` keeps running unchanged. The integration sets up no new user as long as the agent reports that it already has HA access (`/v1/health`).
- **Switching over:** Whoever wants the new way takes the entries out of the file and pairs anew.

## Deliberately not

- **HA add-on:** a separate, later step.
- **Encryption between integration and agent:** The normal case is the same machine (see system doc, connections).
