// Package keys parses key bindings, builds the keymap trie from defaults
// and keymap.toml, and turns key presses into actions with chords and
// count prefixes (docs/specs/04-config-keymap-theme.md, "keymap.toml").
//
// A key token is the canonical spelling used in keymap.toml: an optional
// modifier prefix in the order C- (Ctrl), M- (Alt), S- (Shift), then a
// key name ("space", "page_down", "f5") or a single printable character.
// Shifted printable characters are written as themselves ("A", "?"), never
// as "S-a".
package keys

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// named lists the accepted key names (spec 04 key grammar).
var named = map[string]bool{
	"space": true, "enter": true, "escape": true, "tab": true, "backspace": true,
	"delete": true, "insert": true, "up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "page_up": true, "page_down": true,
}

func init() {
	for i := 1; i <= 12; i++ {
		named[fmt.Sprintf("f%d", i)] = true
	}
}

// aliases maps alternative spellings to canonical names.
var aliases = map[string]string{
	"esc": "escape", "return": "enter", "pgup": "page_up", "pgdown": "page_down",
	"pageup": "page_up", "pagedown": "page_down", "del": "delete", "ins": "insert",
	"bs": "backspace", " ": "space",
}

// ParseKey validates one key token and returns its canonical form.
func ParseKey(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty key")
	}
	var ctrl, alt, shift bool
	rest := s
	for len(rest) > 2 && rest[1] == '-' {
		switch rest[0] {
		case 'C', 'c':
			ctrl = true
		case 'M', 'm', 'A', 'a':
			alt = true
		case 'S', 's':
			shift = true
		default:
			return "", fmt.Errorf("unknown modifier %q in %q", rest[:2], s)
		}
		rest = rest[2:]
	}
	name := rest
	if n, ok := aliases[strings.ToLower(name)]; ok {
		name = n
	}
	switch {
	case named[strings.ToLower(name)]:
		name = strings.ToLower(name)
	case utf8.RuneCountInString(name) == 1:
		r, _ := utf8.DecodeRuneInString(name)
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("control character in %q", s)
		}
		if shift && !ctrl && !alt {
			// "S-a" means "A"; shift on other printables is already in the char.
			name = strings.ToUpper(name)
			shift = false
		} else if ctrl || alt {
			// Terminals cannot tell C-a from C-A; normalise to lower case
			// and keep an explicit S- for the shifted form.
			if lower := strings.ToLower(name); lower != name {
				name, shift = lower, true
			}
		}
	default:
		return "", fmt.Errorf("unknown key %q", s)
	}
	return join(ctrl, alt, shift, name), nil
}

func join(ctrl, alt, shift bool, name string) string {
	var b strings.Builder
	if ctrl {
		b.WriteString("C-")
	}
	if alt {
		b.WriteString("M-")
	}
	if shift {
		b.WriteString("S-")
	}
	b.WriteString(name)
	return b.String()
}

// ParseSequence parses a space-separated binding ("g l", "C-w h").
func ParseSequence(s string) ([]string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty binding")
	}
	out := make([]string, len(fields))
	for i, f := range fields {
		k, err := ParseKey(f)
		if err != nil {
			return nil, err
		}
		out[i] = k
	}
	return out, nil
}

// teaNames maps Bubble Tea keystroke names to canonical names (verified
// against tea.Key.Keystroke in charm.land/bubbletea/v2 v2.1.1).
var teaNames = map[string]string{
	"esc": "escape", "pgup": "page_up", "pgdown": "page_down",
}

// FromTea converts a key press to its canonical token, or "" for keys the
// keymap cannot express (bare modifiers, media keys).
func FromTea(k tea.Key) string {
	ctrl := k.Mod&tea.ModCtrl != 0
	alt := k.Mod&tea.ModAlt != 0
	// Printable text without Ctrl/Alt: the produced character is the key
	// ("A", "?", "+"); shift is already applied.
	if !ctrl && !alt && k.Text != "" && utf8.RuneCountInString(k.Text) == 1 && k.Text != " " {
		return k.Text
	}
	stroke := k.Keystroke()
	parts := strings.Split(stroke, "+")
	name := parts[len(parts)-1]
	if name == "" && strings.HasSuffix(stroke, "+") { // the "+" key itself
		name = "+"
	}
	if n, ok := teaNames[name]; ok {
		name = n
	}
	shift := k.Mod&tea.ModShift != 0
	if utf8.RuneCountInString(name) == 1 {
		if !ctrl && !alt {
			if shift {
				return strings.ToUpper(name)
			}
			return name
		}
	} else if !named[name] {
		return ""
	}
	return join(ctrl, alt, shift, name)
}
