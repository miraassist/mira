#!/usr/bin/env python3
"""The test requirements for the newest Home Assistant release
(.github/workflows/ha-next.yml), so a release that breaks the integration
shows up the day after it came out, not when a user installs it.

The test plugin (pytest-homeassistant-custom-component) pins one Home
Assistant version and comes out for stable releases only; betas are not
tested, the newest plugin does not start with them.

  scripts/ha_next.py plugin
      The newest plugin and the Home Assistant it pins, as GitHub outputs
      "plugin" and "ha".

  scripts/ha_next.py pins requirements_test.txt
      Run inside the environment with that Home Assistant installed: the
      packages of requirements_test.txt besides the plugin, at the versions
      that Home Assistant's own components pin.
"""

from __future__ import annotations

import json
import os
import re
import sys
import urllib.request
from pathlib import Path

PLUGIN = "pytest-homeassistant-custom-component"


def _pypi(path: str) -> dict:
    with urllib.request.urlopen(f"https://pypi.org/pypi/{path}/json", timeout=30) as resp:
        return json.load(resp)


def _name(requirement: str) -> str:
    return re.split(r"[=<>!~\[ ;]", requirement, maxsplit=1)[0].strip().lower().replace("_", "-")


def plugin() -> None:
    version = _pypi(PLUGIN)["info"]["version"]
    requires = _pypi(f"{PLUGIN}/{version}")["info"]["requires_dist"]
    ha = next(r.split("==", 1)[1] for r in requires if _name(r) == "homeassistant")
    lines = [f"plugin={version}", f"ha={ha}"]
    print("\n".join(lines))
    if out := os.environ.get("GITHUB_OUTPUT"):
        with open(out, "a", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")


def pins(requirements: str) -> None:
    import homeassistant.components

    wanted = []
    for line in Path(requirements).read_text(encoding="utf-8").splitlines():
        line = line.split("#", 1)[0].strip()
        if line and _name(line) != PLUGIN:
            wanted.append(_name(line))
    pinned: dict[str, str] = {}
    for manifest in Path(homeassistant.components.__path__[0]).glob("*/manifest.json"):
        for req in json.loads(manifest.read_text(encoding="utf-8")).get("requirements", []):
            pinned.setdefault(_name(req), req)
    missing = [name for name in wanted if name not in pinned]
    if missing:
        sys.exit(f"no component of this Home Assistant pins: {', '.join(missing)}")
    print("\n".join(pinned[name] for name in wanted))


if __name__ == "__main__":
    if sys.argv[1:] == ["plugin"]:
        plugin()
    elif len(sys.argv) == 3 and sys.argv[1] == "pins":
        pins(sys.argv[2])
    else:
        sys.exit(__doc__)
