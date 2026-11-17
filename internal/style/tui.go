package style

import (
	"image/color"

	"github.com/charmbracelet/colorprofile"

	"charm.land/lipgloss/v2"

	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// Selection is the [theme] transparent_selection setting.
type Selection string

// Selection modes (spec 08, "Transparent mode").
const (
	SelBar       Selection = "bar"
	SelReverse   Selection = "reverse"
	SelUnderline Selection = "underline"
	SelSolid     Selection = "solid"
)

// UIOptions configure TUI styles.
type UIOptions struct {
	Transparent bool
	Selection   Selection // used in transparent mode; opaque mode always fills
	PopupFill   bool
	// NoColor (NO_COLOR or --no-color): attributes only; selection by reverse.
	NoColor bool
	Icons   IconSet
	// Profile is the terminal's color depth ([theme] color_depth or
	// detected); RGB tokens are downsampled to it once per build.
	// Unknown/TrueColor keep them.
	Profile colorprofile.Profile
}

// UI is every lipgloss style the TUI uses, built from a theme. It is the
// only place TUI colors are chosen; components never touch theme tokens.
// Rebuild it (cheap) on theme or option changes and bump Version.
type UI struct {
	Opts    UIOptions
	Theme   theme.Theme
	Version uint64

	// Base is the default text style. The opaque root background is not a
	// style: nested spans end with SGR reset, which would cut a parent fill
	// short. The TUI sets tea.View.BackgroundColor (RootBg) instead, so the
	// terminal's own default background becomes bg and is restored on exit.
	Base lipgloss.Style
	// Panel is a side pane (sidebars).
	Panel lipgloss.Style

	Header       lipgloss.Style // header row
	HeaderActive lipgloss.Style // current page name
	HeaderLink   lipgloss.Style // ← Back / Forward →, enabled
	HeaderDim    lipgloss.Style // disabled nav arrows
	Pending      lipgloss.Style // pending chord/count ("g-", "15")

	Title                            lipgloss.Style // page titles
	Text                             lipgloss.Style
	Muted                            lipgloss.Style
	Accent                           lipgloss.Style
	Border                           lipgloss.Style // separators (fg only)
	Section                          lipgloss.Style // headings inside pages
	ErrorText, WarnText, SuccessText lipgloss.Style

	// Selected styles the cursor row; SelectedGutter is the left marker
	// drawn in bar mode ("" in other modes).
	Selected       lipgloss.Style
	SelectedGutter string

	InfoBar lipgloss.Style // selection info line

	BarTitle       lipgloss.Style
	BarArtist      lipgloss.Style
	BarTime        lipgloss.Style
	BarIcon        lipgloss.Style
	BarIconActive  lipgloss.Style
	ProgressFilled lipgloss.Style
	ProgressEmpty  lipgloss.Style

	Popup      lipgloss.Style // bordered box
	PopupTitle lipgloss.Style

	ToastInfo, ToastSuccess, ToastWarn, ToastError lipgloss.Style

	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
}

// NewUI builds TUI styles from t.
func NewUI(t theme.Theme, o UIOptions, version uint64) *UI {
	if o.Icons.Name == "" {
		o.Icons = UnicodeIcons
	}
	if o.Selection == "" {
		o.Selection = SelBar
	}
	t = theme.Downsample(t, o.Profile)
	u := &UI{Opts: o, Theme: t, Version: version}
	n := lipgloss.NewStyle
	if o.NoColor {
		u.noColor()
		return u
	}
	fg := func(c color.Color) lipgloss.Style { return n().Foreground(c) }
	// fill paints a background unless transparent.
	fill := func(s lipgloss.Style, c color.Color) lipgloss.Style {
		if o.Transparent {
			return s
		}
		return s.Background(c)
	}

	u.Base = fg(t.Fg)
	u.Panel = fg(t.Fg)
	u.Header = fill(fg(t.Muted), t.Surface)
	u.HeaderActive = fill(fg(t.Accent).Bold(true), t.Surface)
	u.HeaderLink = fill(fg(t.Fg), t.Surface)
	u.HeaderDim = fill(fg(t.Border), t.Surface)
	u.Pending = fill(fg(t.Warn).Bold(true), t.Surface)

	u.Title = fg(t.Accent).Bold(true)
	u.Text = fg(t.Fg)
	u.Muted = fg(t.Muted)
	u.Accent = fg(t.Accent)
	u.Border = fg(t.Border)
	u.Section = fg(t.Fg).Bold(true)
	u.ErrorText, u.WarnText, u.SuccessText = fg(t.Error), fg(t.Warn), fg(t.Success)
	u.InfoBar = fill(fg(t.Muted), t.Surface)

	switch {
	case t.IsReverseSelection():
		// ansi source: no selected_bg to fill with.
		u.Selected = n().Reverse(true)
	case !o.Transparent || o.Selection == SelSolid:
		u.Selected = n().Foreground(t.SelectedFg).Background(t.SelectedBg).Bold(true)
	case o.Selection == SelReverse:
		u.Selected = n().Reverse(true)
	case o.Selection == SelUnderline:
		u.Selected = fg(t.Accent).Underline(true)
	default: // bar
		u.Selected = fg(t.Accent).Bold(true)
		u.SelectedGutter = "▌"
	}

	u.BarTitle = fg(t.Fg).Bold(true)
	u.BarArtist = fg(t.Muted)
	u.BarTime = fg(t.Muted)
	u.BarIcon = fg(t.Muted)
	u.BarIconActive = fg(t.Accent)
	u.ProgressFilled = fg(t.ProgressFilled)
	u.ProgressEmpty = fg(t.ProgressEmpty)

	u.Popup = n().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).Padding(0, 1).Foreground(t.Fg)
	if !o.Transparent || o.PopupFill {
		u.Popup = u.Popup.Background(t.Surface).BorderBackground(t.Surface)
	}
	u.PopupTitle = fg(t.Accent).Bold(true)

	toast := func(c color.Color) lipgloss.Style {
		s := n().Border(lipgloss.RoundedBorder()).BorderForeground(c).Foreground(t.Fg).Padding(0, 1)
		if !o.Transparent {
			s = s.Background(t.Surface).BorderBackground(t.Surface)
		}
		return s
	}
	u.ToastInfo, u.ToastSuccess, u.ToastWarn, u.ToastError = toast(t.Border), toast(t.Success), toast(t.Warn), toast(t.Error)

	u.HelpKey = fg(t.Accent)
	u.HelpDesc = fg(t.Fg)
	return u
}

