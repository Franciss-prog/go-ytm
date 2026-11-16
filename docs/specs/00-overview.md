# 00 — Overview, scope and parity

Status: **draft, awaiting approval** · Owner: lead engineer · Last updated: 2026-11-16

## Goals

- Feature parity with [ytm-player](https://github.com/peternaame-boop/ytm-player) (Python/Textual,
  MIT, reference commit = `master` as of 2026-11-16) for everything a daily user touches.
- A single static Go binary (`ytm`), `CGO_ENABLED=0`, that meets the performance budgets below.
- First-class wallpaper-driven theming (pywal, matugen, ansi) with live reload and a transparent mode.
- A CLI that looks like the same product as the TUI (Charm stack), and stays plain when piped.

## Non-goals

- Transliterating the Python code. The Python repo is a **behavioral reference only**.
- Video playback, podcasts/episodes UI, uploads management, YouTube (non-Music) browsing.
- Account creation, OAuth device flow (the original dropped it too; cookie auth only).
- libmpv / cgo of any kind. Python runtime of any kind (yt-dlp is an external binary we shell out to).
- A daemon/headless player without the TUI (possible later; IPC design does not preclude it).
- The original's `update_check` against PyPI is replaced by a GitHub-releases check (M12), not ported.

## Module and naming decisions

| Item | Decision | Why |
|---|---|---|
| Go module path | `github.com/Franciss-Prog/go-ytm` | Must match the git remote (`Franciss-Prog/go-ytm`). Product name stays **ytm-go**. |
| Binary name | `ytm` | Same muscle memory as the original. Conflicts with the Python `ytm` if both installed; documented. |
| Config dir | `~/.config/ytm-go/` | Avoid clobbering the Python app's `~/.config/ytm-player/`. `ytm import-config` (M12, optional) can migrate. |
| Go version | 1.25+ in `go.mod` (`go 1.25`), CI tests 1.25 + stable | Charm v2 requires a recent toolchain; local toolchain is 1.27. |

## Performance budgets

| Metric | Budget | Measured in |
|---|---|---|
| Cold start → interactive UI (no network) | < 150 ms | M4, M12 |
| Keypress → repaint | < 16 ms | M4, M5 |
| Scroll 1,000-row list | no dropped frames @ 60 fps | M5 |
| Idle CPU | < 1 % | M4, M12 |
| RSS with art + lyrics open | < 80 MB | M8, M12 |
| Theme apply + full repaint | < 50 ms | M4b |
| Wallpaper change → recolored | < 300 ms end-to-end | M4b |
| Track skip → audio | network-bound only; next URL always prefetched | M3, M7 |

`ytm --pprof[=addr]` exposes `net/http/pprof` on `127.0.0.1:6060` (M0). `ytm --trace-frames` (M4)
logs per-frame Update/View timings to the debug log.

## Parity matrix

Legend: **M#** = milestone that delivers it. "≈" = delivered with an intentional difference (see note).

### Playback & queue
| Feature (original) | ytm-go | Note |
|---|---|---|
| mpv backend, play/pause/seek/volume/mute | M1 | JSON IPC subprocess instead of libmpv |
| Gapless (preload next) | M1/M3 | mpv playlist append + `--prefetch-playlist` |
| Stream prefetch for next track | M3 | |
| Audio quality high/medium/low | M3 | yt-dlp format strings |
| Repeat off/all/one, shuffle | M5 | |
| Play random from queue (`.`) | M5 | |
| Autoplay related tracks (↻ marker) | M6 | |
| Radio, multi-seed radio, playlist radio | M6 | |
| Discovery mix (`D`) | M6 | |
| Per-playlist shuffle lock | M7 | `shuffle_prefs.json` |
| Session resume (track, position, queue, volume) | M7 | |
| History threshold `history_min_listen_seconds` | M7 | |
| Report plays to YT Music history | M6 | `sync_history_to_ytmusic` |

