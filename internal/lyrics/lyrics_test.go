package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const ms = time.Millisecond

func TestParseLRC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Line
	}{
		{"basic", "[00:01.50]one\r\n[00:03.00] two ", []Line{{1500 * ms, "one"}, {3 * time.Second, "two"}}},
		{"fraction digits", "[00:01.5]a\n[00:01.05]b\n[00:01.005]c\n[00:02]d", []Line{{1005 * ms, "c"}, {1050 * ms, "b"}, {1500 * ms, "a"}, {2 * time.Second, "d"}}},
		{"repeated stamps", "[00:10.00][00:01.00]chorus\n[00:05.00]verse", []Line{{time.Second, "chorus"}, {5 * time.Second, "verse"}, {10 * time.Second, "chorus"}}},
		{"tags and junk dropped", "[ti:Song]\n[ar:Someone]\nno stamp\n\n[00:01.00]x", []Line{{time.Second, "x"}}},
		{"empty text kept", "[00:01.00]\n[00:02.00]y", []Line{{time.Second, ""}, {2 * time.Second, "y"}}},
		{"offset earlier, clamped", "[offset:+1500]\n[00:01.00]a\n[00:04.00]b", []Line{{0, "a"}, {2500 * ms, "b"}}},
		{"negative offset", "[offset:-500]\n[00:01.00]a", []Line{{1500 * ms, "a"}}},
		{"colon fraction, long minutes", "[100:00:20]a", []Line{{100*time.Minute + 200*ms, "a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLRC(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLRC = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIndex(t *testing.T) {
	l := &Lyrics{Synced: true, Lines: []Line{{time.Second, "a"}, {3 * time.Second, "b"}, {3 * time.Second, "b2"}, {5 * time.Second, "c"}}}
	for pos, want := range map[time.Duration]int{0: -1, 999 * ms: -1, time.Second: 0, 2 * time.Second: 0, 3 * time.Second: 2, time.Hour: 3} {
		if got := l.Index(pos); got != want {
			t.Errorf("Index(%v) = %d, want %d", pos, got, want)
		}
	}
	if (&Lyrics{Lines: l.Lines}).Index(time.Hour) != -1 || (*Lyrics)(nil).Index(0) != -1 {
		t.Error("unsynced/nil lyrics must have no current line")
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct{ title, artist, want string }{
		{"Get Lucky (Official Video)", "Daft Punk", "Get Lucky"},
		{"Get Lucky [Official Music Video] (feat. Pharrell Williams)", "", "Get Lucky"},
		{"Song (ft. A (B remix)) [HD]", "", "Song"},
		{"Here Comes the Sun (Remastered 2009)", "", "Here Comes the Sun"},
		{"Track (Someone Remix)", "", "Track"},
		{"Track (Live at Wembley)", "", "Track"},
		{"Daft Punk - Get Lucky (Audio)", "daft punk", "Get Lucky"},
		{"Official Video", "", "Official Video"}, // bare words kept
		{"(Official Video)", "", "(Official Video)"},
		{"Love (Part 2)", "", "Love (Part 2)"},
	}
	for _, tt := range tests {
		if got := SanitizeTitle(tt.title, tt.artist); got != tt.want {
			t.Errorf("SanitizeTitle(%q, %q) = %q, want %q", tt.title, tt.artist, got, tt.want)
		}
	}
}

func TestLRCLIBGet(t *testing.T) {
	var gotQuery, gotUA string
	status, retry, ctype, body := 200, "", "application/json", `{"instrumental":false,"plainLyrics":"a","syncedLyrics":"[00:01.00]a"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotUA = r.URL.RawQuery, r.UserAgent()
		if retry != "" {
			w.Header().Set("Retry-After", retry)
		}
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	clk := clock.NewFake(time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC))
	c := &LRCLIB{BaseURL: srv.URL, UserAgent: "ytm-go/test (x)", Clock: clk}
	ctx := context.Background()

	r, err := c.Get(ctx, "Get Lucky", "Daft Punk", "RAM", 369600*ms)
	if err != nil || r == nil || *r.SyncedLyrics != "[00:01.00]a" {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	if gotQuery != "album_name=RAM&artist_name=Daft+Punk&duration=369&track_name=Get+Lucky" || gotUA != "ytm-go/test (x)" {
		t.Errorf("query %q, UA %q", gotQuery, gotUA)
	}
	if _, _ = c.Get(ctx, "a", "b", "", 0); gotQuery != "artist_name=b&track_name=a" {
		t.Errorf("no duration/album: query %q", gotQuery)
	}

	status, body = 404, `{"message":"Failed to find specified track","name":"TrackNotFound","statusCode":404}`
	if r, err := c.Get(ctx, "a", "b", "", 0); r != nil || err != nil {
		t.Errorf("404 = %+v, %v; want nil, nil", r, err)
	}

	status, ctype, body = 200, "text/plain; charset=utf-8", "Failed to deserialize query string"
	if _, err := c.Get(ctx, "a", "b", "", 0); err == nil {
		t.Error("plain-text reply must be an error")
	}

	status, retry, ctype = 429, "30", "application/json"
	if _, err := c.Get(ctx, "a", "b", "", 0); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("429 err = %v", err)
	}
	status, retry, body = 200, "", `{}`
	gotQuery = ""
	clk.Advance(29 * time.Second)
	if _, err := c.Get(ctx, "a", "b", "", 0); !errors.Is(err, ErrRateLimited) || gotQuery != "" {
		t.Errorf("before Retry-After: err %v, sent %q", err, gotQuery)
	}
	clk.Advance(2 * time.Second)
	if _, err := c.Get(ctx, "a", "b", "", 0); err != nil {
		t.Errorf("after Retry-After: %v", err)
	}
}

type fakeYT struct {
	lyrics   *ytmusic.Lyrics
	err      error
	watchErr error
	gotID    string
}

func (f *fakeYT) WatchPlaylist(_ context.Context, r ytmusic.WatchRequest) (ytmusic.WatchPlaylist, error) {
	return ytmusic.WatchPlaylist{LyricsID: "MPLY" + r.VideoID}, f.watchErr
}

func (f *fakeYT) Lyrics(_ context.Context, id string) (*ytmusic.Lyrics, error) {
	f.gotID = id
	return f.lyrics, f.err
}

type fakeLRCLIB struct {
	rec   *Record
	err   error
	title string
	calls int
}

func (f *fakeLRCLIB) Get(_ context.Context, title, _, _ string, _ time.Duration) (*Record, error) {
	f.calls++
	f.title = title
	return f.rec, f.err
}

func ptr(s string) *string { return &s }

func TestFind(t *testing.T) {
	ytTimed := &ytmusic.Lyrics{Timed: true, Source: "Source: LyricFind", Lines: []ytmusic.LyricLine{{Start: time.Second, Text: "yt"}}}
	ytPlain := &ytmusic.Lyrics{Source: "Source: Musixmatch", Lines: []ytmusic.LyricLine{{Text: "yt plain"}}}
	synced := &Record{SyncedLyrics: ptr("[00:02.00]lrc"), PlainLyrics: ptr("lrc plain")}
	plainOnly := &Record{PlainLyrics: ptr("lrc plain\r\nline 2")}
	down := errors.New("down")
	tests := []struct {
		name       string
		yt         *fakeYT
		lrc        *fakeLRCLIB
		wantSource string
		wantFirst  string
		wantSynced bool
		wantErr    bool
		wantLRC    int
	}{
		{"youtube timed wins", &fakeYT{lyrics: ytTimed}, &fakeLRCLIB{rec: synced}, "YouTube Music · LyricFind", "yt", true, false, 0},
		{"lrclib synced beats youtube plain", &fakeYT{lyrics: ytPlain}, &fakeLRCLIB{rec: synced}, "LRCLIB", "lrc", true, false, 1},
		{"youtube plain beats lrclib plain", &fakeYT{lyrics: ytPlain}, &fakeLRCLIB{rec: plainOnly}, "YouTube Music · Musixmatch", "yt plain", false, false, 1},
		{"lrclib plain last", &fakeYT{}, &fakeLRCLIB{rec: plainOnly}, "LRCLIB", "lrc plain", false, false, 1},
		{"youtube down, lrclib ok", &fakeYT{watchErr: down}, &fakeLRCLIB{rec: synced}, "LRCLIB", "lrc", true, false, 1},
		{"nothing", &fakeYT{}, &fakeLRCLIB{}, "", "", false, false, 1},
		{"instrumental is nothing", &fakeYT{}, &fakeLRCLIB{rec: &Record{Instrumental: true}}, "", "", false, false, 1},
		{"both down", &fakeYT{err: down}, &fakeLRCLIB{err: down}, "", "", false, true, 1},
		{"lrclib down, youtube plain ok", &fakeYT{lyrics: ytPlain}, &fakeLRCLIB{err: down}, "YouTube Music · Musixmatch", "yt plain", false, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Finder{YT: tt.yt, LRCLIB: tt.lrc}
			l, err := f.Find(context.Background(), Track{VideoID: "v1", Title: "Song (Official Video)", Artist: "A", Duration: time.Minute})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tt.wantFirst == "" {
				if l != nil {
					t.Fatalf("lyrics = %+v, want nil", l)
				}
			} else if l == nil || l.Source != tt.wantSource || l.Lines[0].Text != tt.wantFirst || l.Synced != tt.wantSynced {
				t.Fatalf("lyrics = %+v", l)
			}
			if tt.lrc.calls != tt.wantLRC || (tt.wantLRC > 0 && tt.lrc.title != "Song") {
				t.Errorf("lrclib calls %d title %q", tt.lrc.calls, tt.lrc.title)
			}
		})
	}
	yt := &fakeYT{lyrics: ytTimed}
	if _, _ = (Finder{YT: yt}).Find(context.Background(), Track{VideoID: "v9"}); yt.gotID != "MPLYv9" {
		t.Errorf("lyrics id %q", yt.gotID)
	}
}
