"""Mira's access to Home Assistant (docs/spec/hacs-default.md, 3): a
long-lived token of the owner, one at a time; what became of it is told by
its id."""

from homeassistant.auth.const import GROUP_ID_ADMIN, GROUP_ID_USER
from homeassistant.auth.models import TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN
from homeassistant.helpers import issue_registry as ir
from homeassistant.setup import async_setup_component

from custom_components.mira.access import OLD_TOKEN_NAME, TOKEN_NAME, make_owner_token, token_state
from custom_components.mira.const import CONF_AGENT_TOKEN, DOMAIN
from custom_components.mira.coordinator import ISSUE_ACCESS

from .conftest import AGENT_URL


async def test_the_token_is_the_owners_and_replaces_only_miras_earlier_one(hass, hass_owner_user):
    own = await hass.auth.async_create_refresh_token(
        hass_owner_user, client_name="Mein Skript", token_type=TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN
    )
    first, _ = await make_owner_token(hass)
    second, access = await make_owner_token(hass)
    assert first != second
    names = sorted(t.client_name for t in hass_owner_user.refresh_tokens.values() if t.client_name)
    assert names == sorted([TOKEN_NAME, "Mein Skript"])
    assert hass.auth.async_get_refresh_token(own.id) is not None
    refresh = hass.auth.async_validate_access_token(access)
    assert refresh is not None and refresh.id == second and refresh.user.is_owner


async def test_what_became_of_the_token(hass, hass_owner_user):
    assert token_state(hass, None) == ""
    refresh_id, _ = await make_owner_token(hass)
    assert token_state(hass, refresh_id) == "owner"
    old_user = await hass.auth.async_create_user("Mira", group_ids=[GROUP_ID_ADMIN])
    old = await hass.auth.async_create_refresh_token(
        old_user, client_name="Mira agent", token_type=TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN
    )
    assert token_state(hass, old.id) == "old"
    hass.auth.async_remove_refresh_token(hass.auth.async_get_refresh_token(refresh_id))
    assert token_state(hass, refresh_id) == "gone"


async def _with_token(hass, aioclient_mock, entry, refresh_id):
    """Mira set up, the entry holding the token the integration made for her."""
    hass.config.internal_url = "http://ha.test:8123"
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False})
    aioclient_mock.post(f"{AGENT_URL}/v1/setup", json={"status": "ok"})
    hass.config_entries.async_update_entry(entry, data={**entry.data, CONF_AGENT_TOKEN: refresh_id})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


def _setups(aioclient_mock):
    return [c[2] for c in aioclient_mock.mock_calls if c[0] == "POST" and str(c[1]).endswith("/v1/setup")]


async def test_HD21_an_existing_install_is_asked_for_the_new_access(
    hass, aioclient_mock, entry, hass_owner_user, hass_client
):
    old_user = await hass.auth.async_create_user("Mira", group_ids=[GROUP_ID_USER])
    old = await hass.auth.async_create_refresh_token(
        old_user, client_name=OLD_TOKEN_NAME, token_type=TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN
    )
    await _with_token(hass, aioclient_mock, entry, old.id)
    issue = ir.async_get(hass).async_get_issue(DOMAIN, ISSUE_ACCESS)
    assert issue is not None and issue.is_fixable
    assert issue.translation_placeholders == {"owner": hass_owner_user.name}
    assert not _setups(aioclient_mock)

    assert await async_setup_component(hass, "repairs", {})
    client = await hass_client()
    resp = await client.post("/api/repairs/issues/fix", json={"handler": DOMAIN, "issue_id": ISSUE_ACCESS})
    flow = await resp.json()
    assert flow["step_id"] == "confirm" and flow["description_placeholders"] == {"owner": hass_owner_user.name}
    resp = await client.post(f"/api/repairs/issues/fix/{flow['flow_id']}", json={})
    assert (await resp.json())["type"] == "create_entry"

    (sent,) = _setups(aioclient_mock)
    refresh = hass.auth.async_validate_access_token(sent["ha_token"])
    assert refresh is not None and refresh.user.is_owner and entry.data[CONF_AGENT_TOKEN] == refresh.id
    assert hass.auth.async_get_refresh_token(old.id) is None
    assert await hass.auth.async_get_user(old_user.id) is not None  # the old user stays
    assert ir.async_get(hass).async_get_issue(DOMAIN, ISSUE_ACCESS) is None


async def test_HD27_a_revoked_token_is_made_anew(hass, aioclient_mock, entry, hass_owner_user, caplog):
    refresh_id, _ = await make_owner_token(hass)
    await _with_token(hass, aioclient_mock, entry, refresh_id)
    assert not _setups(aioclient_mock)
    # The owner revokes it in the profile.
    hass.auth.async_remove_refresh_token(hass.auth.async_get_refresh_token(refresh_id))
    await entry.runtime_data.coordinator.async_refresh()
    (sent,) = _setups(aioclient_mock)
    refresh = hass.auth.async_validate_access_token(sent["ha_token"])
    assert refresh is not None and refresh.user.is_owner and entry.data[CONF_AGENT_TOKEN] == refresh.id
    assert "Made a new token for Mira, the old one was revoked" in caplog.text
