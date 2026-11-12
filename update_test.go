package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.2.0", "v1.3.0", true},
		{"v1.2.0", "v1.2.1", true},
		{"v1.2.0", "v2.0.0", true},
		{"v1.10.0", "v1.9.0", false}, // numbers, not text
		{"v1.2.0", "v1.2.0", false},
		{"v1.3.0", "v1.2.0", false},
		{"v1.2.0-3-gabc1234", "v1.2.0", false}, // built from source past the tag
		{"v1.2.0-3-gabc1234-dirty", "v1.3.0", true},
		{"v1.2.0-dirty", "v1.2.1", true},
		{"v1.3.0-rc.1", "v1.3.0", true},
		{"v1.2.0", "v1.3.0-rc.1", false}, // pre-releases are never offered
		{"dev", "v1.3.0", false},
		{"abc1234", "v1.3.0", false},
		{"v1.2.0", "", false},
	}
	for _, c := range cases {
		if got := isNewer(c.current, c.latest); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestNewestStable(t *testing.T) {
	if got := newestStable([]string{"v1.2.0", "v1.10.0-rc.1", "v1.9.0", "junk", "v1.3.0"}); got != "v1.9.0" {
		t.Errorf("got %q, want v1.9.0", got)
	}
	if got := newestStable([]string{"v2.0.0-rc.1"}); got != "" {
		t.Errorf("only a pre-release: got %q, want none", got)
	}
}

// fakeRelease serves release tag with a binary for this platform, and points the binary to
// replace at a temp file. It returns that file. bad makes checksums.txt disagree with the binary.
func fakeRelease(t *testing.T, tag string, bad bool) string {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	exe := filepath.Join(t.TempDir(), "tdash")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := []byte("new " + tag)
	sum := sha256.Sum256(bin)
	if bad {
		sum = sha256.Sum256([]byte("something else"))
	}
	sums := hex.EncodeToString(sum[:]) + "  " + assetName("tdash", tag) + "\n" +
		strings.Repeat("0", 64) + "  tdash_" + tag + "_plan9_mips\n"
	oldLatest, oldFetch, oldExe := latestFn, fetchFn, exeFn
	t.Cleanup(func() { latestFn, fetchFn, exeFn = oldLatest, oldFetch, oldExe })
	latestFn = func(context.Context, string) (string, error) { return tag, nil }
	fetchFn = func(_ context.Context, app, gotTag, file string) ([]byte, error) {
		switch {
		case gotTag != tag:
			return nil, errors.New("no such release")
		case file == "checksums.txt":
			return []byte(sums), nil
		case file == assetName(app, tag):
			return bin, nil
		}
		return nil, errors.New("404")
	}
	exeFn = func() (string, error) { return exe, nil }
	return exe
}

func TestInstallReplacesBinary(t *testing.T) {
	exe := fakeRelease(t, "v1.3.0", false)
	if _, err := install("tdash", "v1.3.0"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new v1.3.0" {
		t.Errorf("binary is %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, want 0755", fi.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".*")); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	exe := fakeRelease(t, "v1.3.0", true)
	if _, err := install("tdash", "v1.3.0"); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Errorf("the binary changed to %q", b)
	}
}

func TestUpdateCLI(t *testing.T) {
	exe := fakeRelease(t, "v1.3.0", false)
	var out strings.Builder
	if err := updateCLI("tdash", "v1.2.0", strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" || !strings.Contains(out.String(), "Not updated") {
		t.Errorf("after n: binary %q, output %q", b, out.String())
	}
	out.Reset()
	if err := updateCLI("tdash", "v1.2.0", strings.NewReader("y\n"), &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new v1.3.0" || !strings.Contains(out.String(), "Updated") {
		t.Errorf("after y: binary %q, output %q", b, out.String())
	}
	out.Reset()
	if err := updateCLI("tdash", "v1.3.0", nil, &out); err != nil || !strings.Contains(out.String(), "up to date") {
		t.Errorf("up to date: err %v, output %q", err, out.String())
	}
	if err := updateCLI("tdash", "dev", nil, &out); err == nil {
		t.Error("a dev build updated")
	}
}

func TestUpdaterInApp(t *testing.T) {
	exe := fakeRelease(t, "v1.3.0", false)
	u := newUpdater("tdash", "v1.2.0", false)
	run := func(cmd tea.Cmd) {
		if cmd == nil {
			t.Fatal("no command")
		}
		if ok, _ := u.Update(cmd()); !ok {
			t.Fatal("the updater didn't take its own message")
		}
	}
	run(u.Init())
	if s, _ := u.Status(); s != "v1.3.0 available · u update" {
		t.Fatalf("status %q", s)
	}
	if v := u.StatusView(); !strings.Contains(v, "⬆ v1.3.0 available · u update") {
		t.Errorf("badge %q", v)
	}
	// The next run shows it at once, from the cache, before checking.
	if u2 := newUpdater("tdash", "v1.2.0", false); u2.latest != "v1.3.0" {
		t.Errorf("cached latest %q", u2.latest)
	}

	if ok, _ := u.Key("j"); ok {
		t.Error("took a key that isn't its own")
	}
	u.Key("u")
	if s, _ := u.Status(); !u.Asking() || s != "Update tdash v1.2.0 → v1.3.0? y/n" {
		t.Fatalf("asking %v, status %q", u.Asking(), s)
	}
	if ok, _ := u.Key("j"); !ok {
		t.Error("a key went past the open question")
	}
	u.Key("n")
	if u.Asking() || u.latest != "v1.3.0" {
		t.Fatal("n didn't close the question, or forgot the release")
	}
	u.Key("u")
	_, cmd := u.Key("y")
	run(cmd)
	if b, _ := os.ReadFile(exe); string(b) != "new v1.3.0" {
		t.Fatalf("binary %q", b)
	}
	if s, _ := u.Status(); s != "updated to v1.3.0 · restart now? y/n" {
		t.Fatalf("status %q", s)
	}
	if _, cmd := u.Key("y"); cmd == nil || u.Restart != exe {
		t.Errorf("restart: cmd %v, Restart %q", cmd, u.Restart)
	}

	// Builds that aren't from a tag, and --no-update-check, never check.
	if newUpdater("tdash", "dev", false).Init() != nil || newUpdater("tdash", "v1.2.0", true).Init() != nil {
		t.Error("a dev build or --no-update-check checks")
	}
}

func TestUpdateInStatusLine(t *testing.T) {
	fakeRelease(t, "v1.3.0", false)
	m, _ := resizeModel(t, "")
	m.upd = newUpdater("tdash", "v1.2.0", false)
	m.Update(m.upd.checkCmd()())
	if s := m.statusLine(); !strings.Contains(s, "v1.3.0 available · u update") {
		t.Errorf("status line %q", s)
	}
	m.key("u")
	m.key("tab") // the question takes every key
	if m.focus != -1 || !strings.Contains(m.statusLine(), "Update tdash v1.2.0 → v1.3.0? y/n") {
		t.Errorf("focus %d, status line %q", m.focus, m.statusLine())
	}
	m.key("esc")
	if m.upd.Asking() {
		t.Error("esc didn't close the question")
	}
}
