// Package art fetches, decodes and scales cover art, and encodes it for the
// kitty graphics protocol (docs/specs/09-ytkew-look.md, "Cover art").
// Drawing it as terminal cells (half blocks, kitty placeholders) is styling
// and lives in internal/style.
package art

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // decoder: YouTube art is requested as JPEG
	_ "image/png"  // decoder: some uploads are PNG
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// MaxBytes bounds a downloaded image.
const MaxBytes = 4 << 20

// lh3Size matches the size/format suffix of a googleusercontent URL
// ("=w120-h120-p-l90-rj").
var lh3Size = regexp.MustCompile(`=[a-zA-Z0-9-]+$`)

// ytimgVideo matches i.ytimg.com video thumbnails, capturing the video id.
var ytimgVideo = regexp.MustCompile(`^https://i\.ytimg\.com/vi/([A-Za-z0-9_-]{11})/`)

// URL returns the URL to fetch for a thumbnail at about px pixels square,
// as JPEG: googleusercontent art gets "=w{px}-h{px}-l90-rj" (the "rj"
// suffix selects JPEG; "rw" would be WebP, verified live 2026-11-17),
// video thumbnails become hqdefault.jpg. Other URLs are returned as is;
// "" stays "".
func URL(thumb string, px int) string {
	switch {
	case thumb == "":
		return ""
	case strings.Contains(thumb, ".googleusercontent.com/"):
		return lh3Size.ReplaceAllString(thumb, "") + fmt.Sprintf("=w%d-h%d-l90-rj", px, px)
	}
	if m := ytimgVideo.FindStringSubmatch(thumb); m != nil {
		return "https://i.ytimg.com/vi/" + m[1] + "/hqdefault.jpg"
	}
	return thumb
}

// Fetcher downloads and decodes an image. Implementations must honor ctx.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (image.Image, error)
}

// HTTPFetcher fetches over HTTP.
type HTTPFetcher struct {
	Client *http.Client // nil: http.DefaultClient
}

// ErrTooLarge is returned for images over MaxBytes.
var ErrTooLarge = errors.New("art: image too large")

// Fetch implements Fetcher.
func (f HTTPFetcher) Fetch(ctx context.Context, url string) (image.Image, error) {
	c := f.Client
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("art: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("art: fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("art: fetch: HTTP %d", resp.StatusCode)
	}
	body := io.LimitReader(resp.Body, MaxBytes+1)
	lr := &countingReader{r: body}
	img, _, err := image.Decode(lr)
	if lr.n > MaxBytes {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, fmt.Errorf("art: decode: %w", err)
	}
	return img, nil
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// Square crops img to its centered square (video thumbnails are 4:3 or
// 16:9 with bars; album art is already square).
func Square(img image.Image) image.Image {
	b := img.Bounds()
	s := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-s)/2, b.Min.Y+(b.Dy()-s)/2
	dst := image.NewRGBA(image.Rect(0, 0, s, s))
	draw.Draw(dst, dst.Bounds(), img, image.Pt(x0, y0), draw.Src)
	return dst
}

// Scale resizes img to w×h with a box filter (each output pixel averages
// the source pixels it covers). Good enough for terminal cells, no
// dependency.
func Scale(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 0), max(h, 0)))
	b := img.Bounds()
	if w <= 0 || h <= 0 || b.Empty() {
		return dst
	}
	for y := range h {
		sy0 := b.Min.Y + y*b.Dy()/h
		sy1 := max(b.Min.Y+(y+1)*b.Dy()/h, sy0+1)
		for x := range w {
			sx0 := b.Min.X + x*b.Dx()/w
			sx1 := max(b.Min.X+(x+1)*b.Dx()/w, sx0+1)
			var r, g, bl, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 0xff})
		}
	}
	return dst
}

// Cache keeps the last few decoded, square images by URL (LRU).
type Cache struct {
	mu    sync.Mutex
	size  int
	order []string
	m     map[string]image.Image
}

// NewCache returns a cache holding up to size images.
func NewCache(size int) *Cache { return &Cache{size: max(size, 1), m: map[string]image.Image{}} }

// Get returns the cached image for url.
func (c *Cache) Get(url string) (image.Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	img, ok := c.m[url]
	if ok {
		c.touch(url)
	}
	return img, ok
}

// Put stores img for url, evicting the least recently used entry.
func (c *Cache) Put(url string, img image.Image) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[url]; !ok && len(c.order) >= c.size {
		delete(c.m, c.order[0])
		c.order = c.order[1:]
	}
	c.m[url] = img
	c.touch(url)
}

func (c *Cache) touch(url string) {
	for i, u := range c.order {
		if u == url {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, url)
}
