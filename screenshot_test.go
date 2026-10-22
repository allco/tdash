package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestScreenshots renders the example dashboards with made-up data and writes each screen as ANSI
// text to $TDASH_SCREENSHOTS. scripts/screenshots.sh turns those into the PNGs in docs/screenshots.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("TDASH_SCREENSHOTS")
	if dir == "" {
		t.Skip("set TDASH_SCREENSHOTS to a directory to render screenshots")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)

	shots := []struct {
		name          string
		model         func(*testing.T) *model
		keys          []string
		width, height int
	}{
		// Services focused, a row selected, scrolled sideways to the URL column.
		{name: "table", keys: []string{"tab", "tab", "tab", "down", "down", "down", "right", "right", "right", "right"}, width: 140, height: 42},
		// The spline zoomed to the full screen, with the cursor on a point.
		{name: "chart", keys: []string{"tab", "z", "left", "left", "left", "left", "left", "left", "left", "left", "left", "left"}, width: 140, height: 30},
		{name: "help", keys: []string{"?"}, width: 100, height: 30},
		// The agents status board, with the open stack focused and a part selected.
		{name: "agents", model: agentsModel, keys: []string{"tab", "tab", "tab", "down", "down"}, width: 160, height: 44},
	}
	for _, shot := range shots {
		if shot.model == nil {
			shot.model = demoModel
		}
		m := shot.model(t)
		m.Update(tea.WindowSizeMsg{Width: shot.width, Height: shot.height})
		for _, k := range shot.keys {
			m.View() // the frame measures each pane, and scrolling needs those sizes
			m.key(k)
		}
		if err := os.WriteFile(filepath.Join(dir, shot.name+".ansi"), []byte(freezeColors(m.View())), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// freezeColors rewrites the basic background colors (SGR 40–47 and 100–107) as their 256-color
// equivalents, because freeze drops the basic ones. The terminal shows the same colors either way.
func freezeColors(s string) string {
	return sgr.ReplaceAllStringFunc(s, func(seq string) string {
		ps := strings.Split(sgr.FindStringSubmatch(seq)[1], ";")
		for i := 0; i < len(ps); i++ {
			if ps[i] == "38" || ps[i] == "48" { // extended color: skip its arguments
				if i+1 < len(ps) && ps[i+1] == "5" {
					i += 2
				} else if i+1 < len(ps) && ps[i+1] == "2" {
					i += 4
				}
				continue
			}
			n, err := strconv.Atoi(ps[i])
			switch {
			case err != nil:
			case n >= 40 && n <= 47:
				ps[i] = "48;5;" + strconv.Itoa(n-40)
			case n >= 100 && n <= 107:
				ps[i] = "48;5;" + strconv.Itoa(n-100+8)
			}
		}
		return "\x1b[" + strings.Join(ps, ";") + "m"
	})
}

// agentsModel is examples/agents/agents.json, fed from its made-up status.md.
func agentsModel(t *testing.T) *model {
	t.Helper()
	cfg, err := loadConfig("examples/agents/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel("examples/agents/agents.json", cfg)
	m.splash = false
	for k, src := range m.sources {
		v, err := fetch(context.Background(), src.ref, cfg.dir)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		src.value, src.has, src.updated = v, true, time.Date(2026, 10, 22, 16, 11, 30, 0, time.Local)
	}
	for i := range m.panes {
		m.deliver(i)
	}
	return m
}

// demoModel is examples/demo.json with made-up data in place of its commands and URL, so the
// screenshots don't change from run to run.
func demoModel(t *testing.T) *model {
	t.Helper()
	cfg, err := loadConfig("examples/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel("examples/demo.json", cfg)
	m.splash = false
	updated := time.Date(2026, 10, 22, 14, 32, 5, 0, time.Local)

	for i, ps := range m.panes {
		key := ps.srcKey
		src := m.sources[key]
		if src == nil {
			continue
		}
		var raw string
		switch {
		case key == "supplier:metrics":
			raw = demoMetrics()
		case key == "supplier:releases":
			raw = demoReleases()
		case ps.cfg.Title == "Host":
			raw = `"# web-01\n\n{green}●{/} up 12 days, 3:41\n\n{dim}load{/} 0.42 0.38 0.35\n\n` + "`14:32:05`" + `"`
		default:
			v, err := fetch(context.Background(), src.ref, cfg.dir)
			if err != nil {
				t.Fatal(err)
			}
			src.value, src.has, src.updated = v, true, updated
			m.deliver(i)
			continue
		}
		v, err := decodeJSON([]byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		src.value, src.has, src.updated = v, true, updated
		m.deliver(i)
	}
	return m
}

func demoMetrics() string {
	r := rand.New(rand.NewSource(7))
	series := func(start, step float64) string {
		v := start
		out := make([]string, 40)
		for i := range out {
			v = max(2, min(98, v+r.Float64()*2*step-step))
			out[i] = fmt.Sprintf("%.1f", v)
		}
		return strings.Join(out, ",")
	}
	cpu, mem := series(45, 10), series(60, 3)
	lat := make([]string, 300)
	for i := range lat {
		v := 0.0
		for range 6 {
			v += r.Float64()
		}
		lat[i] = fmt.Sprintf("%.0f", 40+v*60)
	}
	return fmt.Sprintf(`{"load": {"cpu": [%s], "memory": [%s]}, "latency": [%s]}`, cpu, mem, strings.Join(lat, ","))
}

func demoReleases() string {
	var rows []string
	day := time.Date(2026, 10, 20, 9, 14, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		tag := fmt.Sprintf("v2.%d.%d", 4-i/4, 3-i%4)
		names := []string{"Faster renderer", "Mouse fixes", "Better key handling", "Bug fixes"}
		rows = append(rows, fmt.Sprintf(`{"tag_name": %q, "published_at": %q, "name": %q, "html_url": "https://example.com/releases/%s"}`,
			tag, day.Format(time.RFC3339), tag+": "+names[i%4], tag))
		day = day.AddDate(0, 0, -9-i%5)
	}
	return "[" + strings.Join(rows, ",") + "]"
}
