#!/usr/bin/env python3
"""Puts the text of a freeze SVG on the terminal's cell grid, in place.

freeze writes each screen line as one <text> of <tspan>s and lets the font advance the text. That
breaks on wide characters: a terminal gives an emoji such as ✅ two cells, but a monospace font draws
it one cell wide (or a fallback font draws it at its own width), and everything after it on the line
shifts. This gives every run of text an explicit x at its cell, and draws wide characters with a
colour emoji font, as a terminal would.

Usage: svg-grid.py file.svg
"""
import html
import re
import sys
import unicodedata

TEXT = re.compile(r'(<text x="([\d.]+)px"[^>]*>)(.*?)</text>', re.S)
TSPAN = re.compile(r"<tspan([^>]*?)(?:/>|>(.*?)</tspan>)", re.S)
EMOJI_FONT = "Apple Color Emoji, Noto Color Emoji, Segoe UI Emoji, sans-serif"


def width(ch):
    """Cells a character takes, as Go's ansi.StringWidth counts them."""
    if unicodedata.combining(ch) or ch in "‍︎️":
        return 0
    return 2 if unicodedata.east_asian_width(ch) in "WF" else 1


def runs(text):
    """Splits text into (run, cells, wide) with each wide character in a run of its own."""
    out = []
    for ch in text:
        w = width(ch)
        if w == 0 and out:
            r, c, wide = out[-1]
            out[-1] = (r + ch, c, wide)
        elif w == 2 or not out or out[-1][2]:
            out.append((ch, w, w == 2))
        else:
            r, c, _ = out[-1]
            out[-1] = (r + ch, c + w, False)
    return out


def main():
    path = sys.argv[1]
    svg = open(path, encoding="utf-8").read()

    lines = [[(attrs, html.unescape(t or "")) for attrs, t in TSPAN.findall(m.group(3))]
             for m in TEXT.finditer(svg)]
    cols = max((sum(width(c) for _, t in spans for c in t) for spans in lines), default=0)
    first = TEXT.search(svg)
    if not cols or not first:
        return
    x0 = float(first.group(2))
    total = float(re.search(r'<svg width="([\d.]+)"', svg).group(1))
    cell = (total - 2 * x0) / cols

    def place(m):
        col, out = 0, []
        for attrs, text in TSPAN.findall(m.group(3)):
            for run, cells, wide in runs(html.unescape(text or "")):
                extra = f' font-family="{EMOJI_FONT}" font-size="12px"' if wide else ""
                out.append(f'<tspan{attrs} x="{x0 + col * cell:.2f}px"{extra}>'
                           f"{html.escape(run, quote=False)}</tspan>")
                col += cells
        return m.group(1) + "".join(out) + "</text>"

    open(path, "w", encoding="utf-8").write(TEXT.sub(place, svg))


if __name__ == "__main__":
    main()
