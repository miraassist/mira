#!/usr/bin/env bash
# Run the CI checks locally before a push: gofmt, go vet, staticcheck, go test -race,
# ruff (lint and format) and pytest for the integration, the syntax of the
# page's JavaScript (Node, from nvm when it is not on the PATH), and Home
# Assistant's hassfest (Docker image).
# The HACS validation needs the GitHub repository and only runs in CI.
#
#   scripts/check.sh              # everything
#   scripts/check.sh --no-docker  # without hassfest
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
docker=1
# staticcheck 2026.2.2; the same version as in .github/workflows/ci.yml.
staticcheck_version=v0.8.2
# ruff with its default rules; the same version as in .github/workflows/ci.yml.
ruff_version=0.17.3
[[ "${1:-}" == "--no-docker" ]] && docker=0

step() { printf '\n== %s\n' "$*"; }

step gofmt
unformatted=$(gofmt -l .)
if [[ -n "$unformatted" ]]; then
	echo "$unformatted"
	exit 1
fi

step "go vet"
go vet ./...

step staticcheck
go run "honnef.co/go/tools/cmd/staticcheck@$staticcheck_version" ./...

step "go test -race"
# A small deadline: a hanging test must not cost Go's default of 10 minutes
# per package; the panic names the test that stands (CLAUDE.md, Releasing).
go test -race -timeout 120s ./... | grep -v '^?'

step ruff
uvx "ruff@$ruff_version" check custom_components tests
uvx "ruff@$ruff_version" format --check custom_components tests

step pytest
if [[ -x .venv/bin/pytest ]]; then
	.venv/bin/pytest -q tests
else
	python3 -m pytest -q tests
fi

step "page scripts"
if ! command -v node >/dev/null && [[ -s "${NVM_DIR:-$HOME/.nvm}/nvm.sh" ]]; then
	set +u
	# shellcheck disable=SC1091
	. "${NVM_DIR:-$HOME/.nvm}/nvm.sh"
	set -u
fi
if ! command -v node >/dev/null; then
	echo "node not found: install it with nvm (nvm install --lts)"
	exit 1
fi
node scripts/check-js.mjs

if ((docker)); then
	step hassfest
	out=$(docker run --rm -v "$repo":/github/workspace ghcr.io/home-assistant/hassfest 2>&1 || true)
	grep -v -E '^Validating ' <<<"$out"
	grep -q '^Invalid integrations: 0$' <<<"$out"
fi

printf '\nall checks passed\n'
