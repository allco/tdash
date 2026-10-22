package main

// rect is where a pane goes on the dashboard canvas, in cells. y counts from the top of the
// whole dashboard, not the screen, since the dashboard can be taller than the screen.
type rect struct{ x, y, w, h int }

// layoutGrid places every pane, in row-major order, and returns the total height. Widths split
// each row by span; heights are whole multiples of the base unit. With unit "auto" (0) the unit
// is picked so the rows fill avail lines, but never below minUnit; any leftover lines from the
// rounding go to the last row.
func layoutGrid(rows []Row, width, avail int, unit Unit, minUnit int) ([]rect, int) {
	total := 0
	for _, r := range rows {
		total += r.Height
	}
	u := int(unit)
	extra := 0
	if u == 0 {
		u = max(minUnit, avail/max(total, 1))
		extra = max(0, avail-u*total)
	}

	var rects []rect
	y := 0
	for i, r := range rows {
		h := r.Height * u
		if i == len(rows)-1 {
			h += extra
		}
		spans := 0
		for _, p := range r.Panes {
			spans += p.Span
		}
		// Cumulative rounding, so the panes always add up to the full width.
		cum := 0
		for _, p := range r.Panes {
			x := width * cum / spans
			cum += p.Span
			rects = append(rects, rect{x: x, y: y, w: width*cum/spans - x, h: h})
		}
		y += h
	}
	return rects, y
}
