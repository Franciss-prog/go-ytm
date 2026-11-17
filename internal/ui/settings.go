package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// settingRow is one line of the Settings popup (spec 08, "Settings popup").
type settingRow struct {
	label   string
	key     string   // config key persisted on save
	choices []string // nil = toggle
	// ptr returns the value inside cfg (a *string or *bool).
	ptr func(cfg *config.Config) any
	// themed rows need the theme re-resolved when they change.
	themed bool
}

var settingRows = []settingRow{
	{
		label: "Theme source", key: "theme.source", choices: []string{"static", "pywal", "matugen", "ansi", "file"}, themed: true,
		ptr: func(c *config.Config) any { return &c.Theme.Source },
	},
	{
		label: "Theme name", key: "theme.name", choices: theme.BuiltinNames(), themed: true,
		ptr: func(c *config.Config) any { return &c.Theme.Name },
	},
	{
		label: "Transparent", key: "theme.transparent", themed: true,
		ptr: func(c *config.Config) any { return &c.Theme.Transparent },
	},
	{
		label: "Selection style", key: "theme.transparent_selection", choices: []string{"bar", "reverse", "underline", "solid"},
		ptr: func(c *config.Config) any { return &c.Theme.TransparentSelection },
	},
	{
		label: "Popup fill", key: "theme.popup_fill",
		ptr: func(c *config.Config) any { return &c.Theme.PopupFill },
	},
	{
		label: "Album art", key: "ui.album_art_mode", choices: []string{"blocks", "off"},
		ptr: func(c *config.Config) any { return &c.UI.AlbumArtMode },
	},
	{
		label: "Enforce contrast", key: "theme.enforce_contrast", themed: true,
		ptr: func(c *config.Config) any { return &c.Theme.EnforceContrast },
	},
	{
		label: "Live reload", key: "theme.watch", themed: true,
		ptr: func(c *config.Config) any { return &c.Theme.Watch },
	},
}

// settings is the open Settings popup: a cursor and the values at open
// (esc reverts to them).
type settings struct {
	cursor int
	orig   config.Config
}

// settingsSavedMsg reports the outcome of persisting Settings.
type settingsSavedMsg struct {
	n   int
	err error
}

func (m *Model) openSettings() {
	m.settings = &settings{orig: m.o.Config}
}

// settingsKey handles a key while Settings is open.
func (m *Model) settingsKey(msg tea.KeyPressMsg) tea.Cmd {
	s := m.settings
	row := settingRows[s.cursor]
	switch msg.String() {
	case "up", "k", "shift+tab":
		s.cursor = (s.cursor + len(settingRows) - 1) % len(settingRows)
	case "down", "j", "tab":
		s.cursor = (s.cursor + 1) % len(settingRows)
	case "left", "h":
		m.changeSetting(row, -1)
	case "right", "l", "space":
		m.changeSetting(row, +1)
	case "enter":
		changes := settingsChanges(&s.orig, &m.o.Config)
		m.settings = nil
		if len(changes) == 0 {
			return nil
		}
		return m.persist(changes)
	case "esc", "q", ",":
		themed := slices.ContainsFunc(settingRows, func(r settingRow) bool {
			return r.themed && settingValue(r, &s.orig) != settingValue(r, &m.o.Config)
		})
		m.o.Config = s.orig
		m.settings = nil
		m.applySettings(themed)
	}
	return nil
}

// changeSetting steps a choice by dir or flips a toggle, then applies it
// live (preview).
func (m *Model) changeSetting(r settingRow, dir int) {
	switch p := r.ptr(&m.o.Config).(type) {
	case *bool:
		*p = !*p
	case *string:
		i := slices.Index(r.choices, *p)
		*p = r.choices[(i+dir+len(r.choices))%len(r.choices)]
	}
	m.applySettings(r.themed)
}

