"""Mira's access to Home Assistant (docs/spec/hacs-default.md, 4): a
long-lived token of an admin that an admin pastes; the integration makes
none. A token that does not work raises a repair asking for a new one."""

from datetime import timedelta

from homeassistant.auth.const import GROUP_ID_ADMIN, GROUP_ID_USER
from homeassistant.auth.models import TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN, TOKEN_TYPE_NORMAL
from homeassistant.helpers import issue_registry as ir
from homeassistant.setup import async_setup_component

from custom_components.mira import async_remove_entry
from custom_components.mira.access import MADE_TOKEN_NAME, check_token
from custom_components.mira.const import CONF_AGENT_TOKEN, CONF_TOKEN_GIVEN, DOMAIN
from custom_components.mira.coordinator import ISSUE_ACCESS

from .conftest import AGENT_URL


async def admin_token(hass, user, name="Mira"):
    """A long-lived token as one made in the profile: (refresh token, token)."""
    refresh = await hass.auth.async_create_refresh_token(
        user,
        client_name=name,
        token_type=TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN,
        access_token_expiration=timedelta(days=3650),
    )
    return refresh, hass.auth.async_create_access_token(refresh)


async def test_HD31_a_pasted_token_is_checked(hass, hass_owner_user):
    assert check_token(hass, "not a token") == (None, "invalid_token")
    session = await hass.auth.async_create_refresh_token(hass_owner_user, "http://client.test", TOKEN_TYPE_NORMAL)
    assert check_token(hass, hass.auth.async_create_access_token(session)) == (None, "token_not_long_lived")
    user = await hass.auth.async_create_user("Jemand", group_ids=[GROUP_ID_USER])
    _, token = await admin_token(hass, user)
    assert check_token(hass, token) == (None, "token_not_admin")
    refresh, token = await admin_token(hass, hass_owner_user)
    assert check_token(hass, f" {token}\n") == (refresh, "")


async def _with_token(hass, aioclient_mock, entry, refresh_id, given=True):
    """Mira set up, the entry holding the id of the token she was given."""
    hass.config.internal_url = "http://ha.test:8123"
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False})
    aioclient_mock.post(f"{AGENT_URL}/v1/setup", json={"status": "ok"})
    hass.config_entries.async_update_entry(
        entry, data={**entry.data, CONF_AGENT_TOKEN: refresh_id, **({CONF_TOKEN_GIVEN: True} if given else {})}
    )
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


def _setups(aioclient_mock):
    return [c[2] for c in aioclient_mock.mock_calls if c[0] == "POST" and str(c[1]).endswith("/v1/setup")]


def _issue(hass):
    return ir.async_get(hass).async_get_issue(DOMAIN, ISSUE_ACCESS)


def _tokens(user):
    return sorted(t.client_name for t in user.refresh_tokens.values() if t.client_name)


async def test_HD33_a_revoked_token_raises_a_repair_nothing_is_made(hass, aioclient_mock, entry, hass_owner_user):
    refresh, _ = await admin_token(hass, hass_owner_user)
    await _with_token(hass, aioclient_mock, entry, refresh.id)
    assert _issue(hass) is None
    hass.auth.async_remove_refresh_token(refresh)
    await entry.runtime_data.coordinator.async_refresh()
    issue = _issue(hass)
    assert issue is not None and issue.is_fixable and issue.severity is ir.IssueSeverity.ERROR
    assert not _setups(aioclient_mock)
    assert _tokens(hass_owner_user) == []


async def test_HD33_a_deactivated_or_demoted_user_raises_the_repair(hass, aioclient_mock, entry, hass_owner_user):
    user = await hass.auth.async_create_user("Mira", group_ids=[GROUP_ID_ADMIN])
    refresh, _ = await admin_token(hass, user)
    await _with_token(hass, aioclient_mock, entry, refresh.id)
    await hass.auth.async_update_user(user, group_ids=[GROUP_ID_USER])
    await entry.runtime_data.coordinator.async_refresh()
    assert _issue(hass) is not None
    assert not _setups(aioclient_mock)


