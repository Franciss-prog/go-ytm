// Package auth implements `ytm setup`: reading a YouTube Music browser
// session (via yt-dlp, a cookies file or pasted request headers), finding
// the signed-in accounts, and storing credentials in the config dir.
package auth

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Cookie is one entry of a Netscape cookies file.
type Cookie struct {
	Domain   string // e.g. ".youtube.com"
	Path     string
	Secure   bool
	HTTPOnly bool
	Expires  int64 // unix seconds; 0 = session cookie
	Name     string
	Value    string
}

// expired reports whether the cookie has expired at now.
func (c Cookie) expired(now time.Time) bool {
	return c.Expires != 0 && c.Expires < now.Unix()
}

func hasControlChars(ss ...string) bool {
	for _, s := range ss {
		if strings.ContainsAny(s, "\t\r\n") {
			return true
		}
	}
	return false
}

const httpOnlyPrefix = "#HttpOnly_"

// ParseNetscape reads a Netscape/Mozilla cookies.txt file (the format
// yt-dlp --cookies writes). Comment and blank lines are skipped;
// "#HttpOnly_" lines are cookies.
func ParseNetscape(r io.Reader) ([]Cookie, error) {
	var out []Cookie
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		httpOnly := false
		if strings.HasPrefix(text, httpOnlyPrefix) {
			httpOnly = true
			text = strings.TrimPrefix(text, httpOnlyPrefix)
		} else if strings.HasPrefix(text, "#") || strings.TrimSpace(text) == "" {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) == 6 { // empty value with the trailing tab trimmed by an editor
			f = append(f, "")
		}
		if len(f) != 7 {
			return nil, fmt.Errorf("cookies file line %d: want 7 tab-separated fields, got %d", line, len(f))
		}
		exp, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cookies file line %d: bad expiry %q", line, f[4])
		}
		out = append(out, Cookie{
			Domain: f[0], Path: f[2], Secure: strings.EqualFold(f[3], "TRUE"), HTTPOnly: httpOnly,
			Expires: exp, Name: f[5], Value: f[6],
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read cookies file: %w", err)
	}
	return out, nil
}

// WriteNetscape writes cookies in Netscape format. Cookies whose name or
// value contain tabs or newlines are skipped (they would corrupt the file).
func WriteNetscape(w io.Writer, cookies []Cookie) error {
	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n# Written by ytm-go. Do not edit.\n\n")
	for _, c := range cookies {
		if hasControlChars(c.Domain, c.Path, c.Name, c.Value) {
			continue
		}
		prefix := ""
		if c.HTTPOnly {
			prefix = httpOnlyPrefix
		}
		fmt.Fprintf(&b, "%s%s\t%s\t%s\t%s\t%d\t%s\t%s\n", prefix, c.Domain,
			boolField(strings.HasPrefix(c.Domain, ".")), c.Path, boolField(c.Secure), c.Expires, c.Name, c.Value)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func boolField(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}

func domainIn(domain string, roots ...string) bool {
	d := strings.ToLower(strings.TrimPrefix(domain, "."))
	for _, r := range roots {
		if d == r || strings.HasSuffix(d, "."+r) {
			return true
		}
	}
	return false
}

// musicCookie reports whether a browser would send c to music.youtube.com.
func musicCookie(c Cookie) bool {
	d := strings.ToLower(strings.TrimPrefix(c.Domain, "."))
	return d == "youtube.com" || d == "music.youtube.com"
}

// CookieHeader builds the Cookie header for music.youtube.com requests from
// a browser jar: unexpired youtube.com cookies, minus the multi-KB "ST-*"
// tracking cookies that can push the header past the server's size limit
// (ytm-player services/auth.py _save_youtube_cookies).
func CookieHeader(cookies []Cookie, now time.Time) string {
	seen := map[string]bool{}
	var parts []string
	// More specific domains first, so music.youtube.com wins over youtube.com.
	for _, pass := range []bool{true, false} {
		for _, c := range cookies {
			specific := strings.EqualFold(strings.TrimPrefix(c.Domain, "."), "music.youtube.com")
			if specific != pass || !musicCookie(c) || c.expired(now) || strings.HasPrefix(c.Name, "ST-") ||
				c.Name == "" || seen[c.Name] || hasControlChars(c.Name, c.Value) || strings.ContainsAny(c.Name+c.Value, ";") {
				continue
			}
			seen[c.Name] = true
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

// HasSession reports whether the jar holds a signed-in YouTube session
// (ytmusicapi signs requests with __Secure-3PAPISID).
func HasSession(cookies []Cookie, now time.Time) bool {
	for _, c := range cookies {
		if musicCookie(c) && c.Name == "__Secure-3PAPISID" && c.Value != "" && !c.expired(now) {
			return true
		}
	}
	return false
}

// StreamJar selects the youtube.com/google.com cookies yt-dlp needs for
// stream resolution (stream_cookies.txt).
func StreamJar(cookies []Cookie) []Cookie {
	var out []Cookie
	for _, c := range cookies {
		if domainIn(c.Domain, "youtube.com", "google.com") && !hasControlChars(c.Name, c.Value) {
			out = append(out, c)
		}
	}
	return out
}

// CookiesFromHeader turns a raw Cookie header into jar entries scoped to
// .youtube.com — the domain the browser restricted that header to.
func CookiesFromHeader(header string, now time.Time) []Cookie {
	exp := now.Add(2 * 365 * 24 * time.Hour).Unix()
	var out []Cookie
	for part := range strings.SplitSeq(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || hasControlChars(name, value) {
			continue
		}
		out = append(out, Cookie{Domain: ".youtube.com", Path: "/", Secure: true, Expires: exp, Name: name, Value: value})
	}
	return out
}
