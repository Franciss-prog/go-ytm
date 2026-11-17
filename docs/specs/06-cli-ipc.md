# 06 — CLI, IPC, and CLI styling

Status: **draft** · Milestones: M0 (skeleton, style, doctor stub), M2 (setup, search), M9 (rest)

## Goals

- Every original subcommand, same names and flags, plus `theme`, `changelog`, `completion`, `config check`.
- CLI looks like the TUI (same theme tokens/sources) on a TTY; plain, stable, ANSI-free when piped
  or with `--json`.
- IPC lets scripts and WM keybindings drive a running TUI.

## Non-goals

- Remote (network) control. IPC is local-only.

## Global flags

| Flag | Meaning |
|---|---|
| `--version` / `-v` | version, commit, date, Go version (via fang `WithVersion`/`WithCommit`) |
| `--debug` | DEBUG log level + human logs on stderr |
| `--config-dir DIR` | override config dir (also `YTM_GO_CONFIG_DIR`) |
| `--no-color` | disable styling (also `NO_COLOR`, `CLICOLOR=0`, `TERM=dumb`) |
| `--json` | (per command where meaningful) machine output |
| `--pprof[=ADDR]` | start pprof HTTP server (default `127.0.0.1:6060`) for the life of any command; address printed to stderr |

## Output policy (implemented once in `internal/style`)

```
styled  := stdout is TTY && !NO_COLOR && !--no-color && TERM != "dumb" && CLICOLOR != "0" && !--json
          (CLICOLOR_FORCE=1 forces styled even when piped, except with --json)
```
- Results → **stdout**. Spinners, progress, prompts, warnings → **stderr** (spinner only if stderr is a TTY).
- `--json` → compact single-line JSON, stable field names (snake_case), no ANSI ever. Non-TTY without
  `--json` → plain text (tab-separated tables, no box drawing, no icons).
- Color profile via `colorprofile.Detect(os.Stdout, os.Environ())`; lipgloss output written through a
  profile-aware writer so 256/16-color terminals degrade.
- Theme: CLI loads the same `[theme]` config and source (pywal/matugen/…) once, no watcher.
  Transparent mode → no background fills in CLI output either.
- `--no-color` is bridged to `NO_COLOR=1` before fang runs so help/errors honor it too.
- Doctor/config rows that would double-count (e.g. a `config.toml` header row above its own
  diagnostics) are `info`; only the individual diagnostics carry warn/fail.
- Interactive prompts (Huh) require stdin+stderr TTY; otherwise exit 2 with a message naming the
  non-interactive flags.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic failure (network, API error) |
| 2 | usage error / needs TTY / invalid flag |
| 3 | not authenticated (run `ytm setup`) |
| 4 | TUI not running (IPC commands) |
| 5 | dependency missing (mpv / yt-dlp) |
| 130 | interrupted (Ctrl-C) |

## Commands

| Command | Flags | TTY output | `--json` shape |
|---|---|---|---|
| `ytm` | `--debug`, `--pprof` | launches TUI | — |
| `setup` | `--browser B`, `--slots N` (10), `--slot N`, `--manual`, `--yes` | Huh wizard (prompts on stderr; `ACCESSIBLE=1` → Huh accessible mode) | — |
| `search Q` | `-f/--filter songs\|videos\|albums\|artists\|playlists`, `-l/--limit` (20, 1–500), `--json` | styled table (`#`, type, title, artist, album/info, time, id; empty columns hidden) | `[Track\|Album\|Artist\|Playlist]` with `"type"` (+ `"top":true` on the top result) |
| `stats` | `--json` | cards + tables | `{"total_plays":…,"total_seconds":…,"top_tracks":[…],"top_artists":[…],"by_day":[…]}` |
| `history` | `-l/--limit` (50), `--json` | table | `[{"video_id","title","artist","played_at","listened_seconds"}]` |
| `history search` | `-l` (50), `--json` | list | `[{"query","searched_at"}]` |
| `cache status` | `--json` | card | `{"location","size_bytes","max_bytes","entries"}` |
| `cache clear` | `--yes` | Huh confirm | — (refuses while TUI runs, exit 1) |
| `import URL` | `--name`, `--multi`, `--yes`, `--json` | Bubble Tea progress + picker | report JSON |
| `doctor` | `--json` | sectioned report; **exit 1 if any check fails** | `{"title","meta","sections":[{"name","status","items":[{"label","status","value","detail"}]}],"problems":N,"warnings":N}` (status ∈ ok/warn/fail/info) |
| `config` | — | opens `$EDITOR` (fallbacks as original) | — |
| `config check [file\|-]` | `--json` | diagnostics list; **exit 1 on errors** (warnings exit 0) | `[{"file","line","col","key","severity","message"}]` |
| `config path` / `config defaults` | `--json` (path) | plain: config.toml path / default TOML | `{"config_file","log_file","dirs":{"config","data","cache","state","runtime"}}` |
| `theme reload` (IPC, M9) / `theme show` (M4b) | `--json` | swatches | `{"source","name","dark","transparent","path","error","wallpaper","alpha","tokens":{…},"contrast_against","adjustments":[…],"notes":[…]}` |
| `changelog` | | Glamour markdown | |
| `completion bash\|zsh\|fish\|powershell` | | cobra | |
| `play` `pause` `next` `prev` | | one-line confirm | `{"ok":true}` |
| `toggle` (new) | | | play/pause toggle for WM bindings |
| `seek S` | `+10`, `-- -5`, `1:30`, `1:02:03` | | `{"ok":true,"position":…}` |
| `play-index N` | zero-based playback order | | |
| `like` `dislike` `unlike` | | | |
| `now` | `--json` | now-playing card | `{"video_id","title","artists":[…],"album","duration","position","paused","volume","like"}` |
| `status` | `--json` | compact card | `{"state","repeat","shuffle","volume","muted","queue_length","position_in_queue"}` |
| `queue` | `--json` | styled list | `{"current":N,"items":[Track…]}` |
| `queue add ID…` / `queue clear` | | | |

