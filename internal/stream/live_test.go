//go:build live

package stream

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
	"github.com/Franciss-Prog/go-ytm/internal/player"
)

// Live tests against YouTube with the installed yt-dlp (and mpv, if
// present). Not run in CI:
//
//	YTM_LIVE=1 go test -tags live -run Live -v ./internal/stream
const (
	liveA = "dQw4w9WgXcQ" // public, long-lived
	liveB = "9bZkp7q19f0"
)

func liveOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("YTM_LIVE") != "1" {
		t.Skip("set YTM_LIVE=1")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
}

func TestLiveResolve(t *testing.T) {
	liveOnly(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := NewCache(YtDlp{}, nil, nil)
	defer c.Close(context.Background()) //nolint:errcheck
	hc := &http.Client{}
	defer hc.CloseIdleConnections()
	for _, q := range []Quality{High, Medium, Low} {
		start := time.Now()
		s, err := c.Resolve(ctx, liveA, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		t.Logf("%s: %s %d kbit/s, expires in %v, took %v", q, s.Codec, s.Bitrate, time.Until(s.Expires).Round(time.Minute), time.Since(start).Round(time.Millisecond))
		if !s.Fresh(time.Now()) || time.Until(s.Expires) < time.Hour {
			t.Errorf("%s: expiry %v", q, s.Expires)
		}
		if err := s.Wait(ctx, clock.Real{}); err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
		req.Header.Set("Range", "bytes=0-1023")
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			t.Errorf("%s: googlevideo answered %s", q, resp.Status)
		}
	}
	if _, err := c.Resolve(ctx, "aaaaaaaaaaa", High); err == nil {
		t.Error("bogus id resolved")
	} else {
		t.Logf("bogus id: %v", err)
	}
}

// TestLiveGapless plays a resolved track in real mpv, appends the
// prefetched next one and skips into it.
func TestLiveGapless(t *testing.T) {
	liveOnly(t)
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := NewCache(YtDlp{}, nil, nil)
	defer c.Close(context.Background()) //nolint:errcheck

	results := make(chan Result, 2)
	p := NewPrefetcher(c, func(r Result) { results <- r })
	defer p.Close()

	first, err := c.Playable(ctx, liveA, Low)
	if err != nil {
		t.Fatal(err)
	}
	p.Want(Low, liveB)

	dir := t.TempDir()
	pl := player.Start(ctx, player.Options{
		Launcher: &player.MPVLauncher{RuntimeDir: dir, ExtraArgs: []string{"--ao=null"}},
		Volume:   10,
		Log:      slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccancel()
		_ = pl.Close(cctx)
	}()
	started := make(chan string, 4)
	go func() {
		for ev := range pl.Events() {
			if ts, ok := ev.(player.TrackStarted); ok {
				started <- ts.Item.ID
			}
		}
	}()

	if err := pl.Load(ctx, player.Item{ID: liveA, URL: first.URL}, 200*time.Second); err != nil {
		t.Fatal(err)
	}
	expect := func(id string) {
		t.Helper()
		select {
		case got := <-started:
			if got != id {
				t.Fatalf("started %s, want %s", got, id)
			}
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s (state %+v)", id, pl.State())
		}
	}
	expect(liveA)
	r := <-results
	if r.Err != nil {
		t.Fatalf("prefetch: %v", r.Err)
	}
	if err := r.Stream.Wait(ctx, clock.Real{}); err != nil {
		t.Fatal(err)
	}
	if err := pl.Append(player.Item{ID: liveB, URL: r.Stream.URL}); err != nil {
		t.Fatal(err)
	}
	// liveA is 213 s long and starts at 200 s: mpv advances on its own.
	expect(liveB)
	t.Logf("gapless advance ok; state %+v", pl.State().Status)
}
