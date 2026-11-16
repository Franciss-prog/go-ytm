# Progress

Last updated: 2026-11-16

## Milestones

| # | Slug | Status | PR | Notes |
|---|---|---|---|---|
| P0 | specs | **approved** 2026-11-16 | [#1](https://github.com/Franciss-prog/go-ytm/pull/1) | all proposed defaults accepted |
| M0 | scaffold | **in review** | branch `feat/scaffold` (stacked on #1) | CLI skeleton, config loader, logging, style, doctor stub |
| M1 | player-core | **in review** | branch `feat/player-core` (stacked on #2) | mpv launcher, JSON IPC, controller, crash restore, fakempv |
| M2 | ytmusic-auth-search | not started | | |
| M3 | stream-resolve | not started | | |
| M4 | tui-shell | not started | | |
| M4b | dynamic-theme | not started | | |
| M5 | tui-library-search-queue | not started | | |
| M6 | browse-liked-history | not started | | |
| M7 | session-cache-offline | not started | | |
| M8 | lyrics | not started | | |
| M9 | cli-ipc | not started | | |
| M10 | mpris-media-keys | not started | | |
| M11 | integrations | not started | | |
| M12 | polish-release | not started | | |

## Decisions

| Date | Decision | Where |
|---|---|---|
| 2026-11-16 | Module path `github.com/Franciss-Prog/go-ytm` (matches remote); product ytm-go; binary `ytm` | 00 |
| 2026-11-16 | Charm **v2** stack under `charm.land/*/v2` (bubbletea/lipgloss v2.1.1, bubbles v2.2.1, huh v2.0.3, fang v2.0.1, glamour v2.0.1, log v2.0.1) — verified on proxy + `go doc` | 01 |
| 2026-11-16 | No usable Go YT Music client → internal `ytmusic` package porting ytmusicapi (MIT) shapes | 03 |
| 2026-11-16 | Cookie extraction via `yt-dlp --cookies-from-browser` (primary), kooky as fallback; final call in M2 | 03 |
| 2026-11-16 | Comment-preserving config writes via `go-toml/v2/unstable` byte-range splicing (verified `Node.Raw Range`) | 04 |
| 2026-11-16 | IPC on Windows uses named pipes (original used TCP + token) | 06 |
| 2026-11-16 | Config dir `~/.config/ytm-go/` (not shared with the Python app) | 00 |
| 2026-11-16 | Theme tokens are `image/color.Color` (RGB / ANSI index / `lipgloss.NoColor` for terminal default) — no wrapper type | 08 |
| 2026-11-16 | Config validation is tag-driven (`oneof`, `range`) over a reflect walk of the raw TOML map, so every bad key is reported (not just the first) | 04 |
| 2026-11-16 | `doctor` exits 1 on any failed check; `config check` exits 1 on errors only | 06 |
| 2026-11-16 | `--no-color` bridged to `NO_COLOR=1` so fang's help/errors honor it | 06 |
| 2026-11-16 | Background-SGR detection uses a real SGR parser (`style.BackgroundSGRs`), not a regex | 08 |
| 2026-11-16 | `loadfile` via named arguments (portable across mpv's positional change) | 02 |
| 2026-11-16 | Consecutive relative seeks are summed when coalesced, not dropped | 02 |
| 2026-11-16 | Re-resolve on `EndError` belongs to queue/stream (M3); controller only reports | 02 |
| 2026-11-16 | Deps added: `go-winio` v0.6.4 (Windows named pipes, listed in 01), `goleak` v1.3.0 (test-only leak checks, required by 01) | 01 |

## Verified facts (with how)

- mpv v0.41.0 JSON IPC reply/event shapes, `loadfile … append-play` → `playlist_entry_id` (local probe).
- Unix socket path > 108 bytes fails (`AF_UNIX path too long`, local probe).
- matugen 4.2.0 template syntax `{{colors.<role>.default|dark|light.hex}}`, `{{mode}}` (rendered locally).
- Owner's `~/.cache/wal/colors.json` is matugen-written, lacks `special`, has empty colorN entries.
- yt-dlp 2026.10.26 has `--cookies-from-browser` and `--cookies FILE`.
- fang v2.0.1 renders help/errors through `colorprofile.NewWriter(w, os.Environ())`: honors NO_COLOR and
  non-TTY, not app flags; it adds `completion` and hidden `man` commands (read source).
- go-toml v2.4.3 `unstable.Parser.Shape(range)` gives 1-based line/col (used for diagnostics).
- mpv 0.41.0: named-arg `loadfile` with `"options":"start=12.500"` works; `set_property pause true`
  before `loadfile` loads paused; `playlist-clear` keeps the current entry; `time-pos` first reports
  with no `data` (null) before a file loads (local probe).
- mpv 0.41.0 + lavfi: reported duration is a growing estimate and seeks land ~0.2 s short — the
  integration test allows ±0.5 s (local run).
- Real-mpv integration (`-tags mpv`): load at offset, pause, absolute/relative seek, volume,
  `kill -9` → relaunch resumes paused at the same position and volume. Passes locally (3/3).

## Measurements (M0, local, Linux amd64, Go 1.27.4)

| What | Result | Budget |
|---|---|---|
| `ytm --version` / `config path` / `config check --json` cold run | 1.7 ms median (30 runs) | 150 ms |
| `ytm doctor --json` | 198 ms median — bounded by `yt-dlp --version` alone (188 ms); probes run in parallel | n/a |
| Binary size (`-s -w`, trimpath) | 12.8 MB | n/a |

## Measurements (M1, local, Linux amd64)

| What | Result |
|---|---|
| `mpvipc` round trip vs fakempv over `net.Pipe` | 9.4 µs/op, 38 allocs |
| mpv event decode | 658 ns/op (105 MB/s) |
| Coverage | player 78 %, mpvipc 88 %, clock 94 % |

## Approved by owner (2026-11-16)

- Keys: `settings = ,`, `toggle_transparency = C-t`, `command_palette = :`, `theme_picker = g t`.
- New config keys: `[theme]` section, `[ui] icons|album_art_mode|mouse`, `[yt_dlp] binary`,
  `[theme] color_depth|ansi_accent`, `[theme.pywal_map]`, `[keys] chord_timeout_ms`.
- CLI prints human output by default, compact JSON with `--json`.
- New commands: `toggle`, `theme reload|show`, `changelog`, `config check|path|defaults`, `completion`.
- mpv launched with `--no-config`; opt-in `[playback] mpv_config` / `mpv_args`.
- Binary `ytm`, config dir `~/.config/ytm-go/`.
- All other per-spec open questions: proposed defaults accepted.

## Open questions (need owner)

- None currently.

## Known gaps

- macOS Now Playing and Windows SMTC without cgo are unproven (M10 risk).
- M1: Windows named-pipe transport compiles (cross-build) but is not exercised against a real mpv on
  Windows; unit tests run there over `net.Pipe`. Real-mpv CI job is Linux only.
- M1: mpv versions older than 0.41.0 untested locally; CI `mpv-integration` covers Ubuntu's mpv.
- M1: `doctor` does not yet warn on old mpv versions (spec 02 edge case) — deferred to M3.
- `golangci-lint` / `staticcheck` not installed locally; Makefile falls back to `go run` (works, ~40 s cold).
- CI green on linux/macOS/windows × stable/oldstable (PR #2). CLI golden files are Linux reference
  output and skip on Windows (host path rules differ); policy, no-ANSI, JSON and exit-code tests run there.
- fang queries the terminal background (OSC 11) when rendering help on a TTY; harmless on real
  terminals, visible as junk only under `script`-style captures.
