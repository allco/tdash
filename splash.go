package main

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// splashMin is how long the splash stays up at least, so it doesn't just flash when the first
// load is quick. It stays longer while that load is still running, but never past splashMax.
const (
	splashMin = 1200 * time.Millisecond
	splashMax = 3 * time.Second
)

var splashLogo = []string{
	"████████╗██████╗  █████╗ ███████╗██╗  ██╗",
	"╚══██╔══╝██╔══██╗██╔══██╗██╔════╝██║  ██║",
	"   ██║   ██║  ██║███████║███████╗███████║",
	"   ██║   ██║  ██║██╔══██║╚════██║██╔══██║",
	"   ██║   ██████╔╝██║  ██║███████║██║  ██║",
	"   ╚═╝   ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝",
}

// cornerLogo is the small logo, shown at the top of the help.
var cornerLogo = []string{
	"╺┳╸ ╺┳┓ ┏━┓ ┏━┓ ╻ ╻",
	" ┃   ┃┃ ┣━┫ ┗━┓ ┣━┫",
	" ╹  ╺┻┛ ╹ ╹ ┗━┛ ╹ ╹",
}

// splashColors shades the logo top to bottom, from cyan into violet.
var splashColors = []lipgloss.Color{"51", "45", "39", "33", "63", "99"}

type splashDoneMsg struct{}

type splashMaxMsg struct{}

func splashCmd() tea.Cmd {
	return tea.Tick(splashMin, func(time.Time) tea.Msg { return splashDoneMsg{} })
}

func splashMaxCmd() tea.Cmd {
	return tea.Tick(splashMax, func(time.Time) tea.Msg { return splashMaxMsg{} })
}

// splashView centres the logo, the version and the loading state in the window.
func (m *model) splashView() string {
	var logo string
	if lipgloss.Width(splashLogo[0]) <= m.width {
		logo = strings.Join(logoLines(splashLogo), "\n")
	} else {
		logo = lipgloss.NewStyle().Bold(true).Foreground(splashColors[0]).Render("tdash")
	}

	status := dimStyle.Render("dashboards in your terminal")
	if m.loadingAny() {
		status = m.spinner.View() + dimStyle.Render("loading data…")
	}
	sub := version
	if m.cfg.Title != "" {
		sub += " · " + m.cfg.Title
	}
	content := lipgloss.JoinVertical(lipgloss.Center,
		logo,
		"",
		dimStyle.Render(sub),
		"",
		status,
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// logoLines renders logo shaded like the splash.
func logoLines(logo []string) []string {
	lines := make([]string, len(logo))
	for i, l := range logo {
		c := splashColors[i*(len(splashColors)-1)/max(len(logo)-1, 1)]
		lines[i] = lipgloss.NewStyle().Bold(true).Foreground(c).Render(l)
	}
	return lines
}
