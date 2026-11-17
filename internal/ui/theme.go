package ui

import (
	"image/color"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// ThemeService re-resolves the theme off the UI goroutine. Its methods
// must not block; results come back as ThemeChangedMsg.
type ThemeService interface {
	// Set applies new [theme] settings (re-resolve, re-target watching).
	Set(c config.Theme)
	// Reload forces a re-resolve (sent even when unchanged).
	Reload()
	// SetTerminalBg reports the terminal's background (OSC 11 answer).
	SetTerminalBg(c color.Color)
}

// ThemeChangedMsg carries a resolved theme into Update (sent by the theme
// manager's goroutine).
type ThemeChangedMsg struct{ Result theme.Result }

// themeChanged swaps in a new theme. Nothing but styles changes: page
// state, scroll, popups and the player are untouched (spec 08).
func (m *Model) themeChanged(r theme.Result) tea.Cmd {
	if r.Err != nil {
		m.log.Warn("ui: theme source failed", "err", r.Err, "cause", r.Cause)
		// A broken or half-written file keeps the current colors; a
		// settings change to a missing source shows the fallback.
		if r.Cause == theme.CauseSettings {
			m.o.Theme = r.Theme
			m.restyle()
		}
		msg := r.Err.Error()
		if msg == m.themeErr {
			return nil // toast once per distinct problem
		}
		m.themeErr = msg
		text := "Theme: " + msg
		if r.Cause == theme.CauseSettings {
			text += " — using " + r.Theme.Name
		} else {
			text += " — keeping current colors"
		}
		return m.toasts.push(Toast{Severity: SevWarn, Text: text})
	}
	recovered := m.themeErr != ""
	m.themeErr = ""
	m.o.Theme = r.Theme
	m.restyle()
	for _, n := range r.Notes {
		m.log.Info("ui: theme note", "note", n)
	}
	switch {
	case r.Cause == theme.CauseReload:
		return m.toasts.push(Toast{Severity: SevSuccess, Text: "Theme reloaded: " + r.Theme.Name})
	case recovered:
		return m.toasts.push(Toast{Severity: SevSuccess, Text: "Theme colors found: using " + r.Theme.Name})
	}
	return nil
}

// toggleTransparency flips [theme] transparent, applies it and persists it.
func (m *Model) toggleTransparency() tea.Cmd {
	m.o.Config.Theme.Transparent = !m.o.Config.Theme.Transparent
	m.applySettings(true)
	state := "off"
	if m.o.Config.Theme.Transparent {
		state = "on"
	}
	return tea.Batch(
		m.toasts.push(Toast{Severity: SevInfo, Text: "Transparent background " + state}),
		m.persist([]config.Change{{Key: "theme.transparent", Value: m.o.Config.Theme.Transparent}}),
	)
}
