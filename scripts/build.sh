#!/usr/bin/env bash
#
# Builds tdash from this checkout, with `git describe` as the version that --version prints.
#
# Usage: scripts/build.sh [output]   (default: ./tdash)
set -euo pipefail

cd "$(dirname "$0")/.."

name=tdash
out="${1:-./$name}"
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

go build -trimpath -ldflags "-X main.version=$version" -o "$out" .
echo "built $out ($name $version)"
