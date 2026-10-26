# Config reference

Every field tdash accepts in its config file. [README.md](../README.md#configuration) introduces
them with examples, and [examples/demo.json](../examples/demo.json) uses all of them.

## The file

- JSON with comments (`//`, `/* */`) and trailing commas allowed (JSONC).
- Unknown fields are errors. So are wrong types and unknown values. An error names the field's
  path, e.g. `layout.rows[1].panes[0].chart: unknown value "pie" (want spline or histogram)`.
- Relative paths (`file`, command working directories) are relative to the directory of the
  config file.
- `tdash` with no argument reads `./tdash.json`. tdash watches the file and reloads within a
  second of a save. An invalid save keeps the dashboard as it was and shows the error.

Durations (`refresh`, `timeout`) use Go syntax: `500ms`, `5s`, `1m30s`, `2h`.

Colors (`theme`, column `colors`, Markdown color tags) are one of:

- a name: `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white`, `gray` (also
  `grey`, `brightblack`), `brightred`, `brightgreen`, `brightyellow`, `brightblue`,
  `brightmagenta`, `brightcyan`, `brightwhite`
- a 256-color index: `0`–`255`
- truecolor: `#rrggbb`

## Top level

| Field | Type | Default | Description |
|---|---|---|---|
| `title` | string | none | Shown in the status line |
| `refresh` | duration \| `"off"` | none | Default refresh for all data. See [Refresh](#refresh) |
| `theme` | [theme](#theme) | | Colors of the UI |
| `suppliers` | object of name → [source](#sources) | none | Named data sources that panes share |
| `layout` | [layout](#layout) | | **Required.** The rows and panes |

## `theme`

| Field | Type | Default | Description |
|---|---|---|---|
| `accent` | color | `cyan` (6) | Focused border, focused title background, highlights |
| `border` | color | 240 (gray) | Borders of panes that aren't focused |

A value that isn't a color falls back to the default.

## `layout`

| Field | Type | Default | Description |
|---|---|---|---|
| `unit` | `"auto"` \| integer ≥ 1 | `"auto"` | Lines per height unit. `"auto"` divides the terminal height by the total units, so the dashboard fills the screen |
| `minUnit` | integer | `4` | With `"auto"`, a unit is never fewer lines than this. If the rows don't fit, the dashboard scrolls |
| `rows` | array of [row](#row) | | **Required**, at least one |

Leftover lines from rounding go to the last row.

### Row

| Field | Type | Default | Description |
|---|---|---|---|
| `height` | integer ≥ 1 | `1` | Height in units |
| `panes` | array of [pane](#pane) | | **Required**, at least one. They share the row's width by `span` |

## Pane

Fields every pane type takes:

| Field | Type | Default | Description |
|---|---|---|---|
| `type` | `"table"` \| `"markdown"` \| `"chart"` | | **Required** |
| `title` | string | none | Shown in the top border |
| `span` | integer ≥ 1 | `1` | Share of the row's width: `span: 2` next to `span: 1` gets two thirds |
| `data` | [data](#data) | none | Where the pane's content comes from. A pane without data is empty |
| `refresh` | duration \| `"off"` | none | Refresh for this pane's data. See [Refresh](#refresh) |
| `action` | [action](#action) | none | What `enter` does on the selected row (tables) |
| `id` | string | none | Accepted, but not used yet |

Tab order is row by row, left to right.

### `table`

| Field | Type | Default | Description |
|---|---|---|---|
| `columns` | array of [column](#column) | from the data | Which fields to show, in order. Without it, the columns come from the data |

The data can be:
- an array of objects;
- an array of arrays, where the first array is the header;
- `{"columns": […], "rows": […]}`.

Columns take their natural width, up to 60 cells. The last column is never capped. A longer cell
is cut with `…`, except in the selected row (marked with `▌`, in bold): it expands to show every
cell in full, wrapped within its column, between `┄` dividers. A table wider than its pane scrolls sideways. The header row stays visible while you scroll.

#### Column

| Field | Type | Default | Description |
|---|---|---|---|
| `key` | string | | The field of the row to show |
| `title` | string | `key` | Header text |
| `align` | `"left"` \| `"right"` | right for numbers, else left | |
| `colors` | object of value → color | none | Colors a cell by its value. A key matches the whole value, or else its start, so `{"✅": "green"}` colors `✅ ready`. The longest matching key wins |

### `markdown`

There are no pane-specific fields. The data is a string, or an array of lines. Anything else is
shown as JSON.

Supported:
- headings, bold, italic, inline code and code blocks;
- lists, block quotes, horizontal rules and links;
- color tags: `{red}…{/}`, `{red on yellow}…{/}`, `{bold green}…{/}`, `{on blue}…{/}` and `{#ff8800}…{/}`.

Color tags can also set `italic`, `underline`, `dim` and `reverse`. Tags nest, and `{/}` closes
the last one. `\{` is a literal brace.

Text is word-wrapped to the pane width. Indented lines right after a list item continue that
item. Markdown pipe tables are not rendered as tables. Use a `table` pane for those.

### `chart`

| Field | Type | Default | Description |
|---|---|---|---|
| `chart` | `"spline"` \| `"histogram"` | | **Required** for `type: "chart"` |
| `min`, `max` | number | from the data | Fix the y axis (spline) |
| `bins` | integer | fit to the width | Number of bins, when a histogram is given raw numbers |

The spline chart's data can be:
- an array of numbers;
- `[{"y": …}, …]`;
- an object of name → array of numbers;
- `{"series": [{"name", "color", "values"}]}`.

The histogram's data can be:
- raw numbers, which it bins;
- `[{"label", "value"}, …]`;
- an object of label → value.

### `action`

Set exactly one of `open`, `run` or `copy`.

| Field | Type | Description |
|---|---|---|
| `open` | string | Open this URL in the browser |
| `copy` | string | Copy this text to the clipboard |
| `run` | string | Run this through `$SHELL -c` (`/bin/sh` if unset, `cmd /C` on Windows), from the config's directory |
| `tty` | bool | With `run`: give the command the terminal until it exits. Without it, the command runs in the background, has a 1-minute limit, and its result shows in the status line |

`{{field}}` takes a value from the selected row:
- `{{a.b}}` reaches into nested objects;
- `{{0}}` takes the first cell of an array row;
- a missing field becomes an empty string.

In `run`, each value is shell-quoted. A row object with its own `action` field uses that instead of
the pane's.

## Data

A pane's `data` has at most one source:

| Field | Type | Description |
|---|---|---|
| `inline` | any JSON | The data itself |
| `file` | string | A file path. `.json` files are parsed as JSON. Any other file is text, unless `format` says otherwise |
| `command`, `url` | | A command or URL, with the [source](#sources) fields |
| `supplier` | string | The name of one of the top-level `suppliers` |
| `select` | string | Picks part of the data. See [Select](#select) |

`select` works with any source. With `supplier`, set `format`, `refresh` and `timeout` on the
supplier itself.

## Sources

A source is a command or a URL. Each top-level supplier needs exactly one of them. A pane's `data`
can have one inline, too.

| Field | Type | Default | Description |
|---|---|---|---|
| `command` | string \| array of strings | | A string runs through `$SHELL -c`, so pipes work. An array runs directly, with no shell. It must exit 0 |
| `cwd` | string | the config's directory | Commands only |
| `env` | object of name → string | none | Commands only. Added to the environment |
| `url` | string | | `http://` or `https://`. It must answer with a 2xx status |
| `method` | string | `GET` | URLs only |
| `headers` | object of name → string | none | URLs only |
| `body` | string \| JSON | none | URLs only. A JSON value is sent with `Content-Type: application/json` |
| `format` | `"json"` \| `"text"` \| `"lines"` \| `"auto"` | `json` | How the output is read. `lines` gives an array of lines. `auto` tries JSON, then falls back to text |
| `refresh` | duration \| `"off"` | none | See [Refresh](#refresh) |
| `timeout` | duration | `10s` | The fetch is cancelled after this |

`${VAR}` in `command`, `url`, `headers`, `body` and `env` values is replaced with the environment
variable. Plain `$VAR` is left for the shell. URL responses are read up to 16 MB. JSON objects
keep their key order.

If a fetch fails, the pane shows the error above its last good data. The error includes the exit
code or HTTP status, and the first lines of stderr or of the response body.

A config can run any command with your permissions, so only open configs you trust.

## Select

`select` is a small JSONPath. It starts with `$`, which is the whole result.

| Step | Example | Meaning |
|---|---|---|
| `.name` | `$.items` | A field of an object |
| `[n]` | `$.items[0]`, `$.items[-1]` | An array element. A negative index counts from the end |
| `[*]` | `$.items[*].name` | Applies the rest of the path to every element, giving an array |
| `.length` | `$.items.length` | The length of an array, a string, or an object's key count, unless the object has a field named `length` |

A path that doesn't match (a missing field, an index out of range) shows as an error in the
pane.

## Refresh

A source's refresh interval is the first of these that is set:
1. the source's own `refresh`;
2. the pane's `refresh`;
3. the top-level `refresh`.

A named supplier without its own `refresh` follows the fastest pane that uses it. `"off"` turns
refresh off at that level. The minimum is `500ms`. Without any refresh, data is fetched at start
and when you press `r`.

The next fetch is scheduled only after the current one finishes, so fetches never overlap. `p`
pauses all timers.
