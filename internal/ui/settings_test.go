package ui

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

type fakeThemes struct {
	mu     sync.Mutex
	sets   []config.Theme
	reload int
	bg     color.Color
}

func (f *fakeThemes) Set(c config.Theme)          { f.mu.Lock(); f.sets = append(f.sets, c); f.mu.Unlock() }
func (f *fakeThemes) Reload()                     { f.mu.Lock(); f.reload++; f.mu.Unlock() }
func (f *fakeThemes) SetTerminalBg(c color.Color) { f.mu.Lock(); f.bg = c; f.mu.Unlock() }

type patchRec struct {
	path    string
	changes []config.Change
	err     error
}

func (p *patchRec) patch(path string, ch []config.Change) error {
	p.path, p.changes = path, append(p.changes, ch...)
	return p.err
}

func settingsModel(t *testing.T) (*Model, *fakeThemes, *patchRec) {
	t.Helper()
	ft, pr := &fakeThemes{}, &patchRec{}
	m, _ := newTestModel(t, 100, 30, func(o *Options) {
		o.Themes, o.ConfigPath, o.PatchConfig = ft, "/cfg/config.toml", pr.patch
		// Cancelled: toast timers return at once, so run() never waits.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		o.Context = ctx
	})
	return m, ft, pr
}

var (
	kDown  = tea.Key{Code: tea.KeyDown}
	kRight = tea.Key{Code: tea.KeyRight}
	kSpace = tea.Key{Code: tea.KeySpace, Text: " "}
	kEnter = tea.Key{Code: tea.KeyEnter}
	kEsc   = tea.Key{Code: tea.KeyEscape}
	kTab   = tea.Key{Code: tea.KeyTab}
)

// run executes cmd and feeds resulting messages back (one level).
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			run(m, c)
		}
		return
	}
	if msg != nil {
		m.Update(msg)
	}
}

func TestSettingsPreviewSaveAndRevert(t *testing.T) {
	m, ft, pr := settingsModel(t)
	press(m, ch(','))
	if !m.SettingsOpen() || !strings.Contains(plain(m), "Settings") {
		t.Fatal("Settings popup not shown")
	}
	// Row 2 = Transparent: flipping it previews immediately.
	press(m, kDown, kDown, kSpace)
	if !m.Styles().Opts.Transparent || len(ft.sets) != 1 || !ft.sets[0].Transparent {
		t.Fatalf("transparent preview: styles=%v sets=%v", m.Styles().Opts.Transparent, ft.sets)
	}
	// Row 3 = Selection style: style-only, no re-resolve.
	press(m, kDown, kRight) // solid (default) wraps to bar
	if m.o.Config.Theme.TransparentSelection != "bar" || len(ft.sets) != 1 {
		t.Errorf("selection: %s, sets=%d", m.o.Config.Theme.TransparentSelection, len(ft.sets))
	}
	// esc reverts everything and re-resolves the theme once.
	press(m, kEsc)
	if m.SettingsOpen() || m.Styles().Opts.Transparent || m.o.Config.Theme.TransparentSelection != "solid" || len(ft.sets) != 2 {
		t.Errorf("revert: open=%v transparent=%v sel=%s sets=%d", m.SettingsOpen(), m.Styles().Opts.Transparent, m.o.Config.Theme.TransparentSelection, len(ft.sets))
	}
	if len(pr.changes) != 0 {
		t.Error("esc persisted")
	}

	// enter saves only the changed keys.
	press(m, ch(','), kDown, kDown, kDown, kDown, kSpace) // popup fill
	cmd := press(m, kEnter)
	run(m, cmd)
	if len(pr.changes) != 1 || pr.changes[0].Key != "theme.popup_fill" || pr.changes[0].Value != true || pr.path != "/cfg/config.toml" {
		t.Errorf("saved %v to %s", pr.changes, pr.path)
	}
	if !strings.Contains(plain(m), "Settings saved") {
		t.Error("no saved toast")
	}
}

func TestSettingsSaveError(t *testing.T) {
	m, _, pr := settingsModel(t)
	pr.err = errors.New("disk full")
	press(m, ch(','), kSpace)
	run(m, press(m, kEnter))
	if !strings.Contains(plain(m), "Couldn't save settings") {
		t.Error("no error toast")
	}
}

