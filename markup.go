package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// attrs is the styling of a run of text.
type attrs struct {
	fg, bg                                  string
	bold, italic, underline, faint, reverse bool
}

func (a attrs) style() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(a.bold).Italic(a.italic).Underline(a.underline).Faint(a.faint).Reverse(a.reverse)
	if a.fg != "" {
		s = s.Foreground(lipgloss.Color(a.fg))
	}
	if a.bg != "" {
		s = s.Background(lipgloss.Color(a.bg))
	}
	return s
}

// seg is a run of text with one style.
type seg struct {
	text string
	a    attrs
}

var colorNames = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
	"gray": "8", "grey": "8", "brightblack": "8", "brightred": "9", "brightgreen": "10", "brightyellow": "11",
	"brightblue": "12", "brightmagenta": "13", "brightcyan": "14", "brightwhite": "15",
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// parseColor reads a color name, a 256-color index or #rrggbb, as a lipgloss color string.
func parseColor(s string) (string, bool) {
	s = strings.ToLower(s)
	if c, ok := colorNames[s]; ok {
		return c, true
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return s, true
	}
	if hexColor.MatchString(s) {
		return s, true
	}
	return "", false
}

// colorOr is c as a lipgloss color, or def when c is empty or not a color.
func colorOr(c, def string) string {
	if v, ok := parseColor(c); ok {
		return v
	}
	return def
}

// parseTag reads the inside of a color tag such as "red on yellow" or "bold #ff8800", applied on
// top of base. ok is false when it isn't a tag, so the braces are kept as text.
func parseTag(s string, base attrs) (attrs, bool) {
	words := strings.Fields(s)
	if len(words) == 0 {
		return base, false
	}
	a := base
	for i := 0; i < len(words); i++ {
		switch w := strings.ToLower(words[i]); w {
		case "bold":
			a.bold = true
		case "italic":
			a.italic = true
		case "underline":
			a.underline = true
		case "dim":
			a.faint = true
		case "reverse":
			a.reverse = true
		case "on":
			if i+1 >= len(words) {
				return base, false
			}
			c, ok := parseColor(words[i+1])
			if !ok {
				return base, false
			}
			a.bg = c
			i++
		default:
			c, ok := parseColor(w)
			if !ok {
				return base, false
			}
			a.fg = c
		}
	}
	return a, true
}

// inlineSegs parses the inline markup of one line: **bold**, *italic*, `code`, [links](url),
// {color tags}…{/} and \ escapes.
func inlineSegs(s string, base attrs) []seg {
	var segs []seg
	var buf strings.Builder
	stack := []attrs{base}
	bold, italic := false, false
	cur := func() attrs {
		a := stack[len(stack)-1]
		a.bold = a.bold || bold
		a.italic = a.italic || italic
		return a
	}
	flush := func() {
		if buf.Len() > 0 {
			segs = append(segs, seg{buf.String(), cur()})
			buf.Reset()
		}
	}

	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '\\':
			if i+1 < len(rs) && strings.ContainsRune(`\{}*_`+"`"+`[]`, rs[i+1]) {
				buf.WriteRune(rs[i+1])
				i++
				continue
			}
		case '{':
			if j := indexRune(rs, '}', i+1); j > 0 {
				in := string(rs[i+1 : j])
				if in == "/" {
					flush()
					if len(stack) > 1 {
						stack = stack[:len(stack)-1]
					}
					i = j
					continue
				}
				if a, ok := parseTag(in, stack[len(stack)-1]); ok {
					flush()
					stack = append(stack, a)
					i = j
					continue
				}
			}
		case '*':
			flush()
			if i+1 < len(rs) && rs[i+1] == '*' {
				bold = !bold
				i++
			} else {
				italic = !italic
			}
			continue
		case '`':
			if j := indexRune(rs, '`', i+1); j > 0 {
				flush()
				a := cur()
				a.fg = "180"
				segs = append(segs, seg{string(rs[i+1 : j]), a})
				i = j
				continue
			}
		case '[':
			if j := indexRune(rs, ']', i+1); j > 0 && j+1 < len(rs) && rs[j+1] == '(' {
				if k := indexRune(rs, ')', j+2); k > 0 {
					flush()
					a := cur()
					a.fg = "39"
					a.underline = true
					segs = append(segs, seg{string(rs[i+1 : j]), a})
					i = k
					continue
				}
			}
		}
		buf.WriteRune(c)
	}
	flush()
	return segs
}

func indexRune(rs []rune, r rune, from int) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == r {
			return i
		}
	}
	return -1
}

