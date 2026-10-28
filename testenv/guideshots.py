"""Takes the guide's pictures of Mira's page (docs/guide/img) in the test HA.

    pip install playwright && playwright install chromium
    python testenv/guideshots.py            # German and English
    python testenv/guideshots.py --lang de  # one language
    python testenv/guideshots.py --out /tmp/shots

Needs the test HA and the test Mira on :8788 (see README.md); logs in as
admin and as anna from testenv/.env. Each user's Home Assistant language is
switched for the pictures and set back afterwards. The browser's clock is
set to the morning, so the start page says "Guten Morgen". What the pictures
show is the test house as it is: for the messages picture send a few first
(action mira.send_message with importance 1), so some are waiting.
"""

import argparse
import datetime
import json
import os
import pathlib

from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parents[1]
HA = os.environ.get("TESTHA_URL", "http://localhost:8123")
HASS = "document.querySelector('home-assistant').hass"
MORNING = datetime.datetime.fromisoformat("2026-10-26T09:30:00+01:00")
WIDTH = 1280


def env() -> dict[str, str]:
    out = {}
    for line in (ROOT / "testenv" / ".env").read_text().splitlines():
        key, sep, value = line.partition("=")
        if sep and not key.startswith("#"):
            out[key.strip()] = value.strip()
    return out


class Session:
    """A browser page logged in to the test HA on Mira's page, in one language."""

    def __init__(self, browser, lang: str, user: str, password: str):
        self.lang = lang
        self.ctx = browser.new_context(viewport={"width": WIDTH, "height": 800},
                                       locale="de-DE" if lang == "de" else "en-US")
        self.ctx.clock.set_system_time(MORNING)
        self.page = self.ctx.new_page()
        self.page.goto(f"{HA}/mira")
        self.page.fill("input[name=username]", user)
        self.page.fill("input[name=password]", password)
        self.page.keyboard.press("Enter")
        self.page.wait_for_url("**/mira")
        self.ready()
        self.old = self.page.evaluate(
            f"{HASS}.connection.sendMessagePromise({{type: 'frontend/get_user_data', key: 'language'}}).then(r => r.value)")
        self.set_language({**(self.old or {}), "language": lang})
        self.page.reload()
        self.ready()

    def ready(self):
        # The panel builds its frame anew once after loading; wait for that.
        self.ui = self.page.frame_locator("iframe")
        self.ui.locator("mira-home .home-grid").wait_for()
        self.page.wait_for_timeout(3000)
        self.frame = next(f for f in self.page.frames if "/api/mira/" in f.url)

    def set_language(self, value):
        self.page.evaluate(
            f"{HASS}.connection.sendMessagePromise({{type: 'frontend/set_user_data', key: 'language', value: {json.dumps(value)}}})")

    def go(self, tab: str, wait: str = "#view section.card"):
        self.frame.evaluate(f"go({tab!r})")
        self.ui.locator(wait).first.wait_for()
        self.page.wait_for_timeout(1500)

    def shot(self, out: pathlib.Path, name: str, height: int):
        self.page.set_viewport_size({"width": WIDTH, "height": height})
        self.page.wait_for_timeout(600)
        self.page.screenshot(path=out / f"{name}-{self.lang}.png")

    def close(self):
        self.set_language(self.old)
        self.ctx.close()


def pictures(browser, lang: str, out: pathlib.Path, logins: dict[str, str]):
    s = Session(browser, lang, "admin", logins["TESTHA_ADMIN_PASSWORD"])
    try:
        s.shot(out, "mira-uebersicht", 1080)
        s.go("tasks"); s.shot(out, "aufgaben-liste", 760)
        s.go("messages"); s.shot(out, "meldungen", 700)
        s.go("skills", "mira-skills details.item"); s.shot(out, "mira-skills", 900)
        s.go("agents", "details.item.agent"); s.shot(out, "mira-agenten", 900)
        s.go("models", "mira-models details.item"); s.shot(out, "mira-modelle", 600)
        # The hooks: the household's first one opened (testenv/hooks).
        s.go("hooks", "mira-hooks details.item")
        s.ui.locator("mira-hooks details.item summary").first.click()
        s.shot(out, "mira-hooks", 900)
        s.go("notifier"); s.shot(out, "benachrichtigung", 800)
        # The model dialog: the second model's entry, its last button is "Edit".
        s.go("models", "mira-models details.item")
        s.page.set_viewport_size({"width": WIDTH, "height": 900})
        s.ui.locator("mira-models details.item summary").nth(1).click()
        s.ui.locator("mira-models details.item[open] button.secondary").last.click()
        s.ui.locator("dialog[open]").wait_for()
        s.shot(out, "mira-bearbeiten", 900)
    finally:
        s.close()
    s = Session(browser, lang, "anna", logins["TESTHA_ANNA_PASSWORD"])
    try:
        s.go("memory"); s.shot(out, "gedaechtnis", 760)
        s.page.set_viewport_size({"width": WIDTH, "height": 560})
        s.ui.locator("#view button.danger").first.click()
        s.ui.locator("dialog#dlg[open]").wait_for()
        s.shot(out, "dialog-vergessen", 560)
        s.ui.locator("#dlg-cancel").click()
    finally:
        s.close()


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--lang", choices=["de", "en"], action="append")
    ap.add_argument("--out", type=pathlib.Path, default=ROOT / "docs" / "guide" / "img")
    args = ap.parse_args()
    logins = env()
    args.out.mkdir(parents=True, exist_ok=True)
    for lang in args.lang or ["de", "en"]:
        # Chromium formats time fields after the system's language.
        loc = "de_DE.UTF-8" if lang == "de" else "en_US.UTF-8"
        with sync_playwright() as p:
            browser = p.chromium.launch(env={**os.environ, "LANG": loc, "LC_ALL": loc, "LANGUAGE": lang})
            try:
                pictures(browser, lang, args.out, logins)
            finally:
                browser.close()
        print(f"{lang}: pictures in {args.out}")


if __name__ == "__main__":
    main()
