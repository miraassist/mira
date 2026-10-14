"""HTTP client for the Lares agent."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import aiohttp


class LaresError(Exception):
    """The agent could not be reached or answered with an error."""


class LaresAuthError(LaresError):
    """The agent rejected the token."""


@dataclass
class AgentReply:
    """The agent's answer to one turn."""

    text: str
    conversation_id: str
    continue_conversation: bool


class LaresClient:
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
                    raise LaresAuthError("token rejected")
                if resp.status >= 400:
                    raise LaresError(f"{method} {path}: HTTP {resp.status}")
                return await resp.json()
        except (aiohttp.ClientError, TimeoutError) as err:
            raise LaresError(f"{method} {path}: {err!r}") from err

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
            },
        )
        return AgentReply(
            text=str(data.get("text", "")),
            conversation_id=str(data.get("conversation_id") or conversation_id or ""),
            continue_conversation=bool(data.get("continue_conversation", False)),
        )

    async def status(self) -> dict[str, Any]:
        """Pending notice count and quiet mode."""
        return await self._request("GET", "/v1/status", 10)

    async def set_quiet_mode(self, enabled: bool) -> None:
        """Switch the agent's quiet mode."""
        await self._request("PUT", "/v1/quiet_mode", 10, json={"enabled": enabled})

    async def add_notice(self, notice: dict[str, Any]) -> dict[str, Any]:
        """Queue a notice; the agent decides when and where it is delivered."""
        return await self._request("POST", "/v1/notices", 10, json=notice)
