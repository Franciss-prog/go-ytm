package ui

import (
	"context"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// fakeArt serves a gradient image and records the URLs asked for.
type fakeArt struct {
	mu   sync.Mutex
	urls []string
}

func (f *fakeArt) Fetch(_ context.Context, url string) (image.Image, error) {
	f.mu.Lock()
	f.urls = append(f.urls, url)
	f.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 120, 255})
		}
	}
	return img, nil
}

// update sends msg and runs the commands it returns.
func update(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	drain(m, cmd)
}

// raws runs cmd's tree and returns the raw escapes it would write.
func raws(cmd tea.Cmd) []string {
	var out []string
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.RawMsg:
			out = append(out, msg.Msg.(string))
		}
	}
	return out
}

// artModel is on the track page with a playing track that has art.
func artModel(t *testing.T, mut ...func(*Options)) (*Model, *fakeArt) {
	t.Helper()
	fa := &fakeArt{}
	m, _ := newTestModel(t, 140, 40, append([]func(*Options){func(o *Options) { o.Art = fa }}, mut...)...)
	ts := scenarioTracks()
	ts[1].ThumbnailURL = "https://lh3.googleusercontent.com/cover=w60-h60-l90-rj"
	m.Queue().AddMany(ts)
	m.Queue().JumpTo(1)
	m.queueChanged()
	update(m, PlayerEventMsg{playerState(playing)})
	drain(m, press(m, ch('g'), ch('n')))
	return m, fa
}

func TestCoverArtHalfBlocks(t *testing.T) {
	m, fa := artModel(t)
	if len(fa.urls) != 1 || fa.urls[0] != "https://lh3.googleusercontent.com/cover=w400-h400-l90-rj" {
		t.Fatalf("fetched %v", fa.urls)
	}
	cols, rows := m.trackArtSize()
	if cols == 0 {
		t.Fatal("no art box at 140×40")
	}
	out := plain(m)
	if n := strings.Count(out, "▀"); n != cols*rows {
		t.Fatalf("%d half blocks, want %d×%d:\n%s", n, cols, rows, out)
	}
	// The same track again comes from the cache.
	m.Queue().JumpTo(0)
	update(m, PlayerEventMsg{playerState(playing)})
	m.Queue().JumpTo(1)
	update(m, PlayerEventMsg{playerState(playing)})
	if len(fa.urls) != 1 {
		t.Errorf("refetched: %v", fa.urls)
	}
	// toggle_album_art hides it.
	drain(m, press(m, tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	if strings.Contains(plain(m), "▀") {
		t.Error("C-a did not hide the art")
	}
}

func TestCoverArtTransparentPaintsOnlyArt(t *testing.T) {
	m, _ := artModel(t, func(o *Options) { o.Config.Theme.Transparent = true; o.Config.Theme.TransparentSelection = "bar" })
	for i, line := range strings.Split(m.View().Content, "\n") {
		if len(style.BackgroundSGRs(line)) > 0 && !strings.Contains(line, "▀") {
			t.Errorf("line %d paints a background outside the art: %q", i, ansi.Strip(line))
		}
	}
}

func TestCoverArtOffAndNoColor(t *testing.T) {
	for name, mut := range map[string]func(*Options){
		"off":      func(o *Options) { o.Config.UI.AlbumArtMode = "off" },
		"no color": func(o *Options) { o.NoColor = true },
	} {
		m, _ := artModel(t, mut)
		if strings.Contains(plain(m), "▀") {
			t.Errorf("%s: art drawn", name)
		}
	}
}

func TestCoverArtKitty(t *testing.T) {
	m, _ := artModel(t)
	if m.art.kitty {
		t.Fatal("kitty assumed without a reply")
	}
	// The terminal answers the query; the next Update transmits.
	_, cmd := m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: 31}, Payload: []byte("OK")})
	seqs := raws(cmd)
	if len(seqs) != 1 || !strings.Contains(seqs[0], "U=1") || !strings.Contains(seqs[0], "i=1,") {
		t.Fatalf("transmit: %d raw writes", len(seqs))
	}
	cols, rows := m.trackArtSize()
	out := m.View().Content
	if n := strings.Count(out, string(kitty.Placeholder)); n != cols*rows || strings.Contains(out, "▀") {
		t.Fatalf("%d placeholders, want %d", n, cols*rows)
	}
	if len(style.BackgroundSGRs(out)) != 0 && m.o.Config.Theme.Transparent {
		t.Error("kitty art painted backgrounds")
	}
	// Same size and track: nothing resent.
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40}); len(raws(cmd)) != 0 {
		t.Error("retransmitted without a change")
	}
	// A resize re-places it as a new image and frees the old one.
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	seqs = raws(cmd)
	if len(seqs) != 1 || !strings.HasPrefix(seqs[0], "\x1b_Ga=d,d=I,i=1,") || !strings.Contains(seqs[0], "i=2,") {
		t.Fatalf("resize: %q", seqs)
	}
	// Quit frees the image.
	if _, cmd := m.Update(tea.KeyPressMsg(ch('q'))); cmd == nil || m.art.id != 0 {
		t.Error("quit kept the kitty image")
	}
}
