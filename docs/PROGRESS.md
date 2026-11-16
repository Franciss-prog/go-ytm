# Progress

Last updated: 2026-11-17

## Milestones

| # | Slug | Status | PR | Notes |
|---|---|---|---|---|
| P0 | specs | **approved** 2026-11-16 | [#1](https://github.com/Franciss-prog/go-ytm/pull/1) | all proposed defaults accepted |
| M0 | scaffold | **in review** | branch `feat/scaffold` (stacked on #1) | CLI skeleton, config loader, logging, style, doctor stub |
| M1 | player-core | **in review** | [#3](https://github.com/Franciss-prog/go-ytm/pull/3) (stacked on #2) | mpv launcher, JSON IPC, controller, crash restore, fakempv |
| M2 | ytmusic-auth-search | **in review** | [#4](https://github.com/Franciss-prog/go-ytm/pull/4) (stacked on #3) | InnerTube client, search, setup, auth store, doctor Auth |
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
| 2026-11-17 | Cookie extraction = `yt-dlp --cookies-from-browser … --cookies FILE` with **no URL** (0.25 s, offline; jar saved on exit). kooky rejected | 03 |
| 2026-11-17 | InnerTube `clientVersion` uses today's UTC date like ytmusicapi (no build constant / CI refresh job) | 03 |
| 2026-11-17 | `hl` fixed to `en`; result types come from page types/browse ids, not labels | 03 |
| 2026-11-17 | Token bucket in-house on `clock.Clock` (no `x/time/rate`); singleflight from `x/sync` (already in graph, now direct) | 03 |
| 2026-11-17 | Deps added: `charm.land/huh/v2` v2.0.3 (setup prompts, listed in 01); `golang.org/x/sync` promoted indirect → direct | 01 |
| 2026-11-17 | `ytm search` with a rejected session falls back to signed-out results + warning (exit 0) | 03 |
| 2026-11-17 | No InnerTube `key` param (yt-dlp omits it on cookie-auth calls; GitGuardian flagged the public web key) | 03 |
| 2026-11-17 | CLI error handler capitalizes only plain first words (fang title-cased `yt-dlp` → `Yt-Dlp`) | 06 |

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
- ytmusicapi pinned at `86fcd17` (2026-11-12): search `params`, continuation query
  (`&ctoken=…&continuation=…&type=next`), SAPISIDHASH from `__Secure-3PAPISID`, account-menu paths (read source).
- InnerTube 2026-11-17 (signed out, live): unfiltered search = `musicCardShelfRenderer` + one
  `itemSectionRenderer` per item, subtitle starts with a type label, **no duration** on rows;
  filtered = one `musicShelfRenderer` with `nextContinuationData`; albums/artists keep the type
  label even when filtered. Fixtures recorded then scrubbed.
- yt-dlp 2026.10.26 browsers: brave, chrome, chromium, edge, firefox, opera, safari, vivaldi, whale
  (no zen/helium); keyrings basictext, gnomekeyring, kwallet, kwallet5, kwallet6 (`--help`).
- yt-dlp with no URL still writes `--cookies` (exit 2); missing profile → `ERROR: could not find
  firefox cookies database` and no file (local probe + `TestRealYtDlpExportsWithoutURL`).
- Signed-out `account/account_menu` has no `activeAccountHeaderRenderer` (live); bogus cookies via
  `ytm setup --manual` exit 3 (live).
- `ytm search` live: unfiltered, all 5 filters, 45/60-result continuation paging, suggestions
  (`YTM_LIVE=1 go test -tags live`).

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

## Measurements (M2, local, Linux amd64)

| What | Result |
|---|---|
| `ytm search --json daft punk` (live, signed out) | 527 ms median of 5 |
| `ytm search -f songs -l 60` (3 pages) | 1.5 s median of 3 |
| Parse unfiltered search page | 36.5 µs/op, 127 allocs |
| yt-dlp cookie export (no URL) | 0.25 s |
| Binary size (`-s -w`, trimpath) | 15 MB (was 12.8 MB; + Huh/Bubble Tea) |

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
- M1: mpv versions older than 0.41.0 untested (local and CI `mpv-integration` runner both 0.41.0).
- M1: `doctor` does not yet warn on old mpv versions (spec 02 edge case) — deferred to M3.
- M2: **not tested against a real signed-in browser.** Extraction is proven with a throwaway Firefox
  profile; account probing/renewal against fakes and a *synthetic* `account_menu` fixture
  (`testdata/responses/account_synthetic.json`). Needs one owner run of `ytm setup` to confirm, and a
  scrubbed signed-in capture to replace the synthetic fixture.
- M2: brand-account **listing** in setup deferred (needs a signed-in capture); `brand_account_id` is honored.
- M2: Helium path is Linux-only (as in ytm-player); Zen/Helium/keyring retries unverified on real installs.
- M2: no spinner during `ytm search`/`setup` network calls (sub-second typical); revisit with M4's Bubble Tea.
- `golangci-lint` / `staticcheck` not installed locally; Makefile falls back to `go run` (works, ~40 s cold).
- CI green on linux/macOS/windows × stable/oldstable (PR #2). CLI golden files are Linux reference
  output and skip on Windows (host path rules differ); policy, no-ANSI, JSON and exit-code tests run there.
- fang queries the terminal background (OSC 11) when rendering help on a TTY; harmless on real
  terminals, visible as junk only under `script`-style captures.
