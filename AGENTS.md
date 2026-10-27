# AGENTS.md

Notes for an agent picking up work on **tdash**: a terminal dashboard laid out and fed by a JSON
config. Read [SPEC.md](SPEC.md) for the design (including what isn't built yet) and
[README.md](README.md) for the user-facing behaviour. This file covers how we work, how the code
fits together, and what was learned along the way.

## Working agreements

- **Commit and push after every change**, to `origin/main` (`git@github.com:allco/tdash.git`).
  Don't ask first. The user asked for this.
- **Author identity:** commit with the name and email already set in this repo's local git config
  (`git config --local user.email`); the user chose that address for this project. Don't change it
  or fall back to the global config. If the local setting is missing, ask the user.
- End commit messages with `Co-Authored-By: Claude <noreply@anthropic.com>`.
- Before committing: `gofmt -l .` (must print nothing), `go vet ./...`, `go test ./...`. The
  post-merge workflow runs the same checks.
- Keep SPEC.md, README.md and the field reference `docs/config.md` in step with the code. When
  behaviour changes, update all three, plus the `--help` text in `main.go` and the help overlay
  (`helpKeys` in `ui.go`) if keys change.
- The sibling project `../tglab` is the style reference: flat `package main`, the splash/logo
  approach, the README layout, the release workflow and the screenshot pipeline all come from it.
  When the user says "like tglab", look there first.

## Decisions the user made

- The name is **tdash** (binary `tdash`, module `github.com/allco/tdash`). An early working name,
  "tdashboard", is gone everywhere.
- Stack: **Go + Bubble Tea + Lip Gloss** (chosen over Rust/ratatui).
- Panes scroll **both vertically and horizontally**, with the arrow keys and the mouse. The
  dashboard as a whole scrolls only vertically.
- Auto-refresh is set in the config (top-level, per pane, per supplier).
- Data can come from **suppliers**: commands or URLs that produce JSON, optionally named and shared
  between panes with `select`.
- The config file is **watched**: saving it reloads the dashboard (polling, not fsnotify; see
  below).
- Logo and splash in tglab's style. The release GitHub Action is the same as tglab's.
- README screenshots live in `docs/screenshots/`, and PNGs are stored with **Git LFS**
  (`.gitattributes` tracks `*.png`).

## Code map

Everything is in one `package main` at the root:

| File | What's in it |
|---|---|
| `main.go` | flags (`--config`, `--no-mouse`, `--version`, `--help`), `version` (set by `-ldflags` in releases), program start with alt screen and mouse |
| `config.go` | config structs, JSONC via `hujson.Standardize`, `DisallowUnknownFields`, `validate()` with path-style errors (`layout.rows[1].panes[0].chart: …`), defaults, `parseRefresh` (0 unset, -1 "off") |
| `supplier.go` | `fetch()` for inline/file/command/url, `decodeJSON` into `*Object` (**keeps key order**; numbers are `float64`), `parseOutput` formats, `${VAR}` expansion (only the braced form, so shell `$VAR` survives), the `select` path parser/evaluator |
| `layout.go` | `layoutGrid`: spans → widths (cumulative rounding), units → heights, auto unit, leftover lines to the last row |
| `markup.go` | the Markdown renderer (line-based, no library), inline parsing into styled `seg`s, color tags `{red on yellow}…{/}`, `wrapSegs` (word wrap that keeps styles per line), `joinContinuations` |
| `pane.go` | the `Pane` interface and the `mdPane`, `tablePane`, `splinePane` (braille + Catmull-Rom) and `histPane` panes |
| `ui.go` | the root model: sources and refresh timers, `deliver` (select → `SetData`), keys, mouse, actions, config watching, `frame()` (border, title, error lines, scroll, clip, scrollbars), status line, help |
| `splash.go` | the big and small TDASH logos, the splash timing (min 1.2s, max 3s) |
| `tdash_test.go` | unit tests + `TestDemo` (renders `examples/demo.json`; `TDASH_SHOW=1` prints it) |
| `screenshot_test.go` | `TestScreenshots` (only with `TDASH_SCREENSHOTS=<dir>`), made-up demo data, `freezeColors` |
| `examples/` | `demo.json` (uses every feature), data files, `scripts/metrics.sh` |
| `examples/agents/` | an AI agents status board: `status.md` (made up) → `status2json.py` → `agents.json` |
| `scripts/` | `build.sh` (version from `git describe`), `install.sh` (into `~/bin` or `$INSTALL_DIR`), `screenshots.sh` and its helper `svg-grid.py` |

### How the pieces talk

- **Panes produce, the frame displays.** A pane's `Content(w, h, focused)` returns its *full*
  content: `header` lines (sticky, e.g. a table header), `body` lines (may be wider than `w`), and
  `cursor` (a body line to keep in view). `model.frame()` does all scrolling, clipping
  (`ansi.TruncateLeft` / `ansi.Truncate`), error lines and scrollbars. A new pane type should only
  implement `Pane`.
- `Pane.Key` gets first go at keys in a focused pane (table selection, chart cursor). Returning
  `false` lets the frame scroll instead. `ps.follow` makes the next render scroll to `cursor`.
- The frame stores the sizes from the last render on `paneState` (`bodyH`, `bodyLen`, `wide`,
  `iw`, `topH`). Keys and the mouse need them, so **in tests call `m.View()` before sending keys**,
  or scrolling clamps to zero.
- **Sources:** each named supplier is one source (`"supplier:<name>"`); each pane with its own data
  is one source (`"pane:<index>"`). A fetch is a `tea.Cmd` returning `dataMsg`. The next `tickMsg` is
  scheduled only after a fetch finishes, so fetches never overlap. Messages carry `gen`; `build()`
  (on reload) bumps it so stale messages are dropped.
- **Reload** (`r` or a config change): `build(cfg, old)` reuses a pane's state (selection, scroll)
  when its config is unchanged at the same position, and reuses a source's last data when its ref is
  unchanged. Then everything is fetched again.
- **Config watching:** `configCheckMsg` every second compares the file's mtime+size
  (`fileStamp`). Polling was chosen over fsnotify because editors that save by renaming break
  file-system watches. An invalid config keeps the old dashboard and shows the error.
- The model is a pointer (`*model`) on purpose. `View()` writes render sizes into `paneState`.
- **The selected table row** is styled like tglab's: a pink `▌` (212) in a one-cell margin that
  every table line has, bold light-yellow text (229; a cell with a `colors` match keeps its color,
  in bold), and no background. Cells cut with `…` wrap within their column (`expandRow`), and a
  row that grows is framed by grey (241) `┄` dividers. `tablePane.lines` maps body lines back to
  rows for clicks, and `content.cursorEnd` makes the frame scroll the whole block into view.

## Gotchas learned the hard way

- **Go regexp is RE2:** no backreferences (`\1`). The horizontal-rule pattern spells out its three
  variants.
- **Running the TUI in a pty for a smoke test:** `script`'s pty is 0×0, so the app renders nothing.
  Use
  `script -q /dev/null sh -c 'stty cols 120 rows 40; ./tdash examples/demo.json'`, feed keys through a
  pipe, and wrap it with `perl -e 'alarm 20; exec @ARGV' …` (macOS has no `timeout`).
- **Screenshots** (`scripts/screenshots.sh`: test → ANSI → `freeze` → SVG → headless Chrome → PNG):
  - `freeze` hangs when stdin is an open pipe, so the script passes `</dev/null`. Run the script
    with `< /dev/null` too.
  - `freeze`'s default font has no braille, and fallback fonts draw braille at another width, which
    breaks every line with a chart. The script embeds **JuliaMono** (downloaded once to
    `~/.cache/tdash`), which has braille and every glyph tdash uses at one width. DejaVu Sans Mono
    does **not** have braille.
  - `freeze` drops **reverse video** and the **basic background colors** (SGR 40–47, 100–107).
    The UI therefore uses explicit colors for the focused title (bg = accent), and `freezeColors`
    in `screenshot_test.go` rewrites basic backgrounds to `48;5;N`. The selected table row has no
    background at all; it is styled like tglab's (see below).
  - Emoji (✅ 🚂 💬 …) take two cells in a terminal, but fonts draw them at other widths, which
    shifts everything after them. `scripts/svg-grid.py` rewrites each SVG so every run of text sits
    at its exact cell (an explicit `x` per run, `east_asian_width` W/F = two cells, matching
    `ansi.StringWidth`), and draws wide characters with the color emoji font.
  - Needs `freeze` (`go install github.com/charmbracelet/freeze@v0.2.2`, lands in `~/go/bin`),
    Google Chrome and `python3`.
  - Screenshots must be safe to publish: made-up data only. `examples/agents/` is the public
    stand-in for the user's private agents dashboard, with an invented status file.
- `pkg/browser` prints the opener's output to stdout, which would land on top of the TUI. `init()`
  in `ui.go` silences it.
- `gh` is not installed on this machine, so CI runs can't be checked from here.

## Status

Built and pushed: everything in README's feature list. That covers the grid (span, units, auto
unit, dashboard scroll), the table/markdown/spline/histogram panes, inline/file/command/url data,
named suppliers with `select`, refresh precedence, timeouts, errors in the pane, actions
(open/run/copy, tty), focus/zoom/help/pause/reload, mouse, two-way pane scrolling with scrollbars
in the borders, config watching, the splash and logo, screenshots, release and post-merge
workflows.

Not built yet (see SPEC.md §5.4, §8, §10, §11):
- pane types `stat`, `gauge`, `log`, `list`, `kv`, `bar`, `sparkline`
- watching local `file` data sources (only the config is watched)
- `/` filter, table sorting (`s`), `←` `→` to move between table columns
- the trust prompt before running a config's commands, and `--trust`
- JSON Schema generation, themes beyond `accent`/`border`, `theme.focusBackground` (the spec's
  example config mentions it, but the code rejects it as an unknown field)
- links in Markdown can't be selected or opened yet; only table rows have actions
- no release has been tagged yet (`git tag v0.1.0 && git push origin v0.1.0` starts one)

Open questions for the user are listed in SPEC.md §10.

## Private dashboards

The user keeps personal dashboards **outside this repo**, under `~/.config/tdash/<name>/` (a
config plus any helper scripts). They can contain internal work data, so don't copy them into the
repo, its examples or its screenshots. Make a made-up public version instead, as
`examples/agents/` is for the agents dashboard. When a status file they come from changes shape (new columns, new emoji),
update the dashboard's config (and its parser script, if it has one) there. Check the result by
loading the config, running the fetches, and printing `m.View()` from a throwaway test, which you
delete afterwards.
