package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// content is what a pane draws, before the frame scrolls and clips it. Lines can be wider than
// the pane; the frame scrolls them sideways.
type content struct {
	header []string // stays at the top while the body scrolls up and down (table headers)
	body   []string
	cursor int // body line to keep in view after a key moved it, -1 for none
	// cursorEnd is the last line of the cursor's block (an expanded table row), kept in view too
	// when it fits. Zero means the block is the cursor line alone.
	cursorEnd int
}

// Pane is one kind of pane: it turns data into content and handles its own keys.
type Pane interface {
	SetData(v any) error
	Content(w, h int, focused bool) content
	// Key handles a key that means something to this pane type, such as moving a table's
	// selection. It returns false for keys that should scroll the pane instead.
	Key(k string, bodyH int) bool
	// Click handles a click on a body line.
	Click(line int)
	// Selected is the selected item, for actions, or nil.
	Selected() any
}

func newPane(cfg PaneConfig, accent string) Pane {
	switch cfg.Type {
	case "table":
		return &tablePane{cfg: cfg, accent: accent, sel: -1}
	case "chart":
		if cfg.Chart == "histogram" {
			return &histPane{cfg: cfg, accent: accent, cur: -1}
		}
		return &splinePane{cfg: cfg, accent: accent, cur: -1}
	default:
		return &mdPane{accent: accent}
	}
}

// formatValue shows a JSON value in a cell or a label.
func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(plain(v))
		return string(b)
	}
}

// plain turns decoded data back into types encoding/json can marshal.
func plain(v any) any {
	switch t := v.(type) {
	case *Object:
		m := make(map[string]any, len(t.Keys))
		for _, k := range t.Keys {
			m[k] = plain(t.Vals[k])
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = plain(e)
		}
		return out
	}
	return v
}

// ---- markdown ----

type mdPane struct {
	accent string
	text   string
}

func (p *mdPane) SetData(v any) error {
	switch t := v.(type) {
	case string:
		p.text = t
	case []any:
		lines := make([]string, len(t))
		for i, l := range t {
			lines[i] = formatValue(l)
		}
		p.text = strings.Join(lines, "\n")
	case nil:
		p.text = ""
	default:
		// Anything else is shown as JSON, which beats an error while setting up a supplier.
		b, _ := json.MarshalIndent(plain(v), "", "  ")
		p.text = "```\n" + string(b) + "\n```"
	}
	return nil
}

func (p *mdPane) Content(w, h int, focused bool) content {
	return content{body: renderMarkdown(p.text, w, p.accent), cursor: -1}
}

func (p *mdPane) Key(string, int) bool { return false }
func (p *mdPane) Click(int)            {}
func (p *mdPane) Selected() any        { return nil }

// ---- table ----

type tablePane struct {
	cfg    PaneConfig
	accent string
	cols   []Column
	rows   [][]string
	raw    []any // the row items, for actions
	sel    int   // selected row, -1 for none
	lines  []int // the row each body line of the last render shows, -1 for a divider
}

