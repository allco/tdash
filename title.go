package main

import (
	"errors"
	"fmt"
	"strings"
)

// titlePart is a piece of a pane title: literal text, or a placeholder filled from the pane's
// data at render time.
type titlePart struct {
	text string // the literal text, when this is not a placeholder

	ph   bool
	rows bool         // {{rows}}
	path []selectStep // {{$.path}}, on the source's value before the pane's select
	// group numbers the bracket pair, "(…)" or "[…]", that the part is in, 0 for none. A group
	// whose placeholders all have no value is left out, with the spaces before it.
	group int
}

// parseTitle reads a title with {{rows}} and {{$.path}} placeholders.
func parseTitle(s string) ([]titlePart, error) {
	var toks []titlePart
	rest := s
	for rest != "" {
		start := strings.Index(rest, "{{")
		if start < 0 {
			toks = append(toks, titlePart{text: rest})
			break
		}
		end := strings.Index(rest[start:], "}}")
		if end < 0 {
			return nil, fmt.Errorf(`unclosed "{{" in %q`, s)
		}
		end += start
		inner := strings.TrimSpace(rest[start+2 : end])
		p := titlePart{ph: true}
		switch {
		case inner == "rows":
			p.rows = true
		case strings.HasPrefix(inner, "$"):
			steps, err := parseSelect(inner)
			if err != nil {
				return nil, fmt.Errorf("{{%s}}: %w", inner, err)
			}
			p.path = steps
		case inner == "":
			return nil, errors.New(`empty placeholder "{{}}" (want {{rows}} or {{$.path}})`)
		default:
			return nil, fmt.Errorf("unknown placeholder {{%s}} (want {{rows}} or {{$.path}})", inner)
		}
		if start > 0 {
			toks = append(toks, titlePart{text: rest[:start]})
		}
		toks = append(toks, p)
		rest = rest[end+2:]
	}
	return groupBrackets(toks), nil
}

// groupBrackets splits the literal parts at top-level brackets and numbers each bracket pair.
func groupBrackets(toks []titlePart) []titlePart {
	var parts []titlePart
	emit := func(text string, group int) {
		if text != "" {
			parts = append(parts, titlePart{text: text, group: group})
		}
	}
	group, groups, depth := 0, 0, 0
	var open, want byte
	for _, t := range toks {
		if t.ph {
			t.group = group
			parts = append(parts, t)
			continue
		}
		s := t.text
		for s != "" {
			if group == 0 {
				i := strings.IndexAny(s, "([")
				if i < 0 {
					emit(s, 0)
					break
				}
				j := len(strings.TrimRight(s[:i], " "))
				emit(s[:j], 0)
				groups++
				group, open, depth = groups, s[i], 1
				want = map[byte]byte{'(': ')', '[': ']'}[open]
				emit(s[j:i+1], group)
				s = s[i+1:]
				continue
			}
			k := -1
			for x := 0; x < len(s) && k < 0; x++ {
				switch s[x] {
				case open:
					depth++
				case want:
					if depth--; depth == 0 {
						k = x
					}
				}
			}
			if k < 0 {
				emit(s, group)
				break
			}
			emit(s[:k+1], group)
			s = s[k+1:]
			group = 0
		}
	}
	if group != 0 {
		// An unclosed bracket is plain text.
		for i := range parts {
			if parts[i].group == group {
				parts[i].group = 0
			}
		}
	}
	return parts
}

// renderTitle fills the placeholders. value reports a placeholder's value, or false when it has
// none yet; such a placeholder is left out, and so is a bracket group with no value at all.
func renderTitle(parts []titlePart, value func(titlePart) (string, bool)) string {
	vals := make([]string, len(parts))
	shown := map[int]bool{0: true}
	hasPH := map[int]bool{}
	for i, p := range parts {
		if !p.ph {
			continue
		}
		hasPH[p.group] = true
		if v, ok := value(p); ok && v != "" {
			vals[i] = v
			shown[p.group] = true
		}
	}
	var b strings.Builder
	for i, p := range parts {
		if hasPH[p.group] && !shown[p.group] {
			continue
		}
		if p.ph {
			b.WriteString(vals[i])
		} else {
			b.WriteString(p.text)
		}
	}
	return strings.TrimSpace(b.String())
}

// title is the pane's title with its placeholders filled from its data.
func (m *model) title(ps *paneState) string {
	if ps.title == nil {
		return ps.cfg.Title
	}
	src := m.sources[ps.srcKey]
	return renderTitle(ps.title, func(p titlePart) (string, bool) {
		if src == nil || !src.has {
			return "", false
		}
		if p.rows {
			if !ps.hasData {
				return "", false
			}
			n := ps.pane.Count()
			return fmt.Sprint(n), n >= 0
		}
		v, err := applySelect(src.value, p.path)
		if err != nil {
			return "", false
		}
		return formatValue(v), true
	})
}
