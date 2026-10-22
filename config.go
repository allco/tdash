package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tailscale/hujson"
)

// Config is the whole dashboard, as read from the JSON (or JSONC) config file.
type Config struct {
	Title     string            `json:"title"`
	Refresh   string            `json:"refresh"`
	Theme     Theme             `json:"theme"`
	Suppliers map[string]Source `json:"suppliers"`
	Layout    Layout            `json:"layout"`

	dir string // directory of the config file; relative paths resolve against it
}

type Theme struct {
	Accent string `json:"accent"`
	Border string `json:"border"`
}

type Layout struct {
	Unit    Unit  `json:"unit"`
	MinUnit int   `json:"minUnit"`
	Rows    []Row `json:"rows"`
}

// Unit is the base row height in lines. 0 means "auto": fit the rows to the terminal.
type Unit int

func (u *Unit) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		if s == "auto" {
			*u = 0
			return nil
		}
		return fmt.Errorf(`want "auto" or a number of lines, got %q`, s)
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil || n < 1 {
		return fmt.Errorf(`want "auto" or a number of lines ≥ 1, got %s`, b)
	}
	*u = Unit(n)
	return nil
}

type Row struct {
	Height int          `json:"height"`
	Panes  []PaneConfig `json:"panes"`
}

type PaneConfig struct {
	Type    string  `json:"type"`
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Span    int     `json:"span"`
	Data    DataRef `json:"data"`
	Refresh string  `json:"refresh"`
	Action  *Action `json:"action"`

	// table
	Columns []Column `json:"columns"`
	// chart
	Chart string   `json:"chart"`
	Bins  int      `json:"bins"`
	Min   *float64 `json:"min"`
	Max   *float64 `json:"max"`
}

type Column struct {
	Key    string            `json:"key"`
	Title  string            `json:"title"`
	Align  string            `json:"align"`
	Colors map[string]string `json:"colors"`
}

// Action is what enter does on a selected item. Exactly one of Open, Run and Copy is set.
type Action struct {
	Open string `json:"open"`
	Run  string `json:"run"`
	Copy string `json:"copy"`
	TTY  bool   `json:"tty"`
}