// noColor fills u with attribute-only styles (spec 08: NO_COLOR → no
// colors, selection by reverse).
func (u *UI) noColor() {
	p := lipgloss.NewStyle()
	b := p.Bold(true)
	u.Base, u.Panel, u.Header, u.HeaderLink, u.Text, u.Muted, u.Border, u.InfoBar = p, p, p, p, p, p, p, p
	u.HeaderDim = p.Faint(true)
	u.HeaderActive, u.Pending, u.Title, u.Accent, u.Section = b, b, b, b, b
	u.ErrorText, u.WarnText, u.SuccessText = b, b, p
	u.Selected = p.Reverse(true)
	u.BarTitle, u.BarArtist, u.BarTime, u.BarIcon = b, p, p, p
	u.BarIconActive = b
	u.ProgressFilled, u.ProgressEmpty = p, p.Faint(true)
	u.Popup = p.Border(lipgloss.RoundedBorder()).Padding(0, 1)
	u.PopupTitle = b
	toast := p.Border(lipgloss.RoundedBorder()).Padding(0, 1)
	u.ToastInfo, u.ToastSuccess, u.ToastWarn, u.ToastError = toast, toast, toast, toast
	u.HelpKey, u.HelpDesc = b, p
}

// RootBg and RootFg are the terminal default colors to set through
// tea.View: the theme's bg/fg when opaque, nil (terminal default) when
// transparent or colorless.
func (u *UI) RootBg() color.Color {
	if u.Opts.Transparent || u.Opts.NoColor || isDefault(u.Theme.Bg) {
		return nil
	}
	return u.Theme.Bg
}

// RootFg is the terminal default foreground to set; see RootBg.
func (u *UI) RootFg() color.Color {
	if u.Opts.NoColor || isDefault(u.Theme.Fg) {
		return nil
	}
	return u.Theme.Fg
}

func isDefault(c color.Color) bool {
	_, ok := c.(lipgloss.NoColor)
	return c == nil || ok
}

// PaintsBackground reports whether u may emit background colors at all;
// the transparent-render test uses it.
func (u *UI) PaintsBackground() bool {
	return !u.Opts.NoColor && (!u.Opts.Transparent || u.Opts.Selection == SelSolid || u.Opts.PopupFill)
}
