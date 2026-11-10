#!/usr/bin/env python3
"""Hands one real device's Zeroconf announcement to the test HA.

The test HA runs in a bridge network on purpose and hears no announcement of the
LAN (it would find the whole house). This script listens on this machine with
`avahi-browse` (pushed: avahi reports each announcement, nothing is polled) for
exactly the one device whose announced name holds the name given (its MAC, say), and when it announces itself calls the test action
`found_fake.zeroconf`, which starts the real integration's Zeroconf discovery
(docs/spec/found-devices.md, Testing). Then it ends.

    set -a; . testenv/.env; set +a
    python3 testenv/zeroconf_relay.py --domain shelly --name shelly1-3494546A75B7

Needs `avahi-browse` (package avahi-utils) and TESTHA_ADMIN_TOKEN.
"""

from __future__ import annotations

import argparse
import json
import os
import shlex
import subprocess
import sys
import urllib.request


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--domain", required=True, help="the integration, e.g. shelly")
    p.add_argument("--name", required=True,
                   help="the announced instance name or a part of it (its MAC), e.g. 3494546a75b7")
    p.add_argument("--type", default="_http._tcp", help="the service type (default _http._tcp)")
    p.add_argument("--ha", default="http://localhost:8123", help="the test HA")
    p.add_argument("--iface", default="", help="the home network's interface (default: the one of the default route)")
    args = p.parse_args()
    token = os.environ.get("TESTHA_ADMIN_TOKEN")
    if not token:
        print("TESTHA_ADMIN_TOKEN is not set (set -a; . testenv/.env; set +a)", file=sys.stderr)
        return 2

    want = args.name.lower()
    # Only the home network counts: while Mira's machine is joined to the
    # device's own Wi-Fi, avahi hears the device there too (192.168.33.1).
    iface = args.iface or default_iface()
    # What avahi has cached already is old (a device reset since keeps its
    # announcement there until it expires): it is skipped until avahi removes
    # it or the device announces itself at another address.
    old = subprocess.run(["avahi-browse", "-r", "-p", "-t", args.type], capture_output=True, text=True, timeout=30).stdout
    stale = {}
    for line in old.splitlines():
        f = line.split(";")
        if len(f) >= 9 and f[0] == "=" and f[1] == iface and f[2] == "IPv4" and want in f[3].lower():
            stale[f[3]] = f[7]
    if stale:
        print(f"skipping what avahi has cached already: {stale}", flush=True)
    print(f"waiting for {args.name} ({args.type}) to announce itself on {iface} ...", flush=True)
    # -r resolves, -p parseable; without -t avahi keeps reporting until we stop.
    proc = subprocess.Popen(["avahi-browse", "-r", "-p", args.type], stdout=subprocess.PIPE, text=True)
    try:
        for line in proc.stdout:
            f = line.rstrip("\n").split(";")
            # +/-/=;iface;proto;name;type;domain;host;address;port;txt
            if len(f) < 4 or f[1] != iface or f[2] != "IPv4" or want not in f[3].lower():
                continue
            if f[0] == "-" and stale.pop(f[3], None):
                print(f"avahi removed the old {f[3]}", flush=True)
            if f[0] != "=" or len(f) < 9 or stale.get(f[3]) == f[7]:
                continue
            host, port = f[7], int(f[8])
            txt = dict(kv.split("=", 1) for kv in shlex.split(";".join(f[9:])) if "=" in kv)
            body = {"domain": args.domain, "name": f[3], "host": host, "port": port, "type": f"{args.type}.local.",
                    "properties": txt}
            print(f"announced at {host}:{port}; handing it to {args.domain}", flush=True)
            req = urllib.request.Request(f"{args.ha}/api/services/found_fake/zeroconf?return_response",
                                         data=json.dumps(body).encode(), method="POST",
                                         headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=30) as resp:
                print(resp.read().decode(), flush=True)
            return 0
    finally:
        proc.terminate()
    print("avahi-browse ended before the device announced itself", file=sys.stderr)
    return 1



def default_iface() -> str:
    """The interface of the default route (ip route), the home network."""
    out = subprocess.run(["ip", "route", "show", "default"], capture_output=True, text=True).stdout.split()
    return out[out.index("dev") + 1] if "dev" in out else ""


if __name__ == "__main__":
    sys.exit(main())
