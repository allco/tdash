#!/usr/bin/env bash
#
# Builds tdash from this checkout and installs it into ~/bin, or into $INSTALL_DIR when set.
#
# Usage: scripts/install.sh
set -euo pipefail

cd "$(dirname "$0")/.."

name=tdash
dir="${INSTALL_DIR:-$HOME/bin}"

mkdir -p "$dir"
# Build next to the target, then rename, so a running copy is never overwritten in place.
scripts/build.sh "$dir/.$name.tmp"
mv -f "$dir/.$name.tmp" "$dir/$name"
echo "installed $dir/$name: $("$dir/$name" --version)"
case ":$PATH:" in *":$dir:"*) ;; *) echo "note: $dir is not on PATH" >&2 ;; esac