func (p *tablePane) SetData(v any) error {
	cols := p.cfg.Columns
	items := v
	if obj, ok := v.(*Object); ok {
		r, ok := obj.Get("rows")
		if !ok {
			return errors.New(`table data wants an array, or an object with "rows"`)
		}
		items = r
		if c, ok := obj.Get("columns"); ok && len(cols) == 0 {
			b, _ := json.Marshal(plain(c))
			if err := json.Unmarshal(b, &cols); err != nil {
				return fmt.Errorf("table columns: %w", err)
			}
		}
	}
	arr, ok := items.([]any)
	if !ok {
		return errors.New(`table data wants an array, or an object with "rows"`)
	}

	p.rows, p.raw = nil, arr
	if len(arr) > 0 {
		if _, isArr := arr[0].([]any); isArr {
			// An array of arrays: the first row is the header, unless columns are configured.
			if len(cols) == 0 {
				for i, h := range arr[0].([]any) {
					cols = append(cols, Column{Key: strconv.Itoa(i), Title: formatValue(h)})
				}
				arr = arr[1:]
				p.raw = arr
			}
			for _, r := range arr {
				ra, _ := r.([]any)
				row := make([]string, len(cols))
				for i, c := range cols {
					if n, err := strconv.Atoi(c.Key); err == nil && n < len(ra) {
						row[i] = formatValue(ra[n])
					}
				}
				p.rows = append(p.rows, row)
			}
			p.cols = cols
			p.clampSel()
			return nil
		}
	}
	if len(cols) == 0 {
		seen := map[string]bool{}
		for _, r := range arr {
			if obj, ok := r.(*Object); ok {
				for _, k := range obj.Keys {
					if !seen[k] {
						seen[k] = true
						cols = append(cols, Column{Key: k})
					}
				}
			}
		}
		if len(cols) == 0 && len(arr) > 0 {
			cols = []Column{{Key: "value"}}
		}
	}
	for _, r := range arr {
		row := make([]string, len(cols))
		for i, c := range cols {
			if obj, ok := r.(*Object); ok {
				row[i] = formatValue(obj.Vals[c.Key])
			} else if c.Key == "value" {
				row[i] = formatValue(r)
			}
		}
		p.rows = append(p.rows, row)
	}
	p.cols = cols
	p.clampSel()
	return nil
}

func (p *tablePane) clampSel() {
	if p.sel >= len(p.rows) {
		p.sel = len(p.rows) - 1
	}
}

const maxColWidth = 60

func (p *tablePane) Content(w, h int, focused bool) content {
	if len(p.cols) == 0 {
		return content{body: []string{lipgloss.NewStyle().Faint(true).Render("no rows")}, cursor: -1}
	}
	widths := make([]int, len(p.cols))
	numeric := make([]bool, len(p.cols))
	for i, c := range p.cols {
		widths[i] = ansi.StringWidth(colTitle(c))
		numeric[i] = len(p.rows) > 0
		for _, r := range p.rows {
			widths[i] = max(widths[i], ansi.StringWidth(r[i]))
			if _, err := strconv.ParseFloat(r[i], 64); err != nil && r[i] != "" {
				numeric[i] = false
			}
		}
		// The last column has nothing after it to push away, so it keeps its full width.
		if i < len(p.cols)-1 {
			widths[i] = min(widths[i], maxColWidth)
		}
	}
	const gap = "  "
	// Every line starts with a one-cell margin for the selection mark, as in tglab.
	const margin = 1 + len(gap)
	total := margin
	for _, wd := range widths {
		total += wd + len(gap)
	}
	total = max(total-len(gap), w)

	cell := func(s string, i int) string {
		s = ansi.Truncate(s, widths[i], "…")
		pad := strings.Repeat(" ", widths[i]-ansi.StringWidth(s))
		if p.cols[i].Align == "right" || (p.cols[i].Align == "" && numeric[i]) {
			return pad + s
		}
		return s + pad
	}

	head := make([]string, len(p.cols))
	for i, c := range p.cols {
		head[i] = cell(colTitle(c), i)
	}
	headStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.accent))
	rule := lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", total))

	body := make([]string, 0, len(p.rows))
	p.lines = p.lines[:0]
	cur, curEnd := -1, 0
	blank := strings.Repeat(" ", margin)
	// The selected row looks like tglab's: a pink mark in the margin, bold light-yellow text (cells
	// with a color keep it, in bold), and grey dividers around a row that expanded.
	mark := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Render("▌") + gap
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(strings.Repeat("┄", total))
	for r, row := range p.rows {
		if focused && r == p.sel {
			cur = len(body)
			block := p.expandRow(row, widths, cell)
			if len(block) > 1 && r > 0 {
				body = append(body, divider)
				p.lines = append(p.lines, -1)
			}
			for l, cells := range block {
				for i, v := range row {
					st := selStyle
					if c, ok := cellColor(p.cols[i].Colors, v); ok {
						st = st.Foreground(lipgloss.Color(colorOr(c, "")))
					}
					cells[i] = st.Render(cells[i])
				}
				prefix := blank
				if l == 0 {
					prefix = mark
				}
				body = append(body, prefix+strings.Join(cells, gap))
				p.lines = append(p.lines, r)
			}
			if len(block) > 1 && r < len(p.rows)-1 {
				body = append(body, divider)
				p.lines = append(p.lines, -1)
			}
			curEnd = len(body) - 1
			continue
		}
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = cell(v, i)
			if c, ok := cellColor(p.cols[i].Colors, v); ok {
				cells[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(colorOr(c, ""))).Render(cells[i])
			}
		}
		body = append(body, blank+strings.Join(cells, gap))
		p.lines = append(p.lines, r)
	}
	return content{header: []string{blank + headStyle.Render(strings.Join(head, gap)), rule}, body: body,
		cursor: cur, cursorEnd: curEnd}
}

