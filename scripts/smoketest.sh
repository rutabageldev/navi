#!/usr/bin/env bash
# smoketest.sh — Run the smoke test suite against an environment.
#
# Usage: ./scripts/smoketest.sh [env]
set -euo pipefail

ENV="${1:-staging}"
make smoketest ENV="$ENV"
