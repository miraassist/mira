#!/usr/bin/env bash
# Run the CI checks locally before a push: gofmt, go vet, go test -race,
# the integration's pytest and Home Assistant's hassfest (Docker image).
# The HACS validation needs the GitHub repository and only runs in CI.
#
#   scripts/check.sh              # everything
#   scripts/check.sh --no-docker  # without hassfest
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
docker=1
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

step "go test -race"
go test -race ./... | grep -v '^?'

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
