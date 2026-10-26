package main

import (
	"context"
	"os"
	"os/exec"
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
	rects, total := layoutGrid(rows, 100, 31, 0, 4)
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
	if _, total := layoutGrid(rows, 100, 9, 0, 4); total != 12 {
		t.Errorf("total = %d, want 12 (3 units × minUnit 4)", total)
	}
	// A fixed unit ignores the screen height.
	if _, total := layoutGrid(rows, 100, 50, 5, 4); total != 15 {
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
		`{"layout": {"rows": [{"panes": [{"type": "table", "colour": 1}]}]}}`: `unknown field "colour"`,
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
