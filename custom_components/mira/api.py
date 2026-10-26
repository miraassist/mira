"""HTTP client for the Mira agent."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import re

import aiohttp


def _minutes(value: Any) -> int | None:
    """The minutes of a duration the agent writes like "10m", "1h30m" or "0s"."""
    parts = re.fullmatch(r"(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?", str(value or ""))
    if not parts or not any(parts.groups()):
        return None
    hours, minutes, seconds = (int(p or 0) for p in parts.groups())
    return hours * 60 + minutes + seconds // 60


class MiraError(Exception):
    """The agent could not be reached or answered with an error."""


class MiraAuthError(MiraError):
    """The agent rejected the token."""


class MiraBadCode(MiraError):
    """The pairing code is wrong, used up or expired."""


class MiraNotFound(MiraError):
    """The agent does not have that path: an older agent than this
    integration (docs/spec/versionen.md)."""


class MiraInvalidConfig(MiraError):
    """The agent refused a config change; the message says why. For the
    model, reason is one of unreachable, bad_key, no_model, no_tools,
    server_error."""

    def __init__(self, message: str, reason: str = "") -> None:
        super().__init__(message)
        self.reason = reason


@dataclass
class AgentReply:
    """The agent's answer to one turn."""

    text: str
    conversation_id: str
    continue_conversation: bool
    # For Telegram: the answer as Telegram HTML, split into messages.
    html: list[str] | None = None