### Library & pages
| Feature | ytm-go | Note |
|---|---|---|
| Library (playlists, albums, artists) | M5 | |
| Search (music/all modes, filters, suggestions) | M5 | predictive suggestions debounced |
| Context page (album / artist / playlist) | M5 | artist discography included |
| Queue page (reorder `J/K` w/ counts, delete, autoplay marks) | M5 | |
| Browse: For You (home shelves), Charts (68 regions), New releases, Playlists | M6 | |
| Liked Songs (+ Start Radio) | M6 | |
| Recently Played tabs (All / Local / YT Music) | M6 | |
| Help page (full keymap) | M4 | generated from keymap, Glamour for prose |
| Playlist sidebar, lyrics sidebar | M5 / M8 | |
| Marks, range marks, filter, sort (title/artist/album/duration/date, reverse) | M5 | |
| Actions popup, playlist picker, create/delete playlist, confirm popups | M5 | |
| Selection info bar | M5 | |
| Album art half-blocks | M4b | `album_art_mode = blocks\|off` |
| Downloads for offline, LRU cache | M7 | |
| Synced lyrics (YT + lrclib fallback, title sanitize) | M8 | |
| Transliteration toggle (`T`), BiDi/RTL modes | M8 | pure-Go transliteration (see 05) |
| Chart region picker (`c`) | M6 | |
| Full mouse support | M5 | Bubble Tea v2 mouse msgs |

### CLI / IPC
| Command | ytm-go | Note |
|---|---|---|
| `setup` (+ `--browser`, `--slots`, `--manual`) | M2 | Huh wizard; non-interactive flags |
| `search` (`--filter`, `--limit`, `--json`) | M2 | ≈ styled table on TTY; original always printed JSON |
| `stats`, `history`, `history search` | M9 | ≈ styled on TTY |
| `cache status`, `cache clear` (`--yes`) | M7 (CLI in M9) | |
| `play pause next prev seek play-index` | M9 | |
| `like dislike unlike now status queue (add/clear)` | M9 | |
| `import <spotify-url>` | M11 | |
| `doctor` | M0 stub → M9 full | |
| `config` (open in `$EDITOR`) | M0 | |
| `--version`, `--debug` | M0 | |
| new: `theme reload`, `theme show`, `changelog`, `completion` | M4b / M9 / M12 / M0 | |

### Integrations
| Feature | ytm-go | Note |
|---|---|---|
| MPRIS (Linux) | M10 | `godbus/dbus/v5` |
| macOS Now Playing + media keys | M10 | **risk**: no-cgo path (see 01 risks) |
| Windows media keys / SMTC | M10 | **risk**: no-cgo path |
| Desktop notifications | M10 | D-Bus on Linux; `osascript`/toast on others |
| Discord Rich Presence | M11 | own small IPC client (pipe/socket) |
| Last.fm scrobbling | M11 | own small client (signed REST) |
| Spotify import (API + scraper fallback, 85 % auto-accept) | M11 | |
| Theming: 18+ static themes | M4 (4) → M12 (18) | |
| **New:** pywal / matugen / ansi / file sources, live reload, contrast guard, transparent mode, Settings popup | M4b | headline feature |

## Milestones

| # | Slug | Estimate (focused eng-days) |
|---|---|---|
| M0 | scaffold | 1.5 |
| M1 | player-core | 2.5 |
| M2 | ytmusic-auth-search | 4 |
| M3 | stream-resolve | 1.5 |
| M4 | tui-shell | 3 |
| M4b | dynamic-theme | 3.5 |
| M5 | tui-library-search-queue | 5 |
| M6 | browse-liked-history | 4 |
| M7 | session-cache-offline | 2.5 |
| M8 | lyrics | 2.5 |
| M9 | cli-ipc | 2.5 |
| M10 | mpris-media-keys | 4 (Linux 1, macOS/Windows 3, high variance) |
| M11 | integrations | 4 |
| M12 | polish-release | 3 |
| | **Total** | **≈ 43.5** |

## Edge cases (cross-cutting)

- No mpv / no yt-dlp on PATH → TUI starts, playback actions show an actionable toast; `ytm doctor` ✗.
- Not authenticated → search/charts/home work anonymously (InnerTube allows it); library pages show
  "Run `ytm setup`" empty state.
- Second `ytm` launch while TUI running → refuse with a message (single-instance via IPC socket probe).
- Non-UTF-8 / narrow terminals (< 60 cols) → degrade layout (hide sidebars, compact bar).

## Test plan

Every milestone's PR lists its tests; the cross-cutting strategy lives in `07-testing-ci-release.md`.
The parity matrix is re-checked in M12: each row must point at a test or a manual test step.

## Open questions

1. Binary name `ytm` vs `ytm-go`? (default: `ytm`, matches the prompt's CLI examples)
2. Should ytm-go read the Python app's `~/.config/ytm-player/` as a fallback on first run? (default: no, offer `ytm import-config` in M12)
