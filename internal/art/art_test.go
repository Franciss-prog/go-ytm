package art

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi/kitty"
)

func TestURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"https://lh3.googleusercontent.com/abc=w120-h120-p-l90-rj", "https://lh3.googleusercontent.com/abc=w400-h400-l90-rj"},
		{"https://lh3.googleusercontent.com/abc", "https://lh3.googleusercontent.com/abc=w400-h400-l90-rj"},
		{"https://yt3.googleusercontent.com/x=s120", "https://yt3.googleusercontent.com/x=w400-h400-l90-rj"},
		{"https://i.ytimg.com/vi/34Na4j8AVgA/sddefault.jpg?sqp=abc&rs=x", "https://i.ytimg.com/vi/34Na4j8AVgA/hqdefault.jpg"},
		{"https://example.com/a.png", "https://example.com/a.png"},
	}
	for _, tt := range tests {
		if got := URL(tt.in, 400); got != tt.want {
			t.Errorf("URL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestSquareCropsCenter(t *testing.T) {
	img := solid(160, 90, color.RGBA{0, 0, 0, 255})
	for y := range 90 { // a red band in the middle 90 columns
		for x := 35; x < 125; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	sq := Square(img)
	if b := sq.Bounds(); b.Dx() != 90 || b.Dy() != 90 {
		t.Fatalf("bounds %v", b)
	}
	if r, _, _, _ := sq.At(0, 0).RGBA(); r>>8 != 255 {
		t.Error("crop not centered")
	}
}

func TestScaleAverages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	img.SetRGBA(1, 0, color.RGBA{255, 0, 0, 255})
	img.SetRGBA(0, 1, color.RGBA{0, 0, 255, 255})
	img.SetRGBA(1, 1, color.RGBA{0, 0, 255, 255})
	px := Scale(img, 1, 1).RGBAAt(0, 0)
	if px.R < 126 || px.R > 128 || px.B < 126 || px.B > 128 {
		t.Errorf("average %v", px)
	}
	if got := Scale(solid(400, 400, color.RGBA{9, 8, 7, 255}), 30, 15); got.Bounds().Dx() != 30 || got.RGBAAt(29, 14) != (color.RGBA{9, 8, 7, 255}) {
		t.Errorf("downscale %v %v", got.Bounds(), got.RGBAAt(29, 14))
	}
	if Scale(img, 0, 3).Bounds().Dx() != 0 {
		t.Error("zero width")
	}
}

func TestCacheEvictsLeastRecent(t *testing.T) {
	c := NewCache(2)
	a, b, d := solid(1, 1, color.RGBA{}), solid(2, 2, color.RGBA{}), solid(3, 3, color.RGBA{})
	c.Put("a", a)
	c.Put("b", b)
	c.Get("a") // a is now the most recent
	c.Put("d", d)
	if _, ok := c.Get("b"); ok {
		t.Error("b should be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("a evicted")
	}
}

func TestHTTPFetcher(t *testing.T) {
	var jpg, pngb bytes.Buffer
	if err := jpeg.Encode(&jpg, solid(8, 8, color.RGBA{200, 10, 10, 255}), nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngb, solid(4, 4, color.RGBA{1, 2, 3, 255})); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jpg":
			w.Write(jpg.Bytes()) //nolint:errcheck
		case "/a.png":
			w.Write(pngb.Bytes()) //nolint:errcheck
		case "/big":
			w.Write(bytes.Repeat([]byte{0xff}, MaxBytes+10)) //nolint:errcheck
		case "/junk":
			w.Write([]byte("not an image")) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := HTTPFetcher{Client: srv.Client()}
	ctx := context.Background()
	for _, p := range []string{"/a.jpg", "/a.png"} {
		if img, err := f.Fetch(ctx, srv.URL+p); err != nil || img.Bounds().Dx() == 0 {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := f.Fetch(ctx, srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}
	if _, err := f.Fetch(ctx, srv.URL+"/junk"); err == nil {
		t.Error("junk decoded")
	}
	if _, err := f.Fetch(ctx, srv.URL+"/big"); err == nil {
		t.Error("oversized image accepted")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Fetch(cctx, srv.URL+"/a.jpg"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestKittyTransmit(t *testing.T) {
	seq, err := Transmit(7, solid(320, 320, color.RGBA{50, 100, 150, 255}), 12, 6)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(seq, "\x1b_G") || !strings.HasSuffix(seq, "\x1b\\") {
		t.Fatalf("not an APC sequence: %q…", seq[:min(40, len(seq))])
	}
	first, _, _ := strings.Cut(seq, ";")
	for _, opt := range []string{"a=T", "f=100", "i=7", "U=1", "c=12", "r=6", "q=2"} {
		if !strings.Contains(first, opt) {
			t.Errorf("first chunk lacks %s: %q", opt, first)
		}
	}
	// Chunked: every chunk ≤ 4096 payload bytes, m=1 … m=0.
	chunks := strings.Split(strings.TrimSuffix(seq, "\x1b\\"), "\x1b\\")
	if len(chunks) > 1 && (!strings.Contains(chunks[0], "m=1") || !strings.Contains(chunks[len(chunks)-1], "m=0")) {
		t.Errorf("chunk flags: %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if _, payload, _ := strings.Cut(c, ";"); len(payload) > kitty.MaxChunkSize {
			t.Errorf("chunk %d payload %d", i, len(payload))
		}
	}
	if d := Delete(7); d != "\x1b_Ga=d,d=I,i=7,q=2\x1b\\" {
		t.Errorf("delete %q", d)
	}
}

func TestPlaceholdersAndQuery(t *testing.T) {
	row := PlaceholderRow(2, 3)
	if utf8.RuneCountInString(row) != 9 || []rune(row)[0] != kitty.Placeholder ||
		[]rune(row)[1] != kitty.Diacritic(2) || []rune(row)[8] != kitty.Diacritic(2) {
		t.Errorf("row %q", row)
	}
	if r, g, b := IDColor(0x0a0b0c); r != 10 || g != 11 || b != 12 {
		t.Errorf("id color %d %d %d", r, g, b)
	}
	if !QueryOK(kitty.Options{ID: QueryID}, []byte("OK")) || QueryOK(kitty.Options{ID: QueryID}, []byte("ENOENT")) ||
		QueryOK(kitty.Options{ID: 5}, []byte("OK")) {
		t.Error("QueryOK")
	}
	if !strings.Contains(Query, "a=q") || !strings.Contains(Query, "i=31") {
		t.Errorf("query %q", Query)
	}
}
