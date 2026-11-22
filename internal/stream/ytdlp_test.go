package stream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain doubles as a fake yt-dlp: when FAKE_YTDLP is set the test
// binary acts as the subprocess (portable, unlike a shell script).
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_YTDLP") {
	case "":
		goleak.VerifyTestMain(m)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "unavailable":
		fmt.Fprintln(os.Stderr, "ERROR: [youtube] aaaaaaaaaaa: This video is unavailable")
		os.Exit(1)
	case "ok":
		b, _ := os.ReadFile(os.Getenv("FAKE_YTDLP_OUT"))
		os.Stdout.Write(b) //nolint:errcheck
		os.Exit(0)
	}
}

// fakeBin returns a Runner that re-executes the test binary in mode.
func fakeBin(t *testing.T, mode string, extraEnv ...string) Runner {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return func(ctx context.Context, env []string, _ string, args ...string) ([]byte, []byte, error) {
		env = append(append(env, "FAKE_YTDLP="+mode), extraEnv...)
		return ExecRunner(ctx, env, self, append([]string{"-test.run=^$"}, args...)...)
	}
}

var fixed = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

const opusID = "dQw4w9WgXcQ"

func TestQualityFormat(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"high", "bestaudio/best[vcodec!=none][acodec!=none]"},
		{"medium", "bestaudio[abr<=128]/bestaudio/best[vcodec!=none][acodec!=none]"},
		{"low", "bestaudio[abr<=64]/bestaudio/best[vcodec!=none][acodec!=none]"},
		{"bogus", "bestaudio/best[vcodec!=none][acodec!=none]"},
	}
	for _, tt := range tests {
		if got := ParseQuality(tt.in).Format(); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestArgs(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "stream_cookies.txt")
	if err := os.WriteFile(jar, []byte("# jar\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{
		"--ignore-config", "--no-playlist", "--no-warnings", "--simulate",
		"-f", "bestaudio/best[vcodec!=none][acodec!=none]",
		"--extractor-args", "youtube:player_client=default,android",
		"-O", printTemplate,
	}
	url := "https://music.youtube.com/watch?v=" + opusID
	tests := []struct {
		name    string
		y       YtDlp
		cookies string
		want    []string
		env     []string
		source  string
	}{
		{name: "defaults", want: append(slices.Clone(base), url)},
		{
			name: "cookies", y: YtDlp{CookiesFile: "/c.txt"}, cookies: "/tmp/x", source: "/c.txt",
			want: append(slices.Clone(base), "--cookies", "/tmp/x", url),
		},
		{
			name: "cookies file wins over session jar", y: YtDlp{CookiesFile: "/c.txt", SessionJar: jar}, source: "/c.txt",
			want: append(slices.Clone(base), url),
		},
		{name: "session jar present", y: YtDlp{SessionJar: jar}, source: jar, want: append(slices.Clone(base), url)},
		{name: "session jar missing", y: YtDlp{SessionJar: filepath.Join(dir, "nope")}, want: append(slices.Clone(base), url)},
		{
			name: "ca bundle", y: YtDlp{CABundle: "/etc/ca.pem"},
			want: append(slices.Clone(base), "--compat-options", "no-certifi", url), env: []string{"SSL_CERT_FILE=/etc/ca.pem"},
		},
		{
			name: "remote components", y: YtDlp{RemoteComponents: "ejs:npm, ejs:github"},
			want: append(slices.Clone(base), "--remote-components", "ejs:npm", "--remote-components", "ejs:github", url),
		},
		{
			name: "js runtimes replace default", y: YtDlp{JSRuntimes: "node:/usr/bin/node bun"},
			want: append(slices.Clone(base), "--no-js-runtimes", "--js-runtimes", "node:/usr/bin/node", "--js-runtimes", "bun", url),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.y.Args(opusID, High, tt.cookies); !slices.Equal(got, tt.want) {
				t.Errorf("args\n got %q\nwant %q", got, tt.want)
			}
			if got := tt.y.Env(); !slices.Equal(got, tt.env) {
				t.Errorf("env got %q, want %q", got, tt.env)
			}
			if got := tt.y.cookieSource(); got != tt.source {
				t.Errorf("cookie source got %q, want %q", got, tt.source)
			}
		})
	}
}

func TestParseInfoFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/info_opus.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseInfo(opusID, raw, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if s.Codec != "opus" || s.Bitrate != 128 || s.Duration != 213*time.Second {
		t.Errorf("codec/bitrate/duration = %q/%d/%v", s.Codec, s.Bitrate, s.Duration)
	}
	if !s.Expires.Equal(time.Unix(1794896297, 0)) {
		t.Errorf("expires = %v", s.Expires)
	}
	if !s.AvailableAt.Equal(time.Unix(1794874703, 0)) {
		t.Errorf("available_at = %v", s.AvailableAt)
	}
	if s.Headers["User-Agent"] == "" || !strings.Contains(s.URL, "googlevideo.com") {
		t.Errorf("headers/url missing: %+v", s)
	}
}

func TestParseInfoShapes(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantURL string
		codec   string
		expires time.Time
		err     error
	}{
		{
			name: "no expire param falls back to TTL", out: `{"url":"https://x/a","acodec":"mp4a.40.2","abr":129.5}`,
			wantURL: "https://x/a", codec: "mp4a.40.2", expires: fixed.Add(DefaultTTL),
		},
		{
			name:    "requested formats picks audio",
			out:     `{"requested_formats":[{"url":"https://x/v?expire=99","vcodec":"avc1","acodec":"none"},{"url":"https://x/a?expire=100","vcodec":"none","acodec":"opus"}]}`,
			wantURL: "https://x/a?expire=100", codec: "opus", expires: time.Unix(100, 0),
		},
		{
			name: "codec falls back to ext", out: `{"url":"https://x/a?expire=5","acodec":"none","vcodec":"none","ext":"m4a"}`,
			wantURL: "https://x/a?expire=5", codec: "m4a", expires: time.Unix(5, 0),
		},
		{
			name: "stray line before json", out: "WARNING: something\n{\"url\":\"https://x/a?expire=7\"}\n",
			wantURL: "https://x/a?expire=7", codec: "unknown", expires: time.Unix(7, 0),
		},
		{name: "no url", out: `{"acodec":"opus"}`, err: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parseInfo("id", []byte(tt.out), fixed)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.URL != tt.wantURL || s.Codec != tt.codec || !s.Expires.Equal(tt.expires) {
				t.Errorf("got url=%q codec=%q expires=%v", s.URL, s.Codec, s.Expires)
			}
		})
	}
	if _, err := parseInfo("id", []byte("not json"), fixed); err == nil {
		t.Error("garbage parsed")
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		stderr string
		want   error
	}{
		{"ERROR: [youtube] aaaaaaaaaaa: This video is unavailable", ErrUnavailable},
		{"ERROR: [youtube] x: Video unavailable. This video is private", ErrUnavailable},
		{"ERROR: [youtube] x: The uploader has not made this video available in your country", ErrUnavailable},
		{"ERROR: [youtube] x: Requested format is not available. Use --list-formats", ErrUnavailable},
		{"ERROR: [youtube] x: This video requires payment to watch. Music Premium", ErrUnavailable},
		{"ERROR: [youtube] x: Sign in to confirm your age. Use --cookies-from-browser", ErrSignInRequired},
		{"ERROR: [youtube] x: Sign in to confirm you’re not a bot. Use --cookies", ErrSignInRequired},
		{"ERROR: [youtube] x: Join this channel to get access to members-only content", ErrSignInRequired},
		{"ERROR: [youtube] x: This content isn't available, try again later. The current session has been rate-limited", ErrRateLimited},
		{"ERROR: [youtube] x: Unable to download API page: HTTP Error 503", nil},
	}
	for _, tt := range tests {
		err := classify([]byte("[youtube] x: Downloading webpage\n"+tt.stderr+"\n"), errors.New("exit status 1"))
		if tt.want == nil {
			if !retryable(err) {
				t.Errorf("%q: %v should be retryable", tt.stderr, err)
			}
			continue
		}
		if !errors.Is(err, tt.want) {
			t.Errorf("%q: got %v, want %v", tt.stderr, err, tt.want)
		}
		if retryable(err) {
			t.Errorf("%q: %v should not be retryable", tt.stderr, err)
		}
	}
	err := classify([]byte("ERROR: invalid Netscape format cookies file '/x': 'SID\tsecretvalue'"), errors.New("exit 1"))
	if strings.Contains(err.Error(), "secretvalue") {
		t.Errorf("cookie value leaked: %v", err)
	}
}

