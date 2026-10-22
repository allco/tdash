package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pkg/browser"
)

func init() {
	// Whatever the browser opener prints would land on top of the TUI.
	browser.Stdout, browser.Stderr = io.Discard, io.Discard
}

const (
	minWidth, minHeight = 40, 10
	wheelLines          = 3
	sideStep            = 4 // columns per left/right press or wheel tick
	flashFor            = 5 * time.Second
	actionTimeout       = time.Minute
)

var (
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// source is one thing that fetches data: a named supplier, or a pane's own data.
type source struct {
	ref      DataRef
	interval time.Duration // 0: only on R
	timeout  time.Duration
	loading  bool
	has      bool // value holds data from a successful fetch
	value    any
	err      error
	updated  time.Time
}

// paneState is a pane plus what the frame around it keeps: scroll offsets, errors, and the sizes
// of the last render, which keys and the mouse need.
type paneState struct {
	cfg    PaneConfig
	pane   Pane
	srcKey string // "" when the pane has no data source
	sel    []selectStep
	err    error

	yoff, xoff int
	follow     bool // keep the content cursor in view on the next render

	topH, bodyH, bodyLen, wide, iw int
}

type dataMsg struct {
	gen   int
	key   string
	value any
	err   error
}

type tickMsg struct {
	gen int
	key string
}

type actionMsg struct {
	what string
	err  error
}

type clearFlashMsg struct{ at time.Time }

type model struct {
	path string
	cfg  *Config

	panes   []*paneState
	sources map[string]*source
	gen     int // bumped on reload; messages from an older generation are dropped

	focus  int // focused pane, -1 for none
	offset int // first dashboard line on screen
	zoom   bool
	help   bool
	paused bool

	splash   bool
	splashUp bool
	spinner  spinner.Model

	flash    string
	flashErr bool
	flashAt  time.Time

	width, height int
}

func newModel(path string, cfg *Config) *model {
	m := &model{path: path, focus: -1, splash: true, spinner: spinner.New(spinner.WithSpinner(spinner.Dot))}
	m.build(cfg, nil)
	return m
}

func (m *model) accent() string { return colorOr(m.cfg.Theme.Accent, "6") }
func (m *model) border() string { return colorOr(m.cfg.Theme.Border, "240") }

// build sets up panes and sources for cfg. Panes and sources that didn't change from old keep
// their state (selection, scroll, last data), so a reload doesn't blank the screen.
func (m *model) build(cfg *Config, old *model) {
	m.cfg = cfg
	m.gen++
	m.panes = nil
	m.sources = map[string]*source{}
	global, _ := parseRefresh(cfg.Refresh)

	pick := func(ds ...time.Duration) time.Duration {
		for _, d := range ds {
			if d != 0 {
				return max(d, 0) // "off" is -1
			}
		}
		return 0
	}

	// A named supplier without its own refresh follows the fastest pane that uses it, then the
	// global refresh.
	fastest := map[string]time.Duration{}
	for i, pc := range cfg.panes() {
		ps := &paneState{cfg: pc, pane: newPane(pc, m.accent())}
		if old != nil && i < len(old.panes) && sameJSON(old.panes[i].cfg, pc) {
			o := old.panes[i]
			ps.pane, ps.yoff, ps.xoff = o.pane, o.yoff, o.xoff
		}
		ps.sel, _ = parseSelect(pc.Data.Select)
		paneRefresh, _ := parseRefresh(pc.Refresh)
		switch {
		case pc.Data.Supplier != "":
			ps.srcKey = "supplier:" + pc.Data.Supplier
			if _, ok := m.sources[ps.srcKey]; !ok {
				sup := cfg.Suppliers[pc.Data.Supplier]
				timeout, _ := parseTimeout(sup.Timeout)
				m.sources[ps.srcKey] = &source{ref: DataRef{Source: sup}, timeout: timeout}
			}
			if f, ok := fastest[ps.srcKey]; paneRefresh > 0 && (!ok || f <= 0 || paneRefresh < f) {
				fastest[ps.srcKey] = paneRefresh
			} else if !ok && paneRefresh < 0 {
				fastest[ps.srcKey] = paneRefresh
			}
		case len(pc.Data.kinds()) > 0:
			ps.srcKey = fmt.Sprintf("pane:%d", i)
			own, _ := parseRefresh(pc.Data.Refresh)
			timeout, _ := parseTimeout(pc.Data.Timeout)
			src := &source{ref: pc.Data, timeout: timeout, interval: pick(own, paneRefresh, global)}
			if len(pc.Data.Inline) > 0 {
				src.interval = 0
			}
			m.sources[ps.srcKey] = src
		}
		m.panes = append(m.panes, ps)
	}
	for name, sup := range cfg.Suppliers {
		if src, ok := m.sources["supplier:"+name]; ok {
			own, _ := parseRefresh(sup.Refresh)
			src.interval = pick(own, fastest["supplier:"+name], global)
		}
	}

	if old != nil {
		for k, src := range m.sources {
			if o, ok := old.sources[k]; ok && o.has && sameJSON(o.ref, src.ref) {
				src.has, src.value, src.updated = true, o.value, o.updated
			}
		}
		for i := range m.panes {
			m.deliver(i)
		}
	}
	if m.focus >= len(m.panes) {
		m.focus = len(m.panes) - 1
	}
}

func sameJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, splashCmd(), splashMaxCmd()}
	for k := range m.sources {
		cmds = append(cmds, m.fetchCmd(k))
	}
	return tea.Batch(cmds...)
}