class MiraClient:
    """Talks to the agent's /v1 API. The integration never does more than this."""

    def __init__(self, session: aiohttp.ClientSession, url: str, token: str) -> None:
        self._session = session
        self._url = url.rstrip("/")
        self._headers = {"Authorization": f"Bearer {token}"}

    async def _request(self, method: str, path: str, timeout: float, **kwargs: Any) -> Any:
        try:
            async with self._session.request(
                method,
                self._url + path,
                headers=self._headers,
                timeout=aiohttp.ClientTimeout(total=timeout),
                **kwargs,
            ) as resp:
                if resp.status == 401:
                    raise MiraAuthError("token rejected")
                if resp.status == 403 and path == "/v1/pair":
                    raise MiraBadCode("wrong or expired code")
                if resp.status == 422:
                    body = await resp.json(content_type=None) or {}
                    raise MiraInvalidConfig(str(body.get("error", "invalid config")), str(body.get("reason", "")))
                if resp.status >= 400:
                    # The agent says why; the user reads it in the automation's trace.
                    reason = ""
                    try:
                        body = await resp.json(content_type=None)
                        if isinstance(body, dict) and body.get("error"):
                            reason = f": {body['error']}"
                    except (ValueError, aiohttp.ContentTypeError):
                        pass
                    error = MiraNotFound if resp.status == 404 else MiraError
                    raise error(f"{method} {path}: HTTP {resp.status}{reason}")
                return await resp.json()
        except (aiohttp.ClientError, TimeoutError) as err:
            raise MiraError(f"{method} {path}: {err!r}") from err

    async def pairing(self) -> dict[str, Any]:
        """Open: the agent's id, and whether it waits for a code or a setup.
        An agent from before pairing has no such route (MiraError)."""
        return await self._request("GET", "/v1/pair", 10)

    async def pair(self, code: str, ha_url: str, ha_token: str, llm: dict[str, Any] | None) -> str:
        """Pair and set up in one step: the agent keeps Home Assistant's
        access and the model, hands out its token and starts anew. The
        first time, while the agent was never paired, without a code."""
        body = await self._request(
            "POST", "/v1/pair", 30, json={"code": code, "ha_url": ha_url, "ha_token": ha_token, "llm": llm}
        )
        return str(body["token"])

    async def probe(self, llm: dict[str, Any]) -> None:
        """A trial request to the model, through the agent (a local model
        may have to load first, hence the long wait)."""
        await self._request("POST", "/v1/setup/probe", 200, json={"llm": llm})

    async def providers(self) -> dict[str, tuple[str, str]]:
        """The cloud providers the agent knows: key → (name, address). An
        agent older than the list (before 0.8.0) knows none: then only a
        model of one's own can be set up."""
        try:
            body = await self._request("GET", "/v1/setup/providers", 10)
        except MiraNotFound:
            return {}
        return {str(p["key"]): (str(p["name"]), str(p["base_url"])) for p in body.get("providers") or []}

    async def models(self, base_url: str, api_key: str) -> list[str]:
        """The models a model server offers, asked through the agent."""
        body = await self._request("POST", "/v1/setup/models", 20, json={"base_url": base_url, "api_key": api_key})
        return list(body.get("models") or [])

    async def unpair(self) -> None:
        """Home Assistant lets go of the agent: it makes a new token and
        waits to be paired again."""
        await self._request("POST", "/v1/unpair", 10)

    async def setup(self, ha_url: str | None, ha_token: str | None, llm: dict[str, Any] | None) -> None:
        """Hand the agent Home Assistant's access and the model; it tries
        the model with a real request first (a local one may have to load,
        hence the long wait), and starts anew afterwards."""
        body: dict[str, Any] = {}
        if ha_url and ha_token:
            body["ha_url"], body["ha_token"] = ha_url, ha_token
        if llm:
            body["llm"] = llm
        await self._request("POST", "/v1/setup", 200, json=body)

    async def reset_conversation(self, channel: str, conversation_id: str) -> None:
        """Start the conversation afresh ("/neu" on Telegram)."""
        await self._request(
            "POST", "/v1/conversation/reset", 10, json={"channel": channel, "conversation_id": conversation_id}
        )

    async def health(self) -> dict[str, Any]:
        """Check the connection and the token."""
        return await self._request("GET", "/v1/health", 10)

    async def converse(
        self,
        text: str,
        conversation_id: str | None,
        device_id: str | None,
        language: str,
        agent_id: str,
        extra_system_prompt: str | None,
        timeout: float,
        user_id: str | None = None,
        channel: str = "voice",
        person: str = "",
    ) -> AgentReply:
        """Forward one user message and return the agent's answer."""
        data = await self._request(
            "POST",
            "/v1/conversation",
            timeout,
            json={
                "text": text,
                "conversation_id": conversation_id or "",
                "device_id": device_id or "",
                "language": language,
                "agent_id": agent_id,
                "extra_system_prompt": extra_system_prompt or "",
                "user_id": user_id or "",
                "channel": channel,
                "person": person,
            },
        )
        return AgentReply(
            text=str(data.get("text", "")),
            conversation_id=str(data.get("conversation_id") or conversation_id or ""),
            continue_conversation=bool(data.get("continue_conversation", False)),
            html=[str(p) for p in data.get("html") or []] or None,
        )

    async def status(self) -> dict[str, Any]:
        """Pending message count and quiet mode."""
        return await self._request("GET", "/v1/status", 10)

    async def set_quiet_mode(self, enabled: bool) -> None:
        """Switch the agent's quiet mode."""
        await self._request("PUT", "/v1/quiet_mode", 10, json={"enabled": enabled})

    async def send_message(self, message: dict[str, Any]) -> dict[str, Any]:
        """Queue a message; the agent decides when and where it is delivered."""
        return await self._request("POST", "/v1/messages", 10, json=message)

    async def assess(self, request: dict[str, Any]) -> dict[str, Any]:
        """Let the agent judge an observation and queue a message if it is worth one."""
        return await self._request("POST", "/v1/assess", 60, json=request)

    async def decide(self, request: dict[str, Any]) -> dict[str, Any]:
        """Ask the agent a yes/no question about the house; no message."""
        return await self._request("POST", "/v1/decide", 60, json=request)

    async def agent(self, request: dict[str, Any]) -> dict[str, Any]:
        """Call an agent: synchronously the request waits for the run, up to
        its timeout; asynchronously it returns the call id at once."""
        wait = 10 if request.get("mode") == "async" else float(request.get("timeout", 60)) + 15
        return await self._request("POST", "/v1/agent", wait, json=request)

    async def agent_result(self, call_id: str) -> dict[str, Any]:
        """A run's record by its call id."""
        return await self._request("POST", "/v1/agent/result", 10, json={"call_id": call_id})

    async def agent_cancel(self, call_id: str) -> dict[str, Any]:
        """Cancel a running run."""
        return await self._request("POST", "/v1/agent/cancel", 10, json={"call_id": call_id})

    async def mail(self, mail: dict[str, Any]) -> dict[str, Any]:
        """Hand over one mail of a mail calendar; waits while the model reads it."""
        return await self._request("POST", "/v1/mail", 320, json=mail)

    async def compact_profile(self) -> dict[str, Any]:
        """Rebuild the memory profile from the stored facts now."""
        return await self._request("POST", "/v1/ui/memory/compact", 180)

    async def general(self) -> dict[str, Any]:
        """What the options' "General" sets at the agent: the persona and
        whether the web search is on."""
        sections = await self._request("GET", "/v1/ui/config", 10) or {}
        search = (sections.get("web_search") or {}).get("value") or {}
        notifier = (sections.get("notifier") or {}).get("value")
        return {
            "persona": str((sections.get("persona") or {}).get("value") or ""),
            "web_search": bool(search.get("enabled", True)),
            # The whole section, since it is replaced as a whole; the hold in
            # minutes, None when this agent does not know it.
            "notifier": notifier if isinstance(notifier, dict) else None,
            "own_message_hold": _minutes(notifier.get("own_message_hold")) if isinstance(notifier, dict) else None,
        }

    async def set_general(self, changes: dict[str, Any]) -> None:
        """Change the persona and/or the web search switch; the agent checks
        them first (MiraInvalidConfig)."""
        body: dict[str, Any] = {}
        if "persona" in changes:
            body["persona"] = changes["persona"]
        if "web_search" in changes:
            body["web_search"] = {"enabled": bool(changes["web_search"])}
        if "notifier" in changes:
            body["notifier"] = changes["notifier"]
        if body:
            await self._request("PUT", "/v1/ui/config", 10, json=body)

    async def managed_config(self) -> dict[str, Any]:
        """Watchers, persons and rooms as the agent has them now."""
        return await self._request("GET", "/v1/ui/ha", 10)

    async def set_managed_config(self, config: dict[str, Any]) -> dict[str, Any]:
        """Replace watchers, persons and rooms; from now on HA manages them."""
        return await self._request("PUT", "/v1/ui/ha", 20, json=config)

    async def release_managed_config(self) -> None:
        """Hand watchers, persons and rooms back to the agent's own web UI."""
        await self._request("DELETE", "/v1/ui/ha", 10)