// expandRow lays out the selected row so every cell shows its full text: a cell longer than its
// column wraps onto more lines instead of being cut with "…". It returns each line's cells.
func (p *tablePane) expandRow(row []string, widths []int, cell func(string, int) string) [][]string {
	wrapped := make([][]string, len(row))
	n := 1
	for i, v := range row {
		wrapped[i] = []string{v}
		if ansi.StringWidth(v) > widths[i] {
			wrapped[i] = strings.Split(ansi.Wrap(v, widths[i], ""), "\n")
		}
		n = max(n, len(wrapped[i]))
	}
	lines := make([][]string, n)
	for l := range lines {
		lines[l] = make([]string, len(row))
		for i := range row {
			s := ""
			if l < len(wrapped[i]) {
				s = strings.TrimRight(wrapped[i][l], " ")
			}
			lines[l][i] = cell(s, i)
		}
	}
	return lines
}

// cellColor finds a cell's color: the key equal to the value, or else the longest key the value
// starts with, so "✅" colors "✅ ready to merge".
func cellColor(colors map[string]string, v string) (string, bool) {
	if c, ok := colors[v]; ok {
		return c, true
	}
	best, color := -1, ""
	for k, c := range colors {
		if k != "" && strings.HasPrefix(v, k) && len(k) > best {
			best, color = len(k), c
		}
	}
	return color, best >= 0
}

func colTitle(c Column) string {
	if c.Title != "" {
		return c.Title
	}
	return c.Key
}

func (p *tablePane) Key(k string, bodyH int) bool {
	if len(p.rows) == 0 {
		return false
	}
	last := len(p.rows) - 1
	switch k {
	case "up":
		if p.sel < 0 {
			p.sel = last
		} else {
			p.sel = max(0, p.sel-1)
		}
	case "down":
		p.sel = min(last, p.sel+1)
	case "pgup":
		p.sel = max(0, p.sel-max(1, bodyH))
	case "pgdown":
		p.sel = min(last, max(0, p.sel)+max(1, bodyH))
	case "home":
		p.sel = 0
	case "end":
		p.sel = last
	default:
		return false
	}
	return true
}

func (p *tablePane) Click(line int) {
	if line >= 0 && line < len(p.lines) && p.lines[line] >= 0 {
		p.sel = p.lines[line]
	}
}

func (p *tablePane) Selected() any {
	if p.sel < 0 || p.sel >= len(p.raw) {
		return nil
	}
	return p.raw[p.sel]
}

// ---- charts ----

var seriesColors = []string{"81", "213", "221", "120", "209", "147"}

type series struct {
	name  string
	color string
	ys    []float64
}

