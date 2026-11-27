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
	// pre and post are the brackets around a placeholder that stands alone in them, as in
	// "Done ({{rows}})". They are left out with the placeholder when it has no value.
	pre, post string
}

// parseTitle reads a title with {{rows}} and {{$.path}} placeholders.
func parseTitle(s string) ([]titlePart, error) {
	var parts []titlePart
	rest := s
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			if rest != "" {
				parts = append(parts, titlePart{text: rest})
			}
			return parts, nil
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

		lit, after := rest[:start], rest[end+2:]
		if open := strings.TrimRight(lit, " "); open != "" {
			if c := closer(open[len(open)-1]); c != 0 {
				if close := strings.TrimLeft(after, " "); close != "" && close[0] == c {
					from := len(strings.TrimRight(open[:len(open)-1], " "))
					to := len(after) - len(close) + 1
					p.pre, p.post = lit[from:], after[:to]
					lit, after = lit[:from], after[to:]
				}
			}
		}
		if lit != "" {
			parts = append(parts, titlePart{text: lit})
		}
		parts = append(parts, p)
		rest = after
	}
}

func closer(open byte) byte {
	switch open {
	case '(':
		return ')'
	case '[':
		return ']'
	}
	return 0
}

// renderTitle fills the placeholders. value reports a placeholder's value, or false when it has
// none yet; such a placeholder is left out, with its brackets.
func renderTitle(parts []titlePart, value func(titlePart) (string, bool)) string {
	var b strings.Builder
	for _, p := range parts {
		if !p.ph {
			b.WriteString(p.text)
			continue
		}
		if v, ok := value(p); ok && v != "" {
			b.WriteString(p.pre + v + p.post)
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
