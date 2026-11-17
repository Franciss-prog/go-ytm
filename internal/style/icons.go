package style

// IconSet is a named set of glyphs. Use ResolveIcons to pick one.
type IconSet struct {
	Name     string
	OK       string
	Warn     string
	Fail     string
	Info     string
	Bullet   string
	Play     string
	Pause    string
	Heart    string
	Shuffle  string
	Repeat   string
	Ellipsis string
	Mark     string    // marked row
	Autoplay string    // row added by autoplay
	Sort     [2]string // ascending, descending
}

var (
	// UnicodeIcons work in any UTF-8 terminal without special fonts.
	UnicodeIcons = IconSet{
		Name: "unicode", OK: "✓", Warn: "!", Fail: "✗", Info: "·", Bullet: "•",
		Play: "▶", Pause: "⏸", Heart: "♥", Shuffle: "⤮", Repeat: "⟳", Ellipsis: "…",
		Mark: "●", Autoplay: "↻", Sort: [2]string{"↑", "↓"},
	}
	// NerdIcons require a Nerd Font.
	NerdIcons = IconSet{
		Name: "nerd", OK: "", Warn: "", Fail: "", Info: "", Bullet: "",
		Play: "", Pause: "", Heart: "", Shuffle: "", Repeat: "", Ellipsis: "…",
		Mark: "", Autoplay: "", Sort: [2]string{"", ""},
	}
	// ASCIIIcons are safe everywhere.
	ASCIIIcons = IconSet{
		Name: "ascii", OK: "[ok]", Warn: "[!!]", Fail: "[x]", Info: "[i]", Bullet: "*",
		Play: ">", Pause: "||", Heart: "<3", Shuffle: "~", Repeat: "@", Ellipsis: "...",
		Mark: "*", Autoplay: "+", Sort: [2]string{"^", "v"},
	}
)

// ResolveIcons maps the [ui] icons setting to a set, falling back to ASCII
// when the output is plain or the terminal cannot show Unicode.
func ResolveIcons(setting string, styled, unicodeOK bool) IconSet {
	if !styled || !unicodeOK {
		return ASCIIIcons
	}
	switch setting {
	case "nerd":
		return NerdIcons
	case "ascii":
		return ASCIIIcons
	default:
		return UnicodeIcons
	}
}
