# tdashboard — Specification

A terminal (TUI) dashboard, configured with JSON. The screen is a grid of
**panes**. Each pane shows one kind of content: a table, Markdown text, a
chart, and so on.

```
tdashboard [path/to/dashboard.json]
```

---

## 1. Goals

- **Declarative.** One JSON file defines the layout, the panes and their data.
- **Fills the terminal.** Panes use all the available width and height, and the layout reflows when the terminal is resized.
- **Keyboard-driven.** Focus a pane, move around inside it, and act on items.
- **Live-ish.** Reload the config and data on demand (`R`), with optional auto-refresh.
- **No horizontal scrolling.** Only vertical scrolling is allowed.

### Non-goals (for v1)

- Mouse-first interaction (mouse support is nice to have, not required).
- Editing data from inside the dashboard.
- A plugin system with arbitrary code (external commands are used instead, see §6.4).

---

## 2. Layout

### 2.1 Grid model

The dashboard is a table: a list of **rows**, and each row holds a list of
**panes** (cells).

- **Width.** `pane width = available width / number of columns in that row`.
  The rounding remainder goes to the last pane, so the row always fills the full width exactly.
- **Height.** All panes in one row have the same height. Different rows can
  have different heights, but every height is a whole multiple of the
  **base row height** (`unit`).
- **Vertical overflow.** If the total height of all rows is more than the terminal
  height, the dashboard scrolls vertically. A scrollbar is shown on the right.
- **Horizontal overflow** never happens at the dashboard level. Inside a
  pane, content is wrapped or truncated with `…`. It is never scrolled sideways.

### 2.2 Base row height

The base row height (`layout.unit`, in terminal lines) is decided like this:

| `layout.unit`  | Behavior |
|----------------|----------|
| `"auto"` (default) | `unit = max(minUnit, floor(terminalHeight / totalUnits))`, where `totalUnits = Σ row.height`. The dashboard fits the screen exactly unless that would make `unit` smaller than `minUnit`, in which case it scrolls. |
| integer `N`    | Fixed `N` lines per unit. The layout scrolls if it doesn't fit. |

`layout.minUnit` defaults to `4` lines. That is the smallest height that still
fits a border, a title and a couple of content lines. Any leftover lines from
rounding are added to the last row.

### 2.3 Spanning (recommended)

A pane can set `"span": n` to take up `n` column slots in its row. Without
this, a row with one wide chart and two narrow tables can't be expressed.
The width formula then becomes `available width / Σ spans × span`.

### 2.4 Example

```
┌ CPU (spline) ─────────────────────┐┌ Alerts (md) ─────┐   row 1: height 2 units
│      ╭╮      ╭─╮                  ││ **3** firing      │          spans 2 + 1
│ ╭────╯╰──────╯ ╰───╮              ││ {red}DB down{/}   │
│─╯                  ╰───           ││                   │
└───────────────────────────────────┘└───────────────────┘
┌ Services (table) ┐┌ Latency (hist) ┐┌ Notes (md) ───────┐   row 2: height 1 unit
│ api     ● up     ││ ▂▅█▇▃▁         ││ deploy at 17:00   │
└──────────────────┘└────────────────┘└───────────────────┘
```

---

## 3. Focus & keymap

### 3.1 Focus

- At most one pane has focus at a time. At startup nothing is focused.
- The focused pane is drawn with a **colored, bold border** (accent color),
  and its title is shown in reverse video. Unfocused panes have a dim border.
  An optional subtle background tint can be turned on with
  `theme.focusBackground`.
- If focusing a pane puts it partly off-screen, the dashboard scrolls
  so that the pane is visible.

### 3.2 Global keys

