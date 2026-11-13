package main

// Self-update: look for a newer release at start and once a day, show it in the status line,
// and on u (and a y) download the binary for this platform, check it against the release's
// checksums.txt, put it in place of the running one and offer to restart. Where releases come
// from is in update_source.go, the one part that differs between hosts.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	updateEvery    = 24 * time.Hour
	checkTimeout   = 20 * time.Second
	installTimeout = 5 * time.Minute
)

// Seams for tests: where releases come from, and which binary gets replaced.
var (
	latestFn = latestRelease
	fetchFn  = fetchAsset
	exeFn    = os.Executable
)

// ---- versions ----

type semver struct {
	major, minor, patch int
	pre                 bool // a pre-release such as v1.3.0-rc.1
}

var (
	semverRe   = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(-.*)?$`)
	describeRe = regexp.MustCompile(`^(-\d+-g[0-9a-f]+)?(-dirty)?$`)
)

// parseVersion reads a release tag, or a version built from source by git describe
// (v1.2.0-3-gabc1234, -dirty), which counts as the tag it is based on. ok is false for "dev"
// and other builds that aren't based on a tag; those never look for updates.
func parseVersion(v string) (semver, bool) {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return semver{}, false
	}
	var s semver
	s.major, _ = strconv.Atoi(m[1])
	s.minor, _ = strconv.Atoi(m[2])
	s.patch, _ = strconv.Atoi(m[3])
	s.pre = !describeRe.MatchString(m[4])
	return s, true
}

func (a semver) less(b semver) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	if a.patch != b.patch {
		return a.patch < b.patch
	}
	return a.pre && !b.pre
}

// isNewer reports whether release latest should be offered to a build of version current.
// Pre-releases are never offered.
func isNewer(current, latest string) bool {
	c, ok := parseVersion(current)
	l, ok2 := parseVersion(latest)
	return ok && ok2 && !l.pre && c.less(l)
}

// newestStable picks the highest release that isn't a pre-release, from a list of tags.
func newestStable(tags []string) string {
	best, bestV := "", semver{}
	for _, t := range tags {
		v, ok := parseVersion(t)
		if !ok || v.pre {
			continue
		}
		if best == "" || bestV.less(v) {
			best, bestV = t, v
		}
	}
	return best
}

// assetName is the bare binary of a release for this platform, as the release jobs name it.
func assetName(app, tag string) string {
	name := fmt.Sprintf("%s_%s_%s_%s", app, tag, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// ---- the last check, cached between runs ----

type updateCache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

func updateCachePath(app string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, app, "update.json")
}

func readUpdateCache(app string) updateCache {
	var c updateCache
	if p := updateCachePath(app); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &c)
		}
	}
	return c
}

func writeUpdateCache(app string, c updateCache) {
	p := updateCachePath(app)
	if p == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(p, append(b, '\n'), 0o644)
}

// checkLatest asks the host for the newest release and remembers the answer.
func checkLatest(app string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	latest, err := latestFn(ctx, app)
	if err != nil {
		return "", err
	}
	writeUpdateCache(app, updateCache{Checked: time.Now(), Latest: latest})
	return latest, nil
}

// ---- installing ----

// install downloads release tag's binary for this platform, checks it against checksums.txt and
// puts it in place of the running binary. It returns the path it replaced.
func install(app, tag string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	exe, err := exeFn()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	name := assetName(app, tag)
	sums, err := fetchFn(ctx, app, tag, "checksums.txt")
	if err != nil {
		return "", fmt.Errorf("checksums.txt: %w", err)
	}
	want, ok := checksumFor(sums, name)
	if !ok {
		return "", fmt.Errorf("%s has no build for %s/%s", tag, runtime.GOOS, runtime.GOARCH)
	}
	bin, err := fetchFn(ctx, app, tag, name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if got := sha256.Sum256(bin); hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s doesn't match checksums.txt; nothing changed", name)
	}
	return exe, replaceBinary(exe, bin)
}

// checksumFor finds a file's SHA-256 in sha256sum output.
func checksumFor(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// replaceBinary writes the new binary next to the old one and renames it over it, so a failure
// halfway leaves the old binary as it was. Windows can't replace a running binary, but it can
// rename one, so the old one is moved aside first.
func replaceBinary(exe string, bin []byte) error {
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".update")
	if err := os.WriteFile(tmp, bin, mode); err != nil {
		return notWritable(exe, err)
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			os.Remove(tmp)
			return notWritable(exe, err)
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return notWritable(exe, err)
	}
	return nil
}

func notWritable(exe string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("can't replace %s (no permission); update it by hand from the release page", exe)
	}
	return err
}

// brewManaged reports whether the running binary is one Homebrew installed: it resolves to a path
// in a Cellar. Homebrew then owns updates, so the self-update stays off and --update points to
// brew upgrade instead.
func brewManaged() bool {
	exe, err := exeFn()
	if err != nil {
		return false
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return strings.Contains(filepath.ToSlash(exe), "/Cellar/")
}

// ---- the command line: --update ----

// updateCLI is --update: check, ask, install. It reads the answer from in.
func updateCLI(app, current string, in io.Reader, out io.Writer) error {
	if brewManaged() {
		return fmt.Errorf("this %s was installed with Homebrew; update it with: brew upgrade %s", app, app)
	}
	if _, ok := parseVersion(current); !ok {
		return fmt.Errorf("this %s (%s) wasn't built from a release tag, so it can't tell what is newer", app, current)
	}
	latest, err := checkLatest(app)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	if !isNewer(current, latest) {
		fmt.Fprintf(out, "%s %s is up to date (latest release: %s)\n", app, current, latest)
		return nil
	}
	fmt.Fprintf(out, "Update %s %s → %s? [y/N] ", app, current, latest)
	answer, _ := bufio.NewReader(in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Fprintln(out, "Not updated.")
		return nil
	}
	exe, err := install(app, latest)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Updated %s to %s.\n", exe, latest)
	return nil
}

// ---- in the app ----

type updateCheckMsg struct {
	latest string
	err    error
}

type updateTickMsg struct{}

type updateDoneMsg struct {
	tag string
	err error
}

type updateStep int

const (
	updIdle    updateStep = iota
	updConfirm            // asking "update? y/n"
	updWorking            // downloading and installing
	updRestart            // installed; asking "restart now? y/n"
	updDone               // installed, not restarted
)

// updater is the self-update part of an app: it keeps the newest release it has seen, the
// questions it is asking, and whether the app should restart when it quits.
type updater struct {
	app, current string
	off          bool

	latest  string // a newer release, "" while there is none
	step    updateStep
	err     string
	Restart string // set when the user chose to restart: the binary to run after the app quits
}

// newUpdater sets up the updater. It shows a newer release seen by an earlier run right away,
// and stays off for builds that aren't from a release tag and for Homebrew installs.
func newUpdater(app, current string, off bool) *updater {
	u := &updater{app: app, current: current, off: off}
	if _, ok := parseVersion(current); !ok || brewManaged() {
		u.off = true
	}
	if !u.off {
		if c := readUpdateCache(app); isNewer(current, c.Latest) {
			u.latest = c.Latest
		}
	}
	return u
}

// Init checks for a newer release now; each answer schedules the next check a day later.
func (u *updater) Init() tea.Cmd {
	if u.off {
		return nil
	}
	return u.checkCmd()
}

func (u *updater) checkCmd() tea.Cmd {
	app := u.app
	return func() tea.Msg {
		latest, err := checkLatest(app)
		return updateCheckMsg{latest, err}
	}
}

// Update handles the updater's own messages; handled is false for any other message.
func (u *updater) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case updateCheckMsg:
		// A failed check (offline, no glab login) stays quiet and keeps what was known.
		if msg.err == nil && u.step == updIdle {
			u.latest = ""
			if isNewer(u.current, msg.latest) {
				u.latest = msg.latest
			}
		}
		return true, tea.Tick(updateEvery, func(time.Time) tea.Msg { return updateTickMsg{} })
	case updateTickMsg:
		return true, u.checkCmd()
	case updateDoneMsg:
		if msg.err != nil {
			u.step, u.err = updIdle, msg.err.Error()
			return true, nil
		}
		u.current, u.latest, u.err = msg.tag, "", ""
		u.step = updRestart
		return true, nil
	}
	return false, nil
}

// Key handles u, and y / n while a question is open; a question takes every key until it is
// answered. handled is false for keys the app should handle.
func (u *updater) Key(k string) (handled bool, cmd tea.Cmd) {
	switch u.step {
	case updConfirm:
		switch k {
		case "y", "Y":
			u.step, u.err = updWorking, ""
			app, tag := u.app, u.latest
			return true, func() tea.Msg {
				_, err := install(app, tag)
				return updateDoneMsg{tag, err}
			}
		case "n", "N", "esc":
			u.step = updIdle
		case "ctrl+c":
			return false, nil
		}
		return true, nil
	case updRestart:
		switch k {
		case "y", "Y":
			if exe, err := exeFn(); err == nil {
				u.Restart = exe
			}
			return true, tea.Quit
		case "n", "N", "esc":
			u.step = updDone
		case "ctrl+c":
			return false, nil
		}
		return true, nil
	}
	if (k == "u" || k == "U") && u.latest != "" && u.step == updIdle {
		u.step, u.err = updConfirm, ""
		return true, nil
	}
	return false, nil
}

// Asking reports whether a question is open, so the app can show it instead of its own hints.
func (u *updater) Asking() bool { return u.step == updConfirm || u.step == updRestart }

// The updater's part of the status line is a badge, so a new release is hard to miss: black on
// orange, which reads on dark and light terminals alike; red for a failed update.
var (
	updateBadge    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214"))
	updateErrBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160"))
)

// StatusView renders Status as a badge for the status line; "" for nothing.
func (u *updater) StatusView() string {
	s, isErr := u.Status()
	switch {
	case s == "":
		return ""
	case isErr:
		return updateErrBadge.Render(" " + s + " ")
	case u.step == updIdle:
		return updateBadge.Render(" ⬆ " + s + " ")
	}
	return updateBadge.Render(" " + s + " ")
}

// Status is the updater's part of the status line, and whether it is an error; "" for nothing.
func (u *updater) Status() (string, bool) {
	switch {
	case u.step == updConfirm:
		return fmt.Sprintf("Update %s %s → %s? y/n", u.app, u.current, u.latest), false
	case u.step == updWorking:
		return fmt.Sprintf("updating to %s…", u.latest), false
	case u.step == updRestart:
		return fmt.Sprintf("updated to %s · restart now? y/n", u.current), false
	case u.step == updDone:
		return fmt.Sprintf("updated to %s · restart to use it", u.current), false
	case u.err != "":
		return "update failed: " + u.err, true
	case u.latest != "":
		return u.latest + " available · u update", false
	}
	return "", false
}
