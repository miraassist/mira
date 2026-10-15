#!/usr/bin/env bash
# Copy the Mira integration into a Home Assistant over SSH and restart Home
# Assistant, so the new code runs. Needs the SSH add-on ("Terminal & SSH" or
# "Advanced SSH & Web Terminal") with this machine's public key in its
# authorized_keys.
#
#   scripts/deploy-integration.sh                 # copy and restart
#   scripts/deploy-integration.sh --no-restart    # copy only
#
# Settings (environment, or .env in the repo):
#   HA_SSH         user@host of the SSH add-on, default root@<host of HA_HOST>
#   HA_SSH_PORT    default 22
#   HA_CONFIG_DIR  Home Assistant's config dir on that host; found by itself
#                  (/config or /homeassistant) when empty
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
if [[ -f "$repo/.env" ]]; then
	set -a
	# shellcheck disable=SC1091
	. "$repo/.env"
	set +a
fi
restart=1
[[ "${1:-}" == "--no-restart" ]] && restart=0

host=${HA_HOST%%:*}
target=${HA_SSH:-root@$host}
port=${HA_SSH_PORT:-22}
ssh_opts=(-p "$port" -o BatchMode=yes -o ConnectTimeout=10)
remote() { ssh "${ssh_opts[@]}" "$target" "$@"; }

version=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$repo/custom_components/mira/manifest.json")
config_dir=${HA_CONFIG_DIR:-$(remote 'for d in /config /homeassistant; do [ -f "$d/configuration.yaml" ] && echo "$d" && break; done')}
if [[ -z "$config_dir" ]]; then
	echo "no Home Assistant config dir found on $target" >&2
	exit 1
fi
dest="$config_dir/custom_components/mira"
echo "Mira integration $version -> $target:$dest"

# Stale bytecode once kept old code running after a copy: remove it, copy
# without the local one.
remote "mkdir -p '$dest' && rm -rf '$dest/__pycache__'"
tar -C "$repo/custom_components/mira" --exclude=__pycache__ --exclude='*.pyc' -cf - . |
	remote "tar -C '$dest' -xf -"
remote "grep '\"version\"' '$dest/manifest.json'"

if ((restart)); then
	echo "restarting Home Assistant ..."
	remote 'ha core restart' >/dev/null
	echo "done; Home Assistant needs about a minute to come back"
fi