// toSeries reads chart data: an array of numbers, an array of {y} points, an object of name →
// array, or {"series": [{name, color, values}]}.
func toSeries(v any) ([]series, error) {
	nums := func(v any) ([]float64, bool) {
		arr, ok := v.([]any)
		if !ok {
			return nil, false
		}
		out := make([]float64, 0, len(arr))
		for _, e := range arr {
			switch t := e.(type) {
			case float64:
				out = append(out, t)
			case *Object:
				y, ok := t.Get("y")
				f, isNum := y.(float64)
				if !ok || !isNum {
					return nil, false
				}
				out = append(out, f)
			default:
				return nil, false
			}
		}
		return out, true
	}
	if ys, ok := nums(v); ok {
		return []series{{ys: ys}}, nil
	}
	obj, ok := v.(*Object)
	if !ok {
		return nil, errors.New("chart data wants an array of numbers or an object of series")
	}
	if s, ok := obj.Get("series"); ok {
		arr, _ := s.([]any)
		var out []series
		for _, e := range arr {
			so, ok := e.(*Object)
			if !ok {
				return nil, errors.New(`each of "series" wants {name, values}`)
			}
			vals, _ := so.Get("values")
			ys, ok := nums(vals)
			if !ok {
				return nil, errors.New(`each of "series" wants "values": an array of numbers`)
			}
			name, _ := so.Get("name")
			color, _ := so.Get("color")
			out = append(out, series{name: formatValue(name), color: formatValue(color), ys: ys})
		}
		return out, nil
	}
	var out []series
	for _, k := range obj.Keys {
		ys, ok := nums(obj.Vals[k])
		if !ok {
			return nil, fmt.Errorf("chart series %q wants an array of numbers", k)
		}
		out = append(out, series{name: k, ys: ys})
	}
	return out, nil
}