func (m *model) fetchCmd(key string) tea.Cmd {
	src := m.sources[key]
	if src == nil || src.loading {
		return nil
	}
	src.loading = true
	gen, ref, timeout, dir := m.gen, src.ref, src.timeout, m.cfg.dir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		v, err := fetch(ctx, ref, dir)
		return dataMsg{gen: gen, key: key, value: v, err: err}
	}
}

func (m *model) tickCmd(key string) tea.Cmd {
	src := m.sources[key]
	if src == nil || src.interval <= 0 {
		return nil
	}
	gen := m.gen
	return tea.Tick(src.interval, func(time.Time) tea.Msg { return tickMsg{gen: gen, key: key} })
}

// deliver hands pane i its data: the source's value, narrowed by the pane's select.
func (m *model) deliver(i int) {
	ps := m.panes[i]
	src := m.sources[ps.srcKey]
	if src == nil {
		return
	}
	ps.err = src.err
	if !src.has {
		return
	}
	v := src.value
	if ps.sel != nil {
		var err error
		if v, err = applySelect(v, ps.sel); err != nil {
			ps.err = fmt.Errorf("select %s: %w", ps.cfg.Data.Select, err)
			return
		}
	}
	if err := ps.pane.SetData(v); err != nil {
		ps.err = err
	}
}

func (m *model) loadingAny() bool {
	for _, s := range m.sources {
		if s.loading {
			return true
		}
	}
	return false
}