// Source is a data supplier: a command or a URL. Named suppliers live under "suppliers"; a pane
// can also have one inline in its "data".
type Source struct {
	Command *Command          `json:"command"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	Format  string            `json:"format"`
	Refresh string            `json:"refresh"`
	Timeout string            `json:"timeout"`
}

// DataRef is a pane's "data": exactly one of inline, file, command, url or supplier.
type DataRef struct {
	Source
	Inline   json.RawMessage `json:"inline"`
	File     string          `json:"file"`
	Supplier string          `json:"supplier"`
	Select   string          `json:"select"`
}

func (d DataRef) kinds() []string {
	var k []string
	if len(d.Inline) > 0 {
		k = append(k, "inline")
	}
	if d.File != "" {
		k = append(k, "file")
	}
	if d.Command != nil {
		k = append(k, "command")
	}
	if d.URL != "" {
		k = append(k, "url")
	}
	if d.Supplier != "" {
		k = append(k, "supplier")
	}
	return k
}

// Command is either a shell string (run with $SHELL -c) or an argv array (run directly).
type Command struct {
	Shell string
	Argv  []string
}

func (c *Command) UnmarshalJSON(b []byte) error {
	if json.Unmarshal(b, &c.Shell) == nil {
		if strings.TrimSpace(c.Shell) == "" {
			return errors.New("empty command")
		}
		return nil
	}
	if err := json.Unmarshal(b, &c.Argv); err != nil || len(c.Argv) == 0 {
		return errors.New("want a string or a non-empty array of strings")
	}
	return nil
}

func (c Command) String() string {
	if c.Argv != nil {
		return strings.Join(c.Argv, " ")
	}
	return c.Shell
}

const (
	minRefresh     = 500 * time.Millisecond
	defaultTimeout = 10 * time.Second
	defaultMinUnit = 4
)

var (
	paneTypes  = map[string]bool{"table": true, "markdown": true, "chart": true}
	chartTypes = map[string]bool{"spline": true, "histogram": true}
	formats    = map[string]bool{"": true, "json": true, "text": true, "lines": true, "auto": true}
)

// parseRefresh reads a refresh interval: 0 when unset, -1 for "off".
func parseRefresh(s string) (time.Duration, error) {
	switch s {
	case "":
		return 0, nil
	case "off":
		return -1, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf(`want a duration like "5s" or "off", got %q`, s)
	}
	if d < minRefresh {
		return 0, fmt.Errorf("%s is below the minimum of %s", d, minRefresh)
	}
	return d, nil
}

func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return defaultTimeout, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf(`want a duration like "5s", got %q`, s)
	}
	return d, nil
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	std, err := hujson.Standardize(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(std))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cfg.dir = filepath.Dir(abs)
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// validate checks the config and fills in defaults. Errors name the path of the bad field.
func (c *Config) validate() error {
	if _, err := parseRefresh(c.Refresh); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	if c.Layout.MinUnit == 0 {
		c.Layout.MinUnit = defaultMinUnit
	}
	for name, s := range c.Suppliers {
		at := fmt.Sprintf("suppliers.%s", name)
		if (s.Command == nil) == (s.URL == "") {
			return fmt.Errorf("%s: want exactly one of command or url", at)
		}
		if err := validateSource(s, at); err != nil {
			return err
		}
	}
	if len(c.Layout.Rows) == 0 {
		return errors.New("layout.rows: want at least one row")
	}
	for r := range c.Layout.Rows {
		row := &c.Layout.Rows[r]
		at := fmt.Sprintf("layout.rows[%d]", r)
		if row.Height == 0 {
			row.Height = 1
		}
		if row.Height < 0 {
			return fmt.Errorf("%s.height: want a number of units ≥ 1", at)
		}
		if len(row.Panes) == 0 {
			return fmt.Errorf("%s.panes: want at least one pane", at)
		}
		for p := range row.Panes {
			if err := c.validatePane(&row.Panes[p], fmt.Sprintf("%s.panes[%d]", at, p)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Config) validatePane(p *PaneConfig, at string) error {
	if !paneTypes[p.Type] {
		return fmt.Errorf("%s.type: unknown value %q (want table, markdown or chart)", at, p.Type)
	}
	if p.Type == "chart" && !chartTypes[p.Chart] {
		return fmt.Errorf("%s.chart: unknown value %q (want spline or histogram)", at, p.Chart)
	}
	if p.Span == 0 {
		p.Span = 1
	}
	if p.Span < 0 {
		return fmt.Errorf("%s.span: want ≥ 1", at)
	}
	if _, err := parseRefresh(p.Refresh); err != nil {
		return fmt.Errorf("%s.refresh: %w", at, err)
	}
	if p.Action != nil {
		n := 0
		for _, s := range []string{p.Action.Open, p.Action.Run, p.Action.Copy} {
			if s != "" {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("%s.action: want exactly one of open, run or copy", at)
		}
	}

	d := p.Data
	kinds := d.kinds()
	if len(kinds) > 1 {
		return fmt.Errorf("%s.data: want one source, got %s", at, strings.Join(kinds, " and "))
	}
	if d.Supplier != "" {
		if _, ok := c.Suppliers[d.Supplier]; !ok {
			return fmt.Errorf("%s.data.supplier: no supplier named %q", at, d.Supplier)
		}
	}
	if d.Select != "" {
		if _, err := parseSelect(d.Select); err != nil {
			return fmt.Errorf("%s.data.select: %w", at, err)
		}
	}
	return validateSource(d.Source, at+".data")
}

func validateSource(s Source, at string) error {
	if !formats[s.Format] {
		return fmt.Errorf("%s.format: unknown value %q (want json, text, lines or auto)", at, s.Format)
	}
	if _, err := parseRefresh(s.Refresh); err != nil {
		return fmt.Errorf("%s.refresh: %w", at, err)
	}
	if _, err := parseTimeout(s.Timeout); err != nil {
		return fmt.Errorf("%s.timeout: %w", at, err)
	}
	if s.URL != "" && !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
		return fmt.Errorf("%s.url: want an http:// or https:// URL", at)
	}
	return nil
}

// panes lists every pane in row-major order, which is also the Tab order.
func (c *Config) panes() []PaneConfig {
	var out []PaneConfig
	for _, r := range c.Layout.Rows {
		out = append(out, r.Panes...)
	}
	return out
}
