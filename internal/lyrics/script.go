package lyrics

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	anyascii "github.com/anyascii/go"
	"golang.org/x/text/unicode/bidi"
)

// Transliterate returns s in ASCII (anyascii, the library ytm-player
// uses), or "" when s is already ASCII and there is nothing to add.
func Transliterate(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return strings.TrimSpace(anyascii.Transliterate(s))
		}
	}
	return ""
}

// HasRTL reports whether s holds right-to-left letters (Hebrew, Arabic,
// Syriac, Thaana, N'Ko and their presentation forms).
func HasRTL(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hebrew, unicode.Arabic, unicode.Syriac, unicode.Thaana, unicode.Nko) {
			return true
		}
	}
	return false
}

// Visual reorders one line from logical to visual order (UAX #9) for a
// terminal that draws cells left to right. Lines without RTL letters are
// returned unchanged.
func Visual(s string) string {
	if !HasRTL(s) {
		return s
	}
	var p bidi.Paragraph
	if _, err := p.SetString(s, bidi.DefaultDirection(direction(s))); err != nil {
		return s
	}
	o, err := p.Order()
	if err != nil {
		return s
	}
	// x/text's Order returns the runs in logical order (checked against
	// v0.24.0), so rule L2 is applied here: reverse each RTL run, and the
	// run order in an RTL paragraph.
	// shortcut: two levels only (no nested embeddings), enough for lyrics
	// lines; use full level-based L2 if mixed nested text shows up wrong.
	runs := make([]string, o.NumRuns())
	for i := range runs {
		r := o.Run(i)
		runs[i] = r.String()
		if r.Direction() == bidi.RightToLeft {
			runs[i] = bidi.ReverseString(runs[i])
		}
	}
	if !p.IsLeftToRight() {
		slices.Reverse(runs)
	}
	return strings.Join(runs, "")
}

// direction is the paragraph direction by UAX #9 rule P2: that of the
// first strong letter (x/text does not detect it itself).
func direction(s string) bidi.Direction {
	for _, r := range s {
		p, _ := bidi.LookupRune(r)
		switch p.Class() {
		case bidi.L:
			return bidi.LeftToRight
		case bidi.R, bidi.AL:
			return bidi.RightToLeft
		}
	}
	return bidi.LeftToRight
}
