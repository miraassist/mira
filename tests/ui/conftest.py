"""Fixtures for the acceptance tests of Mira's page in the test Home Assistant.

They need the test HA (go run ./cmd/testha up) and the test Mira on :8788
next to it, plus Playwright with Chromium:

    pip install pytest playwright && playwright install chromium
    pytest tests/ui

Without Playwright or without the test HA they are skipped, so the
integration's tests run as before.
"""

import os
import pathlib
import urllib.request

import pytest

sync_api = pytest.importorskip("playwright.sync_api")

ROOT = pathlib.Path(__file__).resolve().parents[2]
HA = os.environ.get("TESTHA_URL", "http://localhost:8123")


def env() -> dict[str, str]:
    """The test HA's logins from testenv/.env, as `testha up` wrote them."""
    out = {}
    path = ROOT / "testenv" / ".env"
    if path.exists():
        for line in path.read_text().splitlines():
            key, sep, value = line.partition("=")
            if sep and not key.startswith("#"):
                out[key.strip()] = value.strip()
    return out


@pytest.fixture(scope="session")
def logins() -> dict[str, str]:
    e = env()
    if "TESTHA_ADMIN_PASSWORD" not in e:
        pytest.skip("no testenv/.env: run go run ./cmd/testha up")
    try:
        urllib.request.urlopen(HA, timeout=3)
    except OSError:
        pytest.skip(f"test HA not reachable at {HA}")
    return e


@pytest.fixture(scope="session")
def browser():
    with sync_api.sync_playwright() as p:
        b = p.chromium.launch()
        yield b
        b.close()


def open_mira(browser, user: str, password: str):
    """Logs in to HA and returns the page and Mira's frame on the start page."""
    page = browser.new_page(viewport={"width": 1400, "height": 1000})
    page.goto(f"{HA}/mira")
    page.fill("input[name=username]", user)
    page.fill("input[name=password]", password)
    page.keyboard.press("Enter")
    page.wait_for_url("**/mira", timeout=20000)
    frame = page.frame_locator("iframe")
    frame.locator("mira-home .home-grid").wait_for(timeout=20000)
    return page, frame


@pytest.fixture
def admin(browser, logins):
    page, frame = open_mira(browser, "admin", logins["TESTHA_ADMIN_PASSWORD"])
    yield frame
    page.close()


@pytest.fixture
def user(browser, logins):
    """Anna, a person of the house who is no admin."""
    if "TESTHA_ANNA_PASSWORD" not in logins:
        pytest.skip("no login for anna in testenv/.env")
    page, frame = open_mira(browser, "anna", logins["TESTHA_ANNA_PASSWORD"])
    yield frame
    page.close()
