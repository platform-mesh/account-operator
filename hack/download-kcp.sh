#!/usr/bin/env bash
set -euo pipefail
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do
  DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
  SOURCE="$(readlink "$SOURCE")"
  [[ "$SOURCE" != /* ]] && SOURCE="$DIR/$SOURCE"
done
SCRIPT_DIR="$(cd -P "$(dirname "$SOURCE")" >/dev/null 2>&1 && pwd)"
echo "$SCRIPT_DIR"

BIN_DIR="$SCRIPT_DIR/../bin"
mkdir -p "$BIN_DIR"

# Allow overriding the version via environment while defaulting to Taskfile value.
KCP_VERSION="${KCP_VERSION:-0.27.1}"
if [[ "$KCP_VERSION" != v* ]]; then
  KCP_VERSION="v${KCP_VERSION}"
fi

PACKAGE="github.com/kcp-dev/kcp/cmd/kcp@${KCP_VERSION}"

# Install the requested version directly into the repository bin dir to avoid
# cloning the repository and running its make targets (which enforce a specific
# Go patch version).
GOBIN="$BIN_DIR" go install "$PACKAGE"
