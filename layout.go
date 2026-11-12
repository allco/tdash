package main

// rect is where a pane goes on the dashboard canvas, in cells. y counts from the top of the
// whole dashboard, not the screen, since the dashboard can be taller than the screen.
type rect struct{ x, y, w, h int }

// layoutGrid places every pane, in row-major order, and returns the total height. Widths split
// each row by span; heights are whole multiples of the base unit. With unit "auto" (0) the unit
// is picked so the rows fill avail lines, but never below minUnit; any leftover lines from the
// rounding go to the last row. Saved sizes, where they still fit the rows, then share the same
// total height and each row's width out by their fractions instead.
func layoutGrid(rows []Row, width, avail int, unit Unit, minUnit int, sizes *Sizes) ([]rect, int) {
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
	heights := make([]int, len(rows))
	sum := 0
	for i, r := range rows {
		heights[i] = r.Height * u
		if i == len(rows)-1 {
			heights[i] += extra
		}
		sum += heights[i]
	}
	if hs := sizes.heights(rows); hs != nil {
		heights = split(sum, hs)
	}

	var rects []rect
	y := 0
	for i, r := range rows {
		fracs := sizes.widths(i, r.Panes)
		if fracs == nil {
			for _, p := range r.Panes {
				fracs = append(fracs, float64(p.Span))
			}
		}
		x := 0
		for _, w := range split(width, fracs) {
			rects = append(rects, rect{x: x, y: y, w: w, h: heights[i]})
			x += w
		}
		y += heights[i]
	}
	return rects, y
}
