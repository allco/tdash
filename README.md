# tdash

A terminal dashboard you describe in one JSON file. The screen is a grid of panes (tables,
Markdown and charts), and each pane gets its data inline, from a file, from a command or from a
URL. Commands and URLs refresh on a timer, so a few lines of config and a shell script turn into a
live status board.

![The demo dashboard, with a table focused](docs/screenshots/table.png)

Focus a pane with `tab`, then move through it. Above, a row of the Services table is selected and
the table is scrolled sideways (see the thumb in its bottom border).

`z` zooms the focused pane to the full screen. In a chart, `←` `→` move a cursor that reads out
the values:

![A zoomed chart with the cursor](docs/screenshots/chart.png)

[examples/agents](examples/agents) turns a Markdown status file, which AI coding agents keep up to
date, into a status board. A small script converts the file to JSON, and the dashboard re-reads it
every 30 seconds:

![An agents status board](docs/screenshots/agents.png)

The screenshots use made-up data. The design behind tdash, including the parts that aren't built
yet, is in [SPEC.md](SPEC.md).

## Install

Prebuilt binaries for macOS, Linux and Windows are attached to each
[release](https://github.com/allco/tdash/releases).

With Go 1.27 or newer:

```sh
go install github.com/allco/tdash@latest
```

Or from a checkout:

```sh
go build -o tdash .
```

## Run

```sh
tdash examples/demo.json         # a config path; the default is ./tdash.json
tdash --config path/to/dash.json
tdash --no-mouse                 # leave the mouse to the terminal, for selecting text
tdash --help                     # also --usage and -h
tdash --version
```

On start, a splash shows while the first data loads (at least 1.2 seconds, at most 3). Any key
skips it.

tdash watches its config file. When you save a change, the dashboard reloads within a second and
keeps the focus, scroll positions and data of the panes that didn't change. If the saved config
has an error, the dashboard stays as it was and the status line shows the error.

## Keys and mouse

| Key | Action |
|---|---|
| `tab` / `shift+tab` | focus the next / previous pane |
| `esc` | unzoom, or remove the focus |
| `↑` `↓` `←` `→` | scroll the focused pane, or the dashboard when nothing is focused |
| | in a table, `↑` `↓` select a row; in a chart, `←` `→` move the cursor |
| `pgup` `pgdn` `home` `end` | scroll by a page, or to the start / end |
| `enter` | run the action of the selected row (open a URL, run a command, copy) |
| `z` | zoom the focused pane to the full screen |
| `r` | reload the config and all data |
| `p` | pause / resume auto-refresh |
| `?` | help |
| `q` / `ctrl+c` | quit |

| Mouse | Action |
|---|---|
| click | focus the pane; in a table, also select the row |
| wheel | scroll the pane under the pointer; if it has nothing to scroll, scroll the dashboard |
| `shift`+wheel, sideways swipe | scroll the pane sideways |

Content wider or taller than its pane scrolls inside the pane. A thick thumb in the right border
or the bottom border shows where you are. The dashboard itself only scrolls vertically. That
happens when the rows don't fit on the screen, and then a scrollbar takes the last column.

## Configuration

The config is JSON. Comments (`//`, `/* */`) and trailing commas are allowed. Unknown fields are
errors, and every error names the field it's about, e.g.
`layout.rows[1].panes[0].chart: unknown value "pie"`. Relative paths (data files, command
working directories) are relative to the config file.

```jsonc
{
  "title": "Prod",                     // shown in the status line
  "refresh": "30s",                    // default auto-refresh for all data; omit = only on r
  "theme": { "accent": "cyan", "border": "gray" },

  "suppliers": {                       // named data sources, shared by panes
    "pods": { "command": "kubectl get pods -o json", "refresh": "10s" }
  },

  "layout": {
    "unit": "auto",                    // base row height: "auto" or a number of lines
    "minUnit": 4,
    "rows": [
      {
        "height": 2,                   // in units
        "panes": [
          { "type": "table", "title": "Pods", "span": 2,
            "columns": [{ "key": "name" }, { "key": "namespace" }],
            "data": { "supplier": "pods", "select": "$.items[*].metadata" } },
          { "type": "markdown", "title": "Notes", "data": { "file": "notes.md" } }
        ]
      },
      {
        "panes": [
          { "type": "chart", "chart": "spline", "title": "Load",
            "data": { "command": ["sh", "load.sh"], "refresh": "2s" } }
        ]
      }
    ]
  }
}
```

[examples/demo.json](examples/demo.json) uses every feature below. Every field, with its type and
default, is listed in [docs/config.md](docs/config.md).

### Layout

The dashboard is a list of rows, and each row is a list of panes. In a row, the width is shared by
`span` (default 1), so `span: 2` next to `span: 1` gets two thirds. A row's `height` (default 1) is
a number of **units**. With `"unit": "auto"`, the unit is the terminal height divided by the total
units, so the dashboard fills the screen exactly. It never goes below `minUnit` lines (default 4);
past that, the dashboard scrolls. A number instead of `"auto"` fixes the unit at that many lines.

### Panes

Every pane has `type`, and optionally `title`, `span`, `data`, `refresh` and `action`.

| `type` | Data | Notes |
|---|---|---|
| `table` | an array of objects; an array of arrays (the first is the header); or `{"columns": […], "rows": […]}` | `columns`: `key`, `title`, `align` (`left`/`right`; numbers align right by default), `colors` |
| `markdown` | a string; an array of lines; anything else is shown as JSON | headings, bold, italic, code, code blocks, lists, quotes, rules, links and color tags |
| `chart` + `"chart": "spline"` | an array of numbers; `[{"y": …}]`; `{"name": [numbers], …}`; or `{"series": [{"name", "color", "values"}]}` | smooth braille lines; `min`, `max` fix the y axis |
| `chart` + `"chart": "histogram"` | raw numbers (binned to fit, or into `bins`); `[{"label", "value"}]`; or `{"label": value, …}` | block-character bars |

A table column's `colors` maps cell values to colors. A key matches the whole value, or else the
start of it, so `{"✅": "green"}` colors `✅ ready to merge`.

### Colors in Markdown

```
{red}text{/}              foreground
{red on yellow}text{/}    foreground and background
{bold green}OK{/}         also: italic, underline, dim, reverse
{#ff8800}orange{/}        truecolor; {208} is a 256-color index
{on blue} label {/}       background only
```

Tags nest, and `{/}` closes the last one. `\{` is a literal brace. Colors are the 16 ANSI names
(`red`, `brightred`, `gray`, …), `0`–`255`, or `#rrggbb`.

### Data

A pane's `data` has one source:

| Source | Example |
|---|---|
| `inline` | `{"inline": [1, 2, 3]}` |
| `file` | `{"file": "services.json"}`: `.json` is parsed; any other file is text |
| `command` | `{"command": "kubectl get pods -o json"}`: a string runs through `$SHELL -c`; an array runs directly |
| `url` | `{"url": "https://api.example.com/status", "headers": {"Authorization": "Bearer ${TOKEN}"}}` |
| `supplier` | `{"supplier": "pods", "select": "$.items"}`: one of the named `suppliers` |

Commands and URLs (inline or named) also take the following fields:

| Field | Default | Notes |
|---|---|---|
| `format` | `json` | `json`, `text`, `lines` (an array of lines) or `auto` (JSON if it parses, else text) |
| `refresh` | none | `"5s"`, `"1m"`, …, or `"off"`; at least `500ms` |
| `timeout` | `10s` | the fetch is cancelled after this |
| `cwd`, `env` | config dir, none | commands only |
| `method`, `headers`, `body` | `GET`, none, none | URLs only; a JSON `body` is sent as `application/json` |

`${VAR}` in commands, URLs, headers, bodies and `env` is replaced from the environment, so secrets
stay out of the config. Plain `$VAR` is left alone for the shell.

**Named suppliers** run once per refresh, however many panes use them. Each pane picks its part
with `select`, a small JSONPath: `$.a.b`, `$.items[0]`, `$.items[-1]`, `$.items[*].name` and
`.length`.

**Refresh.** The interval comes from the first of these that is set: the source's own `refresh`,
then the pane's `refresh`, then the top-level `refresh`. A named supplier without its own follows
the fastest pane that uses it. A fetch still running when the next one is due is not started
again. `r` reloads everything now, and `p` pauses the timers.

**Errors.** A command must exit 0 and a URL must return a 2xx response, and the output has to
parse. If not, the pane shows the error (with the exit code or HTTP status and the first lines of
stderr or the body) above its last good data.

### Actions

`enter` on a selected table row runs the pane's `action`. A row can bring its own `action` field
instead. `{{field}}` takes a value from the row; `{{a.b}}` reaches into nested objects, and
`{{0}}` takes the first cell of an array row.

```jsonc
"action": { "open": "https://grafana.example.com/d/{{name}}" }   // open in the browser
"action": { "copy": "{{id}}" }                                   // copy to the clipboard
"action": { "run": "kubectl logs deploy/{{name}} | less", "tty": true }
```

`run` values are shell-quoted. With `"tty": true` the dashboard steps aside and the command gets
the terminal until it exits. Without it, the command runs in the background and its result shows
in the status line.

A config can run any command with your permissions, so only open configs you trust.

## Development

```sh
go test ./...
go vet ./...
```

To regenerate the screenshots after a UI change, install
[freeze](https://github.com/charmbracelet/freeze) (`go install github.com/charmbracelet/freeze@v0.2.2`),
Google Chrome and `python3`, then run `./scripts/screenshots.sh`. The first run downloads the
[JuliaMono](https://github.com/cormullion/juliamono) font into `~/.cache/tdash`, because the charts'
braille dots need a font that has them. `scripts/svg-grid.py` then puts every character on its
terminal cell, so emoji take two cells as they do in a terminal. The PNGs are stored with Git LFS
(`git lfs install` once).

| File | Contains |
|---|---|
| `main.go` | flags, `--help` text, startup |
| `config.go` | config structs, JSONC loading, validation |
| `supplier.go` | inline / file / command / URL fetching, ordered JSON decoding, `select` |
| `layout.go` | the grid: spans and row units |
| `pane.go` | the table, Markdown, spline and histogram panes |
| `markup.go` | the Markdown renderer and color tags |
| `ui.go` | the Bubble Tea app: refresh timers, focus, keys, mouse, the pane frame, the status line |
| `splash.go` | the start-up splash and the logo |
| `screenshot_test.go`, `scripts/screenshots.sh`, `scripts/svg-grid.py` | renders the README screenshots from made-up data |
| `examples/` | the demo dashboard, its data files and a supplier script |
| `examples/agents/` | the agents status board: a made-up `status.md`, `status2json.py` and `agents.json` |

### CI and releases

- `.github/workflows/post-merge.yml` ("Post merge") checks formatting, vets, tests and builds on
  every push to `main`.
- `.github/workflows/release.yml` runs when a semver tag is pushed. It tests, builds for macOS,
  Linux and Windows (amd64 and arm64) with the tag as the version, and creates a GitHub release
  with each binary, an archive per platform (with the README, SPEC and examples) and a
  `checksums.txt`. A tag with a suffix, like `v0.2.0-rc.1`, becomes a pre-release.

To release:

```sh
git tag v0.1.0
git push origin v0.1.0
```