| Key | Action |
|-----|--------|
| `q` / `Q` | Quit |
| `r` / `R` | Re-read the config and all data files |
| `Tab` | Focus the next pane (row-major order) |
| `Shift+Tab` | Focus the previous pane |
| `Esc` | Remove focus. Also closes any open overlay (help, detail view) |
| `?` | Show a help overlay with all key bindings *(recommended)* |
| `p` | Pause or resume auto-refresh *(recommended)* |
| `↑ ↓` / `PgUp PgDn` (no focus) | Scroll the dashboard vertically |

### 3.3 Keys inside a focused pane

| Key | Action |
|-----|--------|
| `↑ ↓` | Move the selection, or scroll the content |
| `← →` | Change the column (table) or move the cursor along the x axis (chart) |
| `PgUp PgDn` / `Home End` | Jump by a page, or to the start/end |
| `Enter` | Run the **action** of the selected item (§6.4) |
| `z` | Zoom: show the focused pane full-screen, `Esc` to go back *(recommended)* |
| `/` | Filter the table rows or search the text *(recommended)* |

---

## 4. Configuration

### 4.1 Top-level structure

```jsonc
{
  "title": "Prod overview",
  "refresh": "30s",                 // optional auto-refresh of all data (§6.3); omit = manual only
  "theme": {
    "accent": "cyan",
    "border": "gray",
    "focusBackground": false
  },
  "layout": {
    "unit": "auto",
    "minUnit": 4,
    "rows": [
      {
        "height": 2,
        "panes": [
          { "type": "chart", "title": "CPU", "span": 2, "chart": "spline", "data": { "file": "cpu.json" } },
          { "type": "markdown", "title": "Alerts", "data": { "file": "alerts.md" } }
        ]
      },
      {
        "height": 1,
        "panes": [
          { "type": "table",    "title": "Services", "data": { "file": "services.json" } },
          { "type": "chart",    "title": "Latency", "chart": "histogram", "data": { "inline": [12, 15, 15, 18, 40] } },
          { "type": "markdown", "title": "Notes", "data": { "inline": "deploy at **17:00**" } }
        ]
      }
    ]
  }
}
```

### 4.2 Common pane fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `table`, `markdown`, `chart`, `stat`, `log`, … (§5) |
| `id` | string | Optional stable id, used in error messages and actions |
| `title` | string | Shown in the top border |
| `span` | int | Column slots taken, default `1` |
| `data` | object | Data source (§4.3) |
| `refresh` | duration \| `"off"` | Auto-refresh interval for this pane. Overrides the global `refresh` (§6.3) |
| `action` | object | What `Enter` does on the selected item (§6.4) |
| `style` | object | Per-pane overrides for border and accent colors |

### 4.3 Data sources

A pane's `data` takes exactly one source:

| Source | Example | Notes |
|--------|---------|-------|
| `inline` | `{ "inline": [1, 2, 3] }` | Data is embedded in the config |
| `file` | `{ "file": "data/cpu.json" }` | The path is relative to the config file. `.json` is parsed, `.md`/`.txt` are read as text |
| `command` | `{ "command": "kubectl get pods -o json" }` | Stdout is the data (§4.4) |
| `url` | `{ "url": "https://api.example.com/status" }` | The HTTP response body is the data (§4.4) |
| `supplier` | `{ "supplier": "k8s", "select": "$.pods" }` | References a named supplier (§4.4) |

### 4.4 Data suppliers

A **supplier** is an external program or HTTP endpoint that produces the data for one or
more panes. A supplier can be written inline in a pane's `data`, or declared once by name
under the top-level `suppliers` and referenced from several panes. A named supplier is
fetched **once per refresh**, and every pane that uses it gets the same result.