// applySettings restyles for the current config and, when themed, asks
// the theme service to re-resolve (off the UI goroutine).
func (m *Model) applySettings(themed bool) {
	m.restyle()
	if themed && m.o.Themes != nil {
		m.o.Themes.Set(m.o.Config.Theme)
	}
}

// persist writes changes with config.Patch in a Cmd.
func (m *Model) persist(changes []config.Change) tea.Cmd {
	path, patch := m.o.ConfigPath, m.o.PatchConfig
	if path == "" || patch == nil {
		return m.toasts.push(Toast{Severity: SevWarn, Text: "Settings applied for this session (no config file)"})
	}
	return func() tea.Msg {
		return settingsSavedMsg{n: len(changes), err: patch(path, changes)}
	}
}

func settingValue(r settingRow, c *config.Config) string {
	switch p := r.ptr(c).(type) {
	case *bool:
		return fmt.Sprint(*p)
	case *string:
		return *p
	}
	return ""
}

// settingsChanges lists the rows whose value differs between a and b, as
// config changes holding b's values.
func settingsChanges(a, b *config.Config) []config.Change {
	var out []config.Change
	for _, r := range settingRows {
		if settingValue(r, a) == settingValue(r, b) {
			continue
		}
		switch p := r.ptr(b).(type) {
		case *bool:
			out = append(out, config.Change{Key: r.key, Value: *p})
		case *string:
			out = append(out, config.Change{Key: r.key, Value: *p})
		}
	}
	return out
}

// settingsBox renders the popup lines (without placement).
func (m *Model) settingsBox(st *style.UI) []string {
	s := m.settings
	const labelW, valueW = 18, 28
	ctoggle := ""
	if b := m.o.Keymap.Bindings("toggle_transparency"); len(b) > 0 {
		ctoggle = b[0]
	}
	lines := []string{st.PopupTitle.Render("Settings"), ""}
	for i, r := range settingRows {
		var v string
		switch p := r.ptr(&m.o.Config).(type) {
		case *bool:
			v = "[ ]"
			if *p {
				v = "[x]"
			}
		case *string:
			v = "‹ " + *p + " ›"
		}
		note := ""
		switch {
		case r.key == "theme.name" && m.o.Config.Theme.Source != "static":
			note = st.Muted.Render("  (static only)")
		case r.key == "theme.transparent" && ctoggle != "":
			note = st.Muted.Render("  " + ctoggle)
		case r.key == "theme.transparent_selection" && !m.o.Config.Theme.Transparent:
			note = st.Muted.Render("  (transparent only)")
		}
		body := " " + fit(r.label, labelW) + v
		var line string
		if i == s.cursor {
			line = st.SelectedGutter + st.Selected.Render(body)
		} else {
			line = strings.Repeat(" ", ansi.StringWidth(st.SelectedGutter)) + st.Text.Render(body)
		}
		lines = append(lines, fit(line+note, labelW+valueW))
	}
	lines = append(lines, "", st.Muted.Render("←/→ change · space toggle · enter save · esc cancel"))
	return lines
}

// overlaySettings draws the popup centered over rows.
func (m *Model) overlaySettings(rows []string, width int, st *style.UI) {
	box := st.Popup.Render(strings.Join(m.settingsBox(st), "\n"))
	lines := strings.Split(box, "\n")
	bw := 0
	for _, l := range lines {
		bw = max(bw, ansi.StringWidth(l))
	}
	if bw > width {
		bw = width
	}
	x := max((width-bw)/2, 0)
	y := max((len(rows)-len(lines))/2, 0)
	for i, l := range lines {
		if y+i >= len(rows) {
			break
		}
		rows[y+i] = overlayAt(rows[y+i], l, x, bw, width)
	}
}

// overlayAt replaces cells [x, x+boxW) of line (width cells) with box.
func overlayAt(line, box string, x, boxW, width int) string {
	left := ansi.Truncate(line, x, "")
	right := ansi.TruncateLeft(line, x+boxW, "")
	return fit(fit(left, x)+fit(box, boxW)+right, width)
}
