package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// Runner runs a command with extra environment and returns its output.
// Tests replace it; production uses ExecRunner.
type Runner func(ctx context.Context, env []string, name string, args ...string) (stdout, stderr []byte, err error)

// ExecRunner runs the command with os/exec. Cancelling ctx kills the
// process (exec.CommandContext); WaitDelay bounds the wait for its pipes.
func ExecRunner(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// YtDlp resolves streams by running one yt-dlp process per track:
//
//	yt-dlp --ignore-config --no-playlist --no-warnings --simulate
//	  -f <quality format> --extractor-args youtube:player_client=default,android
//	  -O '%(.{url,…})j' [options] https://music.youtube.com/watch?v=<id>
//
// -O with a field subset prints ~1.6 KB instead of -J's ~650 KB. The player
// clients match ytm-player 2.2.0 (android unblocks madeForKids content).
// A warm yt-dlp process is not possible: --batch-file - reads stdin to EOF
// before it starts (yt_dlp.get_urls → read_batch_urls), and the ~0.2 s
// startup is small next to the ~3.5 s of network work per track.
type YtDlp struct {
	Binary string // default "yt-dlp"
	Run    Runner // default ExecRunner
	Clock  clock.Clock
	Log    *slog.Logger

	// CookiesFile is [yt_dlp] cookies_file (expanded). It wins over SessionJar.
	CookiesFile string
	// SessionJar is stream_cookies.txt, set only when
	// [yt_dlp] use_session_cookies is on.
	SessionJar string
	// CABundle is [yt_dlp] ca_bundle (expanded): exported as SSL_CERT_FILE
	// with --compat-options no-certifi, as ytm-player does.
	CABundle string
	// RemoteComponents is [yt_dlp] remote_components (comma/space list).
	RemoteComponents string
	// JSRuntimes is [yt_dlp] js_runtimes ("runtime[:path]", comma/space list).
	// Like ytm-player's Python options, the list replaces yt-dlp's default.
	JSRuntimes string
	// TempDir holds the per-run cookie copy (default os.TempDir).
	TempDir string
}

// printTemplate selects the info fields Resolve reads.
const printTemplate = "%(.{url,http_headers,acodec,vcodec,abr,tbr,ext,audio_ext,duration,available_at,requested_formats})j"

// WatchURL is the page yt-dlp is given for a video ID.
func WatchURL(id string) string { return "https://music.youtube.com/watch?v=" + id }

func (y YtDlp) binary() string {
	if y.Binary == "" {
		return "yt-dlp"
	}
	return y.Binary
}

func (y YtDlp) now() time.Time {
	if y.Clock == nil {
		return time.Now()
	}
	return y.Clock.Now()
}

func (y YtDlp) log() *slog.Logger {
	if y.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return y.Log
}

// cookieSource returns the jar to hand yt-dlp, if any.
func (y YtDlp) cookieSource() string {
	if y.CookiesFile != "" {
		return y.CookiesFile
	}
	if y.SessionJar != "" {
		if _, err := os.Stat(y.SessionJar); err == nil {
			return y.SessionJar
		}
	}
	return ""
}

// Args returns the yt-dlp arguments for one resolve. cookies is the jar
// path to pass (a private copy, see Resolve) or "".
func (y YtDlp) Args(id string, q Quality, cookies string) []string {
	return y.args([]string{"--simulate", "-f", q.Format()}, []string{"-O", printTemplate}, id, cookies)
}

// DownloadFormat prefers YouTube's opus-in-webm, which mpv plays as is, so
// a download needs no ffmpeg (ytm-player _DOWNLOAD_FORMAT, without its
// FFmpegExtractAudio re-encode).
const DownloadFormat = "bestaudio[ext=webm]/bestaudio/best"

// DownloadArgs returns the yt-dlp arguments that save id's audio into dir
// and print the final file path.
func (y YtDlp) DownloadArgs(id, dir, cookies string) []string {
	return y.args(
		[]string{"-f", DownloadFormat, "--no-part", "-o", filepath.Join(dir, "%(id)s.%(ext)s")},
		[]string{"-O", "after_move:filepath"}, id, cookies)
}

// args wraps mode-specific flags (head, tail) with the shared ones.
func (y YtDlp) args(head, tail []string, id, cookies string) []string {
	args := append([]string{"--ignore-config", "--no-playlist", "--no-warnings"}, head...)
	args = append(args, "--extractor-args", "youtube:player_client=default,android")
	args = append(args, tail...)
	if cookies != "" {
		args = append(args, "--cookies", cookies)
	}
	if y.CABundle != "" {
		args = append(args, "--compat-options", "no-certifi")
	}
	for _, c := range splitList(y.RemoteComponents) {
		args = append(args, "--remote-components", c)
	}
	if rts := splitList(y.JSRuntimes); len(rts) > 0 {
		args = append(args, "--no-js-runtimes")
		for _, r := range rts {
			args = append(args, "--js-runtimes", r)
		}
	}
	return append(args, WatchURL(id))
}

// Env returns the extra environment for yt-dlp.
func (y YtDlp) Env() []string {
	if y.CABundle == "" {
		return nil
	}
	return []string{"SSL_CERT_FILE=" + y.CABundle}
}

// splitList splits a comma/space separated config value.
func splitList(s string) []string {
	return strings.Fields(strings.ReplaceAll(s, ",", " "))
}

// Resolve implements Resolver with a single yt-dlp run (no retries; see
// Cache).
func (y YtDlp) Resolve(ctx context.Context, id string, q Quality) (Stream, error) {
	stdout, err := y.run(ctx, id, func(jar string) []string { return y.Args(id, q, jar) })
	if err != nil {
		return Stream{}, err
	}
	return parseInfo(id, stdout, y.now())
}

// Download saves id's audio into dir and returns the file's path. Callers
// stage dir (the file may be half written on error).
func (y YtDlp) Download(ctx context.Context, id, dir string) (string, error) {
	stdout, err := y.run(ctx, id, func(jar string) []string { return y.DownloadArgs(id, dir, jar) })
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	path := strings.TrimSpace(lines[len(lines)-1])
	if path == "" {
		return "", fmt.Errorf("download %s: yt-dlp printed no file path", id)
	}
	return path, nil
}

// run runs yt-dlp once with the arguments args builds for a cookie jar.
// The jar is copied to a private temp file first: yt-dlp saves --cookies
// back on exit, which would race between two concurrent runs and could
// overwrite a jar `ytm setup` just replaced.
func (y YtDlp) run(ctx context.Context, id string, args func(jar string) []string) ([]byte, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	var jar string
	if src := y.cookieSource(); src != "" {
		dir, err := os.MkdirTemp(y.TempDir, "ytm-stream-")
		if err != nil {
			return nil, fmt.Errorf("cookie temp dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		jar = filepath.Join(dir, "cookies.txt")
		if err := copyFile(src, jar); err != nil {
			// Same as ytm-player: resolve signed out rather than fail.
			y.log().Warn("stream: cookie jar unreadable; resolving without cookies", "err", err)
			jar = ""
		}
	}
	run := y.Run
	if run == nil {
		run = ExecRunner
	}
	bin := y.binary()
	start := y.now()
	stdout, stderr, err := run(ctx, y.Env(), bin, args(jar)...)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	y.log().Debug("stream: yt-dlp finished", "id", id, "took", y.now().Sub(start), "err", err)
	if err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) || errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s: %w", ErrYtDlpNotFound, bin, err)
		}
		return nil, classify(stderr, err)
	}
	return stdout, nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// info is the subset of yt-dlp's info dict printed by printTemplate.
type info struct {
	URL         string            `json:"url"`
	Headers     map[string]string `json:"http_headers"`
	ACodec      string            `json:"acodec"`
	VCodec      string            `json:"vcodec"`
	ABR         float64           `json:"abr"`
	TBR         float64           `json:"tbr"`
	Ext         string            `json:"ext"`
	AudioExt    string            `json:"audio_ext"`
	Duration    float64           `json:"duration"`
	AvailableAt float64           `json:"available_at"`
	Requested   []info            `json:"requested_formats"`
}

// parseInfo turns yt-dlp's printed JSON into a Stream. now dates the
// fallback expiry when the URL has no expire parameter.
func parseInfo(id string, out []byte, now time.Time) (Stream, error) {
	// -O prints one line per video; take the last non-empty line so a
	// stray warning on stdout cannot break decoding.
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	var in info
	if err := json.Unmarshal(lines[len(lines)-1], &in); err != nil {
		return Stream{}, fmt.Errorf("parse yt-dlp output: %w", err)
	}
	pick := in
	if pick.URL == "" {
		// A merged selection nests its parts; take the audio one
		// (same rule as ytm-player's _try_resolve).
		for _, f := range in.Requested {
			if f.URL != "" && (f.VCodec == "none" || f.ACodec != "none") {
				pick = f
				break
			}
		}
	}
	if pick.URL == "" {
		return Stream{}, fmt.Errorf("%w: yt-dlp returned no stream URL", ErrUnavailable)
	}
	s := Stream{
		VideoID:  id,
		URL:      pick.URL,
		Headers:  firstNonEmpty(pick.Headers, in.Headers),
		Expires:  expiry(pick.URL, now),
		Codec:    codec(pick, in),
		Bitrate:  int(firstPositive(pick.ABR, pick.TBR, in.ABR, in.TBR)),
		Duration: time.Duration(in.Duration * float64(time.Second)),
	}
	if at := firstPositive(pick.AvailableAt, in.AvailableAt); at > 0 {
		s.AvailableAt = time.Unix(int64(at), 0)
	}
	return s, nil
}

// expiry reads googlevideo's expire=<unix> query parameter.
func expiry(raw string, now time.Time) time.Time {
	if u, err := url.Parse(raw); err == nil {
		if n, err := strconv.ParseInt(u.Query().Get("expire"), 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0)
		}
	}
	return now.Add(DefaultTTL)
}

