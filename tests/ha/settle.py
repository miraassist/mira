"""Waiting for what a test set off, without time (docs/spec/tests.md, step 5)."""

from __future__ import annotations

import asyncio

from homeassistant.core import HomeAssistant


async def settle(hass: HomeAssistant) -> None:
    """Let what the last change or a moved clock set off run: until Home
    Assistant's executor jobs are done and the event loop has nothing ready
    to run. A run waiting in a delay is a timer, not ready, and stays; Home
    Assistant's async_block_till_done would wait for it. The bound only
    stops a callback that keeps scheduling itself."""
    loop = asyncio.get_running_loop()
    for _ in range(10_000):
        await asyncio.sleep(0)
        # Executor jobs are futures, not tasks, among what Home Assistant tracks.
        jobs = [f for f in (*hass._tasks, *hass._background_tasks) if not isinstance(f, asyncio.Task) and not f.done()]
        if jobs:
            await asyncio.wait(jobs)
            continue
        if not loop._ready:
            return
    raise AssertionError("the event loop never got idle")