// wrapSegs word-wraps styled text to width. The first line starts with first, the following
// lines with cont; both are plain text of the same width.
func wrapSegs(segs []seg, width int, first, cont seg) []string {
	type piece struct {
		seg
		w     int
		space bool
	}
	var pieces []piece
	for _, s := range segs {
		start := 0
		rs := []rune(s.text)
		for i := 1; i <= len(rs); i++ {
			if i == len(rs) || (rs[i] == ' ') != (rs[start] == ' ') {
				t := string(rs[start:i])
				pieces = append(pieces, piece{seg{t, s.a}, ansi.StringWidth(t), rs[start] == ' '})
				start = i
			}
		}
	}

	var lines []string
	var line strings.Builder
	prefix := first
	lineW := ansi.StringWidth(prefix.text)
	avail := width
	line.WriteString(prefix.a.style().Render(prefix.text))
	var pending []piece // spaces not yet written, dropped at a line break
	newLine := func() {
		lines = append(lines, line.String())
		line.Reset()
		line.WriteString(cont.a.style().Render(cont.text))
		lineW = ansi.StringWidth(cont.text)
		pending = nil
	}
	empty := true
	for _, p := range pieces {
		if p.space {
			if !empty {
				pending = append(pending, p)
			}
			continue
		}
		spaceW := 0
		for _, s := range pending {
			spaceW += s.w
		}
		if !empty && lineW+spaceW+p.w > avail {
			newLine()
			spaceW = 0
		}
		for _, s := range pending {
			line.WriteString(s.a.style().Render(s.text))
			lineW += s.w
		}
		pending = nil
		// A word longer than the line is split wherever it has to be.
		text := p.text
		for lineW+ansi.StringWidth(text) > avail && avail-lineW > 0 {
			head := ansi.Truncate(text, avail-lineW, "")
			if head == "" {
				break
			}
			line.WriteString(p.a.style().Render(head))
			text = strings.TrimPrefix(text, head)
			newLine()
		}
		line.WriteString(p.a.style().Render(text))
		lineW += ansi.StringWidth(text)
		empty = false
	}
	lines = append(lines, line.String())
	return lines
}

var (
	hrLine      = regexp.MustCompile(`^\s*(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	orderedItem = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
	bulletItem  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
)

// joinContinuations joins indented lines onto the list item above them, as Markdown does with a
// list item that is wrapped in the source.
func joinContinuations(lines []string) []string {
	var out []string
	inItem, inCode := false, false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			inCode = !inCode
			inItem = false
			out = append(out, l)
			continue
		}
		item := !inCode && (bulletItem.MatchString(l) || orderedItem.MatchString(l))
		if !inCode && !item && inItem && t != "" && strings.HasPrefix(l, "  ") {
			out[len(out)-1] += " " + t
			continue
		}
		inItem = item
		out = append(out, l)
	}
	return out
}

// renderMarkdown renders Markdown to styled lines of at most width cells. Each source line is its
// own line on screen: on a dashboard, line breaks are usually meant.
func renderMarkdown(src string, width int, accent string) []string {
	var out []string
	inCode := false
	codeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("180"))
	dim := attrs{fg: "241"}

	for _, l := range joinContinuations(strings.Split(strings.ReplaceAll(src, "\t", "    "), "\n")) {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			for _, w := range strings.Split(ansi.Hardwrap(l, width, true), "\n") {
				out = append(out, codeStyle.Render(w))
			}
			continue
		}
		switch {
		case strings.TrimSpace(l) == "":
			out = append(out, "")
		case hrLine.MatchString(l):
			out = append(out, dim.style().Render(strings.Repeat("─", width)))
		case headingLine.MatchString(l):
			m := headingLine.FindStringSubmatch(l)
			a := attrs{bold: true, fg: accent, underline: len(m[1]) == 1}
			out = append(out, wrapSegs(inlineSegs(m[2], a), width, seg{}, seg{})...)
		case bulletItem.MatchString(l):
			m := bulletItem.FindStringSubmatch(l)
			ind := m[1] + "• "
			out = append(out, wrapSegs(inlineSegs(m[2], attrs{}), width,
				seg{text: ind, a: attrs{fg: accent}}, seg{text: strings.Repeat(" ", len([]rune(ind)))})...)
		case orderedItem.MatchString(l):
			m := orderedItem.FindStringSubmatch(l)
			ind := m[1] + m[2] + " "
			out = append(out, wrapSegs(inlineSegs(m[3], attrs{}), width,
				seg{text: ind, a: attrs{fg: accent}}, seg{text: strings.Repeat(" ", len(ind))})...)
		case strings.HasPrefix(strings.TrimSpace(l), ">"):
			t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">"))
			bar := seg{text: "│ ", a: dim}
			out = append(out, wrapSegs(inlineSegs(t, attrs{italic: true}), width, bar, bar)...)
		default:
			out = append(out, wrapSegs(inlineSegs(l, attrs{}), width, seg{}, seg{})...)
		}
	}
	return out
}