func (m *model) setFlash(s string, isErr bool) tea.Cmd {
	m.flash, m.flashErr, m.flashAt = s, isErr, time.Now()
	at := m.flashAt
	return tea.Tick(flashFor, func(time.Time) tea.Msg { return clearFlashMsg{at} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case splashDoneMsg:
		m.splashUp = true
		m.splash = m.splash && m.loadingAny()
		return m, nil

	case splashMaxMsg:
		m.splash = false
		return m, nil

	case dataMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		src := m.sources[msg.key]
		src.loading = false
		src.err = msg.err
		if msg.err == nil {
			src.value, src.has, src.updated = msg.value, true, time.Now()
		}
		for i, ps := range m.panes {
			if ps.srcKey == msg.key {
				m.deliver(i)
			}
		}
		if m.splashUp && !m.loadingAny() {
			m.splash = false
		}
		return m, m.tickCmd(msg.key)

	case tickMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		if m.paused {
			return m, m.tickCmd(msg.key)
		}
		return m, m.fetchCmd(msg.key)

	case actionMsg:
		if msg.err != nil {
			return m, m.setFlash(msg.what+": "+msg.err.Error(), true)
		}
		return m, m.setFlash(msg.what, false)

	case clearFlashMsg:
		if msg.at.Equal(m.flashAt) {
			m.flash = ""
		}
		return m, nil

	case tea.MouseMsg:
		m.splash = false
		return m, m.mouse(msg)

	case tea.KeyMsg:
		m.splash = false // any key skips the splash, and still does what it does
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *model) key(k string) tea.Cmd {
	if m.help {
		switch k {
		case "q", "Q", "ctrl+c":
			return tea.Quit
		case "esc", "?", "enter":
			m.help = false
		}
		return nil
	}
	switch k {
	case "q", "Q", "ctrl+c":
		return tea.Quit
	case "r", "R":
		return m.reload()
	case "?":
		m.help = true
	case "p", "P":
		m.paused = !m.paused
		if m.paused {
			return m.setFlash("auto-refresh paused", false)
		}
		return m.setFlash("auto-refresh resumed", false)
	case "tab", "shift+tab":
		if len(m.panes) == 0 {
			return nil
		}
		switch {
		case m.focus < 0 && k == "tab":
			m.focus = 0
		case m.focus < 0:
			m.focus = len(m.panes) - 1
		case k == "tab":
			m.focus = (m.focus + 1) % len(m.panes)
		default:
			m.focus = (m.focus - 1 + len(m.panes)) % len(m.panes)
		}
		m.showFocused()
	case "esc":
		if m.zoom {
			m.zoom = false
		} else {
			m.focus = -1
		}
	case "z", "Z":
		if m.focus >= 0 {
			m.zoom = !m.zoom
		}
	case "enter":
		if m.focus >= 0 {
			return m.runAction(m.panes[m.focus])
		}
	default:
		if m.focus >= 0 {
			m.scrollPane(m.panes[m.focus], k)
		} else {
			m.scrollDashboard(k)
		}
	}
	return nil
}

// scrollPane gives a key to the pane first (table selection, chart cursor); otherwise the key
// scrolls the pane's content.
func (m *model) scrollPane(ps *paneState, k string) {
	if ps.pane.Key(k, ps.bodyH) {
		ps.follow = true
		return
	}
	switch k {
	case "up", "k":
		ps.yoff--
	case "down", "j":
		ps.yoff++
	case "pgup":
		ps.yoff -= max(1, ps.bodyH)
	case "pgdown", " ":
		ps.yoff += max(1, ps.bodyH)
	case "home":
		ps.yoff, ps.xoff = 0, 0
	case "end":
		ps.yoff = ps.bodyLen
	case "left", "h":
		ps.xoff -= sideStep
	case "right", "l":
		ps.xoff += sideStep
	}
	ps.clamp()
}

func (ps *paneState) clamp() {
	ps.yoff = max(0, min(ps.yoff, ps.bodyLen-ps.bodyH))
	ps.xoff = max(0, min(ps.xoff, ps.wide-ps.iw))
}

func (m *model) scrollDashboard(k string) {
	avail := m.height - 1
	switch k {
	case "up", "k":
		m.offset--
	case "down", "j":
		m.offset++
	case "pgup":
		m.offset -= avail
	case "pgdown", " ":
		m.offset += avail
	case "home":
		m.offset = 0
	case "end":
		m.offset = 1 << 30
	}
	m.layout() // clamps the offset
}

// showFocused scrolls the dashboard so the focused pane is on screen.
func (m *model) showFocused() {
	rects, _, _ := m.layout()
	if m.focus < 0 || m.focus >= len(rects) {
		return
	}
	r, avail := rects[m.focus], m.height-1
	if r.y < m.offset {
		m.offset = r.y
	} else if r.y+r.h > m.offset+avail {
		m.offset = min(r.y, r.y+r.h-avail)
	}
}

// layout places the panes for the current terminal size, and clamps the dashboard scroll.
// When the rows don't fit, the last column is kept for the scrollbar.
func (m *model) layout() ([]rect, int, int) {
	l := m.cfg.Layout
	avail := max(1, m.height-1)
	w := m.width
	rects, total := layoutGrid(l.Rows, w, avail, l.Unit, l.MinUnit)
	if total > avail {
		w--
		rects, total = layoutGrid(l.Rows, w, avail, l.Unit, l.MinUnit)
	}
	m.offset = max(0, min(m.offset, total-avail))
	return rects, total, w
}

func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.help {
		return nil
	}
	i, r := m.paneAt(msg.X, msg.Y)
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		d := wheelLines
		if msg.Button == tea.MouseButtonWheelUp {
			d = -d
		}
		if i < 0 {
			m.offset += d
			m.layout()
			return nil
		}
		ps := m.panes[i]
		switch {
		case msg.Shift:
			ps.xoff += d / wheelLines * sideStep
		case ps.bodyLen > ps.bodyH:
			ps.yoff += d
		default:
			// Nothing to scroll in the pane: scroll the dashboard instead.
			m.offset += d
			m.layout()
			return nil
		}
		ps.clamp()
	case msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelRight:
		if i >= 0 {
			ps := m.panes[i]
			if msg.Button == tea.MouseButtonWheelLeft {
				ps.xoff -= sideStep
			} else {
				ps.xoff += sideStep
			}
			ps.clamp()
		}
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		if i < 0 {
			return nil
		}
		m.focus = i
		ps := m.panes[i]
		// The line inside the frame: below the border, any error lines and the header.
		if line := msg.Y - r.y - 1 - ps.topH; line >= 0 && line < ps.bodyH {
			ps.pane.Click(ps.yoff + line)
		}
	}
	return nil
}

