# 05 — UI pages, components, layout

Status: **M4 implemented** (shell, bar, help, toasts) · Milestones: M4 (shell, bar, help), M5, M6, M8

## Goals

- Pages and behavior matching the original: Library, Search, Browse, Context (album/artist/
  playlist), Queue, Liked Songs, Recently Played, Help; playlist + lyrics sidebars; playback bar;
  popups (actions, playlist picker, create playlist, confirm, input, country picker, Spotify import,
  **Settings** new, **command palette** new, **theme picker** new).
- 60 fps scrolling over 1,000+ rows; every view uses theme tokens only and passes the transparent
  render test (see 08).

## Non-goals

- Pixel-perfect copies of Textual widgets. Image protocols (kitty/sixel) for album art (possible later).

## Layout

```
┌ header: ← Back  Library  ·  g- (pending chord)                     Forward → ┐
├──────────────┬───────────────────────────────────────────┬──────────────────┤
│ Playlists    │ main page                                  │ Lyrics (g L)     │
│ (C-e)        │                                            │                  │
│ width =      │                                            │ width = 36       │
│ sidebar_width│                                            │                  │
├──────────────┴───────────────────────────────────────────┴──────────────────┤
│ selection info: full title of focused row                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│ playback bar (3 rows; album art 6×3 cells when enabled)                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

Breakpoints: < 100 cols hides lyrics sidebar (toggle overlays instead); < 80 hides playlist sidebar
and chart "Featured globally" row; < 60 compact bar (no art, 1 row). Height < 12 → bar only + list.

### Rendering strategy (perf)

- Each component caches its rendered string keyed by `(version, width, height, themeVersion)`;
  `View()` concatenates cached blocks. Only dirty components re-render (progress tick touches only
  the bar's time segment).
- Track table renders only visible rows (virtualized); row strings cached per (row, width, selected,
  marked, playing, themeVersion). 1,000-row scroll = O(visible) work.
- Progress tick: 4 Hz while playing, 0 Hz when paused/idle (idle CPU < 1 %).

## Shared component: TrackTable

Columns: mark gutter │ # │ Title │ Artist │ Album │ Duration (widths from `[ui] col_*`, 0 = flex).
Features: cursor, marks (`v`), range marks (`V`), clear (`escape`), filter (`/`, fuzzy-subsequence,
case-insensitive, matches title/artist/album), sort (`s t/a/A/d/D`, `s r`), playing indicator (▶),
autoplay marker (↻), unavailable rows dimmed, progressive loading footer ("Loading… 400/1,200"),
right-click → actions popup, click mark column → toggle mark, Ctrl-click / Shift-click marks.
Selection styles honor `transparent_selection` (see 08).

```
  #  Title                         Artist              Album              Time
  1  Get Lucky                     Daft Punk           Random Access…     6:09
▌ 2  Instant Crush                 Daft Punk, Julian…  Random Access…     5:37   ← cursor (bar mode)
● 3  Lose Yourself to Dance        Daft Punk           Random Access…     5:53   ← marked
▶ 4  Giorgio by Moroder            Daft Punk           Random Access…     9:04   ← playing
  /dance█                                         filter: 2 of 13 · sort: title ↑ · 1 marked
```

## Pages

### Library (`g l`)
Tabs: Playlists · Albums · Artists. Selecting a playlist opens it in-page (header with title,
count, total duration, `Shuffle lock: ON/off` toggle, `[▶ Play] [⤮ Shuffle] [◉ Radio]`).
```
 Playlists  Albums  Artists
 ┌──────────────────────────────────────────────┐
 │ Liked Music                     1,204 songs  │
 │ ▌Late night drives                 87 songs  │
 │ Focus                              43 songs  │
 └──────────────────────────────────────────────┘
```

### Search (`g s`)
Input with predictive suggestions (debounced 150 ms, stale responses dropped); mode toggle
music/all; results grouped: Top result card, Songs (TrackTable), Albums, Artists, Playlists,
Videos (all mode). `tab` cycles result panels. Search history (SQLite) shown when input is empty.
```
 Search ▸ daft punk█                                   mode: music
 ┌ suggestions ─────────┐
 │ daft punk get lucky  │
 │ daft punk around the │
 └──────────────────────┘
 Top result: Daft Punk · Artist                     Songs
 Albums: Discovery · Homework · RAM            ▌1 One More Time   5:20
 Playlists: This Is Daft Punk                   2 Around the W…   7:09
```

### Context page (album / artist / playlist)
Album: header (title, artist, year, track count) + TrackTable. Artist: header + Top songs +
Albums/Singles shelves + "Discography" (artist albums continuation). Playlist: same as Library
playlist view, with continuation loading.

### Browse (`g b`)
Tabs: For You (home shelves, `[ui] home_shelves`) · Charts (region picker `c`, shelf pills:
Featured globally row + country charts) · New Releases · Playlists (up to 25).
```
 For You   Charts   New releases   Playlists                 Region: Global (c)
 Featured globally:  [Coachella 2026]  [Top 100 Music Videos]
 Charts:  [Top 100 Songs]  Trending 20   Weekly Top Songs on Shorts
 ──────────────────────────────────────────────────────────────
 TrackTable…
