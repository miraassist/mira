"""The fake answering machine answers as Mira FRITZ!Box's (docs/spec/fake-devices.md,
"The answering machine", scenarios FG-30 to FG-32)."""

from __future__ import annotations

from pathlib import Path

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.setup import async_setup_component

import custom_components

REPO = Path(__file__).resolve().parents[2]
FAKE = REPO / "testenv" / "fritz_fake"
COUNT = "sensor.anrufbeantworter_neue_nachrichten"
MISSED = "sensor.anrufbeantworter_verpasste_anrufe"


@pytest.fixture
async def box(hass: HomeAssistant, tmp_path, monkeypatch):
    """The fake loaded from testenv as the test HA mounts it: as mira_fritz."""
    (tmp_path / "custom").mkdir()
    (tmp_path / "custom" / "mira_fritz").symlink_to(FAKE)
    monkeypatch.setattr(custom_components, "__path__", [*custom_components.__path__, str(tmp_path / "custom")])
    hass.data.pop("custom_components", None)
    assert await async_setup_component(hass, "mira_fritz", {"mira_fritz": {}})
    await hass.async_block_till_done()


async def messages(hass: HomeAssistant, new_only: bool) -> list[dict]:
    out = await hass.services.async_call(
        "mira_fritz", "get_messages", {"new_only": new_only}, blocking=True, return_response=True
    )
    return out["messages"]


async def test_fg30_new_recordings_raise_the_count_at_once(hass: HomeAssistant, box) -> None:
    seen: list[str] = []
    hass.bus.async_listen(
        "state_changed", lambda e: e.data["entity_id"] == COUNT and seen.append(e.data["new_state"].state)
    )
    assert hass.states.get(COUNT).state == "0"
    assert hass.states.get(MISSED).state == "0"
    await hass.services.async_call("mira_fritz", "fake_add", {"recordings": ["Oma", "Praxis Dr. Kaya"]}, blocking=True)
    await hass.async_block_till_done()
    assert seen == ["2"]


async def test_fg31_the_actions_answer_as_mira_fritzbox(hass: HomeAssistant, box) -> None:
    await hass.services.async_call(
        "mira_fritz", "fake_add", {"recordings": ["Oma", "Ben"], "minutes": 2}, blocking=True
    )
    listed = await messages(hass, True)
    assert [m["name"] for m in listed] == ["Oma", "Ben"]
    first = listed[0]
    assert set(first) == {"id", "name", "date", "minutes", "new", "media_content_id", "answering_machine_name"}
    assert first["new"] is True and first["minutes"] == 2 and first["answering_machine_name"] == "Anrufbeantworter"

    await hass.services.async_call("mira_fritz", "mark_heard", {"message_ids": [first["id"]]}, blocking=True)
    await hass.async_block_till_done()
    assert hass.states.get(COUNT).state == "1"
    assert [m["name"] for m in await messages(hass, True)] == ["Ben"]
    assert len(await messages(hass, False)) == 2

    calls = await hass.services.async_call(
        "mira_fritz", "get_calls", {"kind": "missed", "new_only": True}, blocking=True, return_response=True
    )
    assert calls == {"calls": []}
    book = await hass.services.async_call("mira_fritz", "get_phonebook", {}, blocking=True, return_response=True)
    assert book["contacts"] == []


async def test_fg32_the_box_is_emptied(hass: HomeAssistant, box) -> None:
    await hass.services.async_call("mira_fritz", "fake_add", {"recordings": ["Oma"]}, blocking=True)
    await hass.services.async_call("mira_fritz", "fake_reset", {}, blocking=True)
    await hass.async_block_till_done()
    assert await messages(hass, False) == []
    assert hass.states.get(COUNT).state == "0"
