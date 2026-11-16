package style

import (
	"image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// HuhTheme maps theme tokens onto Huh form styles so prompts (e.g. `ytm
// setup`) match the rest of the CLI. In transparent mode no style sets a
// background: buttons are marked by color, bold and underline instead.
func HuhTheme(t theme.Theme, transparent bool) huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		s := huh.ThemeBase(isDark)
		fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

		f := &s.Focused
		f.Base = f.Base.BorderForeground(t.Accent)
		f.Card = f.Base
		f.Title = fg(t.Accent).Bold(true)
		f.NoteTitle = fg(t.Accent).Bold(true).MarginBottom(1)
		f.Description = fg(t.Muted)
		f.ErrorIndicator = f.ErrorIndicator.Foreground(t.Error)
		f.ErrorMessage = f.ErrorMessage.Foreground(t.Error)
		f.SelectSelector = f.SelectSelector.Foreground(t.Accent)
		f.NextIndicator = f.NextIndicator.Foreground(t.Accent)
		f.PrevIndicator = f.PrevIndicator.Foreground(t.Accent)
		f.Option = fg(t.Fg)
		f.MultiSelectSelector = f.MultiSelectSelector.Foreground(t.Accent)
		f.SelectedOption = fg(t.Success)
		f.SelectedPrefix = fg(t.Success).SetString("✓ ")
		f.UnselectedPrefix = fg(t.Muted).SetString("• ")
		f.UnselectedOption = fg(t.Fg)
		f.Directory = fg(t.Accent)
		f.File = fg(t.Fg)
		f.TextInput.Cursor = fg(t.Accent)
		f.TextInput.Placeholder = fg(t.Muted)
		f.TextInput.Prompt = fg(t.Accent)
		f.TextInput.Text = fg(t.Fg)

		button := lipgloss.NewStyle().Padding(0, 2).MarginRight(1)
		if transparent {
			f.FocusedButton = button.Foreground(t.Accent).Bold(true).Underline(true)
			f.BlurredButton = button.Foreground(t.Muted)
		} else {
			f.FocusedButton = button.Foreground(t.Bg).Background(t.Accent).Bold(true)
			f.BlurredButton = button.Foreground(t.Fg).Background(t.Surface)
		}
		f.Next = f.FocusedButton

		s.Blurred = s.Focused
		s.Blurred.Base = s.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		s.Blurred.Card = s.Blurred.Base
		s.Blurred.NextIndicator = lipgloss.NewStyle()
		s.Blurred.PrevIndicator = lipgloss.NewStyle()
		s.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")

		s.Group.Title = f.Title
		s.Group.Description = f.Description
		s.Help.ShortKey = fg(t.Muted)
		s.Help.ShortDesc = fg(t.Muted)
		s.Help.ShortSeparator = fg(t.Border)
		s.Help.FullKey = fg(t.Muted)
		s.Help.FullDesc = fg(t.Muted)
		s.Help.FullSeparator = fg(t.Border)
		s.Help.Ellipsis = fg(t.Border)
		return s
	})
}
