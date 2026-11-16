# ytm-go

> A fast, beautiful YouTube Music client for your terminal, rewritten in Go.

**Status: 🚧 planning / pre-alpha.** Nothing here is released yet. Everything below describes the
design goals and the planned feature set, not what works today. See the [roadmap](#roadmap).

`ytm-go` is a ground-up Go rewrite of [ytm-player](https://github.com/peternaame-boop/ytm-player)
(MIT). The original is great but the Python/Textual stack can feel laggy. `ytm-go` aims for the same
feature set in a single static binary that stays smooth, and adds first-class wallpaper-driven
theming and transparency.

---

## Why

- **Lag.** Slow repaints and UI stalls during network/mpv calls. Go + Bubble Tea with a non-blocking
  update loop and goroutines for all I/O is the fix.
- **Install friction.** One static binary instead of a Python environment (pipx, venvs, PEP 668 issues).
- **Rice-friendly.** Built for people who theme their desktop from their wallpaper (pywal, matugen)
  and run transparent terminals.

## Planned features

### Playback and library

- Search, Library, Browse (For You, Charts), Queue, Liked Songs, Recently Played
- Radio and discovery mixes, autoplay of related tracks
- Gapless playback, next-track prefetch, resume where you left off
- Offline downloads with an LRU audio cache (size-capped)
- Per-playlist shuffle lock
- Synced lyrics sidebar (lrclib), optional transliteration, RTL text support
- Spotify playlist import with fuzzy matching

### Interface

- Vim-style keys with chords and counts (`g` `l`, `15J`), fully rebindable
- Full mouse support (seek, marks, right-click actions)
- Marks and range selection, filter and sort in every track list
- Album art in the terminal (half-block rendering)
- Built with the [Charm](https://charm.sh) stack: Bubble Tea, Bubbles, Huh, Lip Gloss

### Dynamic theming and transparency

- **pywal**: reads `~/.cache/wal/colors.json`
- **matugen**: Material You roles via a template shipped in this repo
- **ansi**: uses your terminal's own palette, so anything that recolors the terminal recolors the app
- **static / file**: built-in themes or your own `theme.toml`
- **Live reload**: change your wallpaper and the running player recolors without flicker or interrupting playback
- **Contrast guard**: unreadable wallpaper palettes get adjusted automatically (toggleable)
- **Transparent mode**: the app stops painting backgrounds so your terminal's transparency/blur shows through
- Settings popup (`,`) to change all of this live

> **Note on transparency:** a terminal app cannot set real opacity. Your terminal emulator controls that.
> Transparent mode makes `ytm-go` use the terminal's default background everywhere so the terminal's own
> opacity or blur shows through. Set the opacity in your terminal's config.

### CLI and scripting

- Pretty output on a TTY, plain/JSON when piped (`--json`, `NO_COLOR` respected)
- Control a running player from another terminal or a keybinding in your window manager
- Diagnostics with `ytm doctor`

```sh
ytm search "daft punk" --filter songs --json | jq '.[0]'
ytm play | pause | next | prev
ytm seek +10
ytm like
ytm queue add VIDEO_ID
ytm now            # now-playing card
ytm stats          # listening stats
ytm import "https://open.spotify.com/playlist/..."
ytm theme reload   # force a theme refresh
ytm doctor         # diagnostic report
```

## Performance goals

These are targets, to be measured and published in PRs.

| Metric                                           | Target   |
| ------------------------------------------------ | -------- |
| Cold start to interactive UI (excluding network) | < 150 ms |
| Keypress to repaint                              | < 16 ms  |
| Idle CPU                                         | < 1%     |
| Memory with art + lyrics open                    | < 80 MB  |
| Theme change to full repaint                     | < 50 ms  |

A `--pprof` flag will ship so lag can be profiled instead of guessed.

## How it works

```
 ┌────────────┐   messages   ┌──────────────┐   JSON IPC   ┌───────┐
 │ Bubble Tea │ ◄──────────► │  player core │ ◄──────────► │  mpv  │
 │    TUI     │              └──────┬───────┘              └───────┘
 └─────┬──────┘                     │
       │                   ┌────────┴────────┐
       │                   │ stream resolver │── yt-dlp
       │                   └────────┬────────┘
       │                            │
 ┌─────┴──────┐   ┌─────────────────┴──┐   ┌──────────────┐
 │ theme/     │   │ YouTube Music API  │   │ SQLite store │
 │ settings   │   │ (InnerTube)        │   │ + audio cache│
 └─────┬──────┘   └────────────────────┘   └──────────────┘
       │ fsnotify
  pywal / matugen files
```

- **No cgo.** mpv runs as a subprocess controlled over its JSON IPC socket; SQLite is pure Go.
- **Non-blocking UI.** The update loop never waits on the network, disk, or mpv.
- **Everything behind interfaces** (mpv, YouTube Music, yt-dlp) so it can be tested with fakes.

## Requirements (planned)

- [mpv](https://mpv.io/)
- [yt-dlp](https://github.com/yt-dlp/yt-dlp)
- A truecolor terminal is recommended (256 and 16 colors degrade gracefully)
- Linux, macOS, Windows (Linux is the primary target)

## Installation (planned)

Not available yet. Planned channels:

```sh
# from source (once there is a release)
go install github.com/Franciss-Prog/go-ytm/cmd/ytm@latest

# later: GitHub releases, AUR, Homebrew, Scoop, Nix
```

## Configuration (planned)

Files live in `~/.config/ytm-go/` (`$XDG_CONFIG_HOME` respected, `%APPDATA%\ytm-go\` on Windows):

| File          | Purpose                                    |
| ------------- | ------------------------------------------ |
| `config.toml` | General, playback, cache, UI, integrations |
| `keymap.toml` | Keybinding overrides                       |
| `theme.toml`  | Custom theme colors                        |

Example theming setup:

```toml
[theme]
source = "pywal"            # static | pywal | matugen | ansi | file
transparent = true
transparent_selection = "bar"   # bar | reverse | underline | solid
enforce_contrast = true
watch = true                # live reload on wallpaper change
fallback = "ytm-dark"       # used if the source file is missing or invalid
```

### pywal

Run `wal -i <wallpaper>` as usual. `ytm-go` watches `~/.cache/wal/colors.json` and updates live.

### matugen

Add the template from `contrib/matugen/` to your matugen config so it writes
`~/.config/ytm-go/matugen-colors.toml`, then set `source = "matugen"`. (Template and exact config
snippet will be added in the dynamic-theme milestone, verified against current matugen docs.)

### Transparent terminal

Set opacity in your terminal emulator (for example `background_opacity` in kitty, `opacity` in
alacritty or foot), then set `transparent = true` here.

## Roadmap

| Milestone | Scope                                                                             |
| --------- | --------------------------------------------------------------------------------- |
| M0        | Scaffold, CI, config loader, styled CLI base                                      |
| M1        | mpv IPC player core                                                               |
| M2        | Auth wizard, YouTube Music search                                                 |
| M3        | Stream resolution and prefetch                                                    |
| M4        | TUI shell, theme system                                                           |
| **M4b**   | **pywal / matugen / ansi theming, live reload, transparent mode, Settings popup** |
| M5        | Library, Search, Queue pages                                                      |
| M6        | Browse, Liked Songs, Recently Played, radio                                       |
| M7        | Session resume, audio cache, offline downloads                                    |
| M8        | Synced lyrics                                                                     |
| M9        | Full CLI and IPC control                                                          |
| M10       | MPRIS, media keys, notifications                                                  |
| M11       | Discord RPC, Last.fm, Spotify import                                              |
| M12       | Performance pass, docs, packaging, v0.1.0                                         |

Each milestone ships as its own pull request, driven by written specs in `docs/specs/`.

## Project layout (planned)

```
cmd/ytm/                 entry point
internal/app/            application wiring
internal/ui/             Bubble Tea models, pages, popups
internal/style/          all Lip Gloss styles, icons, theme tokens
internal/player/         mpv IPC client and state machine
internal/ytmusic/        YouTube Music API client
internal/stream/         yt-dlp resolver and URL cache
internal/store/          SQLite, session, audio cache
internal/config/         TOML config, keymap, theme
internal/ipc/            CLI <-> running TUI
internal/lyrics/         lrclib client and sync
internal/integrations/   MPRIS, Discord, Last.fm, Spotify import
docs/specs/              design specs
contrib/matugen/         matugen template
```

## Contributing

Not open yet. Once the specs and scaffold land, contributions will follow the workflow in `CLAUDE.md`:
one PR per change, Conventional Commits, tests required (`go test -race ./...`), lint clean.

## Disclaimer

`ytm-go` is an unofficial, third-party client. It is not affiliated with or endorsed by YouTube or
Google. It relies on undocumented YouTube Music endpoints and on `yt-dlp`, so it may break when
YouTube changes things. Use it with your own account and at your own risk, and respect YouTube's
terms of service.

## Credits and license

- Inspired by and behaviorally modeled on [ytm-player](https://github.com/peternaame-boop/ytm-player)
  by peternaame-boop and contributors (MIT).
- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), Lip Gloss, Bubbles and Huh from
  [Charm](https://charm.sh), plus mpv and yt-dlp.

Released under the [MIT License](LICENSE).
