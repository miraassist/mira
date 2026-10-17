"""HTTP client for the Mira agent."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import aiohttp


class MiraError(Exception):
    """The agent could not be reached or answered with an error."""


class MiraAuthError(MiraError):
    """The agent rejected the token."""


class MiraBadCode(MiraError):
    """The pairing code is wrong, used up or expired."""


class MiraInvalidConfig(MiraError):
    """The agent refused a config change; the message says why."""


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
                    body = await resp.json(content_type=None)
                    raise MiraInvalidConfig(str((body or {}).get("error", "invalid config")))
                if resp.status >= 400:
                    raise MiraError(f"{method} {path}: HTTP {resp.status}")
                return await resp.json()
        except (aiohttp.ClientError, TimeoutError) as err:
            raise MiraError(f"{method} {path}: {err!r}") from err

    async def pairing(self) -> dict[str, Any]:
        """Open: the agent's id, and whether it waits for a code or a setup.
        An agent from before pairing has no such route (MiraError)."""
        return await self._request("GET", "/v1/pair", 10)

    async def pair(self, code: str) -> str:
        """Trade the pairing code for the agent's token."""
        body = await self._request("POST", "/v1/pair", 10, json={"code": code})
        return str(body["token"])

    async def models(self, base_url: str, api_key: str) -> list[str]:
        """The models a model server offers, asked through the agent."""
        body = await self._request("POST", "/v1/setup/models", 20, json={"base_url": base_url, "api_key": api_key})
        return list(body.get("models") or [])

    async def setup(self, ha_url: str | None, ha_token: str | None, llm: dict[str, Any] | None) -> None:
        """Hand the agent Home Assistant's access and the model; it starts
        anew afterwards."""
        body: dict[str, Any] = {}
        if ha_url and ha_token:
            body["ha_url"], body["ha_token"] = ha_url, ha_token
        if llm:
            body["llm"] = llm
        await self._request("POST", "/v1/setup", 30, json=body)

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

    async def compact_profile(self) -> dict[str, Any]:
        """Rebuild the memory profile from the stored facts now."""
        return await self._request("POST", "/v1/ui/memory/compact", 180)

    async def persona(self) -> str:
        """The persona as the agent has it now."""
        sections = await self._request("GET", "/v1/ui/config", 10)
        return str(((sections or {}).get("persona") or {}).get("value") or "")

    async def set_persona(self, text: str) -> None:
        """Change the persona; the agent checks it first (MiraInvalidConfig)."""
        await self._request("PUT", "/v1/ui/config", 10, json={"persona": text})

    async def managed_config(self) -> dict[str, Any]:
        """Watchers, persons and rooms as the agent has them now."""
        return await self._request("GET", "/v1/ui/ha", 10)

    async def set_managed_config(self, config: dict[str, Any]) -> dict[str, Any]:
        """Replace watchers, persons and rooms; from now on HA manages them."""
        return await self._request("PUT", "/v1/ui/ha", 20, json=config)

    async def release_managed_config(self) -> None:
        """Hand watchers, persons and rooms back to the agent's own web UI."""
        await self._request("DELETE", "/v1/ui/ha", 10)
