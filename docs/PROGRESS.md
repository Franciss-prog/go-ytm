# Progress

Last updated: 2026-11-22

## Milestones

| # | Slug | Status | PR | Notes |
|---|---|---|---|---|
| P0 | specs | **approved** 2026-11-16 | [#1](https://github.com/Franciss-prog/go-ytm/pull/1) | all proposed defaults accepted |
| M0 | scaffold | **merged** 2026-11-16 | [#2](https://github.com/Franciss-prog/go-ytm/pull/2) | CLI skeleton, config loader, logging, style, doctor stub |
| M1 | player-core | **merged** 2026-11-16 | [#3](https://github.com/Franciss-prog/go-ytm/pull/3) | mpv launcher, JSON IPC, controller, crash restore, fakempv |
| M2 | ytmusic-auth-search | **merged** 2026-11-16 | [#4](https://github.com/Franciss-prog/go-ytm/pull/4) | InnerTube client, search, setup, auth store, doctor Auth |
| M3 | stream-resolve | **merged** 2026-11-17 | [#5](https://github.com/Franciss-prog/go-ytm/pull/5) | yt-dlp resolver, URL cache, prefetcher, doctor mpv version |
| M4 | tui-shell | **merged** 2026-11-17 | [#6](https://github.com/Franciss-prog/go-ytm/pull/6) | keymap engine, root model, header, bar, toasts, Help, app wiring |
| M4b | dynamic-theme | **merged** 2026-11-17 | [#7](https://github.com/Franciss-prog/go-ytm/pull/7) | sources, contrast, depth, watcher, Patch, Settings, `theme show` |
| M5 | tui-library-search-queue | **merged** 2026-11-17 | [#8](https://github.com/Franciss-prog/go-ytm/pull/8) | queue pkg, ytmusic browse/library/edit calls, TrackTable, playback owner (resolve/gapless/retry), Queue, Library, Context, Search pages, popups, playlist sidebar, mouse |
| M5b | ytkew-look | **merged** 2026-11-17 (spec 09 approved 2026-11-17) | [#9](https://github.com/Franciss-prog/go-ytm/pull/9) | side quest, spec 09: frame, rows, Library columns, track page, cover art, visualizer |
| M6 | browse-liked-history | **merged** 2026-11-22 | [#10](https://github.com/Franciss-prog/go-ytm/pull/10) | ytmusic watch/radio/home/charts/explore/history, radio + autoplay + discovery + history sync, Browse, Liked Songs, Recently Played, country picker |
| M7 | session-cache-offline | **merged** 2026-11-22 | [#11](https://github.com/Franciss-prog/go-ytm/pull/11) | SQLite store (play/search history, cache index), session.json resume, shuffle_prefs.json lock, yt-dlp downloads + LRU audio cache, search history, Download action |
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
| 2026-11-17 | Quality format strings copied from ytm-player 2.2.0 (`bestaudio/…` + combined fallback), replacing the draft's opus-first strings | 03 |
| 2026-11-17 | One yt-dlp process per resolve; warm process rejected (startup 0.2 s of ~4 s; `--batch-file -` reads stdin to EOF) | 03 |
| 2026-11-17 | yt-dlp `-O '%(.{fields})j'` instead of `-J` (1.6 KB vs 650 KB output) | 03 |
| 2026-11-17 | Cookie jar copied to a private temp file per resolve (yt-dlp writes `--cookies` back on exit) | 03 |
| 2026-11-17 | Every URL handed to mpv waits for `available_at` (`Cache.Playable`) | 03 |
| 2026-11-17 | Fake yt-dlp = re-executed test binary, not a shell script (Windows CI) | 03 |
| 2026-11-17 | `doctor` warns on mpv < 0.33 (deferred from M1) | 02 |
| 2026-11-17 | Opaque root background via `tea.View.BackgroundColor` (terminal default bg), not SGR fills | 05, 08 |
| 2026-11-17 | Glamour not added: Help is Lip Gloss text from the live keymap | 01, 05 |
| 2026-11-17 | Keymap: user entry replaces an action's defaults; prefix bindings dropped; `5 g g` = row 5 | 04 |
| 2026-11-17 | Timers via ctx-aware Cmd, not `tea.Tick` (its goroutine outlives the program) | 05 |
| 2026-11-17 | `toggle_transparency` is session-only until `config.Patch` lands (M4b) | 05 |
| 2026-11-17 | Root `ytm` without a TTY exits 2 with a hint; `--trace-frames` added (spec 00) | 06 |
| 2026-11-17 | Deps added (test-only): `x/exp/teatest/v2`, `x/exp/golden`; `bubbletea/v2` v2.1.1 promoted to direct | 01 |
| 2026-11-17 | Deps added: `fsnotify` v1.10.1 (listed in 01); `go-colorful` v1.4.1 promoted to direct (has OkLab/OkLch, no in-house OKLCH) | 01 |
| 2026-11-17 | `Dark` = luminance < 0.179 (equal white/black contrast), not 0.5 | 08 |
| 2026-11-17 | Built-in themes are embedded TOML parsed like `theme.toml`; nord/gruvbox from nord.css / gruvbox.vim | 08 |
| 2026-11-17 | `theme show --json` contrast ratios rounded to 2 decimals (full precision differed on macOS arm64) | 06, 08 |
| 2026-11-17 | Track page = centered kew-style card (art above title); up next split dropped, Queue page keeps it (owner) | 09 |
| 2026-11-17 | Hover prefetch: cursor at rest 0.4 s on a track resolves it (`[playback] prefetch_on_hover`, default on). Cold Enter 10.3 s → 1.2 s to `0:01` live (owner) | 03, 04 |
| 2026-11-17 | Broken palette file keeps current colors; Settings → missing source shows fallback; toast once per error | 08 |
| 2026-11-17 | `config.Patch` writes basic strings, refuses keys inside inline tables, re-parses before rename | 04 |
| 2026-11-17 | `ytm theme reload` deferred to M9 (IPC); SIGUSR1 reloads now | 06, 08 |
| 2026-11-17 | OSC 11 queried only at startup in transparent mode; then COLORFGBG; else black | 08 |
| 2026-11-17 | Queue owner lives in the root UI model (`ui/playback.go`); pages send `PlayMsg`/`EnqueueMsg` | 05 |
| 2026-11-17 | Pages stay in package `ui` (a `pages` subpackage would need the root's message types in a third package) | 01, 05 |
| 2026-11-17 | TrackTable filter is fuzzy per field (title, artist, album), not over the joined string (too loose) | 05 |
| 2026-11-17 | `sort_date` = source order (playlist order / playback order); no date-added field in responses | 05 |
| 2026-11-17 | `V` marks from the last toggled row to the cursor; with no anchor it marks the cursor and anchors | 05 |
| 2026-11-17 | Playlist sidebar ported from ytm-player 2.2.0 `playlist_sidebar.py`: pinned nav rows, Liked Music left out, mouse-only pinned rows; enter keeps focus in the sidebar | 05 |
| 2026-11-17 | Sidebar loads its own `LibraryPlaylists` (not shared with the Library page): one extra call at start, simpler ownership | 05 |
| 2026-11-17 | Mouse semantics from ytm-player 2.2.0 `track_table.py` / `progress_bar.py`: click cursor row = select, scrub 3 s/tick, commit after 0.6 s; wheel moves the cursor (not the viewport) | 05 |
| 2026-11-17 | Nerd icons for Recent/Mix = Font Awesome `f017`/`f001` (same codepoints in Nerd Fonts) | 05 |
| 2026-11-17 | ytkew look (spec 09) reimplemented from screenshots only; ytkew is GPL-3.0, no code ported | 09 |
| 2026-11-17 | Cover art as JPEG via `=…-rj` URL suffix (verified live); no WebP dependency | 09 |
| 2026-11-17 | Kitty art via `x/ansi/kitty` Unicode placeholders; `ultraviolet` promoted indirect → direct for `KittyGraphicsEvent` | 09, 01 |
| 2026-11-17 | Visualizer = `pw-cat` sink-monitor capture + in-house FFT (no dependency); Linux only | 09 |

| 2026-11-22 | Radio/autoplay/discovery follow ytm-player `_playback.py` (refill at ≤ 3 left, 5 seeds, 6 discovery sources in rotation) | 03, 05 |
| 2026-11-22 | `Explore` replaces the draft `NewReleases`; `AddHistoryItem` folds in `Song` (player call) | 03 |
| 2026-11-22 | Charts tab = list of chart playlists opening the playlist page (not pills over a table) | 05 |
| 2026-11-22 | Recently Played → Local is in memory until M7's store; no new `start_radio` key (popup + click) | 05 |
| 2026-11-22 | Signed-out 200 pages (history, `VLLM`, `logged_in=0`) trigger silent renewal | 03 |
| 2026-11-22 | History pings send cookies to `https` `*.youtube.com` only | 03 |

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
- yt-dlp 2026.10.26 `-O '%(.{url,…})j'` prints one JSON line; `available_at` present on every
  signed-out resolve; URL `expire` = resolve + 6 h (live, `TestLiveResolve`).
- googlevideo answers **403 until `available_at`** (≈3 s after resolve): mpv fails "loading failed"
  (`HTTP error 403 Forbidden`); after it, 48/48 ranged GETs returned 206 (Python probe). One
  403 in 12 Go GETs after the wait was seen once (not reproduced on rerun).
- Medium and low resolve the same itag (250, ~61 kbit/s) for the test track; high = itag 251 opus 128.
- `--js-runtimes` adds to the default `deno`; the Python API's `js_runtimes` replaces it
  (`yt_dlp/__init__.py` builds the dict from the option list), so we pass `--no-js-runtimes` first.
- yt-dlp error texts: "This video is unavailable" (live), others read from
  `extractor/youtube/_video.py` (`raise_no_formats(reason)`, "sign in" → login hint, rate-limit note).
- Real mpv 0.41.0 plays a resolved googlevideo URL at an offset and advances gaplessly into an
  appended prefetched URL (`TestLiveGapless`, live).
- Bubble Tea v2.1.1 `Key.Keystroke()` names: `space`, `enter`, `esc`, `pgup`/`pgdown`,
  `shift+tab`, `shift+backspace`, `ctrl+a`, `alt+x`; shifted printables arrive as `Text` ("A", "?")
  (probed in a test). `Program.Send` blocks before start and is a no-op after exit (`go doc`).
- `tea.Tick`'s goroutine waits out its full duration after the program exits (goleak in
  `internal/app`), hence the ctx-aware timer.
- matugen 4.2.0 renders every role of `contrib/matugen/ytm-go.toml` (isolated `-c` config, script).
- xterm default 16 colors: `/usr/share/X11/app-defaults/XTerm-color` + X11 `rgb.txt`.
- Terminal opacity keys: kitty `background_opacity` (kitty source), alacritty `[window] opacity`
  (`man 5 alacritty`), foot `[colors-dark] alpha` since 1.26 (foot CHANGELOG), WezTerm
  `window_background_opacity` (wezterm.org).
- go-toml unstable: scalar value `Raw` includes quotes; KeyValue `Raw` = `key = value` without
  trailing comment; Array/InlineTable value and Table `Raw` are empty (probe test).
- Real binary in a PTY (110×30, pywal source): wallpaper rename → recolored 116–118 ms; SIGUSR1 →
  "Theme reloaded"; half-written file keeps colors + toast, recovery toast; Settings save and `C-t`
  wrote `popup_fill`/`transparent` keeping comments.
- Real binary in a PTY (110×30, signed in): sidebar lists the owner's library playlists, `C-w h`,
  `j`, enter opens the playlist page, `C-e` hides the sidebar.
- InnerTube 2026-11-22 (signed out, live, `TestRecordM6`): `next` radio returns 50 panel items with
  `nextRadioContinuationData`; home = carousels of flat song rows + two-row playlists with a
  `sectionListRenderer` continuation; charts first section is the country selector, ~70
  `musicFormBooleanChoice` region tokens in `frameworkUpdates`; explore sections keyed by header
  browse id. Signed-out history/LM answer 200 with `logged_in=0`.
- Real binary in a PTY (120×34, 2026-11-22, owner's account via in-process renewal): Browse For You
  and Charts load, `c` → "united st" → US charts, enter opens "Top 100 Live Performances" (100
  songs); Liked Songs lists the account's 6 likes; Recently Played lists real account plays.
- Real binary in a PTY (110×30): renders, `?`/`j`/`g s`/backspace/`3+`/`q` work, alt screen
  entered and left, OSC 11 sets the root bg, exit 0, `--trace-frames` logs every frame.

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

## Measurements (M3, local, Linux amd64)

| What | Result |
|---|---|
| yt-dlp resolve, wall (live, signed out) | 3.7–4.9 s typical, one 10.2 s outlier (12 runs) |
| `yt-dlp --version` / import + init | 0.18 s / 0.2 s |
| `-J` vs `-O` field subset output | 668 KB vs 1.6 KB |
| Parse `-O` output | 7.5 µs/op, 15 allocs |
| Cache hit | 497 ns/op, 0 allocs |
| Coverage | stream 93 % |

## Measurements (M4, local, Linux amd64)

| What | Result | Budget |
|---|---|---|
| Cold start → first frame (`ytm` in a PTY, fork to "Nothing playing" on screen) | 23 ms median (15 runs) | 150 ms |
| Keypress → repaint (`j` on Help, 140×40, Update + View) | 0.38 ms/op | 16 ms |
| Playback bar tick (StateChanged + full View, 140×40) | 0.15 ms/op | — |
| Idle CPU (no playback, 10 s) | 0.5 % | 1 % |

## Measurements (M4b, local, Linux amd64)

| What | Result | Budget |
|---|---|---|
| Wallpaper change → recolored (real binary, PTY) | 116–118 ms (3 runs) | 300 ms |
| `ThemeChangedMsg` + full repaint 200×60 | 1.1 ms/op | 50 ms |
| Styles build (256-color downsample) | 45 µs/op | 1 ms |
| `EnforceContrast` (worst-case palette) | 0.23 ms/op | — |

## Measurements (M5, local, Linux amd64)

| What | Result | Budget |
|---|---|---|
| `BenchmarkScroll1000` (TrackTable, 1,000 rows) | 0.13 ms/op, 96 allocs | 16 ms frame |
| `BenchmarkTrackTableView1000` | 0.10 ms/op, 21 allocs | — |
| Keypress → repaint (root model) | 0.41 ms/op | 16 ms |
| Coverage | queue 91 %, ui 87 %, ui/components 75 %, ytmusic 85 % | — |

## Measurements (M5b, local, Linux amd64)

| What | Result | Budget |
|---|---|---|
| Spectrum analysis frame (2048 FFT, 96 bands) | 37 µs/op, 0 allocs (~0.1 % core at 30 fps) | 2 % CPU |
| pw-cat while on the track page (live) | 0.0 % CPU | — |
| `BenchmarkScroll1000` after the row restyle | 0.12 ms/op | 16 ms |

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
- M3: not tested signed in (`use_session_cookies`, `cookies_file`), with `ca_bundle`,
  `remote_components` or `js_runtimes` against real yt-dlp — args are unit-tested only.
- M3: age-gated / region-blocked / Premium-only classification is from yt-dlp source, not live
  captures (only "This video is unavailable" was observed).
- M3: `Stream.Headers` are parsed but not sent to mpv (ytm-player does not either; URLs play
  without them). Revisit if 403s appear after `available_at`.
- M3: re-resolve-once-then-skip policy and prefetch → `Append` wiring land with the queue owner
  (M4/M5); M3 ships `Cache.Refresh`, `Cache.Playable`, `Prefetcher`.
- M4b: `ytm theme reload` needs IPC (M9); palette "Reload theme" needs the palette (M5).
- M4b: OSC 4 palette queries not implemented (16-color mapping assumes xterm defaults); OSC 11 is
  only asked at startup in transparent mode. tmux passthrough untested.
- M4b: not run against a real `wal -i` (would rewrite the owner's palette); pywal fixtures are
  pywal's own schemes plus the owner's file. Not tested on macOS/Windows terminals.
- M5: playlist writes (add, create, remove, delete, like) only tested against the fake client;
  the ytmusic write bodies were never sent live (see 03).
- M5: picker has no recently-used ordering yet.
- M5: Search page not driven live in a terminal yet; covered by fake-API tests and goldens.
- M5: mouse driven live (tmux SGR events, signed in): sidebar click opens, row click / right-click
  popup / wheel / Back work. **Bar glyph clicks and wheel scrubbing not run live** (nothing was
  playing); fake-player tests only. Ctrl/Shift-click depend on the terminal passing modifiers.
- M5: `sidebar_overflow = "wrap"` only
  tested with goldens/unit tests, not live. The main page keeps drawing its cursor while the
  sidebar has focus (only the sidebar title/cursor show focus).
- `TestRestartBackoffAndGiveUp` (player) failed once under `make test` (its state-after-give-up
  check, `controller_test.go:412`); 20/20 passes on rerun. Timing flake, not investigated yet.
- M6: **radio, autoplay, discovery mix and the history ping were not run live** (they start
  playback and write to the account history); covered by fake-player/fake-API tests only.
  `AddHistoryItem` has never been sent to YouTube.
- M6: history fixture is synthetic; `TestRecordM6` cannot renew a stale session itself, so the
  signed-in capture needs a fresh `ytm setup` first.
- M6: no benchmarks for the new pages (lists of ≤ 200 rows reuse List/TrackTable).
- `popup-confirm-delete` golden scenario renders no popup (the root column has nothing to
  delete); pre-existing, noticed in M6, not fixed.
- M4b: album-art transparency rules wait for album art (no art yet, no exemption registered).
- fang queries the terminal background (OSC 11) when rendering help on a TTY; harmless on real
  terminals, visible as junk only under `script`-style captures.
- M7: driven live once in tmux (2026-11-22, scratch XDG dirs): search logged, a track downloaded
  (1.6 MB webm) and played from disk, session saved on quit and restored, play/pause resumed at
  the saved position. That run found the memoized-URL bug fixed in `fb5e96e`. Not tested: a full
  cache eviction on disk, `[cache] location`, downloads signed in, macOS/Windows paths.
- M7: listen time stored is the time at `history_min_listen_seconds`, not the final one (M9 stats).
- M7: shuffle lock has no keyboard toggle on the playlist page itself (mouse line, or the library
  playlist popup); `shuffle_prefs.json` cap drops arbitrary entries, not LRU.
- M7: `play_pause` with nothing loaded now starts the queue's current track (was a "Nothing to
  play yet" toast); needed for resume, also applies after the queue ends.
- M7: `cache status` / `cache clear` CLI land with M9 (spec 00).
