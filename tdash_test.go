package main

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestLayoutGrid(t *testing.T) {
	rows := []Row{
		{Height: 2, Panes: []PaneConfig{{Span: 2}, {Span: 1}}},
		{Height: 1, Panes: []PaneConfig{{Span: 1}, {Span: 1}, {Span: 1}}},
	}
	rects, total := layoutGrid(rows, 100, 31, 0, 4, nil)
	// unit = 31/3 = 10; the leftover line goes to the last row.
	want := []rect{{0, 0, 66, 20}, {66, 0, 34, 20}, {0, 20, 33, 11}, {33, 20, 33, 11}, {66, 20, 34, 11}}
	if total != 31 {
		t.Errorf("total = %d, want 31", total)
	}
	for i, r := range rects {
		if r != want[i] {
			t.Errorf("rect %d = %+v, want %+v", i, r, want[i])
		}
	}

	// Too small for minUnit: the dashboard gets taller than the screen and scrolls.
	if _, total := layoutGrid(rows, 100, 9, 0, 4, nil); total != 12 {
		t.Errorf("total = %d, want 12 (3 units × minUnit 4)", total)
	}
	// A fixed unit ignores the screen height.
	if _, total := layoutGrid(rows, 100, 50, 5, 4, nil); total != 15 {
		t.Errorf("total = %d, want 15", total)
	}
}

func TestSelect(t *testing.T) {
	v, err := decodeJSON([]byte(`{"items": [{"name": "a", "n": 1}, {"name": "b", "n": 2}], "x": {"y": "z"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"$":                      `{"items":[{"n":1,"name":"a"},{"n":2,"name":"b"}],"x":{"y":"z"}}`,
		"$.x.y":                  "z",
		"$.items[1].name":        "b",
		"$.items[-1].n":          "2",
		"$.items[*].name":        `["a","b"]`,
		"$.items.length":         "2",
		"$.items[0].name.length": "1",
	}
	for path, want := range cases {
		steps, err := parseSelect(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got, err := applySelect(v, steps)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if s := formatValue(got); s != want {
			t.Errorf("%s = %s, want %s", path, s, want)
		}
	}
	for _, bad := range []string{"items", "$.items[x]", "$.a..b", "$.items[0"} {
		if _, err := parseSelect(bad); err == nil {
			t.Errorf("parseSelect(%q): want an error", bad)
		}
	}
	steps, _ := parseSelect("$.missing")
	if _, err := applySelect(v, steps); err == nil {
		t.Error("$.missing: want an error")
	}
}

func TestDecodeKeepsKeyOrder(t *testing.T) {
	v, err := decodeJSON([]byte(`{"z": 1, "a": 2, "m": 3}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(v.(*Object).Keys, ","); got != "z,a,m" {
		t.Errorf("keys = %s", got)
	}
	if _, err := decodeJSON([]byte(`[1] [2]`)); err == nil {
		t.Error("want an error for trailing data")
	}
}

func TestInlineTags(t *testing.T) {
	segs := inlineSegs(`a {red on yellow}b {bold}c{/}{/} \{d} {nope}e **f** `+"`g`", attrs{})
	var got []string
	for _, s := range segs {
		got = append(got, s.text)
	}
	if strings.Join(got, "|") != "a |b |c| {d} {nope}e |f| |g" {
		t.Errorf("segments = %q", got)
	}
	if segs[1].a.fg != "1" || segs[1].a.bg != "3" || !segs[2].a.bold || segs[2].a.fg != "1" {
		t.Errorf("styles = %+v %+v", segs[1].a, segs[2].a)
	}
	if segs[3].a.fg != "" {
		t.Errorf("tag not closed: %+v", segs[3].a)
	}
}

func TestWrapSegs(t *testing.T) {
	lines := wrapSegs([]seg{{text: "one two three four", a: attrs{}}}, 9, seg{text: "• "}, seg{text: "  "})
	var plain []string
	for _, l := range lines {
		plain = append(plain, ansi.Strip(l))
	}
	if strings.Join(plain, "|") != "• one two|  three|  four" {
		t.Errorf("lines = %q", plain)
	}
}

func TestFillAction(t *testing.T) {
	v, _ := decodeJSON([]byte(`{"name": "it's", "meta": {"id": 7}}`))
	if got := fill("echo {{name}} {{ meta.id }}", v, shellQuote); got != `echo 'it'\''s' '7'` {
		t.Errorf("got %s", got)
	}
}

func TestConfigErrors(t *testing.T) {
	cases := map[string]string{
		`{"layout": {"rows": [{"panes": [{"type": "pie"}]}]}}`:                                      `layout.rows[0].panes[0].type: unknown value "pie"`,
		`{"layout": {"rows": [{"panes": [{"type": "chart", "chart": "pie"}]}]}}`:                    `layout.rows[0].panes[0].chart: unknown value "pie"`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "data": {"supplier": "x"}}]}]}}`:         `no supplier named "x"`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "data": {"file": "a", "url": "b"}}]}]}}`: "want one source",
		`{"refresh": "1ms", "layout": {"rows": [{"panes": [{"type": "table"}]}]}}`:                  "below the minimum",
		`{"layout": {"rows": []}}`: "at least one row",
		`{"layout": {"rows": [{"panes": [{"type": "table", "colour": 1}]}]}}`:                                     `unknown field "colour"`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "columns": [{"key": "a", "width": "wide"}]}]}]}}`:      `unknown value "wide"`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "columns": [{"key": "a", "width": 0}]}]}]}}`:           `width: want a whole number`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "columns": [{"key": "a", "cut": "left"}]}]}]}}`:        `columns[0].cut: unknown value "left"`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "columns": [{"key": "a", "width": 9, "min": 3}]}]}]}}`: `columns[0].min and max: not used with a fixed width`,
		`{"layout": {"rows": [{"panes": [{"type": "table", "columns": [{"key": "a", "min": 9, "max": 3}]}]}]}}`:   `columns[0].max: want ≥ min`,
	}
	dir := t.TempDir()
	for src, want := range cases {
		path := dir + "/c.json"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n  got %v\n  want %q", src, err, want)
		}
	}
}