// paneAt finds the pane under a screen position, and where that pane is on screen.
func (m *model) paneAt(x, y int) (int, rect) {
	if y >= m.height-1 {
		return -1, rect{}
	}
	if m.zoom && m.focus >= 0 {
		return m.focus, rect{0, 0, m.width, m.height - 1}
	}
	rects, _, _ := m.layout()
	for i, r := range rects {
		r.y -= m.offset
		if x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h {
			return i, r
		}
	}
	return -1, rect{}
}

func (m *model) reload() tea.Cmd {
	cfg, err := loadConfig(m.path)
	if err != nil {
		return m.setFlash(err.Error(), true)
	}
	old := *m
	m.build(cfg, &old)
	cmds := []tea.Cmd{m.setFlash("reloaded", false)}
	for k := range m.sources {
		cmds = append(cmds, m.fetchCmd(k))
	}
	return tea.Batch(cmds...)
}

// ---- actions ----

var placeholder = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// fill replaces {{field}} placeholders with values from item, each passed through esc.
func fill(tmpl string, item any, esc func(string) string) string {
	return placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		v := item
		for _, f := range strings.Split(placeholder.FindStringSubmatch(m)[1], ".") {
			switch t := v.(type) {
			case *Object:
				v = t.Vals[f]
			case []any:
				n, err := strconv.Atoi(f)
				if err != nil || n < 0 || n >= len(t) {
					return ""
				}
				v = t[n]
			default:
				return ""
			}
		}
		return esc(formatValue(v))
	})
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (m *model) runAction(ps *paneState) tea.Cmd {
	item := ps.pane.Selected()
	if item == nil {
		return nil
	}
	act := ps.cfg.Action
	if obj, ok := item.(*Object); ok {
		if a, ok := obj.Get("action"); ok {
			var own Action
			b, _ := json.Marshal(plain(a))
			if json.Unmarshal(b, &own) == nil {
				act = &own
			}
		}
	}
	if act == nil {
		return m.setFlash("no action for this pane", false)
	}
	same := func(s string) string { return s }
	switch {
	case act.Open != "":
		u := fill(act.Open, item, same)
		return func() tea.Msg { return actionMsg{"opened " + u, browser.OpenURL(u)} }
	case act.Copy != "":
		v := fill(act.Copy, item, same)
		return func() tea.Msg { return actionMsg{"copied " + v, clipboard.WriteAll(v)} }
	default:
		line := fill(act.Run, item, shellQuote)
		shell, flag := shellCommand()
		if act.TTY {
			c := exec.Command(shell, flag, line)
			c.Dir = m.cfg.dir
			return tea.ExecProcess(c, func(err error) tea.Msg { return actionMsg{"ran " + line, err} })
		}
		dir := m.cfg.dir
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
			defer cancel()
			c := exec.CommandContext(ctx, shell, flag, line)
			c.Dir = dir
			out, err := c.CombinedOutput()
			if err != nil && len(out) > 0 {
				err = fmt.Errorf("%w: %s", err, firstLines(string(out), 1))
			}
			return actionMsg{"ran " + line, err}
		}
	}
}

