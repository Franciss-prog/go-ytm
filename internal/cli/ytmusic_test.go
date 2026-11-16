package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/auth"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// fakeYT implements ytmusic.Client for CLI tests.
type fakeYT struct {
	creds *ytmusic.Credentials
	world *fakeWorld
}

type fakeWorld struct {
	mu         sync.Mutex
	accounts   map[int]ytmusic.AccountInfo // by slot, for sessions with __Secure-3PAPISID=good
	rejectAuth bool                        // signed-in requests fail with ErrUnauthenticated
	searchErr  error
	queries    []string
}

func (f fakeYT) Search(_ context.Context, q string, flt ytmusic.SearchFilter, limit int) (ytmusic.SearchResults, error) {
	f.world.mu.Lock()
	f.world.queries = append(f.world.queries, q+"|"+string(flt))
	f.world.mu.Unlock()
	if f.creds != nil && f.world.rejectAuth {
		return ytmusic.SearchResults{}, ytmusic.ErrUnauthenticated
	}
	if f.world.searchErr != nil {
		return ytmusic.SearchResults{}, f.world.searchErr
	}
	items := sampleResults()
	if flt == ytmusic.FilterSongs {
		items = items[1:3]
	}
	if q == "nothing" {
		items = nil
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return ytmusic.SearchResults{Items: items}, nil
}

func (f fakeYT) SearchSuggestions(context.Context, string) ([]string, error) { return nil, nil }

// Close makes the fake an io.Closer like ytmusic.API, so slot probing
// registers closers from several goroutines (covered by -race).
func (f fakeYT) Close() error { return nil }

func (f fakeYT) Account(context.Context) (ytmusic.AccountInfo, error) {
	if f.creds == nil || !strings.Contains(f.creds.Cookie, "__Secure-3PAPISID=good") {
		return ytmusic.AccountInfo{}, ytmusic.ErrUnauthenticated
	}
	if a, ok := f.world.accounts[f.creds.AuthUser]; ok {
		return a, nil
	}
	return ytmusic.AccountInfo{}, ytmusic.ErrUnauthenticated
}

func sampleResults() []ytmusic.SearchResult {
	dp := []ytmusic.Artist{{Name: "Daft Punk", ID: "UCRr1xG_2WIDs18a6cIiCxeA"}}
	return []ytmusic.SearchResult{
		{Type: ytmusic.TypeArtist, Top: true, Artist: &ytmusic.ArtistRef{Name: "Daft Punk", ID: "UCRr1xG_2WIDs18a6cIiCxeA", Audience: "65.9M monthly audience"}},
		{Type: ytmusic.TypeSong, Track: &ytmusic.Track{
			VideoID: "-xFur30vvVc", Title: "Touch (2021 Epilogue) (feat. Paul Williams)",
			Artists: append(dp, ytmusic.Artist{Name: "Paul Williams"}), Album: &ytmusic.AlbumRef{Name: "Random Access Memories (10th Anniversary Edition)", ID: "MPREb_eMBjPmWySjR"},
			Duration: 3 * time.Minute, Available: true,
		}},
		{Type: ytmusic.TypeVideo, Track: &ytmusic.Track{VideoID: "4whD6uAryMs", Title: "Digital Love", Artists: dp, Duration: 4*time.Minute + 59*time.Second, IsVideo: true, Available: true}},
		{Type: ytmusic.TypeAlbum, Album: &ytmusic.AlbumRef{Name: "Discovery", ID: "MPREb_7ltM34kr0mH", Kind: "Album", Artists: dp, Year: "2001"}},
		{Type: ytmusic.TypePlaylist, Playlist: &ytmusic.PlaylistRef{Title: "Presenting Daft Punk", ID: "RDCLAK5uy_n20FRYQXNt1p1wS55Nj2r14IouO5weaYU", Author: "YouTube Music", Count: "35 songs"}},
	}
}

// fakeExtractor serves jars by browser name.
type fakeExtractor struct {
	jars map[string][]auth.Cookie
	err  map[string]error
}

func (f fakeExtractor) Extract(_ context.Context, browser string) ([]auth.Cookie, error) {
	if err := f.err[browser]; err != nil {
		return nil, err
	}
	if j, ok := f.jars[browser]; ok {
		return j, nil
	}
	return nil, errors.New("could not find " + browser + " cookies database")
}

const far = 4102444800

func signedInJar() []auth.Cookie {
	return []auth.Cookie{
		{Domain: ".youtube.com", Path: "/", Secure: true, Expires: far, Name: "__Secure-3PAPISID", Value: "good"},
		{Domain: ".youtube.com", Path: "/", Expires: far, Name: "ST-xyz", Value: "huge"},
		{Domain: ".google.com", Path: "/", Expires: far, Name: "NID", Value: "n"},
	}
}

var (
	jane = ytmusic.AccountInfo{Name: "Jane Doe", Handle: "@jane", ChannelID: "UCjane"}
	band = ytmusic.AccountInfo{Name: "Jane's Band", ChannelID: "UCband"}
)

// withFakes installs fake API/extractor factories for one test.
func withFakes(t *testing.T, w *fakeWorld, ex fakeExtractor) {
	t.Helper()
	prevYT, prevEx := YTMusicFactory, ExtractorFactory
	YTMusicFactory = func(o ytmusic.Options) ytmusic.Client { return fakeYT{creds: o.Credentials, world: w} }
	ExtractorFactory = func(*App) auth.Extractor { return ex }
	t.Cleanup(func() { YTMusicFactory, ExtractorFactory = prevYT, prevEx })
}

func saveSession(t *testing.T, cookie string) func(string) {
	return func(cfg string) {
		err := auth.Store{Dir: cfg}.Save(auth.Session{
			Credentials: ytmusic.Credentials{Cookie: cookie},
			Account:     auth.Account{Name: "Old Account", ChannelID: "UCold", Source: "firefox"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchPlain(t *testing.T) {
	withFakes(t, &fakeWorld{}, fakeExtractor{})
	r := run(t, runOpts{}, "search", "daft", "punk")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("code=%d stderr=%q", r.code, r.stderr)
	}
	assertNoANSI(t, r.stdout)
	golden(t, "search_plain.txt", r.stdout)

	r = run(t, runOpts{}, "search", "-f", "songs", "daft")
	golden(t, "search_songs_plain.txt", r.stdout)
}

func TestSearchStyled(t *testing.T) {
	withFakes(t, &fakeWorld{}, fakeExtractor{})
	r := run(t, runOpts{tty: true}, "search", "daft", "punk")
	if r.code != 0 || !strings.Contains(r.stdout, "\x1b[") {
		t.Fatalf("code=%d, styled output expected:\n%s", r.code, r.stdout)
	}
	golden(t, "search_styled.txt", r.stdout)

	// Transparent mode: no background SGR anywhere.
	r = run(t, runOpts{tty: true, config: "[theme]\ntransparent = true\n"}, "search", "daft", "punk")
	assertNoBackground(t, r.stdout)
}

func TestSearchJSON(t *testing.T) {
	withFakes(t, &fakeWorld{}, fakeExtractor{})
	r := run(t, runOpts{tty: true}, "search", "--json", "-l", "3", "daft", "punk")
	if r.code != 0 {
		t.Fatalf("code=%d stderr=%s", r.code, r.stderr)
	}
	assertNoANSI(t, r.stdout)
	var got []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("%v\n%s", err, r.stdout)
	}
	if len(got) != 3 || got[0]["type"] != "artist" || got[0]["top"] != true || got[1]["video_id"] != "-xFur30vvVc" || got[1]["duration_seconds"] != 180.0 {
		t.Errorf("got %v", got)
	}
	if strings.Count(r.stdout, "\n") != 1 {
		t.Error("JSON output is not one compact line")
	}

	r = run(t, runOpts{}, "search", "--json", "nothing")
	if r.code != 0 || strings.TrimSpace(r.stdout) != "[]" {
		t.Errorf("empty: code=%d out=%q", r.code, r.stdout)
	}
	r = run(t, runOpts{}, "search", "nothing")
	if r.code != 0 || !strings.Contains(r.stdout, "No results.") {
		t.Errorf("empty plain: %q", r.stdout)
	}
}

func TestSearchUsageErrors(t *testing.T) {
	withFakes(t, &fakeWorld{}, fakeExtractor{})
	for _, args := range [][]string{
		{"search"},
		{"search", "-f", "podcasts", "x"},
		{"search", "-l", "0", "x"},
	} {
		if r := run(t, runOpts{}, args...); r.code != ExitUsage {
			t.Errorf("%v: code=%d stderr=%s", args, r.code, r.stderr)
		}
	}
}

func TestSearchAPIErrors(t *testing.T) {
	cases := []struct {
		err  error
		code int
		msg  string
	}{
		{errors.New("dial tcp: lookup music.youtube.com: no such host"), ExitFailure, "could not reach YouTube Music"},
		{&ytmusic.HTTPError{Status: 429, RetryAfter: 30 * time.Second}, ExitFailure, "try again in 30s"},
		{&ytmusic.HTTPError{Status: 500}, ExitFailure, "HTTP 500"},
	}
	for _, c := range cases {
		withFakes(t, &fakeWorld{searchErr: c.err}, fakeExtractor{})
		r := run(t, runOpts{}, "search", "x")
		if r.code != c.code || !containsFold(r.stderr, c.msg) {
			t.Errorf("%v: code=%d stderr=%s", c.err, r.code, r.stderr)
		}
	}
}

func TestSearchExpiredSessionFallsBack(t *testing.T) {
	w := &fakeWorld{rejectAuth: true}
	withFakes(t, w, fakeExtractor{})
	r := run(t, runOpts{prep: saveSession(t, "__Secure-3PAPISID=stale")}, "search", "daft")
	if r.code != 0 {
		t.Fatalf("code=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "session has expired") || !strings.Contains(r.stderr, "ytm setup") {
		t.Errorf("stderr = %q", r.stderr)
	}
	if !strings.Contains(r.stdout, "Daft Punk") || len(w.queries) != 2 {
		t.Errorf("queries=%v stdout=%s", w.queries, r.stdout)
	}
}

func readSaved(t *testing.T, home string) auth.Session {
	t.Helper()
	s, err := auth.Store{Dir: filepath.Join(home, ".config", "ytm-go")}.Load()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSetupNonInteractiveBrowser(t *testing.T) {
	w := &fakeWorld{accounts: map[int]ytmusic.AccountInfo{0: jane, 2: band}}
	withFakes(t, w, fakeExtractor{jars: map[string][]auth.Cookie{"firefox": signedInJar()}})

	r := run(t, runOpts{}, "setup", "--browser", "firefox", "--slot", "2")
	if r.code != 0 {
		t.Fatalf("code=%d stderr=%s", r.code, r.stderr)
	}
	assertNoANSI(t, r.stdout)
	if !strings.Contains(r.stdout, "Signed in as Jane's Band. Saved to "+filepath.Join("$HOME", ".config", "ytm-go")) {
		t.Errorf("stdout = %q", r.stdout)
	}
	s := readSaved(t, r.home)
	if s.Credentials.AuthUser != 2 || s.Credentials.Cookie != "__Secure-3PAPISID=good" || s.Account.ChannelID != "UCband" || s.Account.Source != "firefox" {
		t.Errorf("saved %+v", s)
	}
	jar, err := os.ReadFile(filepath.Join(r.home, ".config", "ytm-go", auth.StreamCookiesFile))
	if err != nil || !strings.Contains(string(jar), "NID") || !strings.Contains(string(jar), "ST-xyz") {
		t.Errorf("stream jar: %v\n%s", err, jar)
	}

	// Several accounts and no --slot: refuse with the list.
	r = run(t, runOpts{}, "setup", "--browser", "firefox")
	if r.code != ExitUsage || !strings.Contains(r.stderr, "pass --slot") || !strings.Contains(r.stderr, "Jane Doe") {
		t.Errorf("multi: code=%d stderr=%s", r.code, r.stderr)
	}

	// Existing session: --yes required when not interactive.
	r = run(t, runOpts{prep: saveSession(t, "__Secure-3PAPISID=old")}, "setup", "--browser", "firefox", "--slot", "0")
	if r.code != ExitUsage || !strings.Contains(r.stderr, "--yes") {
		t.Errorf("existing: code=%d stderr=%s", r.code, r.stderr)
	}
	r = run(t, runOpts{prep: saveSession(t, "__Secure-3PAPISID=old")}, "setup", "--browser", "firefox", "--slot", "0", "--yes")
	if r.code != 0 || readSaved(t, r.home).Account.Name != "Jane Doe" {
		t.Errorf("--yes: code=%d stderr=%s", r.code, r.stderr)
	}
}

func TestSetupErrors(t *testing.T) {
	w := &fakeWorld{accounts: map[int]ytmusic.AccountInfo{0: jane}}
	signedOut := []auth.Cookie{{Domain: ".youtube.com", Name: "YSC", Value: "v", Expires: far}}
	withFakes(t, w, fakeExtractor{
		jars: map[string][]auth.Cookie{"firefox": signedOut, "brave": signedInJar()},
		err:  map[string]error{"chrome": &exec.Error{Name: "yt-dlp", Err: exec.ErrNotFound}},
	})
	cases := []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"setup"}, ExitUsage, "stdin is not a terminal; pass --browser/--slot or --manual < headers.txt"},
		{[]string{"setup", "--browser", "netscape"}, ExitUsage, "unknown browser"},
		{[]string{"setup", "--browser", "firefox", "--manual"}, ExitUsage, "mutually exclusive"},
		{[]string{"setup", "--slots", "0", "--browser", "firefox"}, ExitUsage, "--slots"},
		{[]string{"setup", "--browser", "firefox"}, ExitUnauth, "no signed-in YouTube session"},
		{[]string{"setup", "--browser", "chrome"}, ExitMissingDep, "yt-dlp is required"},
		{[]string{"setup", "--browser", "vivaldi"}, ExitFailure, "could not read cookies from Vivaldi"},
		{[]string{"setup", "--browser", "brave", "--slot", "4"}, ExitUnauth, "checked slot 4 only"},
	}
	for _, c := range cases {
		r := run(t, runOpts{}, c.args...)
		if r.code != c.code || !containsFold(r.stderr, c.msg) {
			t.Errorf("%v: code=%d stderr=%s", c.args, r.code, r.stderr)
		}
	}
}

func TestSetupManualFromStdin(t *testing.T) {
	w := &fakeWorld{accounts: map[int]ytmusic.AccountInfo{1: jane}}
	withFakes(t, w, fakeExtractor{})
	headers := "cookie\n__Secure-3PAPISID=good; PREF=x\nuser-agent\nUA/9\nx-goog-authuser\n1\n"
	r := run(t, runOpts{stdin: headers}, "setup", "--manual")
	if r.code != 0 {
		t.Fatalf("code=%d stderr=%s", r.code, r.stderr)
	}
	s := readSaved(t, r.home)
	if s.Credentials.AuthUser != 1 || s.Credentials.UserAgent != "UA/9" || s.Account.Source != "manual" {
		t.Errorf("saved %+v", s)
	}
	if !strings.Contains(r.stdout, "renewal is unavailable") {
		t.Errorf("stdout = %q", r.stdout)
	}
	r = run(t, runOpts{stdin: "user-agent: x\n"}, "setup", "--manual")
	if r.code != ExitUsage || !containsFold(r.stderr, "no Cookie header") {
		t.Errorf("no cookie: code=%d stderr=%s", r.code, r.stderr)
	}
	r = run(t, runOpts{stdin: "cookie: a=b\n"}, "setup", "--manual")
	if r.code != ExitUnauth {
		t.Errorf("signed out: code=%d stderr=%s", r.code, r.stderr)
	}
}

func TestSetupCookiesFileAndAutoDetect(t *testing.T) {
	w := &fakeWorld{accounts: map[int]ytmusic.AccountInfo{0: jane}}
	withFakes(t, w, fakeExtractor{jars: map[string][]auth.Cookie{"chrome": signedInJar()}})

	// [yt_dlp] cookies_file wins when no --browser is given.
	r := run(t, runOpts{
		config: "[yt_dlp]\ncookies_file = \"~/cookies.txt\"\n",
		prep: func(cfg string) {
			home := filepath.Dir(filepath.Dir(cfg))
			f, err := os.Create(filepath.Join(home, "cookies.txt"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := auth.WriteNetscape(f, signedInJar()); err != nil {
				t.Fatal(err)
			}
		},
	}, "setup")
	if r.code != 0 || readSaved(t, r.home).Account.Source != "cookies_file" {
		t.Fatalf("cookies_file: code=%d stderr=%s", r.code, r.stderr)
	}

	// Auto-detect walks the browser list (interactive picker → "Detect automatically").
	r = run(t, runOpts{tty: true, inTTY: true, env: []string{"ACCESSIBLE=1"}, stdin: "1\n"}, "setup")
	if r.code != 0 {
		t.Fatalf("auto: code=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "Found a YouTube session in Chrome") || readSaved(t, r.home).Account.Source != "chrome" {
		t.Errorf("auto: stderr=%s", r.stderr)
	}
}

func TestSetupInteractivePicksAccount(t *testing.T) {
	w := &fakeWorld{accounts: map[int]ytmusic.AccountInfo{0: jane, 1: band}}
	withFakes(t, w, fakeExtractor{jars: map[string][]auth.Cookie{"firefox": signedInJar()}})
	// Accessible mode reads numbered choices from stdin: source 2 (Firefox),
	// then account 2 (Jane's Band), then confirm replacing the old session.
	r := run(t, runOpts{
		tty: true, inTTY: true, env: []string{"ACCESSIBLE=1"}, stdin: "2\n2\ny\n",
		prep: saveSession(t, "__Secure-3PAPISID=old"),
	}, "setup")
	if r.code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	s := readSaved(t, r.home)
	if s.Account.Name != "Jane's Band" || s.Credentials.AuthUser != 1 {
		t.Errorf("saved %+v", s.Account)
	}
	if !strings.Contains(r.stderr, "Accounts found") || !strings.Contains(r.stderr, "Old Account") {
		t.Errorf("prompts missing from stderr:\n%s", r.stderr)
	}

	// Declining the replacement keeps the old session.
	r = run(t, runOpts{
		tty: true, inTTY: true, env: []string{"ACCESSIBLE=1"}, stdin: "2\n1\nn\n",
		prep: saveSession(t, "__Secure-3PAPISID=old"),
	}, "setup")
	if r.code != 0 || readSaved(t, r.home).Account.Name != "Old Account" {
		t.Errorf("decline: code=%d stderr=%s", r.code, r.stderr)
	}
}

func assertNoANSI(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "\x1b[") {
		t.Errorf("unexpected escape codes:\n%q", s)
	}
}

func assertNoBackground(t *testing.T, s string) {
	t.Helper()
	if bg := style.BackgroundSGRs(s); len(bg) > 0 {
		t.Errorf("background SGRs in transparent mode: %v", bg)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