// TestDemo loads the example dashboard, fetches everything but the network, and renders it.
// TDASH_SHOW=1 prints the screen.
func TestDemo(t *testing.T) {
	cfg, err := loadConfig("examples/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel("examples/demo.json", cfg)
	m.splash = false
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for k, src := range m.sources {
		if src.ref.URL != "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		v, err := fetch(ctx, src.ref, cfg.dir)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", k, err)
		}
		m.Update(dataMsg{gen: m.gen, key: k, value: v, err: err})
	}
	for _, ps := range m.panes {
		if ps.err != nil {
			t.Errorf("%s: %v", ps.cfg.Title, ps.err)
		}
	}
	m.key("tab")
	m.key("tab")
	m.key("tab")
	m.key("down")
	m.key("down")
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 40 {
		t.Errorf("%d lines, want 40", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 120 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	if os.Getenv("TDASH_SHOW") != "" {
		os.Stdout.WriteString(view + "\n")
	}
}

func TestMarkdownContinuations(t *testing.T) {
	got := joinContinuations([]string{"- one", "  two", "- three", "", "  code", "1. four", "   five"})
	want := []string{"- one two", "- three", "", "  code", "1. four five"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q", got)
	}
}

func TestSelectedRowExpands(t *testing.T) {
	long := strings.Repeat("word ", 20) + "end"
	p := newPane(PaneConfig{Type: "table"}, "81").(*tablePane)
	data, _ := decodeJSON([]byte(`[{"a": "one", "b": "x"}, {"a": "` + long + `", "b": "y"}, {"a": "three", "b": "z"}]`))
	if err := p.SetData(data); err != nil {
		t.Fatal(err)
	}
	if c := p.Content(80, 20, true); len(c.body) != 3 {
		t.Fatalf("nothing selected: %d lines, want 3", len(c.body))
	}
	p.sel = 1
	c := p.Content(80, 20, true)
	if len(c.body) < 6 { // divider, at least two lines of the row, divider, two other rows
		t.Fatalf("selected row didn't expand: %d lines", len(c.body))
	}
	text := ansi.Strip(strings.Join(c.body, "\n"))
	if !strings.Contains(text, "end") || strings.Contains(text, "…") {
		t.Errorf("the selected cell isn't shown in full:\n%s", text)
	}
	if c.cursor != 1 || c.cursorEnd != len(c.body)-2 {
		t.Errorf("cursor block %d–%d, want 1–%d", c.cursor, c.cursorEnd, len(c.body)-2)
	}
	p.Click(len(c.body) - 1)
	if p.sel != 2 {
		t.Errorf("click on the last line selected row %d, want 2", p.sel)
	}
	p.Click(0)
	if p.sel != 0 {
		t.Errorf("click on the first line selected row %d, want 0", p.sel)
	}
}

func TestCellColor(t *testing.T) {
	colors := map[string]string{"✅": "green", "✅ ready": "cyan", "down": "red"}
	for v, want := range map[string]string{"✅ clean": "green", "✅ ready to merge": "cyan", "down": "red", "up": ""} {
		if c, _ := cellColor(colors, v); c != want {
			t.Errorf("%s: got %q, want %q", v, c, want)
		}
	}
}

func TestConfigChangeReloads(t *testing.T) {
	path := t.TempDir() + "/dash.json"
	write := func(title string, mod time.Time) {
		src := `{"title": "` + title + `", "layout": {"rows": [{"panes": [{"type": "markdown", "data": {"inline": "hi"}}]}]}}`
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().Add(-time.Hour)
	write("one", start)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(path, cfg)

	m.Update(configCheckMsg{})
	if m.cfg.Title != "one" {
		t.Fatalf("reloaded without a change")
	}
	write("two", start.Add(time.Minute))
	m.Update(configCheckMsg{})
	if m.cfg.Title != "two" || m.flash != "config changed, reloaded" {
		t.Errorf("title %q, flash %q", m.cfg.Title, m.flash)
	}

	// A broken config keeps the dashboard and says why.
	if err := os.WriteFile(path, []byte(`{"layout": `), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(configCheckMsg{})
	if m.cfg.Title != "two" || !m.flashErr {
		t.Errorf("title %q, flash %q", m.cfg.Title, m.flash)
	}
}

// TestAgentsExample loads the agents example through its parser script and checks every pane
// gets its data.
func TestAgentsExample(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	m := agentsModel(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 44})
	for _, ps := range m.panes {
		if ps.err != nil {
			t.Errorf("%s: %v", ps.cfg.Title, ps.err)
		}
	}
	if view := m.View(); !strings.Contains(view, "!831") || !strings.Contains(view, "Review agent") {
		t.Error("the stack or the agents are missing from the view")
	}
}

func TestWrapKeepsStyledWordsTogether(t *testing.T) {
	segs := inlineSegs("aaaa bbbb `cc`.", attrs{})
	var plain []string
	for _, l := range wrapSegs(segs, 12, seg{}, seg{}) {
		plain = append(plain, ansi.Strip(l))
	}
	if strings.Join(plain, "|") != "aaaa bbbb|cc." {
		t.Errorf("lines = %q", plain)
	}
}

// TestMouseToggle checks that m hands the mouse to the terminal and takes it back.
func TestMouseToggle(t *testing.T) {
	cfg, err := loadConfig("examples/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel("examples/demo.json", cfg)
	m.splash = false
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.key("m")
	if !m.noMouse || !strings.Contains(m.statusLine(), "mouse off") {
		t.Fatalf("after m: noMouse=%v, status %q", m.noMouse, m.statusLine())
	}
	m.key("m")
	if m.noMouse || strings.Contains(m.statusLine(), "mouse off") {
		t.Fatalf("after m m: noMouse=%v, status %q", m.noMouse, m.statusLine())
	}
	m.key("tab")
	m.key("m")
	m.key("esc")
	if m.noMouse || m.focus < 0 {
		t.Fatalf("after m esc: noMouse=%v, focus %d (esc should only turn the mouse on)", m.noMouse, m.focus)
	}
}

func TestColWidths(t *testing.T) {
	n := func(v int) *int { return &v }
	fr := func(f float64) ColWidth { return ColWidth{FR: f} }
	cases := []struct {
		name    string
		cols    []Column
		natural []int
		avail   int
		want    []int
	}{
		{"fixed, auto and shares split what is left 2:1",
			[]Column{{Width: ColWidth{Cells: 20}}, {}, {Width: fr(2)}, {Width: fr(1)}},
			[]int{30, 8, 200, 200}, 100, []int{20, 8, 48, 24}},
		{"a share grows no wider than its content; the rest goes to the other share",
			[]Column{{Width: fr(1)}, {Width: fr(1)}}, []int{6, 200}, 100, []int{6, 94}},
		{"min holds when the pane is narrow, and the table overflows",
			[]Column{{Width: ColWidth{Cells: 30}}, {Width: fr(1), Min: n(10)}}, []int{30, 50}, 20, []int{30, 10}},
		{"max caps a share",
			[]Column{{Width: fr(1), Max: n(15)}, {Width: fr(1)}}, []int{200, 200}, 100, []int{15, 85}},
		{"auto keeps the 60 cap but not on the last column; max replaces it, 0 removes it",
			[]Column{{}, {Max: n(0)}, {Max: n(10)}, {}}, []int{90, 90, 90, 90}, 400, []int{60, 90, 10, 90}},
		{"shares round to exactly what is left",
			[]Column{{Width: fr(1)}, {Width: fr(1)}, {Width: fr(1)}}, []int{99, 99, 99}, 50, []int{17, 16, 17}},
		{"a share never goes below 3 cells without a min",
			[]Column{{Width: ColWidth{Cells: 50}}, {Width: fr(1)}}, []int{50, 40}, 40, []int{50, 3}},
	}
	for _, c := range cases {
		if got := colWidths(c.cols, c.natural, c.avail); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCutCell(t *testing.T) {
	s := "feature/login-fix"
	for mode, want := range map[string]string{
		"":       "feature/l…",
		"end":    "feature/l…",
		"start":  "…login-fix",
		"middle": "featu…-fix",
	} {
		if got := cutCell(s, 10, mode); got != want {
			t.Errorf("cut %q: got %q, want %q", mode, got, want)
		}
	}
	if got := cutCell("short", 10, "middle"); got != "short" {
		t.Errorf("a cell that fits was cut: %q", got)
	}
}

// TestShareColumnFillsPane checks that a table with a share column fits its pane, cuts the long
// cell, and still shows it in full when its row is selected.
func TestShareColumnFillsPane(t *testing.T) {
	long := strings.Repeat("word ", 30) + "end"
	cfg := PaneConfig{Type: "table", Columns: []Column{{Key: "a", Width: ColWidth{Cells: 6}}, {Key: "b", Width: ColWidth{FR: 1}}}}
	p := newPane(cfg, "81").(*tablePane)
	data, _ := decodeJSON([]byte(`[{"a": "one", "b": "` + long + `"}, {"a": "two", "b": "x"}]`))
	if err := p.SetData(data); err != nil {
		t.Fatal(err)
	}
	c := p.Content(50, 20, true)
	for _, l := range append(c.header, c.body...) {
		if w := ansi.StringWidth(l); w > 50 {
			t.Errorf("line is %d cells wide, want ≤ 50: %q", w, ansi.Strip(l))
		}
	}
	p.sel = -1
	if text := ansi.Strip(strings.Join(p.Content(50, 20, true).body, "\n")); !strings.Contains(text, "…") {
		t.Errorf("the long cell isn't cut:\n%s", text)
	}
	p.sel = 0
	if text := ansi.Strip(strings.Join(p.Content(50, 20, true).body, "\n")); !strings.Contains(text, "end") {
		t.Errorf("the selected row doesn't show the long cell in full:\n%s", text)
	}
}

// resizeModel is a 100×31 terminal (30 lines above the status line): two panes side by side over
// one wide pane, 15 lines each.
func resizeModel(t *testing.T, rows string) (*model, string) {
	t.Helper()
	path := t.TempDir() + "/dash.json"
	if rows == "" {
		md := `{"type": "markdown", "data": {"inline": "hi"}}`
		rows = `[{"panes": [` + md + `, ` + md + `]}, {"panes": [` + md + `]}]`
	}
	if err := os.WriteFile(path, []byte(`{"layout": {"rows": `+rows+`}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(path, cfg)
	m.splash = false
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 31})
	m.View()
	return m, path
}

func dragMouse(m *model, x0, y0, x1, y1 int) {
	m.Update(tea.MouseMsg{X: x0, Y: y0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: x1, Y: y1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m.Update(tea.MouseMsg{X: x1, Y: y1, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
}

func TestDragResizesAndSaves(t *testing.T) {
	m, path := resizeModel(t, "")
	rects, _, _ := m.layout()
	if rects[1].x != 50 || rects[2].y != 15 {
		t.Fatalf("start layout %+v", rects)
	}

	dragMouse(m, 49, 5, 69, 5) // the right border of the left pane, 20 cells right
	dragMouse(m, 10, 15, 10, 20)
	rects, _, _ = m.layout()
	if rects[0].w != 70 || rects[1].x != 70 || rects[1].w != 30 {
		t.Errorf("after the column drag: %+v %+v", rects[0], rects[1])
	}
	if rects[0].h != 20 || rects[2].y != 20 || rects[2].h != 10 {
		t.Errorf("after the row drag: %+v %+v", rects[0], rects[2])
	}
	if m.drag != nil {
		t.Error("the drag didn't end on release")
	}

	// The sizes are saved next to the config, and come back on the next start.
	saved := sizesPath(path)
	if !strings.HasSuffix(saved, "/dash.sizes.json") {
		t.Fatalf("sizes path %s", saved)
	}
	cfg, _ := loadConfig(path)
	m2 := newModel(path, cfg)
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 31})
	if r2, _, _ := m2.layout(); !slices.Equal(r2, rects) {
		t.Errorf("after a restart %+v, want %+v", r2, rects)
	}
	// They scale with the terminal.
	m2.Update(tea.WindowSizeMsg{Width: 200, Height: 31})
	if r2, _, _ := m2.layout(); r2[0].w != 140 {
		t.Errorf("at width 200 the left pane is %d wide, want 140", r2[0].w)
	}

	// = goes back to the config and deletes the file.
	m.key("=")
	if rects, _, _ = m.layout(); rects[1].x != 50 {
		t.Errorf("after = the border is at %d, want 50", rects[1].x)
	}
	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Errorf("= left the sizes file: %v", err)
	}
}

func TestDragKeepsMinimumSize(t *testing.T) {
	m, _ := resizeModel(t, "")
	dragMouse(m, 50, 5, 0, 5)
	dragMouse(m, 80, 14, 80, 0) // x=10 would now be the left pane's side border
	rects, _, _ := m.layout()
	if rects[0].w != minPaneW || rects[0].h != minRowH {
		t.Errorf("left pane %+v, want %d wide and %d high", rects[0], minPaneW, minRowH)
	}
}

func TestClickInsidePaneDoesNotResize(t *testing.T) {
	m, path := resizeModel(t, "")
	dragMouse(m, 20, 5, 30, 8)
	if r, _, _ := m.layout(); r[1].x != 50 || r[2].y != 15 {
		t.Errorf("a drag inside a pane resized it: %+v", r)
	}
	if _, err := os.Stat(sizesPath(path)); !os.IsNotExist(err) {
		t.Errorf("a sizes file was written: %v", err)
	}
}

func TestResizeKeys(t *testing.T) {
	m, _ := resizeModel(t, "")
	m.key("tab") // the left pane
	m.key("ctrl+shift+right")
	m.key("ctrl+shift+down")
	rects, _, _ := m.layout()
	if rects[0].w != 50+sideStep || rects[0].h != 16 {
		t.Errorf("after ctrl+shift+→ ↓: %+v", rects[0])
	}
	m.key("tab")
	m.key("tab") // the bottom pane: the last row moves the border above it
	m.key("ctrl+shift+down")
	if rects, _, _ = m.layout(); rects[2].h != 15 {
		t.Errorf("after ↓ on the last row: %+v, want 15 high (14 + 1)", rects[2])
	}
	m.key("tab")
	m.key("tab") // the right pane, the last in its row: → moves its left border
	m.key("ctrl+shift+right")
	if rects, _, _ = m.layout(); rects[1].x != 50 || rects[1].w != 50 {
		t.Errorf("after → on the last pane of a row: %+v, want back at 50", rects[1])
	}
	m.key("ctrl+shift+left")
	m.key("ctrl+shift+up")
	if rects, _, _ = m.layout(); rects[1].x != 50+sideStep || rects[1].h != 14 {
		t.Errorf("after ← ↑: %+v", rects[1])
	}
	m.key(">") // the old keys do nothing now
	m.key("+")
	if r, _, _ := m.layout(); r[1] != rects[1] {
		t.Errorf("an old key resized: %+v", r[1])
	}
}

func TestSizesIgnoredWhenConfigChanges(t *testing.T) {
	m, path := resizeModel(t, "")
	dragMouse(m, 49, 5, 69, 5)
	dragMouse(m, 10, 15, 10, 20)
	// The top row gets a third pane: its widths no longer fit, the row heights still do.
	md := `{"type": "markdown", "data": {"inline": "hi"}}`
	rows := `[{"panes": [` + md + `, ` + md + `, ` + md + `]}, {"panes": [` + md + `]}]`
	if err := os.WriteFile(path, []byte(`{"layout": {"rows": `+rows+`}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.reload()
	rects, _, _ := m.layout()
	if rects[0].w != 33 || rects[0].h != 20 {
		t.Errorf("after adding a pane: %+v, want 33 wide (spans) and 20 high (saved)", rects[0])
	}
}

func TestPointerOverBorders(t *testing.T) {
	m, _ := resizeModel(t, "")
	hover := func(x, y int) string {
		m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion})
		return m.View()
	}
	for _, c := range []struct {
		x, y int
		want string
	}{
		{49, 5, "col-resize"}, // the border between the two top panes
		{50, 5, "col-resize"},
		{20, 15, "row-resize"}, // the border between the rows
		{20, 5, "default"},     // inside a pane
		{20, 30, "default"},    // the status line
	} {
		if v := hover(c.x, c.y); !strings.HasPrefix(v, "\x1b]22;"+c.want+"\x1b\\") {
			t.Errorf("pointer at %d,%d: %q, want %s", c.x, c.y, v[:min(len(v), 24)], c.want)
		}
	}
	// It stays a resize arrow for the whole drag, even when the pointer runs ahead of the border.
	m.Update(tea.MouseMsg{X: 49, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if v := m.View(); !strings.HasPrefix(v, "\x1b]22;col-resize") {
		t.Errorf("pointer during a drag: %q", v[:min(len(v), 24)])
	}
	// The sequence takes no room on screen.
	if w := ansi.StringWidth(strings.SplitN(m.View(), "\n", 2)[0]); w != 100 {
		t.Errorf("first line is %d cells wide, want 100", w)
	}
}

func TestHelpShowsAbout(t *testing.T) {
	cfg, err := loadConfig("examples/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel("examples/demo.json", cfg)
	m.splash = false
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m.key("?")
	v := m.View()
	for _, want := range []string{"About", projectURL("tdash"), releasesURL("tdash")} {
		if !strings.Contains(v, want) {
			t.Errorf("help has no %q", want)
		}
	}
	m.key("m")
	if !m.help || !m.noMouse {
		t.Errorf("m in the help: help %v, noMouse %v", m.help, m.noMouse)
	}
}
