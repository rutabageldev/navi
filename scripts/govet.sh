#!/usr/bin/env bash
# Run go vet on each workspace module that contains Go source files.
# Modules with no .go files are skipped cleanly — this is expected during
# early development when stub directories are present but source is not yet written.
set -euo pipefail

EXIT=0

for module_dir in services/internal services/digest; do
    if find "$module_dir" -name "*.go" -not -name "*_test.go" | grep -q .; then
        go vet "./$module_dir/..." || EXIT=$?
    fi
done

exit $EXIT