// ---- view ----

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("Terminal too small (need ≥ %d×%d)", minWidth, minHeight))
	}
	if m.splash {
		return m.splashView()
	}
	if m.help {
		return m.helpView()
	}

	avail := m.height - 1
	var lines []string
	if m.zoom && m.focus >= 0 {
		lines = m.frame(m.focus, m.width, avail)
	} else {
		rects, total, _ := m.layout()
		canvas := make([]string, 0, total)
		for start := 0; start < len(rects); {
			end := start + 1
			for end < len(rects) && rects[end].y == rects[start].y {
				end++
			}
			frames := make([][]string, 0, end-start)
			for i := start; i < end; i++ {
				frames = append(frames, m.frame(i, rects[i].w, rects[i].h))
			}
			for l := 0; l < rects[start].h; l++ {
				var b strings.Builder
				for _, f := range frames {
					b.WriteString(f[l])
				}
				canvas = append(canvas, b.String())
			}
			start = end
		}
		lines = canvas[m.offset:min(len(canvas), m.offset+avail)]
		if total > avail {
			lines = withScrollbar(lines, m.offset, total, avail, m.accent())
		}
	}
	for len(lines) < avail {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, m.statusLine()), "\n")
}

// withScrollbar adds the dashboard scrollbar as a last column.
func withScrollbar(lines []string, offset, total, visible int, color string) []string {
	size, pos := thumb(offset, total, visible)
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("┃")
	off := dimStyle.Render("│")
	out := make([]string, len(lines))
	for i, l := range lines {
		if i >= pos && i < pos+size {
			out[i] = l + on
		} else {
			out[i] = l + off
		}
	}
	return out
}

// thumb is the size and position of a scrollbar thumb along a track of visible cells.
func thumb(offset, total, visible int) (int, int) {
	size := max(1, visible*visible/max(total, 1))
	pos := 0
	if total > visible {
		pos = offset * (visible - size) / (total - visible)
	}
	return size, pos
}

// frame draws pane i: border, title, error, and its content scrolled and clipped to fit.
func (m *model) frame(i, w, h int) []string {
	ps := m.panes[i]
	focused := i == m.focus
	blank := func() []string {
		out := make([]string, h)
		for j := range out {
			out[j] = strings.Repeat(" ", w)
		}
		return out
	}
	iw, ih := w-2, h-2
	if iw < 1 || ih < 1 {
		return blank()
	}
	ps.iw = iw

	bs := lipgloss.NewStyle().Foreground(lipgloss.Color(m.border()))
	ts := lipgloss.NewStyle().Bold(true)
	if focused {
		bs = lipgloss.NewStyle().Foreground(lipgloss.Color(m.accent())).Bold(true)
		ts = ts.Reverse(true).Foreground(lipgloss.Color(m.accent()))
	}

	// Title, with a mark while its data is loading.
	title := ps.cfg.Title
	src := m.sources[ps.srcKey]
	if src != nil && src.loading {
		title = strings.TrimSpace(title + " ⟳")
	}
	var top string
	if title != "" && iw > 3 {
		t := ansi.Truncate(" "+title+" ", iw-1, "…")
		top = bs.Render("┌─") + ts.Render(t) + bs.Render(strings.Repeat("─", iw-1-ansi.StringWidth(t))+"┐")
	} else {
		top = bs.Render("┌" + strings.Repeat("─", iw) + "┐")
	}

	var errLines []string
	if ps.err != nil {
		e := attrs{fg: "196"}
		errLines = wrapSegs([]seg{{ps.err.Error(), e}}, iw, seg{"✗ ", e}, seg{"  ", e})
		errLines = errLines[:min(len(errLines), max(1, ih/2))]
	}
	var c content
	switch {
	case src == nil || src.has:
		c = ps.pane.Content(iw, ih-len(errLines), focused)
	case ps.err == nil:
		c = content{body: []string{dimStyle.Render("loading…")}, cursor: -1}
	}

	ps.topH = len(errLines) + len(c.header)
	ps.bodyH = max(0, ih-ps.topH)
	ps.bodyLen = len(c.body)
	ps.wide = 0
	for _, l := range append(append([]string{}, c.header...), c.body...) {
		ps.wide = max(ps.wide, ansi.StringWidth(l))
	}
	if ps.follow && c.cursor >= 0 {
		if c.cursor < ps.yoff {
			ps.yoff = c.cursor
		} else if c.cursor >= ps.yoff+ps.bodyH {
			ps.yoff = c.cursor - ps.bodyH + 1
		}
	}
	ps.follow = false
	ps.clamp()

	clip := func(l string, scroll bool) string {
		if scroll && ps.xoff > 0 {
			l = ansi.TruncateLeft(l, ps.xoff, "")
		}
		l = ansi.Truncate(l, iw, "")
		return l + strings.Repeat(" ", max(0, iw-ansi.StringWidth(l)))
	}
	inner := make([]string, 0, ih)
	for _, l := range errLines {
		inner = append(inner, clip(l, false))
	}
	for _, l := range c.header {
		inner = append(inner, clip(l, true))
	}
	for j := ps.yoff; j < len(c.body) && len(inner) < ih; j++ {
		inner = append(inner, clip(c.body[j], true))
	}
	for len(inner) < ih {
		inner = append(inner, strings.Repeat(" ", iw))
	}
	inner = inner[:ih]

	// Scrollbars sit in the border: vertical on the right, horizontal at the bottom.
	vSize, vPos := -1, 0
	if ps.bodyLen > ps.bodyH && ps.bodyH > 0 {
		vSize, vPos = thumb(ps.yoff, ps.bodyLen, ps.bodyH)
	}
	out := make([]string, 0, h)
	out = append(out, top)
	for j, l := range inner {
		right := "│"
		if b := j - ps.topH; vSize > 0 && b >= vPos && b < vPos+vSize {
			right = "┃"
		}
		out = append(out, bs.Render("│")+l+bs.Render(right))
	}
	bottom := strings.Repeat("─", iw)
	if ps.wide > iw {
		size, pos := thumb(ps.xoff, ps.wide, iw)
		bottom = strings.Repeat("─", pos) + strings.Repeat("━", size) + strings.Repeat("─", iw-pos-size)
	}
	out = append(out, bs.Render("└"+bottom+"┘"))
	return out
}

