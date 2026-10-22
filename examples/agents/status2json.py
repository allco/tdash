#!/usr/bin/env python3
"""Turns an agents status Markdown file (on stdin) into JSON for the agents dashboard.

Output:
  {"header": "<markdown>", "questions": "<markdown>",
   "sections": {"<slug>": {"title": ..., "text": "<markdown without tables>",
                           "rows": [{"<column header>": "<cell>", ...}]}}}
Table rows whose first cell is empty continue the row above. An "MR" cell like !4262 gets a
"url" next to it, built from $MR_URL.
"""
import json
import os
import re
import sys

MR_URL = os.environ.get("MR_URL", "https://gitlab.example.com/acme/app/-/merge_requests/")


def slug(s):
    return re.sub(r"[^a-z0-9]+", "_", s.lower()).strip("_")


def cells(line):
    return [c.strip() for c in line.strip().strip("|").split("|")]


def parse_table(lines):
    head = cells(lines[0])
    rows = []
    for l in lines[1:]:
        if re.fullmatch(r"\|[\s|:-]*\|", l.strip()):
            continue
        cs = cells(l) + [""] * len(head)
        if rows and cs[0] == "":
            for h, c in zip(head, cs):
                if c:
                    rows[-1][h] = (rows[-1][h] + " " + c).strip()
            continue
        row = dict(zip(head, cs))
        m = re.search(r"!(\d+)", row.get("MR", ""))
        if m:
            row["url"] = MR_URL + m.group(1)
        rows.append(row)
    return rows


def main():
    src = sys.stdin.read().splitlines()
    out = {"header": "", "questions": "", "sections": {}}
    title_line = ""
    section = None
    body = {}
    order = []
    for l in src:
        if l.startswith("# ") and not title_line:
            title_line = l[2:].strip()
            continue
        if l.startswith("## "):
            section = l[3:].strip()
            order.append(section)
            body[section] = []
            continue
        if section:
            body[section].append(l)

    for name in order:
        lines, text, rows, table = body[name], [], [], []
        for l in lines + [""]:
            if l.lstrip().startswith("|"):
                table.append(l)
                continue
            if table:
                rows += parse_table(table)
                table = []
            text.append(l)
        md = re.sub(r"\n{3,}", "\n\n", "\n".join(text)).strip()
        out["sections"][slug(name)] = {"title": name, "text": md, "rows": rows}

    # "Checkout status · updated 16:11 CEST · merged: Parts 1–6" → a heading and dim lines.
    parts = [p.strip() for p in title_line.split("·")]
    header = ["# " + parts[0]] if parts else []
    for p in parts[1:]:
        k, _, v = p.partition(" ") if not p.startswith("merged:") else ("merged", "", p[len("merged:"):].strip())
        header.append("{dim}%s{/} %s" % (k.rstrip(":"), v))
    stack = out["sections"].get("open_stack", {}).get("text", "")
    if stack:
        header += ["", stack]
    note = out["sections"].get("ongoing_work", {}).get("text", "")
    if note:
        header += ["", "{dim}%s{/}" % note]
    out["header"] = "\n".join(header)

    qs = []
    for key in ("open_questions", "out_of_scope_questions"):
        s = out["sections"].get(key)
        if s and s["text"]:
            qs += ["## " + s["title"], "", s["text"], ""]
    out["questions"] = "\n".join(qs).strip()

    json.dump(out, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