func TestSettingsCaptureKeys(t *testing.T) {
	m, _, _ := settingsModel(t)
	press(m, ch(','), ch('q'))
	if m.quiting {
		t.Error("q quit the app while Settings was open")
	}
	if m.SettingsOpen() {
		t.Error("q should close Settings")
	}
}

func TestToggleTransparencyPersists(t *testing.T) {
	m, ft, pr := settingsModel(t)
	cmd := m.global(keysAction("toggle_transparency"))
	run(m, cmd)
	if !m.Styles().Opts.Transparent || len(ft.sets) != 1 {
		t.Error("not applied")
	}
	if len(pr.changes) != 1 || pr.changes[0] != (config.Change{Key: "theme.transparent", Value: true}) {
		t.Errorf("persisted %v", pr.changes)
	}
}

func TestThemeChangeKeepsState(t *testing.T) {
	m, _, _ := settingsModel(t)
	m.navigate(PageHelp)
	press(m, ch('/'), ch('v'), ch('o'))
	m.Update(PlayerEventMsg{playerState(playing)})
	before := plain(m)
	v0 := m.Styles().Version

	nord, _ := theme.Builtin("nord")
	m.Update(ThemeChangedMsg{theme.Result{Theme: nord, Cause: theme.CauseFile}})
	if m.Styles().Version != v0+1 || theme.FormatColor(m.Styles().Theme.Bg) != "#2e3440" {
		t.Fatalf("theme not applied: v=%d bg=%s", m.Styles().Version, theme.FormatColor(m.Styles().Theme.Bg))
	}
	if after := plain(m); after != before {
		t.Errorf("text changed on theme swap:\n%s\n---\n%s", before, after)
	}
	if m.Current() != PageHelp || !m.pages[PageHelp].Capturing() {
		t.Error("page/filter state lost")
	}
	if v := m.View(); v.BackgroundColor == nil || theme.FormatColor(v.BackgroundColor) != "#2e3440" {
		t.Errorf("root bg = %v", v.BackgroundColor)
	}
}

func TestThemeErrorsToastOnceAndKeepColors(t *testing.T) {
	m, _, _ := settingsModel(t)
	before := theme.FormatColor(m.Styles().Theme.Accent)
	fb, _ := theme.Builtin("nord")
	bad := ThemeChangedMsg{theme.Result{Theme: fb, Err: errors.New("parse pywal colors: unexpected EOF"), Cause: theme.CauseFile}}
	_, c1 := m.Update(bad)
	_, c2 := m.Update(bad)
	if c1 == nil || c2 != nil {
		t.Errorf("toasts: first=%v second=%v (want once)", c1 != nil, c2 != nil)
	}
	if theme.FormatColor(m.Styles().Theme.Accent) != before {
		t.Error("broken file replaced the current colors")
	}
	if !strings.Contains(plain(m), "keeping current colors") {
		t.Error("toast text")
	}
	// Recovery toasts success.
	_, c3 := m.Update(ThemeChangedMsg{theme.Result{Theme: fb, Cause: theme.CauseFile}})
	if c3 == nil || !strings.Contains(plain(m), "Theme colors found") {
		t.Error("no recovery toast")
	}
	// A settings change to a missing source shows the fallback.
	m.Update(ThemeChangedMsg{theme.Result{Theme: fb, Err: errors.New("pywal colors not found"), Cause: theme.CauseSettings}})
	if theme.FormatColor(m.Styles().Theme.Bg) != "#2e3440" {
		t.Error("fallback not applied on settings change")
	}
}

func TestBackgroundColorQuery(t *testing.T) {
	ft := &fakeThemes{}
	m, _ := newTestModel(t, 80, 24, func(o *Options) { o.Themes = ft; o.Config.Theme.Transparent = true })
	if m.Init() == nil {
		t.Fatal("no init cmd")
	}
	m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#fdf6e3")})
	if theme.FormatColor(ft.bg) != "#fdf6e3" {
		t.Errorf("terminal bg not forwarded: %v", ft.bg)
	}
}

func keysAction(name string) keys.Action { return keys.Action{Name: name} }