func (m *model) statusLine() string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.accent()))
	left := accent.Render("tdash")
	if m.cfg.Title != "" {
		left += dimStyle.Render(" · ") + m.cfg.Title
	}

	var info string
	switch {
	case m.flash != "" && m.flashErr:
		info = errStyle.Render(m.flash)
	case m.flash != "":
		info = dimStyle.Render(m.flash)
	case m.focus >= 0:
		if src := m.sources[m.panes[m.focus].srcKey]; src != nil {
			switch {
			case src.loading:
				info = dimStyle.Render("loading…")
			case src.has:
				s := "updated " + src.updated.Format("15:04:05")
				if src.interval > 0 {
					s += " · every " + src.interval.String()
				}
				info = dimStyle.Render(s)
			}
		}
	}

	hints := "tab focus · ? help · q quit"
	switch {
	case m.zoom:
		hints = "esc unzoom · ? help · q quit"
	case m.focus >= 0:
		hints = "↑↓←→ scroll · z zoom · esc unfocus · ? help"
	}
	if m.paused {
		hints = "paused · " + hints
	}
	right := dimStyle.Render(hints)

	line := left
	if info != "" {
		line += "  " + info
	}
	gap := m.width - ansi.StringWidth(line) - ansi.StringWidth(right) - 1
	if gap < 2 {
		return ansi.Truncate(line, m.width-1, "…")
	}
	return line + strings.Repeat(" ", gap) + right
}

var helpKeys = [][2]string{
	{"tab / shift+tab", "focus the next / previous pane"},
	{"esc", "unzoom, or remove the focus"},
	{"↑ ↓ ← →", "scroll the focused pane (or the dashboard)"},
	{"pgup pgdn home end", "scroll by a page, to the start / end"},
	{"", "tables: ↑↓ select a row · charts: ←→ move the cursor"},
	{"enter", "run the action of the selected row"},
	{"z", "zoom the focused pane to the full screen"},
	{"mouse", "click focuses, wheel scrolls (shift+wheel sideways)"},
	{"r", "reload the config and all data"},
	{"p", "pause / resume auto-refresh"},
	{"?", "this help"},
	{"q", "quit"},
}

func (m *model) helpView() string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.accent()))
	lines := append(logoLines(cornerLogo), "", dimStyle.Render(version), "")
	for _, k := range helpKeys {
		lines = append(lines, keyStyle.Render(fmt.Sprintf("%-20s", k[0]))+k[1])
	}
	lines = append(lines, "", dimStyle.Render("config: "+m.path))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(m.accent())).
		Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
