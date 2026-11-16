package auth

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// ErrNoCookieHeader means pasted headers contain no Cookie header.
var ErrNoCookieHeader = errors.New("no Cookie header found in the pasted request headers")

var pseudoHeaders = map[string]bool{":authority": true, ":method": true, ":path": true, ":scheme": true, ":status": true}

// ParseRawHeaders parses request headers copied from browser devtools into
// a lowercase name → value map. It accepts the three formats ytm-player
// handles (services/auth.py _normalize_raw_headers):
//
//  1. "Name: Value" per line (Firefox, older Chrome);
//  2. alternating name / value lines (Chrome "Copy request headers"),
//     including Chrome's "Decoded:" protobuf block after x-client-data;
//  3. one line separated by ESC (or a literal "^[") + "E" (terminal paste).
func ParseRawHeaders(raw string) map[string]string {
	out := map[string]string{}
	add := func(name, value string) {
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if name == "" || pseudoHeaders[name] || strings.HasPrefix(name, ":") {
			return
		}
		out[name] = value
	}

	for _, sep := range []string{"^[E", "\x1bE", "\x1b"} {
		if strings.Contains(raw, sep) {
			parts := strings.Split(raw, sep)
			for i := 0; i+1 < len(parts); i += 2 {
				add(parts[i], parts[i+1])
			}
			return out
		}
	}

	var lines []string
	for l := range strings.SplitSeq(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	lines = stripDecodedBlocks(lines)
	colon := 0
	for _, l := range lines {
		if strings.Contains(l, ": ") {
			colon++
		}
	}
	if len(lines) > 2 && float64(colon) < float64(len(lines))*0.2 {
		for i := 0; i+1 < len(lines); i += 2 {
			add(lines[i], lines[i+1])
		}
		return out
	}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, ":") {
			continue
		}
		if name, value, ok := strings.Cut(l, ":"); ok {
			add(name, value)
		}
	}
	return out
}

// stripDecodedBlocks drops Chrome's "Decoded:" … "}" annotation, which
// otherwise shifts the name/value pairing of every later header. An
// unterminated block is left alone.
func stripDecodedBlocks(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "Decoded:" {
			out = append(out, lines[i])
			continue
		}
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "}" {
				end = j
				break
			}
		}
		if end < 0 {
			out = append(out, lines[i])
			continue
		}
		i = end
	}
	return out
}

// ManualSession is what pasted headers provide.
type ManualSession struct {
	Cookie    string
	UserAgent string
	AuthUser  int // from x-goog-authuser, default 0
}

// ParseManual extracts the session from pasted request headers.
func ParseManual(raw string) (ManualSession, error) {
	h := ParseRawHeaders(raw)
	cookie := h["cookie"]
	if cookie == "" {
		return ManualSession{}, ErrNoCookieHeader
	}
	if strings.ContainsFunc(cookie, unicode.IsControl) {
		return ManualSession{}, errors.New("the Cookie header contains control characters; copy it again")
	}
	s := ManualSession{Cookie: cookie, UserAgent: h["user-agent"]}
	if v := h["x-goog-authuser"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return ManualSession{}, errors.New("x-goog-authuser must be a small number")
		}
		s.AuthUser = n
	}
	return s, nil
}
