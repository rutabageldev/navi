#!/usr/bin/env bash
# Run govulncheck against each module in the go.work workspace.
# Reads the required toolchain version from go.work and prepends the
# corresponding Go SDK bin to PATH so govulncheck uses the right stdlib.
# Exits non-zero if any module has vulnerabilities.
set -euo pipefail

export PATH="$HOME/go/bin:$HOME/bin:$PATH"
export GOTOOLCHAIN=auto

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Resolve the toolchain version from go.work and add its bin to PATH
# so govulncheck scans against the correct stdlib version.
TOOLCHAIN=$(grep '^toolchain ' "$REPO_ROOT/go.work" 2>/dev/null | awk '{print $2}' || true)
if [ -n "$TOOLCHAIN" ] && [ -d "$HOME/sdk/$TOOLCHAIN/bin" ]; then
    export PATH="$HOME/sdk/$TOOLCHAIN/bin:$PATH"
fi

EXIT=0
for module_dir in services/internal services/digest; do
    if find "$REPO_ROOT/$module_dir" -name "*.go" -not -name "*_test.go" | grep -q .; then
        (cd "$REPO_ROOT/$module_dir" && govulncheck ./...) || EXIT=$?
    fi
done
exit $EXIT
