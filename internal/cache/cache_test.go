package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/store"
	"github.com/Franciss-Prog/go-ytm/internal/stream"
)

// fakeDL writes size bytes as dir/<id>.webm, like yt-dlp would.
type fakeDL struct {
	size  int
	err   error
	calls int
	block chan struct{}
}

func (f *fakeDL) Download(_ context.Context, id, dir string) (string, error) {
	f.calls++
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return "", f.err
	}
	p := filepath.Join(dir, id+".webm")
	return p, os.WriteFile(p, make([]byte, f.size), 0o600)
}

func newCache(t *testing.T, maxBytes int64, dl Downloader) *Cache {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Cache{Dir: filepath.Join(t.TempDir(), "audio"), Max: maxBytes, Index: db, DL: dl}
}

func TestDownloadLookupEvict(t *testing.T) {
	ctx := context.Background()
	dl := &fakeDL{size: 40}
	c := newCache(t, 100, dl)

	r, err := c.Download(ctx, "aaaaaaaaaaa")
	if err != nil || r.Already || r.Evicted || filepath.Dir(r.Path) != c.Dir {
		t.Fatalf("first download: %+v %v", r, err)
	}
	if r, _ := c.Download(ctx, "aaaaaaaaaaa"); !r.Already || dl.calls != 1 {
		t.Errorf("second download ran yt-dlp again: %+v calls=%d", r, dl.calls)
	}
	time.Sleep(2 * time.Millisecond) // distinct last_accessed
	_, _ = c.Download(ctx, "bbbbbbbbbbb")
	time.Sleep(2 * time.Millisecond)
	c.Lookup(ctx, "aaaaaaaaaaa") // a is now newer than b
	time.Sleep(2 * time.Millisecond)
	_, _ = c.Download(ctx, "ccccccccccc") // 120 > 100: evict b (LRU)

	if _, ok := c.Lookup(ctx, "bbbbbbbbbbb"); ok {
		t.Error("LRU entry b survived eviction")
	}
	for _, id := range []string{"aaaaaaaaaaa", "ccccccccccc"} {
		if _, ok := c.Lookup(ctx, id); !ok {
			t.Errorf("%s evicted", id)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(c.Dir, ".*")); len(left) != 0 {
		t.Errorf("staging dirs left: %v", left)
	}
}

func TestDownloadTooBigIsEvicted(t *testing.T) {
	c := newCache(t, 10, &fakeDL{size: 50})
	r, err := c.Download(context.Background(), "aaaaaaaaaaa")
	if err != nil || !r.Evicted {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(r.Path); !errors.Is(err, os.ErrNotExist) {
		t.Error("oversized file kept")
	}
}

func TestDownloadErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := newCache(t, 0, &fakeDL{}).Download(ctx, "aaaaaaaaaaa"); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled: %v", err)
	}
	if _, err := newCache(t, 100, &fakeDL{}).Download(ctx, "-rf /"); !errors.Is(err, stream.ErrInvalidID) {
		t.Errorf("bad id: %v", err)
	}
	boom := errors.New("boom")
	c := newCache(t, 100, &fakeDL{err: boom})
	if _, err := c.Download(ctx, "aaaaaaaaaaa"); !errors.Is(err, boom) {
		t.Errorf("downloader error: %v", err)
	}
	if left, _ := os.ReadDir(c.Dir); len(left) != 0 {
		t.Errorf("failed download left files: %v", left)
	}

	dl := &fakeDL{size: 1, block: make(chan struct{})}
	c = newCache(t, 100, dl)
	done := make(chan error)
	go func() { _, err := c.Download(ctx, "aaaaaaaaaaa"); done <- err }()
	for {
		c.mu.Lock()
		busy := c.active["aaaaaaaaaaa"]
		c.mu.Unlock()
		if busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.Download(ctx, "aaaaaaaaaaa"); !errors.Is(err, ErrBusy) {
		t.Errorf("concurrent: %v", err)
	}
	close(dl.block)
	if err := <-done; err != nil {
		t.Error(err)
	}
}

func TestLookupDropsMissingFile(t *testing.T) {
	ctx := context.Background()
	c := newCache(t, 100, &fakeDL{size: 1})
	r, _ := c.Download(ctx, "aaaaaaaaaaa")
	_ = os.Remove(r.Path)
	if _, ok := c.Lookup(ctx, "aaaaaaaaaaa"); ok {
		t.Fatal("missing file reported cached")
	}
	if _, ok, _ := c.Index.CacheGet(ctx, "aaaaaaaaaaa"); ok {
		t.Error("stale index entry kept")
	}
}

type nextResolver struct{ calls int }

func (n *nextResolver) Resolve(context.Context, string, stream.Quality) (stream.Stream, error) {
	n.calls++
	return stream.Stream{URL: "https://remote"}, nil
}

func TestResolverPrefersLocal(t *testing.T) {
	ctx := context.Background()
	c := newCache(t, 100, &fakeDL{size: 1})
	r, _ := c.Download(ctx, "aaaaaaaaaaa")
	next := &nextResolver{}
	res := Resolver{Cache: c, Next: next}
	s, err := res.Resolve(ctx, "aaaaaaaaaaa", stream.High)
	if err != nil || s.URL != r.Path || s.Codec != "webm" || !s.Fresh(time.Now()) || next.calls != 0 {
		t.Errorf("local: %+v %v calls=%d", s, err, next.calls)
	}
	if s, _ := res.Resolve(ctx, "bbbbbbbbbbb", stream.High); s.URL != "https://remote" || next.calls != 1 {
		t.Errorf("fallthrough: %+v", s)
	}
}
