#!/usr/bin/env bash
#
# Regenerates docs/screenshots/*.png from made-up data (see screenshot_test.go).
#
# Needs freeze (go install github.com/charmbracelet/freeze@v0.2.2) and Google Chrome. freeze turns
# the app's coloured output into an SVG; Chrome renders that to PNG, because freeze's own PNG
# export has no fallback for glyphs its font lacks.
#
# The charts draw with braille dots, which freeze's default font doesn't have; any fallback font
# draws them at another width and pushes the rest of the line out of place. So the SVGs embed
# JuliaMono, which has them (and every other glyph tdash draws) at the same width. It's
# downloaded once into ~/.cache/tdash; set FONT to use another monospace font file.
set -euo pipefail

cd "$(dirname "$0")/.."

chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
command -v freeze >/dev/null || { echo "freeze not found; see the header of $0" >&2; exit 1; }
[ -x "$chrome" ] || { echo "Chrome not found at $chrome; set CHROME" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

font="${FONT:-${XDG_CACHE_HOME:-$HOME/.cache}/tdash/JuliaMono-Regular.ttf}"
if [ ! -f "$font" ]; then
  echo "downloading JuliaMono to $font"
  mkdir -p "$(dirname "$font")" "$tmp/font"
  curl -fsSL -o "$tmp/font/juliamono.zip" \
    https://github.com/cormullion/juliamono/releases/download/v0.63.2/JuliaMono-ttf.zip
  unzip -q -j "$tmp/font/juliamono.zip" '*JuliaMono-Regular.ttf' -d "$tmp/font"
  mv "$tmp/font/JuliaMono-Regular.ttf" "$font"
fi

TDASH_SCREENSHOTS="$tmp" go test -run TestScreenshots -count=1 . >/dev/null

mkdir -p docs/screenshots
for ansi in "$tmp"/*.ansi; do
  name="$(basename "$ansi" .ansi)"
  svg="$tmp/$name.svg"
  freeze "$ansi" --language ansi --window --padding 20,24 --margin 0 \
    --border.radius 8 --font.size 14 --font.file "$font" --font.family "JuliaMono" --output "$svg" </dev/null >/dev/null

  # The page is exactly the SVG's size, so the PNG has no border around the window.
  ceil() { awk '{ print int($1) + ($1 > int($1)) }'; }
  width="$(grep -o -m1 'width="[0-9.]*"' "$svg" | grep -o '[0-9.]*' | ceil)"
  height="$(grep -o -m1 'height="[0-9.]*"' "$svg" | grep -o '[0-9.]*' | ceil)"
  printf '<html><body style="margin:0"><img src="%s.svg"></body></html>' "$name" > "$tmp/$name.html"

  "$chrome" --headless=new --disable-gpu --hide-scrollbars \
    --default-background-color=00000000 --force-device-scale-factor=2 \
    --window-size="$width,$height" --screenshot="docs/screenshots/$name.png" \
    "file://$tmp/$name.html" >/dev/null 2>&1
  echo "wrote docs/screenshots/$name.png"
done
