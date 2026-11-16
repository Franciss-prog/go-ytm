package style

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Status is the outcome of a check or line item.
type Status int

// Statuses, ordered by severity.
const (
	StatusInfo Status = iota
	StatusOK
	StatusWarn
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	default:
		return "info"
	}
}

// MarshalJSON encodes the status as a stable lowercase string.
func (s Status) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// Item is one line of a report section.
type Item struct {
	Label  string `json:"label"`
	Status Status `json:"status"`
	Value  string `json:"value,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Section groups items under a heading.
type Section struct {
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

// Status returns the worst status in the section.
func (s Section) Status() Status {
	worst := StatusInfo
	for _, it := range s.Items {
		if it.Status > worst {
			worst = it.Status
		}
	}
	if worst == StatusInfo && len(s.Items) > 0 {
		return StatusOK
	}
	return worst
}

// Report is a titled, sectioned list of checks (used by `ytm doctor`).
type Report struct {
	Title    string    `json:"title"`
	Meta     string    `json:"meta,omitempty"`
	Sections []Section `json:"sections"`
}

// Counts returns the number of failing and warning items.
func (r Report) Counts() (fails, warns int) {
	for _, s := range r.Sections {
		for _, it := range s.Items {
			switch it.Status {
			case StatusFail:
				fails++
			case StatusWarn:
				warns++
			}
		}
	}
	return fails, warns
}

// MarshalJSON adds a computed status to each section.
func (s Section) MarshalJSON() ([]byte, error) {
	type alias Section
	return json.Marshal(struct {
		alias
		Status Status `json:"status"`
	}{alias(s), s.Status()})
}

// Report renders a report. Output ends with a newline.
func (r *Renderer) Report(rep Report) string {
	pad := strings.Repeat(" ", Gutter)
	ind := strings.Repeat(" ", Gutter+Indent)
	var b strings.Builder

	labelW := 0
	valueW := 0
	for _, s := range rep.Sections {
		for _, it := range s.Items {
			labelW = max(labelW, lipgloss.Width(it.Label))
			valueW = max(valueW, lipgloss.Width(it.Value))
		}
	}

	if r.Opts.Mode.Styled {
		title := r.S.Title.Render(rep.Title)
		meta := r.S.Subtitle.Render(rep.Meta)
		b.WriteString(pad + title + "  " + meta + "\n\n")
	} else {
		b.WriteString(pad + rep.Title)
		if rep.Meta != "" {
			b.WriteString(" (" + rep.Meta + ")")
		}
		b.WriteString("\n\n")
	}

	iconW := lipgloss.Width(r.Opts.Icons.OK)
	for _, ic := range []string{r.Opts.Icons.Warn, r.Opts.Icons.Fail, r.Opts.Icons.Info} {
		iconW = max(iconW, lipgloss.Width(ic))
	}

	for i, s := range rep.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(pad + r.S.Section.Render(s.Name) + "\n")
		for _, it := range s.Items {
			icon := r.Icon(it.Status)
			icon += strings.Repeat(" ", iconW-lipgloss.Width(icon))
			line := ind + icon + " " + r.S.Label.Render(padRight(it.Label, labelW))
			if it.Value != "" || it.Detail != "" {
				line += strings.Repeat(" ", ColumnGap) + r.S.Value.Render(padRight(it.Value, valueW))
			}
			if it.Detail != "" {
				line += strings.Repeat(" ", ColumnGap) + r.S.Muted.Render(it.Detail)
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}

	fails, warns := rep.Counts()
	b.WriteString("\n" + pad + r.summary(fails, warns) + "\n")
	return b.String()
}

func (r *Renderer) summary(fails, warns int) string {
	if fails == 0 && warns == 0 {
		return r.S.OK.Render(r.Opts.Icons.OK + " all good")
	}
	parts := []string{}
	if fails > 0 {
		parts = append(parts, r.S.Fail.Render(Plural(fails, "problem")))
	}
	if warns > 0 {
		parts = append(parts, r.S.Warn.Render(Plural(warns, "warning")))
	}
	return strings.Join(parts, r.S.Muted.Render(", "))
}

// Message renders a single status line such as "✓ Saved".
func (r *Renderer) Message(st Status, msg string) string {
	return strings.Repeat(" ", Gutter) + r.Icon(st) + " " + msg + "\n"
}

// Hint renders a muted hint with an inline command highlighted.
func (r *Renderer) Hint(text, cmd string) string {
	return strings.Repeat(" ", Gutter) + r.S.Muted.Render(text) + " " + r.S.Code.Render(cmd) + "\n"
}

// Plural formats "1 thing" / "n things".
func Plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
