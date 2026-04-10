#!/usr/bin/env bash
# Validate all event JSON Schema files in docs/events/schemas/.
# Exits non-zero if any schema is invalid.
set -euo pipefail

schema_files=$(find docs/events/schemas -name "*.json" 2>/dev/null)

if [ -z "$schema_files" ]; then
    echo "validate-schemas: no schema files found — skipping"
    exit 0
fi

fail=0
for f in $schema_files; do
    if ! check-jsonschema --check-metaschema "$f" > /dev/null 2>&1; then
        echo "INVALID: $f"
        fail=1
    fi
done

if [ "$fail" -eq 0 ]; then
    echo "validate-schemas: all schemas valid"
fi

exit "$fail"