func fmtNum(f float64) string {
	a := math.Abs(f)
	switch {
	case a >= 1e6:
		return strconv.FormatFloat(f/1e6, 'f', 1, 64) + "M"
	case a >= 1e4:
		return strconv.FormatFloat(f/1e3, 'f', 1, 64) + "k"
	case a >= 100 || f == math.Trunc(f):
		return strconv.FormatFloat(f, 'f', 0, 64)
	default:
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
}

type splinePane struct {
	cfg    PaneConfig
	accent string
	series []series
	cur    int // x index of the cursor, -1 for none
}

func (p *splinePane) SetData(v any) error {
	s, err := toSeries(v)
	if err != nil {
		return err
	}
	for i := range s {
		s[i].color = colorOr(s[i].color, seriesColors[i%len(seriesColors)])
	}
	p.series = s
	if p.cur >= p.points() {
		p.cur = p.points() - 1
	}
	return nil
}

func (p *splinePane) points() int {
	n := 0
	for _, s := range p.series {
		n = max(n, len(s.ys))
	}
	return n
}

// catmullRom is the smooth curve through ys at fractional index t.
func catmullRom(ys []float64, t float64) float64 {
	n := len(ys)
	if n == 1 {
		return ys[0]
	}
	i := min(int(t), n-2)
	f := t - float64(i)
	at := func(k int) float64 { return ys[max(0, min(n-1, k))] }
	p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
	return 0.5 * ((2 * p1) + (-p0+p2)*f + (2*p0-5*p1+4*p2-p3)*f*f + (-p0+3*p1-3*p2+p3)*f*f*f)
}

// braille dot bits, by [x][y] inside a 2×4 cell.
var brailleBits = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

func (p *splinePane) Content(w, h int, focused bool) content {
	n := p.points()
	if n == 0 {
		return content{body: []string{lipgloss.NewStyle().Faint(true).Render("no data")}, cursor: -1}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range p.series {
		for _, y := range s.ys {
			lo, hi = math.Min(lo, y), math.Max(hi, y)
		}
	}
	if p.cfg.Min != nil {
		lo = *p.cfg.Min
	}
	if p.cfg.Max != nil {
		hi = *p.cfg.Max
	}
	if hi <= lo {
		hi, lo = lo+1, lo-1
	}

	ph := max(1, h-1) // the last line is the legend / cursor readout
	lw := max(ansi.StringWidth(fmtNum(hi)), ansi.StringWidth(fmtNum(lo)))
	pw := max(1, w-lw-1)
	dw, dh := pw*2, ph*4

	cells := make([][]rune, ph)
	colors := make([][]string, ph)
	for r := range cells {
		cells[r] = make([]rune, pw)
		colors[r] = make([]string, pw)
	}
	set := func(x, y int, color string) {
		if x < 0 || x >= dw || y < 0 || y >= dh {
			return
		}
		cells[y/4][x/2] |= brailleBits[x%2][y%4]
		colors[y/4][x/2] = color
	}
	toY := func(v float64) int {
		return int(math.Round((hi - v) / (hi - lo) * float64(dh-1)))
	}
	for _, s := range p.series {
		if len(s.ys) == 0 {
			continue
		}
		prev := -1
		for x := 0; x < dw; x++ {
			t := float64(x) * float64(len(s.ys)-1) / float64(max(1, dw-1))
			y := toY(catmullRom(s.ys, t))
			if prev < 0 {
				prev = y
			}
			// Fill the gap to the previous dot so steep parts stay connected.
			for yy := min(prev, y); yy <= max(prev, y); yy++ {
				set(x, yy, s.color)
			}
			prev = y
		}
	}

	curCol := -1
	if focused && p.cur >= 0 {
		curCol = int(math.Round(float64(p.cur)*float64(dw-1)/float64(max(1, n-1)))) / 2
	}
	labelStyle := lipgloss.NewStyle().Faint(true)
	curStyle := lipgloss.NewStyle().Background(lipgloss.Color("238"))
	var body []string
	for r := 0; r < ph; r++ {
		label, axis := "", "│"
		switch r {
		case 0:
			label, axis = fmtNum(hi), "┤"
		case ph - 1:
			label, axis = fmtNum(lo), "┤"
		}
		var b strings.Builder
		b.WriteString(labelStyle.Render(fmt.Sprintf("%*s%s", lw, label, axis)))
		for c := 0; c < pw; c++ {
			ch := " "
			st := lipgloss.NewStyle()
			if cells[r][c] != 0 {
				ch = string(0x2800 + cells[r][c])
				st = st.Foreground(lipgloss.Color(colors[r][c]))
			}
			if c == curCol {
				st = st.Inherit(curStyle)
			}
			b.WriteString(st.Render(ch))
		}
		body = append(body, b.String())
	}

	// Legend, or the values under the cursor.
	var parts []string
	for _, s := range p.series {
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color(s.color)).Render("■")
		switch {
		case focused && p.cur >= 0 && p.cur < len(s.ys):
			parts = append(parts, strings.TrimSpace(dot+" "+s.name+" "+fmtNum(s.ys[p.cur])))
		case s.name != "":
			parts = append(parts, dot+" "+s.name)
		}
	}
	foot := strings.Join(parts, "  ")
	if focused && p.cur >= 0 {
		foot = labelStyle.Render(fmt.Sprintf("x=%d  ", p.cur)) + foot
	}
	if h > 1 {
		body = append(body, strings.Repeat(" ", lw+1)+foot)
	}
	return content{body: body, cursor: -1}
}

func (p *splinePane) Key(k string, _ int) bool {
	return moveCursor(&p.cur, p.points(), k)
}

func (p *splinePane) Click(int)     {}
func (p *splinePane) Selected() any { return nil }

// moveCursor moves a chart cursor with left/right/home/end.
func moveCursor(cur *int, n int, k string) bool {
	if n == 0 {
		return false
	}
	switch k {
	case "left":
		if *cur < 0 {
			*cur = n - 1
		} else {
			*cur = max(0, *cur-1)
		}
	case "right":
		*cur = min(n-1, *cur+1)
	case "home":
		*cur = 0
	case "end":
		*cur = n - 1
	default:
		return false
	}
	return true
}

type bar struct {
	label string
	value float64
}

type histPane struct {
	cfg    PaneConfig
	accent string
	values []float64 // raw values, binned at render time to fit the width
	bars   []bar     // or precomputed bars
	cur    int
	shown  int // how many bars the last render drew; the cursor moves over those
}

func (p *histPane) SetData(v any) error {
	p.values, p.bars = nil, nil
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			switch x := e.(type) {
			case float64:
				p.values = append(p.values, x)
			case *Object:
				l, _ := x.Get("label")
				val, _ := x.Get("value")
				f, ok := val.(float64)
				if !ok {
					return errors.New(`histogram bars want {"label", "value": number}`)
				}
				p.bars = append(p.bars, bar{formatValue(l), f})
			default:
				return errors.New("histogram data wants numbers or {label, value} bars")
			}
		}
	case *Object:
		for _, k := range t.Keys {
			f, ok := t.Vals[k].(float64)
			if !ok {
				return fmt.Errorf("histogram bar %q wants a number", k)
			}
			p.bars = append(p.bars, bar{k, f})
		}
	default:
		return errors.New("histogram data wants an array or an object of label → number")
	}
	return nil
}