func codec(pick, in info) string {
	for _, c := range []string{pick.ACodec, in.ACodec} {
		if c != "" && c != "none" {
			return c
		}
	}
	for _, e := range []string{pick.AudioExt, pick.Ext, in.AudioExt, in.Ext} {
		if e != "" && e != "none" {
			return e
		}
	}
	return "unknown"
}

func firstNonEmpty(ms ...map[string]string) map[string]string {
	for _, m := range ms {
		if len(m) > 0 {
			return m
		}
	}
	return nil
}

func firstPositive(vs ...float64) float64 {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

// classify maps yt-dlp's last ERROR line to a sentinel. The phrases come
// from YouTube's playability reasons as yt-dlp 2026.10.26 reports them
// (extractor/youtube/_video.py _real_extract: "sign in" reasons get a login
// hint, "try again later" a rate-limit note, geo blocks raise
// GeoRestrictedError) plus yt-dlp's own format error.
func classify(stderr []byte, runErr error) error {
	msg := lastError(stderr)
	if msg == "" {
		msg = runErr.Error()
	}
	msg = redactCookieDetails(msg)
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "sign in") || strings.Contains(l, "login_required") || strings.Contains(l, "members-only") || strings.Contains(l, "join this channel"):
		return fmt.Errorf("%w: %s", ErrSignInRequired, msg)
	case strings.Contains(l, "rate-limited") || strings.Contains(l, "try again later") || strings.Contains(l, "http error 429"):
		return fmt.Errorf("%w: %s", ErrRateLimited, msg)
	case strings.Contains(l, "unavailable") || strings.Contains(l, "not available") ||
		strings.Contains(l, "in your country") || strings.Contains(l, "from your location") ||
		strings.Contains(l, "private video") || strings.Contains(l, "video is private") ||
		strings.Contains(l, "removed") || strings.Contains(l, "premium") ||
		strings.Contains(l, "requested format is not available") || strings.Contains(l, "drm"):
		return fmt.Errorf("%w: %s", ErrUnavailable, msg)
	}
	return fmt.Errorf("yt-dlp: %s", msg)
}

// lastError returns the text of the last "ERROR:" line, without yt-dlp's
// "[youtube] <id>: " prefix.
func lastError(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if rest, ok := strings.CutPrefix(l, "ERROR:"); ok {
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "[") {
				if _, after, ok := strings.Cut(rest, ": "); ok {
					rest = after
				}
			}
			return rest
		}
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// cookieFileError precedes a quoted cookies.txt row (which holds a cookie
// value) in http.cookiejar's parse error; ytm-player withholds it too.
const cookieFileError = "invalid Netscape format cookies file"

func redactCookieDetails(msg string) string {
	head, _, found := strings.Cut(msg, cookieFileError)
	if !found {
		return msg
	}
	return head + cookieFileError + " (details withheld: they can contain cookie values)"
}
