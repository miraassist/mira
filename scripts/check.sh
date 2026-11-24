#!/usr/bin/env bash
# Run the CI checks of the integration locally before a push: ruff (lint and
# format), pytest, and Home Assistant's hassfest (Docker image). The HACS
# validation needs the GitHub repository and only runs in CI. The Mira service
# is in its own repository (miraassist/mira-app) with its own checks; with a
# checkout of it beside this one (or MIRA_APP naming it), pytest also checks
# the automations the service builds.
#
#   scripts/check.sh              # everything
#   scripts/check.sh --no-docker  # without hassfest
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
docker=1
# ruff with its default rules; the same version as in .github/workflows/ci.yml.
ruff_version=0.17.3
for arg in "$@"; do
	case "$arg" in
	--no-docker) docker=0 ;;
	*)
		echo "unknown option: $arg"
		exit 1
		;;
	esac
done
export MIRA_APP="${MIRA_APP:-$repo/../mira-app}"
if [[ ! -d "$MIRA_APP/internal" ]]; then
	echo "no Mira service at $MIRA_APP: its automations are not checked (set MIRA_APP)"
	unset MIRA_APP
fi

step() { printf '\n== %s\n' "$*"; }

step ruff
uvx "ruff@$ruff_version" check custom_components tests
uvx "ruff@$ruff_version" format --check custom_components tests

step pytest
if [[ -x .venv/bin/pytest ]]; then
	.venv/bin/pytest -q tests
else
	python3 -m pytest -q tests
fi

if ((docker)); then
	step hassfest
	out=$(docker run --rm -v "$repo":/github/workspace ghcr.io/home-assistant/hassfest 2>&1 || true)
	grep -v -E '^Validating ' <<<"$out"
	grep -q '^Invalid integrations: 0$' <<<"$out"
fi

printf '\nall checks passed\n'