```

### Queue (`z`)
TrackTable in playback order (shuffle-aware), "Generated from: <source>" header when radio/discovery
(`show_queue_source`), `J/K` reorder with counts, `delete`/`d d` remove, ↻ autoplay rows; removing an
autoplay pick blocks it from being re-added until queue clear/exit.

### Liked Songs (`g y`) & Recently Played (`g r`)
Liked: TrackTable + `[▶ Start Radio]` (5 random seeds). Recently Played: tabs All · Local · YT Music
with the one-line explanation under the tab row, exact semantics per original docs (100+100 rows,
merge by videoID keeping local position, partial-failure footer).

### Help (`?`)
Generated from the live keymap (so rebinds show), grouped by category, searchable with `/`.
Prose sections rendered with Glamour; links to Settings (`,`).

### Sidebars
- Playlists (`C-e`, `C-w h`): library playlists, `j/k`, `enter` opens, right-click actions,
  overflow truncate/wrap.
- Lyrics (`g L`): synced lines; `lyrics_played/current/upcoming` tokens; current line centered;
  auto-scroll pauses for 5 s after manual scroll; `C-f/C-b` scroll 10 lines; "No lyrics" state;
  source label (YouTube / LRCLIB); transliteration (`T`) and bidi per `[ui] bidi_mode`.

### Playback bar
```
 ▗▄▖  Instant Crush                                  ♥   ⤮  ⟳1   ◁◁  ▶  ▷▷
 ▐█▌  Daft Punk, Julian Casablancas · Random Access Memories
 ▝▀▘  2:13 ━━━━━━━━━━━━━━━━━━━━╸─────────────────── 5:37            vol 80%
```
Click progress → seek; scroll on progress → scrub (commits after 0.6 s); scroll on volume → ±5;
click ♥ / ⤮ / ⟳ toggles. Buffering shows spinner in place of ▶.

### Popups
Centered, max 60 × 20, border in `border` token, no fill unless `popup_fill` (08). `escape` closes.
- Actions: Play, Play next, Add to queue, Add to playlist, Start radio, Go to album, Go to artist,
  Like/Unlike, Download for offline, Remove from playlist (when applicable), Copy link.
- Playlist picker (+ create new, duplicate detection like original), Confirm, Input, Country picker
  (68 regions + Global, fuzzy filter), Spotify import (M11).
- Settings (`,`) — see 08 §6. Command palette (`:`) — fuzzy list of all actions + "Theme: …",
  "Settings", "Reload theme".

### Toasts
Bottom-right above bar, max 3 stacked, 4 s, severity tokens (error/warn/success), non-blocking.

## Transliteration & BiDi (M8)

- Transliteration: pure-Go `golang.org/x/text/unicode/norm` + a compact table-based transliterator
  (Unidecode data, ~200 KB embedded) — dependency choice justified in M8 PR; candidates evaluated then.
- BiDi: `golang.org/x/text/unicode/bidi` for `reorder` mode; `auto` reorders only lines containing
  RTL runs when the terminal is not known to do BiDi itself; `passthrough` leaves text alone.

## Edge cases

- Wide chars (CJK, emoji) → width via `charmbracelet/x/ansi` string width; truncation never splits graphemes.
- Empty states: not authenticated, offline, empty playlist, no results, API error (retry button `r`).
- Navigation stack max 20 (as original), state (cursor, scroll, filter, sort) restored on back/forward.
- Theme reload while a popup is open / during filter typing → state untouched (08).

## Test plan

- teatest-based golden snapshots per page at 80×24 and 140×40, opaque and transparent, NO_COLOR.
- Key-dispatch tests per page (actions → expected model changes), mouse event tests.
- Benchmarks: `BenchmarkTrackTableView1000`, `BenchmarkScroll1000`, `BenchmarkPlaybackBarTick`.
- Transparent-render test registry: each page registers a constructor; test renders every page and
  asserts no `48;` SGR (see 08).

## Implementation notes (M4)

- Packages: `internal/ui` (root model, layout, header, bar, toasts, Help, placeholder pages),
  `internal/keys` (grammar, trie, dispatcher), `internal/app` (composition root, shutdown, crash
  guard, `--trace-frames` wrapper). `internal/ui/components` and `pages` subpackages are deferred
  until M5 has more than one real page.
- Layout rows: header (1) · main · selection info (1) · rule (1) · bar (3). Breakpoints as above;
  the lyrics sidebar below 100 columns overlays the right side of the main area.
- **Root background:** opaque mode sets `tea.View.BackgroundColor/ForegroundColor` (terminal default
  colors via OSC 10/11, restored on exit) instead of painting cells: nested spans end in SGR reset,
  which cuts a parent fill short. Only single-line strips (header, info bar), solid selection and
  popups/toasts paint backgrounds. Transparent and NO_COLOR leave the terminal colors alone.
- Help is plain Lip Gloss text generated from the live keymap, filterable with `/`. **Glamour is not
  used** (no dependency added): the prose is three lines, and Glamour's built-in styles paint code
  backgrounds that the transparent test would have to exempt. Revisit if Help grows real prose.
- Actions whose features arrive later (queue, likes, radio, Settings, palette, theme picker, album
  art, lyrics) show an info toast "Not available yet: … arrives in a later version".
- `toggle_transparency` flips for the session only; persisting it needs the comment-preserving
  `config.Patch` (M4b, with Settings).
- Timers (toast expiry, chord timeout) use a context-aware `after` Cmd instead of `tea.Tick`, whose
  goroutine outlives the program by up to the tick length (found by goleak).
- Mouse in M4: header ← Back / Forward →, click on the progress bar seeks, wheel over the volume
  changes it by 5. Volume step 5 per `+`/`-` (× count).
- Tests: a scenario registry (`internal/ui/ui_test.go`) drives the transparent-render test (2 sizes ×
  3 selection modes), NO_COLOR test, width/height fit test (6 sizes) and goldens (plain text at 80×24
  and 140×40, styled opaque/transparent at 80×24, `testdata/TestGoldens/`). Goldens snapshot
  `View().Content` directly; one `teatest` test runs the real program loop. New pages must be added
  to the registry.

## Open questions

1. Lyrics sidebar default width 36 — add `[ui] lyrics_width`? (default yes, 24..80)