Original quirk dropped (decision): original printed pretty JSON by default and compact with
`--json`. ytm-go prints human output by default and compact JSON with `--json`. Scripts using
the original without `--json` must add it (documented in README migration notes).

## Mockups (TTY)

### `ytm doctor`
```
 ytm-go doctor                                             v0.1.0 · linux/amd64

 Environment
  ✓ mpv            v0.41.0  /usr/bin/mpv
  ✓ yt-dlp         2026.10.26
  ! terminal       256 colors (truecolor recommended)
 Paths
  ✓ config         ~/.config/ytm-go
  ✓ socket dir     /run/user/1000/ytm-go (0700, 23 chars)
 Auth
  ✓ account        Jane Doe (@jane) · slot 0
  ✗ stream cookies use_session_cookies = true but stream_cookies.txt missing → run ytm setup
 Theme
  ✓ source         pywal  ~/.cache/wal/colors.json (updated 2 min ago)
  ! contrast       accent adjusted 2.1 → 3.0
 Integrations
  ✓ MPRIS          session bus reachable
 Recent errors (last 24 h)
  none

 1 problem, 2 warnings
```
ASCII icon fallback: `[ok]`, `[!!]`, `[x]`.

### `ytm now`
```
 ╭────────────────────────────────────────────────╮
 │ ▶ Instant Crush                              ♥ │
 │   Daft Punk, Julian Casablancas                │
 │   Random Access Memories                       │
 │   2:13 ━━━━━━━━━━━━╸─────────────── 5:37       │
 ╰────────────────────────────────────────────────╯
```

### `ytm stats`
```
 ╭ Listening ─────────╮ ╭ Top artists ───────────────╮
 │ 312 plays          │ │ 1  Daft Punk          41   │
 │ 18 h 22 m          │ │ 2  Justice            27   │
 │ since 2026-09-01   │ │ 3  Air                19   │
 ╰────────────────────╯ ╰────────────────────────────╯
 Top tracks
  #  Title                 Artist          Plays
  1  Instant Crush         Daft Punk          12
```

### `ytm setup`
```
 ┃ Where should ytm-go read your YouTube Music session from?
 ┃ > Firefox (default profile)
 ┃   Brave
 ┃   Chrome
 ┃   Paste request headers manually
 ┃
 ┃ Accounts found
 ┃ > Jane Doe (@jane)          slot 0
 ┃   Jane's Band (brand)       slot 0 · brand
 ✓ Signed in as Jane Doe. Saved to ~/.config/ytm-go/auth.json
```

## IPC protocol

- Transport: Unix socket `<runtime>/ytm-go/ipc.sock` (0600, dir 0700); Windows named pipe
  `\\.\pipe\ytm-go-<user>` with a security descriptor restricting to the current user (go-winio).
  No TCP (the original used TCP + token on Windows; named pipes avoid that).
- Framing: newline-delimited JSON, one request per connection.
  Request `{"v":1,"id":"<rand>","cmd":"seek","args":{"value":"+10"}}`;
  Response `{"v":1,"id":"…","ok":true,"data":{…}}` or `{"ok":false,"error":{"code":"not_playing","message":"…"}}`.
- Whitelisted commands only (original set + `toggle`, `theme_reload`). Max message 64 KiB, client
  timeout 5 s (as original).
- Single instance: TUI startup dials the socket; if a peer answers `ping`, refuse to start (exit 1,
  "ytm-go is already running"); if stale, remove and bind.
- Replies are sent after the TUI **accepts** the command (e.g. `play-index` acknowledges before audio
  starts), same as original.

## Test plan

- Golden files for each styled command: `testdata/golden/cli/<cmd>.{styled,nocolor,json}.txt`,
  generated with fixed width (80), fixed theme, `CLICOLOR_FORCE=1` for styled.
- Test: for every command supporting `--json`, output contains no `\x1b[` and parses as JSON.
- Test: non-TTY stdout → no ANSI; spinner never written to stdout.
- IPC: server/client tests with fake handler; single-instance; stale socket; oversize message; timeout.
- Huh flows: tested via model updates (Huh forms are tea.Models) + non-interactive flag paths.

## Open questions

1. Approve output-default change (human by default, JSON with `--json`) vs original (JSON default).
2. Approve new commands: `toggle`, `theme reload|show`, `changelog`, `config check|path|defaults`, `completion`.