// binned returns the bars to draw: the precomputed ones, or the raw values counted into bins.
func (p *histPane) binned(w int) []bar {
	if p.bars != nil || len(p.values) == 0 {
		return p.bars
	}
	vals := append([]float64(nil), p.values...)
	sort.Float64s(vals)
	lo, hi := vals[0], vals[len(vals)-1]
	n := p.cfg.Bins
	if n <= 0 {
		n = min(20, max(1, w/3))
	}
	if hi == lo {
		n = 1
	}
	out := make([]bar, n)
	step := (hi - lo) / float64(n)
	for i := range out {
		out[i].label = fmtNum(lo+step*float64(i)) + "–" + fmtNum(lo+step*float64(i+1))
	}
	for _, v := range vals {
		i := n - 1
		if step > 0 {
			i = min(n-1, int((v-lo)/step))
		}
		out[i].value++
	}
	return out
}

var eighths = []rune(" ▁▂▃▄▅▆▇█")

func (p *histPane) Content(w, h int, focused bool) content {
	bars := p.binned(w)
	p.shown = len(bars)
	if len(bars) == 0 {
		return content{body: []string{lipgloss.NewStyle().Faint(true).Render("no data")}, cursor: -1}
	}
	if p.cur >= len(bars) {
		p.cur = len(bars) - 1
	}
	top := 0.0
	for _, b := range bars {
		top = math.Max(top, b.value)
	}
	if top == 0 {
		top = 1
	}
	ph := max(1, h-1)
	bw := max(1, w/len(bars))
	gap := 0
	if bw >= 3 {
		gap = 1
	}

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent))
	curStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	var body []string
	for r := ph - 1; r >= 0; r-- {
		var b strings.Builder
		for i, br := range bars {
			fill := int(math.Round(br.value / top * float64(ph*8)))
			level := max(0, min(8, fill-r*8))
			st := barStyle
			if focused && i == p.cur {
				st = curStyle
			}
			b.WriteString(st.Render(strings.Repeat(string(eighths[level]), bw-gap)))
			b.WriteString(strings.Repeat(" ", gap))
		}
		body = append(body, b.String())
	}

	var foot string
	dim := lipgloss.NewStyle().Faint(true)
	switch {
	case focused && p.cur >= 0:
		foot = curStyle.Render(bars[p.cur].label) + dim.Render(" = ") + fmtNum(bars[p.cur].value)
	case p.bars == nil:
		l, r := strings.Split(bars[0].label, "–")[0], strings.Split(bars[len(bars)-1].label, "–")[1]
		foot = dim.Render(l + strings.Repeat(" ", max(1, len(bars)*bw-gap-len(l)-len(r))) + r)
	default:
		var b strings.Builder
		for _, br := range bars {
			b.WriteString(fmt.Sprintf("%-*s", bw, ansi.Truncate(br.label, bw-gap, "")))
		}
		foot = dim.Render(b.String())
	}
	if h > 1 {
		body = append(body, foot)
	}
	return content{body: body, cursor: -1}
}

func (p *histPane) Key(k string, _ int) bool {
	return moveCursor(&p.cur, p.shown, k)
}

func (p *histPane) Click(int)     {}
func (p *histPane) Selected() any { return nil }
