package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Sizes are pane sizes changed with the mouse or the keyboard, kept in a file next to the config
// (dashboard.json → dashboard.sizes.json). They are fractions, so they scale with the terminal:
// each row's share of the dashboard height, and each pane's share of its row.
type Sizes struct {
	Rows []SizeRow `json:"rows"`
}

type SizeRow struct {
	Height float64   `json:"height"`
	Panes  []float64 `json:"panes"`
}

const (
	minPaneW = 10 // the narrowest a pane can be dragged
	minRowH  = 3  // the lowest a row can be dragged: a border, one line, a border
)

func sizesPath(cfgPath string) string {
	return strings.TrimSuffix(cfgPath, filepath.Ext(cfgPath)) + ".sizes.json"
}

// loadSizes reads the sizes file. A missing or broken file means no sizes; a broken one is
// replaced on the next resize.
func loadSizes(path string) *Sizes {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s Sizes
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// save writes the sizes through a temp file and a rename, so a reader never sees half a file.
func (s *Sizes) save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeSizes(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// heights gives the saved row heights, if they still fit the config: one per row, all above 0.
func (s *Sizes) heights(rows []Row) []float64 {
	if s == nil || len(s.Rows) != len(rows) {
		return nil
	}
	hs := make([]float64, len(rows))
	for i, r := range s.Rows {
		if r.Height <= 0 {
			return nil
		}
		hs[i] = r.Height
	}
	return hs
}

// widths gives row i's saved pane widths, if they still fit: one per pane, all above 0.
func (s *Sizes) widths(i int, panes []PaneConfig) []float64 {
	if s == nil || i >= len(s.Rows) || len(s.Rows[i].Panes) != len(panes) {
		return nil
	}
	for _, f := range s.Rows[i].Panes {
		if f <= 0 {
			return nil
		}
	}
	return s.Rows[i].Panes
}

// split divides total cells by fractions, with cumulative rounding down (as spans always have)
// so the parts add up to total. The small epsilon keeps a saved fraction like 33/100 from coming
// back as 32.999… and losing a cell.
func split(total int, fracs []float64) []int {
	var sum float64
	for _, f := range fracs {
		sum += f
	}
	out := make([]int, len(fracs))
	acc, prev := 0.0, 0
	for i, f := range fracs {
		acc += f
		next := int(math.Floor(float64(total)*acc/sum + 1e-9))
		out[i] = next - prev
		prev = next
	}
	return out
}

// sizesOf records a layout as fractions: what a drag or a resize key saves.
func sizesOf(rows []Row, rects []rect, width, total int) *Sizes {
	s := &Sizes{}
	k := 0
	for _, r := range rows {
		row := SizeRow{Height: float64(rects[k].h) / float64(total)}
		for range r.Panes {
			row.Panes = append(row.Panes, float64(rects[k].w)/float64(width))
			k++
		}
		s.Rows = append(s.Rows, row)
	}
	return s
}
