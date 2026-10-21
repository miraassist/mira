"""What did not start at the agent shows in Home Assistant as a repair."""

from homeassistant.helpers import issue_registry as ir

from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

PROBLEM = {
    "source": "skill",
    "name": "abfall",
    "text": "Die Einstellung „calendar“ fehlt in der mira.yaml unter skills.enabled.",
}


async def test_skill_problem_is_a_repair_until_it_is_gone(hass, aioclient_mock, entry):
    status = {"pending_messages": 0, "quiet_mode": False, "problems": [PROBLEM]}
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=status)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    registry = ir.async_get(hass)
    issue = registry.async_get_issue(DOMAIN, "skill_abfall")
    assert issue is not None
    assert issue.translation_key == "skill_problem"
    assert issue.translation_placeholders == {"name": "abfall", "text": PROBLEM["text"]}
    assert issue.severity is ir.IssueSeverity.WARNING and not issue.is_fixable

    # Fixed and restarted: the agent reports nothing, the repair goes away.
    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False, "problems": []})
    await entry.runtime_data.coordinator.async_refresh()
    assert registry.async_get_issue(DOMAIN, "skill_abfall") is None


async def test_agent_problem_is_a_repair_of_its_own(hass, aioclient_mock, entry):
    """A YAML agent that does not fit says so as an agent, and goes away like a skill's."""
    problem = {"source": "agent", "name": "plauder", "text": "Der Kontext „conversation“ geht hier nicht."}
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False, "problems": [problem]})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    registry = ir.async_get(hass)
    issue = registry.async_get_issue(DOMAIN, "agent_plauder")
    assert issue is not None and issue.translation_key == "agent_problem"

    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False, "problems": []})
    await entry.runtime_data.coordinator.async_refresh()
    assert registry.async_get_issue(DOMAIN, "agent_plauder") is None