async def test_HD34_the_repair_takes_a_new_token(hass, aioclient_mock, entry, hass_owner_user, hass_client):
    old, _ = await admin_token(hass, hass_owner_user, name="alt")
    await _with_token(hass, aioclient_mock, entry, old.id)
    hass.auth.async_remove_refresh_token(old)
    await entry.runtime_data.coordinator.async_refresh()
    assert _issue(hass) is not None

    assert await async_setup_component(hass, "repairs", {})
    client = await hass_client()
    resp = await client.post("/api/repairs/issues/fix", json={"handler": DOMAIN, "issue_id": ISSUE_ACCESS})
    flow = await resp.json()
    assert flow["step_id"] == "confirm" and "token" in str(flow["data_schema"])
    # What the checks refuse: the same error as at the setup; the repair stays.
    resp = await client.post(f"/api/repairs/issues/fix/{flow['flow_id']}", json={"token": "not a token"})
    flow = await resp.json()
    assert flow["step_id"] == "confirm" and flow["errors"] == {"token": "invalid_token"}
    assert _issue(hass) is not None and not _setups(aioclient_mock)

    new, token = await admin_token(hass, hass_owner_user)
    resp = await client.post(f"/api/repairs/issues/fix/{flow['flow_id']}", json={"token": token})
    assert (await resp.json())["type"] == "create_entry"
    (sent,) = _setups(aioclient_mock)
    assert sent["ha_token"] == token
    assert entry.data[CONF_AGENT_TOKEN] == new.id and entry.data[CONF_TOKEN_GIVEN]
    assert _issue(hass) is None
    assert _tokens(hass_owner_user) == ["Mira"]


async def test_HD35_the_repair_goes_when_the_token_works_again(hass, aioclient_mock, entry, hass_owner_user):
    user = await hass.auth.async_create_user("Mira", group_ids=[GROUP_ID_ADMIN])
    refresh, _ = await admin_token(hass, user)
    await _with_token(hass, aioclient_mock, entry, refresh.id)
    await hass.auth.async_update_user(user, group_ids=[GROUP_ID_USER])
    await entry.runtime_data.coordinator.async_refresh()
    assert _issue(hass) is not None
    await hass.auth.async_update_user(user, group_ids=[GROUP_ID_ADMIN])
    await entry.runtime_data.coordinator.async_refresh()
    assert _issue(hass) is None


async def test_HD36_the_repair_is_there_once(hass, aioclient_mock, entry, hass_owner_user, caplog):
    refresh, _ = await admin_token(hass, hass_owner_user)
    await _with_token(hass, aioclient_mock, entry, refresh.id)
    hass.auth.async_remove_refresh_token(refresh)
    for _ in range(3):
        await entry.runtime_data.coordinator.async_refresh()
    assert _issue(hass) is not None
    assert caplog.text.count("Mira's token does not work") == 1


async def test_HD37_the_token_the_integration_made_is_revoked_at_the_update(
    hass, aioclient_mock, entry, hass_owner_user, caplog
):
    made, _ = await admin_token(hass, hass_owner_user, name=MADE_TOKEN_NAME)
    own, _ = await admin_token(hass, hass_owner_user, name="Mein Skript")
    await _with_token(hass, aioclient_mock, entry, made.id, given=False)
    assert hass.auth.async_get_refresh_token(made.id) is None
    assert hass.auth.async_get_refresh_token(own.id) is not None
    assert f"Revoked the token '{MADE_TOKEN_NAME}'" in caplog.text
    assert _issue(hass) is not None
    assert not _setups(aioclient_mock)


async def test_HD37_a_given_token_of_that_name_stays(hass, aioclient_mock, entry, hass_owner_user):
    """An admin who named their pasted token like the old one keeps it."""
    given, _ = await admin_token(hass, hass_owner_user, name=MADE_TOKEN_NAME)
    await _with_token(hass, aioclient_mock, entry, given.id)
    assert hass.auth.async_get_refresh_token(given.id) is not None
    assert _issue(hass) is None


async def test_HD38_removing_mira_revokes_nothing(hass, aioclient_mock, entry, hass_owner_user):
    refresh, _ = await admin_token(hass, hass_owner_user)
    await _with_token(hass, aioclient_mock, entry, refresh.id)
    aioclient_mock.delete(f"{AGENT_URL}/v1/ui/ha", json={})
    aioclient_mock.post(f"{AGENT_URL}/v1/unpair", json={})
    await async_remove_entry(hass, entry)
    assert hass.auth.async_get_refresh_token(refresh.id) is not None
