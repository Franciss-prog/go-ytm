# Progress

Last updated: 2026-11-16

## Milestones

| # | Slug | Status | PR | Notes |
|---|---|---|---|---|
| P0 | specs | **approved** 2026-11-16 | branch `docs/phase-0-specs` | all proposed defaults accepted |
| M0 | scaffold | not started | | |
| M1 | player-core | not started | | |
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

## Verified facts (with how)

- mpv v0.41.0 JSON IPC reply/event shapes, `loadfile … append-play` → `playlist_entry_id` (local probe).
- Unix socket path > 108 bytes fails (`AF_UNIX path too long`, local probe).
- matugen 4.2.0 template syntax `{{colors.<role>.default|dark|light.hex}}`, `{{mode}}` (rendered locally).
- Owner's `~/.cache/wal/colors.json` is matugen-written, lacks `special`, has empty colorN entries.
- yt-dlp 2026.10.26 has `--cookies-from-browser` and `--cookies FILE`.

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
- `golangci-lint` / `staticcheck` not installed locally; Makefile falls back to `go run`.
