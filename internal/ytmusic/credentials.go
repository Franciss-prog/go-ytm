package ytmusic

import (
	"crypto/sha1" //nolint:gosec // SAPISIDHASH is defined as SHA-1 by Google; not used for security here.
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Origin is the YouTube Music web origin used for requests and SAPISIDHASH.
const Origin = "https://music.youtube.com"

// DefaultUserAgent is sent when credentials carry no browser user agent.
const DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

// ErrNoSAPISID means the cookie lacks the value needed to sign requests.
var ErrNoSAPISID = errors.New("cookie has no __Secure-3PAPISID or SAPISID value (not signed in?)")

// Credentials authenticate InnerTube requests with browser cookies.
type Credentials struct {
	Cookie    string // raw Cookie header value
	AuthUser  int    // X-Goog-AuthUser: Google multi-login slot
	UserAgent string // browser user agent; DefaultUserAgent when empty
}

// CookieValue returns the value of the named cookie in a Cookie header.
func CookieValue(header, name string) (string, bool) {
	for part := range strings.SplitSeq(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) == name {
			return strings.Trim(strings.TrimSpace(v), `"`), true
		}
	}
	return "", false
}

// SAPISID returns the cookie value used to sign requests. ytmusicapi signs
// with __Secure-3PAPISID (helpers.py sapisid_from_cookie); plain SAPISID
// carries the same value in browsers and is accepted as a fallback.
func (c Credentials) SAPISID() (string, error) {
	for _, name := range []string{"__Secure-3PAPISID", "SAPISID"} {
		if v, ok := CookieValue(c.Cookie, name); ok && v != "" {
			return v, nil
		}
	}
	return "", ErrNoSAPISID
}

// SAPISIDHash returns the Authorization header value
// "SAPISIDHASH <ts>_<sha1(ts + " " + sapisid + " " + origin)>"
// (ytmusicapi helpers.py get_authorization).
func SAPISIDHash(sapisid, origin string, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	sum := sha1.Sum([]byte(ts + " " + sapisid + " " + origin)) //nolint:gosec // protocol-defined
	return "SAPISIDHASH " + ts + "_" + hex.EncodeToString(sum[:])
}