```jsonc
{
  "suppliers": {
    "k8s": {
      "command": ["kubectl", "get", "pods", "-o", "json"],   // argv array: no shell
      "refresh": "10s",
      "timeout": "5s"
    },
    "status": {
      "url": "https://api.example.com/v1/status",
      "method": "GET",
      "headers": { "Authorization": "Bearer ${STATUS_TOKEN}" },  // ${VAR} = environment variable
      "refresh": "30s"
    },
    "notes": {
      "command": "curl -s https://wiki.example.com/oncall.md",  // string: run via $SHELL -c
      "format": "text"
    }
  },
  "layout": { "rows": [ { "height": 1, "panes": [
    { "type": "table",    "title": "Pods",    "data": { "supplier": "k8s", "select": "$.items" } },
    { "type": "stat",     "title": "Pods up", "data": { "supplier": "k8s", "select": "$.items.length" } },
    { "type": "table",    "title": "Status",  "data": { "supplier": "status", "select": "$.services" } },
    { "type": "markdown", "title": "On-call", "data": { "supplier": "notes" } }
  ] } ] }
}
```

#### Supplier fields

| Field | Applies to | Description |
|-------|------------|-------------|
| `command` | command | A string (run through `$SHELL -c`, so pipes work) or an argv array (run directly, no shell) |
| `cwd` | command | Working directory. Default: the config file's directory |
| `env` | command | Extra environment variables, added to the inherited environment |
| `url` | url | `http://` or `https://` |
| `method` | url | Default `GET` |
| `headers` | url | Map of request headers |
| `body` | url | Request body. A string, or a JSON value that is sent as `application/json` |
| `format` | both | `json` (default), `text` or `lines` (a JSON array of strings, one per line). `auto` picks `json` when the output parses as JSON |
| `refresh` | both | Auto-refresh interval (§6.3) |
| `timeout` | both | Default `10s` |

- `${VAR}` in `command`, `url`, `headers`, `body` and `env` is replaced with the value
  of the environment variable. Keep secrets out of the config file this way.
- **Success** means: a command exits with code 0 and its stdout parses in the given `format`;
  a URL returns a 2xx response and its body parses. Anything else is an error. The error
  message includes the exit code or HTTP status and the first lines of stderr or the body.
  It is shown inside every pane that uses the supplier, and the last good data stays
  visible underneath (dimmed) until the next successful fetch.
- **Commands** are never run in parallel with themselves (§6.3). Stderr is captured and
  is not drawn onto the TUI.

#### Selecting part of the result

`select` on a pane's `data` picks a subtree of the supplier's JSON. That way one call
can feed several panes. The syntax is a small JSONPath subset:
`$.a.b`, `$.items[0]`, `$.items[*].name` and `.length`.
For anything more complex, transform the data in the command itself
(for example `kubectl … | jq '…'`).

#### Supplier output contract

The output must have the shape the pane type expects (§5). For example, a table
expects an array of objects, and a spline chart expects an array of numbers.
Scripts written for tdashboard can also return a pane-ready object that includes
display hints, such as `{ "columns": […], "rows": […] }` for a table.

#### Security

A config can run arbitrary commands, so it should be treated like a script:
- Commands run with the user's own permissions. There is no sandbox.
- *Recommended:* the first time a config with `command` suppliers is opened, ask for
  confirmation. The answer is remembered per config path and content hash.
  `--trust` skips the prompt.

### 4.5 Validation

- The config is validated at load time. Errors point to the path, for example
  `layout.rows[1].panes[0].chart: unknown value "pie"`.
- A pane whose data fails to load shows the error **inside the pane** in red,
  and the rest of the dashboard still renders.
- A published JSON Schema (`tdashboard.schema.json`) gives editor
  autocomplete *(recommended)*.

---

## 5. Pane types

### 5.1 `table`

Data is an array of objects or an array of arrays.

```json
{
  "columns": [
    { "key": "name",   "title": "Service" },
    { "key": "status", "title": "Status", "colors": { "up": "green", "down": "red" } },
    { "key": "p99",    "title": "p99 ms", "align": "right" }
  ],
  "rows": [
    { "name": "api", "status": "up",   "p99": 120 },
    { "name": "db",  "status": "down", "p99": null }
  ]
}
```

