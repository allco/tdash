package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// version is set at release build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

const defaultConfig = "tdash.json"

func main() {
	configFlag := flag.String("config", "", "path to the dashboard config")
	showVersion := flag.Bool("version", false, "print the version")
	noMouse := flag.Bool("no-mouse", false, "leave the mouse to the terminal (text selection)")
	var help bool
	for _, name := range []string{"help", "h", "usage"} {
		flag.BoolVar(&help, name, false, "show this help")
	}
	flag.Usage = func() { printUsage(os.Stderr) }
	flag.Parse()
	if help {
		printUsage(os.Stdout)
		return
	}
	if *showVersion {
		fmt.Println("tdash", version)
		return
	}

	path := *configFlag
	if path == "" && flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	if path == "" {
		path = defaultConfig
	}
	cfg, err := loadConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			fail(fmt.Errorf("no config at %s (pass one: tdash path/to/dashboard.json; see examples/)", path))
		}
		fail(err)
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !*noMouse {
		// All motion, not just drags, so the pointer can change over a border that can be dragged.
		opts = append(opts, tea.WithMouseAllMotion())
	}
	m := newModel(path, cfg)
	m.noMouse = *noMouse
	_, err = tea.NewProgram(m, opts...).Run()
	fmt.Print(pointerSeq("")) // leave the terminal with its own pointer
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tdash:", err)
	os.Exit(1)
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `tdash %s — a terminal dashboard laid out and fed by a JSON config.

Usage:
  tdash [path/to/dashboard.json]
  tdash --config <path>
  tdash --help | --usage | --version

Options:
  --config <path>   dashboard config to use (default: ./%s)
  --no-mouse        don't capture the mouse, so the terminal can select text
  -h, --help, --usage
                    show this help
  --version         print the version

The config is JSON; comments and trailing commas are allowed. See SPEC.md and examples/.
The dashboard reloads by itself when the config file changes.

Keys:
  tab, shift+tab    focus the next / previous pane
  esc               turn the mouse back on, else unzoom, else remove the focus
  arrows            scroll the focused pane, or the dashboard when nothing is focused
                    (tables: up/down select a row; charts: left/right move the cursor)
  pgup/pgdn/home/end
                    scroll by a page, to the start / end
  enter             run the action of the selected row
  z                 zoom the focused pane to the full screen
  < > + -           narrow / widen the focused pane, lower / raise its row
  =                 reset pane sizes to the config (deletes <config>.sizes.json)
  r                 reload the config and all data
  p                 pause / resume auto-refresh
  m                 mouse on / off; off leaves it to the terminal, so text can be selected
                    (esc also turns it back on)
  ?                 help
  q, ctrl+c         quit

Mouse: click focuses a pane (and selects a table row), the wheel scrolls, shift+wheel scrolls
sideways, dragging a border resizes panes (saved to <config>.sizes.json). Most terminals also select text with shift+drag while tdash has the mouse.
`, version, defaultConfig)
}
