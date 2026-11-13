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
	update := flag.Bool("update", false, "update to the newest release, after asking")
	noUpdateCheck := flag.Bool("no-update-check", false, "don't look for newer releases")
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
	if *update {
		if err := updateCLI("tdash", version, os.Stdin, os.Stdout); err != nil {
			fail(err)
		}
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
		if os.IsNotExist(err) && *configFlag == "" && flag.NArg() == 0 {
			printGettingStarted(os.Stdout)
			return
		}
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
	m.upd = newUpdater("tdash", version, *noUpdateCheck || os.Getenv("TDASH_NO_UPDATE_CHECK") != "")
	_, err = tea.NewProgram(m, opts...).Run()
	fmt.Print(pointerSeq("")) // leave the terminal with its own pointer
	if err != nil {
		fail(err)
	}
	if m.upd.Restart != "" {
		fail(restart(m.upd.Restart))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tdash:", err)
	os.Exit(1)
}

// printGettingStarted is what tdash shows when it is run with no arguments and there is no
// ./tdash.json: where the config format and the examples are, and how to have an AI assistant
// write a config.
func printGettingStarted(w io.Writer) {
	docs := projectURL("tdash") + "/tree/main/docs"
	examples := projectURL("tdash") + "/tree/main/examples"
	fmt.Fprintf(w, `tdash %[1]s: a terminal dashboard you describe in one JSON file.

No ./%[2]s here. Run it with a config:

  tdash path/to/dashboard.json

To write a config:

  Format reference  %[3]s
  Examples          %[4]s

Or ask an AI assistant to write one. Give it the reference link above
and describe what you want to see, for example:

  "Write a tdash config (%[3]s)
   that shows CPU, memory and disk usage as charts, and the top 10
   processes by CPU as a table, refreshing every 5 seconds."

  "Write a tdash config that shows my open GitHub pull requests in a
   table and the latest CI runs of the repo in this folder, using the
   gh command line."

  "Write a tdash config for my Kubernetes cluster: pods that aren't
   Running, recent events, and node CPU and memory, using kubectl."

Save it as %[2]s and run tdash, or pass its path.
tdash --help lists the options.
`, version, defaultConfig, docs, examples)
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `tdash %s — a terminal dashboard laid out and fed by a JSON config.

Usage:
  tdash [path/to/dashboard.json]
  tdash --config <path>
  tdash --help | --usage | --version
  tdash --update

Options:
  --config <path>   dashboard config to use (default: ./%s)
  --no-mouse        don't capture the mouse, so the terminal can select text
  --update          update to the newest release, after asking
  --no-update-check don't look for newer releases (or set TDASH_NO_UPDATE_CHECK=1)
  -h, --help, --usage
                    show this help
  --version         print the version

The config is JSON; comments and trailing commas are allowed. See SPEC.md and examples/.
The dashboard reloads by itself when the config file changes. tdash looks for a newer release
at start and once a day; the status line shows one, and u updates (after asking).

Keys:
  tab, shift+tab    focus the next / previous pane
  esc               turn the mouse back on, else unzoom, else remove the focus
  arrows            scroll the focused pane, or the dashboard when nothing is focused
                    (tables: up/down select a row; charts: left/right move the cursor)
  pgup/pgdn/home/end
                    scroll by a page, to the start / end
  enter             run the action of the selected row
  z                 zoom the focused pane to the full screen
  ctrl+shift+← →    narrow / widen the focused pane
  ctrl+shift+↑ ↓    lower / raise the focused pane's row
  =                 reset pane sizes to the config (deletes <config>.sizes.json)
  u                 update to a newer release, when the status line shows one
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
