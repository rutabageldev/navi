#!/usr/bin/env bash
# Verify that generated files are current with their source specs.
# Exits non-zero if any generated file is stale.
set -euo pipefail

export PATH="$HOME/go/bin:$HOME/bin:$PATH"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

fail=0

# --- OpenAPI: services/digest/api/openapi.yaml ---
# Create a temporary config that writes to our temp dir so we can diff.
TMPOUT="$TMPDIR/api.gen.go"
cat services/digest/api/oapi-codegen.yaml \
    | sed "s|output:.*|output: $TMPOUT|" \
    > "$TMPDIR/oapi-codegen.yaml"

oapi-codegen \
    --config "$TMPDIR/oapi-codegen.yaml" \
    services/digest/api/openapi.yaml 2>/dev/null

if ! diff -q "$TMPOUT" services/digest/internal/api/gen/api.gen.go > /dev/null 2>&1; then
    echo "STALE: services/digest/internal/api/gen/api.gen.go is out of date with openapi.yaml"
    echo "Run: cd services/digest/api && oapi-codegen --config oapi-codegen.yaml openapi.yaml"
    fail=1
fi

if [ "$fail" -eq 0 ]; then
    echo "check-generated: all generated files are current"
fi

exit "$fail"
