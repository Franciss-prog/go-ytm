package stream

import (
	"path/filepath"

	"github.com/Franciss-Prog/go-ytm/internal/config"
)

// FromConfig builds the yt-dlp resolver from [yt_dlp]. configDir holds
// stream_cookies.txt (written by `ytm setup`); expand resolves "~" in
// configured paths. [yt_dlp] cookie_browser is not used here: browsers
// are read only by `ytm setup`, never on the playback path.
func FromConfig(c config.YtDlp, configDir string, expand func(string) string) YtDlp {
	if expand == nil {
		expand = func(s string) string { return s }
	}
	y := YtDlp{
		Binary:           c.Binary,
		RemoteComponents: c.RemoteComponents,
		JSRuntimes:       c.JSRuntimes,
	}
	if c.CookiesFile != "" {
		y.CookiesFile = expand(c.CookiesFile)
	} else if c.UseSessionCookies && configDir != "" {
		y.SessionJar = filepath.Join(configDir, sessionJarName)
	}
	if c.CABundle != "" {
		y.CABundle = expand(c.CABundle)
	}
	return y
}

// sessionJarName matches auth.StreamCookiesFile (not imported to keep
// stream free of the auth package).
const sessionJarName = "stream_cookies.txt"