- Column widths are distributed to fit the pane width. Text that overflows is truncated with `…`.
- The header row stays visible while scrolling (sticky header).
- `↑ ↓` select a row. `Enter` runs the action for that row.
- Optional: sort by a column (`s` cycles the sort).

### 5.2 `markdown`

Supported: headings, bold/italic, inline code, code blocks, lists, block quotes,
horizontal rules, links (`Enter` on a selected link opens it), and **colored
spans** (§7). Text is word-wrapped to the pane width and scrolls vertically.

### 5.3 `chart`

| `chart` | Data | Rendering |
|---------|------|-----------|
| `spline` | `[y…]` or `[{x, y}…]`, or several named series | Smooth line drawn with braille characters (2×4 dots per cell) for high resolution |
| `histogram` | Raw values (binned automatically, `bins` optional) or precomputed `[{label, value}]` | Vertical bars drawn with block characters `▁▂▃▄▅▆▇█` |
| `bar` *(recommended)* | `[{label, value}]` | Horizontal bars with labels, good for "top N" lists |
| `sparkline` *(recommended)* | `[y…]` | A single line with no axes, useful inside small panes |

Common chart options: `min`/`max` (axis limits, auto by default), `series`
(a name and color for each series), `legend` (true/false), `xLabel`/`yLabel`.
When the pane is focused, `← →` moves a cursor that shows the exact value
at that x position.

### 5.4 Recommended additional types

| Type | Why |
|------|-----|
| `stat` | One big number with a label, a delta (`▲ 3.2%`) and an optional sparkline. This is the most common dashboard widget |
| `gauge` | A progress or utilization bar with thresholds (green/yellow/red) |
| `log` | Tail of a file or command output. It auto-scrolls to the bottom and pauses auto-scroll while focused |
| `list` | A simple list of selectable items with actions. Lighter than a table |
| `kv` | Key/value pairs (status details, metadata) |

---

## 6. Interaction details

### 6.1 Scrolling

Each pane keeps its own scroll offset. A thin scrollbar is drawn inside the
right border when the content is longer than the pane. The dashboard itself
scrolls only when the rows don't fit (§2.2).

### 6.2 Reload (`R`)

`R` re-reads the config file and every data source. Focus, selection and scroll
positions are kept for panes with the same `id` (or the same position if there
is no `id`). If the new config is invalid, the old dashboard stays on screen
and the error is shown in a status line at the bottom.

*Recommended:* `--watch` reloads automatically when the config or data files change.

### 6.3 Auto-refresh

Auto-refresh is set in the config. It works at two levels:

```jsonc
{
  "refresh": "30s",                       // global default for all panes
  "layout": { "rows": [ { "height": 1, "panes": [
    { "type": "log",   "data": { "command": "tail -n 200 app.log" }, "refresh": "2s"  },  // faster
    { "type": "table", "data": { "file": "services.json" },          "refresh": "off" }   // manual only
  ] } ] }
}
```

- Values are durations (`"500ms"`, `"5s"`, `"1m"`, `"1h"`) or `"off"`. The smallest allowed value is `500ms`.
- Which value applies: a named supplier's own `refresh`, then the pane's `refresh`, then the global
  `refresh`. If none is set, the data only reloads when you press `R`. A named supplier used
  by several panes runs on its own timer, and every pane that uses it updates together.
- Auto-refresh reloads **data only**. To reload the config, press `R` or use `--watch`.
- Each data source refreshes on its own timer. A `command` or `url` that is still running when the
  next refresh is due is not started again, so fetches never overlap. If a fetch runs longer than
  its `timeout` (default `10s`, §4.4), it is cancelled and the panes that use it show an error.
- Focus, selection and scroll position are kept across refreshes. For `log` panes,
  the view stays pinned to the bottom unless you have scrolled up.
- The pane title shows a small indicator while data is loading (`⟳`). The status line
  shows the time of the last refresh for the focused pane.
- *Recommended:* `p` pauses or resumes auto-refresh for the whole dashboard.