func TestResolveRealProcess(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(out, []byte(`{"url":"https://x/a?expire=4102444800","acodec":"opus"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	y := YtDlp{Run: fakeBin(t, "ok", "FAKE_YTDLP_OUT="+out)}
	s, err := y.Resolve(context.Background(), opusID, High)
	if err != nil {
		t.Fatal(err)
	}
	if s.URL != "https://x/a?expire=4102444800" || s.VideoID != opusID {
		t.Errorf("got %+v", s)
	}

	_, err = YtDlp{Run: fakeBin(t, "unavailable")}.Resolve(context.Background(), opusID, High)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("unavailable: got %v", err)
	}
}

func TestResolveCancelKillsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := YtDlp{Run: fakeBin(t, "sleep")}.Resolve(ctx, opusID, High)
		errc <- err
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("cancel took %v", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("resolve did not return after cancel")
	}
}

func TestResolveMissingBinary(t *testing.T) {
	_, err := YtDlp{Binary: filepath.Join(t.TempDir(), "no-yt-dlp")}.Resolve(context.Background(), opusID, High)
	if !errors.Is(err, ErrYtDlpNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := exec.LookPath("definitely-not-yt-dlp"); err == nil {
		t.Skip("unexpected binary on PATH")
	}
	_, err = YtDlp{Binary: "definitely-not-yt-dlp"}.Resolve(context.Background(), opusID, High)
	if !errors.Is(err, ErrYtDlpNotFound) {
		t.Fatalf("PATH lookup: got %v", err)
	}
}

func TestResolveRejectsBadID(t *testing.T) {
	called := false
	y := YtDlp{Run: func(context.Context, []string, string, ...string) ([]byte, []byte, error) {
		called = true
		return nil, nil, nil
	}}
	for _, id := range []string{"", "--exec=rm", "a b", "x&list=1", strings.Repeat("a", 65)} {
		if _, err := y.Resolve(context.Background(), id, High); !errors.Is(err, ErrInvalidID) {
			t.Errorf("%q: got %v", id, err)
		}
	}
	if called {
		t.Error("yt-dlp ran for an invalid id")
	}
}

// TestResolveCopiesCookieJar checks yt-dlp gets a private copy of the jar,
// so its save-on-exit cannot touch the original.
func TestResolveCopiesCookieJar(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "stream_cookies.txt")
	orig := []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t0\tSID\tv\n")
	if err := os.WriteFile(src, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	var seen string
	y := YtDlp{SessionJar: src, TempDir: dir, Run: func(_ context.Context, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		i := slices.Index(args, "--cookies")
		if i < 0 {
			t.Fatal("no --cookies")
		}
		seen = args[i+1]
		got, _ := os.ReadFile(seen)
		if string(got) != string(orig) {
			t.Errorf("copy differs: %q", got)
		}
		_ = os.WriteFile(seen, []byte("rewritten by yt-dlp"), 0o600)
		return []byte(`{"url":"https://x/a"}`), nil, nil
	}}
	if _, err := y.Resolve(context.Background(), opusID, High); err != nil {
		t.Fatal(err)
	}
	if seen == src {
		t.Fatal("yt-dlp got the original jar")
	}
	if got, _ := os.ReadFile(src); string(got) != string(orig) {
		t.Errorf("original jar changed: %q", got)
	}
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Errorf("temp jar not removed: %v", err)
	}
}

func TestDownload(t *testing.T) {
	var got []string
	y := YtDlp{Run: func(_ context.Context, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		got = args
		return []byte("/stage/dQw4w9WgXcQ.webm\n"), nil, nil
	}}
	stage := t.TempDir()
	p, err := y.Download(context.Background(), "dQw4w9WgXcQ", stage)
	if err != nil || p != "/stage/dQw4w9WgXcQ.webm" {
		t.Fatalf("path %q err %v", p, err)
	}
	for _, want := range []string{DownloadFormat, filepath.Join(stage, "%(id)s.%(ext)s"), "after_move:filepath", "--no-part"} {
		if !slices.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
	if slices.Contains(got, "--simulate") {
		t.Error("download args simulate")
	}
	y.Run = func(context.Context, []string, string, ...string) ([]byte, []byte, error) { return nil, nil, nil }
	if _, err := y.Download(context.Background(), "dQw4w9WgXcQ", "/stage"); err == nil {
		t.Error("empty output accepted")
	}
	if _, err := y.Download(context.Background(), "a b", "/stage"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("bad id: %v", err)
	}
}
