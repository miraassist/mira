"""HTTP client for the Mira agent."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import aiohttp


class MiraError(Exception):
    """The agent could not be reached or answered with an error."""


class MiraAuthError(MiraError):
    """The agent rejected the token."""


class MiraInvalidConfig(MiraError):
    """The agent refused a config change; the message says why."""


@dataclass
class AgentReply:
    """The agent's answer to one turn."""

    text: str
    conversation_id: str
    continue_conversation: bool


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
                if resp.status == 422:
                    body = await resp.json(content_type=None)
                    raise MiraInvalidConfig(str((body or {}).get("error", "invalid config")))
                if resp.status >= 400:
                    raise MiraError(f"{method} {path}: HTTP {resp.status}")
                return await resp.json()
        except (aiohttp.ClientError, TimeoutError) as err:
            raise MiraError(f"{method} {path}: {err!r}") from err

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
            },
        )
        return AgentReply(
            text=str(data.get("text", "")),
            conversation_id=str(data.get("conversation_id") or conversation_id or ""),
            continue_conversation=bool(data.get("continue_conversation", False)),
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

    async def managed_config(self) -> dict[str, Any]:
        """Watchers, persons and rooms as the agent has them now."""
        return await self._request("GET", "/v1/ui/ha", 10)

    async def set_managed_config(self, config: dict[str, Any]) -> dict[str, Any]:
        """Replace watchers, persons and rooms; from now on HA manages them."""
        return await self._request("PUT", "/v1/ui/ha", 20, json=config)

    async def release_managed_config(self) -> None:
        """Hand watchers, persons and rooms back to the agent's own web UI."""
        await self._request("DELETE", "/v1/ui/ha", 10)