### 6.4 Actions on selectable items

Selectable items are table rows, list items and Markdown links. `action` is set
on the pane, and can be overridden per item with an `action` field in the data.

```jsonc
"action": { "open": "https://grafana.example.com/d/{{name}}" }          // open URL in the default browser
"action": { "run":  "kubectl logs deploy/{{name}} | less", "tty": true } // run command (suspend TUI if tty)
"action": { "copy": "{{id}}" }                                          // copy to clipboard
```

- `{{field}}` placeholders are filled in from the selected row/item. The values
  are shell-escaped before they are substituted.
- `tty: true` suspends the TUI, gives the terminal to the command, and resumes
  the TUI when the command exits. Without it, the command runs in the background
  and its exit status is shown in the status line.

---

## 7. Colored text in Markdown

Standard Markdown has no colors. The reference in the idea (GitHub's
`$\color{red}{\textsf{text}}$` LaTeX trick) is hard to type and hard to read,
so tdashboard uses a short inline tag syntax:

```
{red}text{/}                  foreground only
{red on yellow}text{/}        foreground and background
{bold green}OK{/}             with a modifier (bold, italic, underline, dim, reverse)
{#ff8800}orange{/}            truecolor hex
{on blue} highlighted {/}     background only
```

- Colors: the 16 ANSI names (`red`, `brightred`, …), the 256-color index (`{208}`) or `#rrggbb`. On terminals without truecolor support, colors are mapped down to the nearest available color.
- Tags can be nested. `{/}` closes the most recently opened tag.
- `\{` produces a literal brace.
- *Optional compatibility:* also accept GitHub's `$\color{red}{text}$` form, so
  the same files look colored on GitHub too.

---

## 8. Recommendations beyond the original idea (summary)

1. **`span`** for panes, so non-uniform grids are possible (§2.3).
2. **Data suppliers** (`command` and `url`, §4.4) and **auto-refresh set in the config** (§6.3), so the dashboard is live.
3. **Per-pane error display**, so one bad data source doesn't take down the whole dashboard (§4.5).
4. **Zoom (`z`)**, to read a dense pane full-screen.
5. **Help overlay (`?`)** and a **status line** (last reload time, errors, action results).
6. **`stat`, `gauge`, `log`, `list`, `kv`** pane types (§5.4).
7. **JSON Schema** for editor autocomplete and validation.
8. **`--watch`** mode, which reloads when files change.
9. **Themes**: `dark`/`light` presets and a `NO_COLOR` environment variable that is respected.
10. **Minimum terminal size**: if the terminal is too small to render anything useful,
    show "Terminal too small (need ≥ 40×10)" instead of broken output.

---

## 9. Open questions

- **Implementation language/stack.** Recommendation: **Go + Bubble Tea + Lip
  Gloss**, which produce a single static binary and have mature TUI and layout
  primitives (the `ntcharts` library covers braille line charts). The
  alternative is **Rust + ratatui**, which has built-in chart, sparkline,
  bar and table widgets.
- Should a row be able to scroll on its own, or does only the whole dashboard scroll?
- Should there be several dashboards in one config, as tabs switched with `1..9`?
- Config format: plain JSON only, or also JSONC/JSON5 (comments are very handy in dashboards)?

---

## 10. Milestones

1. **M1 — Skeleton:** load and validate the config, grid layout with `span` and
   units, borders and titles, resize handling, `q`, `Tab`, `Esc`, focus styling.
2. **M2 — Content:** `markdown` (with colors) and `table` panes, per-pane scrolling, `R` reload.
3. **M3 — Charts:** `spline` and `histogram`, then `sparkline`, `bar` and `stat`.
4. **M4 — Live & actions:** `command` and `url` suppliers, named suppliers with `select`, `refresh`, `--watch`, `Enter` actions.
5. **M5 — Polish:** help overlay, zoom, filter, JSON Schema, themes.
