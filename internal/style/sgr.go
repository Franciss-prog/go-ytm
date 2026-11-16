package style

import (
	"strconv"
	"strings"
)

// BackgroundSGRs returns every SGR parameter group in s that sets a
// background color: 40–47, 100–107, or 48;5;n / 48;2;r;g;b. SGR 49
// (default background) is allowed and not reported. Used by the
// transparent-mode render tests (spec 08); parses parameters properly so
// that e.g. the "107" inside "38;2;255;107;107" is not mistaken for a
// background code.
func BackgroundSGRs(s string) []string {
	var found []string
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return found
		}
		s = s[i+2:]
		end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > ';' || r == ':' })
		if end < 0 {
			return found
		}
		params, final := s[:end], s[end]
		s = s[end+1:]
		if final != 'm' {
			continue
		}
		if hasBackground(params) {
			found = append(found, params)
		}
	}
}

func hasBackground(params string) bool {
	if params == "" {
		return false
	}
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		n, err := strconv.Atoi(ps[i])
		if err != nil {
			continue
		}
		switch {
		case n == 38 || n == 58: // fg / underline color: skip its arguments
			i += extArgs(ps, i)
		case n == 48:
			return true
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			return true
		}
	}
	return false
}

// extArgs returns how many parameters follow an extended color introducer.
func extArgs(ps []string, i int) int {
	if i+1 >= len(ps) {
		return 0
	}
	switch ps[i+1] {
	case "5":
		return 2
	case "2":
		return 4
	}
	return 0
}
