"""The agent's own Home Assistant user (docs/spec/setup.md): never an admin,
never a person's account, and one token of the agent at a time."""

import pytest
from homeassistant.auth.const import GROUP_ID_ADMIN, GROUP_ID_USER

from custom_components.mira.access import TOKEN_NAME, USER_NAME, ensure_agent_access


async def test_an_earlier_mira_user_is_taken_again_but_never_a_persons_or_an_admin(hass):
    """After the integration was set up anew: a person in the house may be
    called Mira too."""
    persons = await hass.auth.async_create_user(USER_NAME, group_ids=[GROUP_ID_USER])
    hass.states.async_set("person.mira", "home", {"user_id": persons.id})
    await hass.auth.async_create_user(USER_NAME, group_ids=[GROUP_ID_ADMIN])
    earlier = await hass.auth.async_create_user(USER_NAME, group_ids=[GROUP_ID_USER])

    user_id, _, _ = await ensure_agent_access(hass)
    assert user_id == earlier.id


async def test_the_agent_never_runs_as_an_admin(hass):
    admin = await hass.auth.async_create_user(USER_NAME, group_ids=[GROUP_ID_ADMIN])
    with pytest.raises(RuntimeError, match="admin rights"):
        await ensure_agent_access(hass, admin.id)
    assert not admin.refresh_tokens


async def test_a_new_token_replaces_the_agents_old_one(hass, hass_owner_user):
    user_id, first, _ = await ensure_agent_access(hass)
    _, second, _ = await ensure_agent_access(hass, user_id)
    user = await hass.auth.async_get_user(user_id)
    assert [t.id for t in user.refresh_tokens.values() if t.client_name == TOKEN_NAME] == [second]
    assert first != second
