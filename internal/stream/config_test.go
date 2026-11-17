package stream

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Franciss-Prog/go-ytm/internal/auth"
	"github.com/Franciss-Prog/go-ytm/internal/config"
)

func TestFromConfig(t *testing.T) {
	if sessionJarName != auth.StreamCookiesFile {
		t.Fatalf("jar name drifted from auth: %q vs %q", sessionJarName, auth.StreamCookiesFile)
	}
	expand := func(s string) string { return strings.Replace(s, "~", "/home/u", 1) }
	dir := filepath.FromSlash("/cfg")
	tests := []struct {
		name string
		in   config.YtDlp
		want YtDlp
	}{
		{"empty", config.YtDlp{}, YtDlp{}},
		{"session cookies", config.YtDlp{UseSessionCookies: true}, YtDlp{SessionJar: filepath.Join(dir, "stream_cookies.txt")}},
		{"cookies file wins", config.YtDlp{UseSessionCookies: true, CookiesFile: "~/c.txt"}, YtDlp{CookiesFile: "/home/u/c.txt"}},
		{
			"extras",
			config.YtDlp{Binary: "/opt/yt-dlp", CABundle: "~/ca.pem", RemoteComponents: "ejs:npm", JSRuntimes: "node", CookieBrowser: "firefox"},
			YtDlp{Binary: "/opt/yt-dlp", CABundle: "/home/u/ca.pem", RemoteComponents: "ejs:npm", JSRuntimes: "node"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromConfig(tt.in, dir, expand)
			if got.Binary != tt.want.Binary || got.CookiesFile != tt.want.CookiesFile || got.SessionJar != tt.want.SessionJar ||
				got.CABundle != tt.want.CABundle || got.RemoteComponents != tt.want.RemoteComponents || got.JSRuntimes != tt.want.JSRuntimes {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
