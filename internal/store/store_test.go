package store

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	clock := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	return d
}

func track(id string) ytmusic.Track {
	return ytmusic.Track{VideoID: id, Title: "T " + id, Artists: []ytmusic.Artist{{Name: "A"}}, Duration: 3 * time.Minute, Available: true}
}

func ids(ts []ytmusic.Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.VideoID
	}
	return out
}

func TestRecentPlaysDedupNewestFirst(t *testing.T) {
	d, ctx := openTest(t), context.Background()
	for _, id := range []string{"a", "b", "a", "c"} {
		if err := d.LogPlay(ctx, track(id), 30*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.RecentPlays(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"c", "a", "b"}; !slices.Equal(ids(got), want) {
		t.Fatalf("got %v, want %v", ids(got), want)
	}
	if got[0].Duration != 3*time.Minute || got[0].Artists[0].Name != "A" || !got[0].Available {
		t.Errorf("track not round-tripped: %+v", got[0])
	}
	if got, _ := d.RecentPlays(ctx, 1); len(got) != 1 {
		t.Errorf("limit: %d rows", len(got))
	}
}

func TestPlayHistoryTrimmed(t *testing.T) {
	d, ctx := openTest(t), context.Background()
	d.maxPlays = 5
	for i := range d.maxPlays + 3 {
		if err := d.LogPlay(ctx, track(string(rune('a'+i%26))), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM play_history`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != d.maxPlays {
		t.Errorf("rows = %d, want %d", n, d.maxPlays)
	}
}

func TestRecentSearches(t *testing.T) {
	d, ctx := openTest(t), context.Background()
	for _, s := range [][2]string{{"daft punk", "songs"}, {"muse", "all"}, {"daft punk", "albums"}} {
		if err := d.LogSearch(ctx, s[0], s[1]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.RecentSearches(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"daft punk", "muse"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCacheIndexLRU(t *testing.T) {
	d, ctx := openTest(t), context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := d.CachePut(ctx, CacheEntry{VideoID: id, Path: "/c/" + id + ".webm", Size: 10}); err != nil {
			t.Fatal(err)
		}
	}
	if e, ok, err := d.CacheGet(ctx, "a"); err != nil || !ok || e.Path != "/c/a.webm" || e.Size != 10 {
		t.Fatalf("get a: %+v %v %v", e, ok, err)
	}
	if _, ok, _ := d.CacheGet(ctx, "zz"); ok {
		t.Error("missing id found")
	}
	es, err := d.CacheEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range es {
		order = append(order, e.VideoID)
	}
	if want := []string{"b", "c", "a"}; !slices.Equal(order, want) {
		t.Errorf("LRU order %v, want %v", order, want)
	}
	if err := d.CacheDelete(ctx, "b", "c"); err != nil {
		t.Fatal(err)
	}
	if es, _ := d.CacheEntries(ctx); len(es) != 1 {
		t.Errorf("after delete: %v", es)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := LoadSession(dir); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	in := Session{
		Volume: 150, Repeat: "all", Shuffle: true, Tracks: []ytmusic.Track{track("a"), track("b")}, Index: 1,
		Context: "PL1", Resume: &Resume{VideoID: "b", Position: 42.5},
	}
	if err := SaveSession(dir, in); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadSession(dir)
	if !ok || err != nil {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.Volume != 100 || got.Repeat != "all" || !got.Shuffle || got.Index != 1 || got.Context != "PL1" ||
		len(got.Tracks) != 2 || got.Tracks[1].Duration != 3*time.Minute || got.Resume.ResumeAt() != 42500*time.Millisecond {
		t.Errorf("round trip: %+v", got)
	}
	if fi, _ := os.Stat(filepath.Join(dir, SessionFile)); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("mode %v", fi.Mode())
	}
}

func TestSessionRejected(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":       "{",
		"other version": `{"schema_version":2,"volume":50}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, SessionFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := LoadSession(dir); ok || err == nil {
				t.Errorf("ok=%v err=%v", ok, err)
			}
		})
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, SessionFile), []byte(`{"schema_version":1,"queue_index":9,"queue_tracks":[]}`), 0o600)
	if s, ok, _ := LoadSession(dir); !ok || s.Index != -1 {
		t.Errorf("out-of-range index kept: %+v", s)
	}
}

func TestShufflePrefs(t *testing.T) {
	dir := t.TempDir()
	p, err := LoadShufflePrefs(dir)
	if err != nil || p.Locked("PL1") {
		t.Fatalf("fresh: %v %v", err, p.Locked("PL1"))
	}
	p.Set("PL1", true)
	p.Set("PL2", true)
	p.Set("PL2", false)
	if !p.Locked("PL1") || p.Locked("PL2") {
		t.Fatal("Set not visible")
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	q, err := LoadShufflePrefs(dir)
	if err != nil || !q.Locked("PL1") || q.Locked("PL2") || q.Locked("") {
		t.Errorf("reload: err=%v PL1=%v PL2=%v", err, q.Locked("PL1"), q.Locked("PL2"))
	}
	_ = os.WriteFile(filepath.Join(dir, ShufflePrefsFile), []byte("nope"), 0o600)
	if r, err := LoadShufflePrefs(dir); err == nil || r.Locked("PL1") {
		t.Error("broken file not reported")
	}
}
