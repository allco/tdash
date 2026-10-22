# tdash

A terminal dashboard laid out and fed by a JSON config. The screen is a grid of panes (tables,
Markdown, charts), and each pane gets its data inline, from a file, from a command, or from a URL.
Commands and URLs can refresh on a timer.

```
┌─ CPU / memory % ──────────────────────────────────────┐┌─ Alerts ─────────────────────────┐
│100┤                                                   ││3 firing                          ┃
│   │⣀⣀⡀           ⢀⣀⣀⣀⣠⢤⣀⡀                             ││• CRITICAL db-primary: replication┃
│   │  ⠉⠉⠉⠉⠓⠒⠦⠤⠖⠚⠉⠉⠉      ⠉⠉⠙⠒⠒⠲⠖⠒⠲⠤⢤⣀⡤⠤⠤⠤⠤⢤⣀⣀⡤⠴⠒⠒⠚⠉⠉││  lag 42s                         │
│   │   ⠉⠓⠦⠴⠋⠳⣄⡀            ⢀⣠⣄                         ││• WARNING api: p99 above 800ms    │
│  0┤          ⠉⠓⠒⠋⠉⠉⠓⠦⠤⢤⣀⣀⡴⠋ ⠈⠧⠴⠒⠋⠉⠉⠉⠉⠓⢦⡀  ⢀⣀          ││                                  │
│    ■ cpu  ■ memory                                    ││                                  │
└───────────────────────────────────────────────────────┘└──────────────────────────────────┘
┌─ Services ─────────────────┐┌─ Latency (ms) ─────────────┐┌─ Host ───────────────────────┐
│Service   Status    p99 ms  ││            ██              ││my-laptop                     │
│────────────────────────────││         ▆▆ ██ ██ ▄▄        ││                              │
│api       up           120  ││      ▂▂ ██ ██ ██ ██        ││● up 21:38                    │
│billing   degraded     830  ││   ▁▁ ██ ██ ██ ██ ██ ▃▃     ││                              │
└━━━━━━━━━━━━━━━━━───────────┘└────────────────────────────┘└──────────────────────────────┘
tdash · Demo  updated 15:24:56 · every 30s                   tab focus · ? help · q quit
```

The full design is in [SPEC.md](SPEC.md).

## Build and run

```sh
go build -o tdash .
./tdash examples/demo.json       # or --config <path>; the default is ./tdash.json
./tdash --help                   # also --usage and -h
./tdash --version
```

Prebuilt binaries for macOS, Linux and Windows are attached to each
[release](https://github.com/allco/tdash/releases). Pushing a tag like `v0.1.0` builds them.

## Keys and mouse

| Key | Action |
|-----|--------|
| `tab` / `shift+tab` | Focus the next / previous pane |
| `esc` | Unzoom, or remove the focus |
| `↑ ↓ ← →` | Scroll the focused pane, or the dashboard when nothing is focused. In a table, `↑ ↓` select a row; in a chart, `← →` move the cursor |
| `pgup pgdn home end` | Scroll by a page, to the start / end |
| `enter` | Run the action of the selected row (open a URL, run a command, copy) |
| `z` | Zoom the focused pane to the full screen |
| `r` | Reload the config and all data |
| `p` | Pause / resume auto-refresh |
| `?` | Help |
| `q` | Quit |

Mouse: click focuses a pane and selects a table row. The wheel scrolls the pane under the pointer,
and `shift`+wheel scrolls it sideways. `--no-mouse` leaves the mouse to the terminal, so text can be
selected.

## A minimal config

```jsonc
{
  "title": "My dashboard",
  "refresh": "30s",
  "suppliers": {
    "pods": { "command": "kubectl get pods -o json", "refresh": "10s" }
  },
  "layout": {
    "rows": [
      {
        "height": 2,
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

[examples/demo.json](examples/demo.json) uses every feature the pilot supports.

## Pilot status

What works now:
- The grid layout: `span`, row heights in units, `unit: "auto"` / a fixed unit, `minUnit`, and
  vertical scrolling of the whole dashboard.
- **Panes:** `table`, `markdown` (with `{red on yellow}…{/}` color tags), and `chart` with `spline` and `histogram`.
- **Data:** `inline`, `file`, `command` and `url`, plus named `suppliers` with `select`, `format`, `refresh`, `timeout`, `${ENV}` and errors shown inside the pane.
- **Interaction:** focus, zoom, help, reload, pause, actions (`open`, `run`, `copy`), and scrolling inside panes both
  ways with keys and the mouse.
- **Visuals:** the start-up splash and the logo.

Not yet: `stat` / `gauge` / `log` / `list` / `kv` / `bar` / `sparkline` panes, `--watch`, the
`/` filter, table sorting, the trust prompt for commands, a JSON Schema, and themes
beyond the accent and border colors.
