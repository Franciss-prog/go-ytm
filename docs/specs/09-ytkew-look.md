# 09 — ytkew look (M5b)

Status: **approved** 2026-11-17 (open questions: proposed defaults) · Milestone: M5b `ytkew-look` (side quest, stacked on M5)

Visual reference: [ytkew](https://github.com/dtDhruv/ytkew) screenshots (`assets/ytkew_*.png`, commit
`316cd85`). ytkew is **GPL-3.0-or-later**; ytm-go is MIT. We reimplement the *look* from the
screenshots and README only. **No ytkew source is read for porting or copied.** Approaches are
designed from our own code and public docs (Bubble Tea, Lip Gloss, kitty graphics protocol,
PipeWire `pw-cat`).

## Goals

- Replace the M4/M5 chrome with a ytkew-style frame: tab bar, one titled bordered panel per page,
  a one-row mini player, and a key-hint footer.
- Library as Miller columns (one column per level), like a file manager.
- A new **track** page: a centered playing card (cover art, metadata, spectrum, progress).
- Cover art: truecolor half-blocks everywhere, kitty graphics where supported.
- Spectrum visualizer on Linux (PipeWire).
- All existing pages, keys, popups and mouse targets keep working. Transparent mode stays first
  class (ytkew's screenshots are a transparent terminal over a blurred wallpaper).

## Non-goals

- Pixel matching ytkew, its keymap (`1–4` tabs, `h/l` skip, `a/d` seek, `F6` help) or its config.
- Colors derived from album art (ytkew has it; possible later, needs a theme source).
- A "classic" layout toggle. The new frame replaces the old one (one set of goldens).
- macOS/Windows visualizer.

## Layout

```
 ytm  queue  library  track  search  browse                        1/30  ⤮ ⟳  vol ▪▪▪▪▫ 80%
────────╯       ╰──────────────────────────────────────────────────────────────────────────────
╭│ library │────────────────────────────────────────────────────────────────────────────────╮
│ Liked Music      ▸│ Avicii        ▸│ Sob Rock          ▸│ John Mayer — Last Train Home     │
│ Artists          ▸│ John Mayer    ▸│ Paradise Valley   ▸│ John Mayer — New Light           │
│ …                                                                                        │
╰────────────────────────────────────────────────────────────────────────────────│ 1/10 │──╯
 ▌ ⏸ Last Train Home — John Mayer          ───────────●──────────────────    1:54 / 3:57
 enter open · h/→ level · A playlist · Z queue · ? help
```

Rows, top to bottom: tab bar (1) · tab rule (1) · panel (rest) · mini player (1) · hints (1).
Selection info (`show_selection_info`) renders inside the panel's bottom border, left side, when on.

- **Tab bar.** App name in `accent`, then tab names; the active tab in `text` bold with a notch in
  the rule below it (`╯ name ╰`, rule broken under the active tab), inactive in `muted`. Right side:
  queue position `i/n`, shuffle/repeat glyphs (`accent` when on, `muted` when off; clickable),
  volume as 5 dots + percent (wheel changes it). Pending
  chord/count (`g-`, `15`) shows left of the position in `accent`.
- **Tabs** map to existing actions: queue (`z`), library (`g l`), track (`g n`, new), search
  (`g s`), browse (`g b`, shown since M6). Liked Songs and Recently Played open under
  *library*; Context pages under the tab they were opened from. Help (`?`) and Settings (`,`) stay
  overlays. Back/Forward (`backspace` / `S-backspace`) unchanged; the header's `← Back / Forward →`
  text is removed, mouse back/forward moves to the side buttons (X1/X2) only.
- **Panel.** Rounded border in `border`; title `│ name │` top-left (page name, or `album · Title`,
  `artist · Name`, `playlist · Name`); counter `│ cursor/total │` bottom-right. Two-panel pages
  (track page, sidebar shown) draw two panels side by side.
- **Mini player.** `▌` in `accent` when playing (muted when paused), ⏵/⏸ (spinner while
  buffering), `title — artist`, ♥ (`accent` when liked, clickable), then a line progress bar with a `●` knob over the right ~40 %, then
  `pos / dur` in `muted`. Hidden on the track page (it has its own progress). "Nothing playing" when
  idle. The 3-row bar and its 6×3 art are removed.
- **Hint footer.** Page-specific `key action` pairs joined by ` · ` in `muted` keys / `text`
  actions, built from the **live keymap** (rebinds show), truncated from the right to fit; `? help`
  always last in `muted`. Replaces nothing (new row).
- **Sidebar.** Hidden at start (new `[ui] show_sidebar`, default **false**; today it is always shown). When toggled (`C-e`) it is a titled panel
  `│ playlists │` left of the main panel; focus rules unchanged.
- **Lyrics** (M8) will be a titled panel on the right, same rules.

Breakpoints: < 80 cols drops the tab names to the active one; < 60 drops the progress bar from the
mini player; height < 12 drops the hint footer and tab rule.

## Rows and selection

TrackTable keeps every feature (marks, filter, sort, playing ▶, ↻, dimmed rows). Changes:

- **No header row.** Sort and filter state stay in the existing footer line.
- Column order `# · Artist · Title · Album · Time`; artist in `muted`, title in `text`, time
  right-aligned in `muted`. Album shows only when the panel is ≥ 120 cols wide.
- `[theme] transparent_selection` default becomes **`solid`**: full-width `selection_bg` fill with
  `selection_fg` text, in opaque *and* transparent mode (the only background SGR a transparent
  frame emits; already allowed by 08). `bar|reverse|underline` remain.
- Drillable rows (Library columns, sidebar) end in `▸` in `muted`; scrollable lists show `↑`/`↓` in
  the right margin at the first/last visible row when more rows exist.

## Library: Miller columns

Replaces the Playlists · Albums · Artists tabs.

- Column 1 (root): Liked Music, Recently Played (account history, since M6), Artists, Albums,
  Playlists.
- Levels: Artists → artist → albums/singles → tracks · Albums → album → tracks · Playlists →
  playlist → tracks · Liked Music → tracks (playlist `LM`).
- Up to 4 columns visible; the focused column and its parent get the most width
  (widths 20 % / 20 % / 25 % / 35 % at 4 columns, deeper levels scroll the leftmost out).
  Track columns render `Artist — Title` in one cell.
- Keys (page-scoped, new actions, see 04): `column_left` = `h` `left`, `column_right` = `right`
  (and `enter` on a drillable row). `l` stays `like_toggle`. Moving the cursor in a column loads the
  next column (debounced 150 ms, stale responses dropped, cached per id for the session).
- `enter` on a track plays the column from that track; `P` (`play_all`, new) plays the whole
  column; all track actions (`a`, `A`, `Z`, `X`, `v`, `/`) work in track columns.
- Mouse: click selects in any column; click on a drillable row opens the next level; wheel scrolls
  the column under the pointer.
- The full playlist page (header, `[▶ Play] [⤮ Shuffle]`, filter, sort) stays reachable with
  `g space` (`current_context`) from a track column or from search.

## Track page (`g n`, tab "track")

Revised 2026-11-17 (owner): a full-width "playing card" like kew's track view replaces the
now playing / up next split. Up next lives on the Queue page only.

```
╭│ now playing │───────────────────────────────────────────────────────────────────╮
│                                                                                   │
│                    ┌──────────────────┐                                           │
│                    │                  │                                           │
│                    │    cover art     │                                           │
│                    │                  │                                           │
│                    └──────────────────┘                                           │
│                                                                                   │
│                    Stairway to Heaven                                             │
│                    Led Zeppelin                                                   │
│                    Led Zeppelin IV                                                │
│                                                                                   │
│                    1:54 / 8:02 · vol 80% · 3/30                                   │
│                                                                                   │
│                    ▂▅█▇▅▃▂▃▆▇▅▃▂▃▅▆▇▅▃▂▃▅▆▇▅▃▂▃▅▆▇▅                               │
│                    ───────────●────────────────────                               │
╰──────────────────────────────────────────────────────────────────────────────────╯
```

- One panel, title "now playing". A card column `cw` = 60 % of the content width, 30–80 cells
  (never wider than the content), centered horizontally; the whole card is centered vertically.
- Top to bottom, left-aligned in the column: art (square, 2 cells per row, as large as fits:
  `rows ≤ cw/2`, hidden below 4 rows), a blank row, title (`text` bold), artist (`accent`), album
  (`muted`), a blank row, `pos / dur · vol N% · i/n` (`muted`; `muted` replaces `vol N%` when
  muted), a blank row, spectrum `cw` wide (when the visualizer is on), progress line `cw` wide
  (`[ui] progress_style`). Rows drop from the art first when the panel is short.
- Clicks on the progress line seek and the wheel scrubs, as on the mini player (hidden here).
- Keys: the global ones (play, seek, volume, next/prev, …). No page-scoped keys.

## Cover art

`[ui] album_art_mode = "auto"|"kitty"|"blocks"|"off"` (was `blocks|off`; default becomes `auto`).

- Source: the track's largest thumbnail ≤ 544 px from the API response, fetched over the shared
  HTTP client, decoded with `image/jpeg` / `image/png` (WebP: see open questions), scaled in-house
  (box filter), cached in memory (LRU 32) and on disk under the cache dir (M7 owns eviction).
- **blocks**: `▀` with fg = upper pixel, bg = lower pixel, 2 px per cell vertically. These are
  background SGRs by design: the art region is registered as an exemption in the transparent-render
  test (08 already reserves this).
- **kitty**: kitty graphics protocol with **Unicode placeholders** (`U=1`, image transmitted once,
  placed by writing placeholder cells with the image id in the fg color). Placeholders are ordinary
  text cells, so Bubble Tea's diffing renderer keeps working. Detected by querying support
  (`a=q`) at startup; supported by kitty and Ghostty (to verify: WezTerm). Images deleted on exit.
- **sixel**: not planned (sixel images are not cells; they fight a diffing renderer). Terminals
  without kitty graphics get blocks.
- `toggle_album_art` (`C-a`) cycles on/off as today.

## Spectrum visualizer (Linux)

- Capture: `pw-cat --record --target <default sink>.monitor --format s16 --rate 48000
  --channels 1 -` (subprocess behind an `AudioTap` interface with a fake), started only while
  the track page is visible **and** playback is running; stopped when either ends.
- Analysis in-house: Hann window, radix-2 FFT of 2048 samples (~60 lines, no dependency),
  log-spaced bands 45 Hz–16 kHz, per-band peak decay, 30 Hz update while visible (0 Hz otherwise).
- Render: one column per band with `▁▂▃▄▅▆▇█` in `accent`, height 6 rows (fewer when short).
- Off when `pw-cat` is missing, not on Linux, or `[ui] visualizer = false`; a failed capture leaves
  the area empty and logs once, never toasts repeatedly.
- Cost budget: < 2 % CPU while visible (measured), 0 when hidden.

## Config and key changes (spec 04 to be updated on approval)

| Key / action | Change |
|---|---|
| `[ui] show_sidebar` | new, bool, default `false` (sidebar was always shown at start) |
| `[theme] transparent_selection` | default `bar` → `solid` |
| `[ui] album_art_mode` | values `auto\|kitty\|blocks\|off`, default `auto` |
| `[ui] visualizer` | new, bool, default `true` |
| `[ui] show_hints` | new, bool, default `true` |
| `track_page` | new action, `g n` |
| `column_left` / `column_right` | new page-scoped actions, `h` `left` / `right` |
| `play_all` | new action, `P` |
| `[ui] progress_style` | applies to the track page; mini player always uses `line` |

## Phases (commits on `feat/ytkew-look`, one PR)

1. **Frame**: tab bar, panel with title/counter, mini player, hint footer, sidebar default, mouse
   targets moved, goldens regenerated, transparent registry updated.
2. **Rows**: TrackTable column order/no header, solid selection default, `▸` and scroll arrows.
3. **Library columns**: Miller columns + new actions.
4. **Track page** (text, progress, up next).
5. **Cover art**: blocks, then kitty placeholders.
6. **Visualizer**: `AudioTap` + FFT + bars.

## Tests

- Goldens regenerated for every page at 80×24 and 140×40, opaque/transparent/NO_COLOR; new pages
  (library columns, track page) added to the scenario registry and the transparent-render test (art
  region exempt only when `blocks`).
- Hint footer built from a rebound keymap shows the rebound keys.
- Miller columns: key/mouse navigation, stale-response drop, cache hits (fake client).
- Art: decode/scale golden on a fixture image; kitty placeholder bytes golden; detection with a fake
  terminal reply.
- Visualizer: FFT against known sines (bin of peak), band edges monotonic, fake tap start/stop tied
  to page visibility and playback; goleak.
- Benchmarks: keypress → repaint stays < 16 ms at 200×60; track page frame with art + spectrum.

## Resolved questions (owner approved defaults, 2026-11-17)

1. Tab bar shows names only; tabs switch with the existing chords (digits stay counts).
2. `g n` opens the track page.
3. WebP: first verify live whether art URLs can be requested as JPEG; add
   `golang.org/x/image/webp` only if not (justified in the PR).
4. The visualizer shows the default sink's monitor (all system audio), as ytkew does.

## Implementation notes

- **Phase 1 (frame), done.** `internal/ui/frame.go` (layout, tab bar + rule, `panel`, mini
  player, hint footer) and `page_frame.go` (per-page `PanelTitle`/`Count`/`Hints`). Header
  `← Back / Forward →` removed; mouse X1/X2 (`tea.MouseBackward/Forward`) walk the history, tabs
  are clickable. Shuffle/repeat glyphs live in the tab bar, ♥ and play/pause in the mini player.
  The frame's styles (`AppName`, `Tab*`, `Panel*`, `Hint*`) are foreground-only; the old header
  `Surface` fill is gone, so the frame paints no background in opaque mode either.
  Page-internal titles (Queue, Context name) moved into panel titles. Hints render the keymap's
  canonical key names (`f2`, not `F2`).
- **Phase 2 (rows), done.** TrackTable has no header row, so the M5 header-click sort is gone
  (sorting stays on `s t/a/A/d/D`, `s r`); clicks address rows from y = 0. The last column holds
  ↑/↓ scroll arrows. Duration is right-aligned with padding before it. In-page tab strips
  (search panels, library tabs) render as `songs · albums · artists`; the search input is
  `⌕ query`. `transparent_selection` defaults to `solid`.
- **Phase 3 (Library columns), done.** `page_library.go` rewritten: a column path over a
  per-session node cache (`playlists`, `albums`, `artists`, `ar:<id>`, `al:<id>`, `pl:<id>`;
  Liked Music = `pl:LM`, never offered for row removal). Previews are debounced with the
  ctx-aware `after` and dropped when a newer cursor move bumped `seq`. Track columns are a
  TrackTable in a new compact mode (`Artist — Title  time`), so marks, filter, sort and every
  track action work there. Artist releases come from the artist page's Albums + Singles
  shelves (not the full discography; `g space` opens the artist page for that). Unfocused
  lists keep their cursor row visible in `accent` (the path). Lists drop the right-hand detail
  when it would leave the label fewer than 16 cells. Wheel goes to the column under the
  pointer (`wheeler`). Driven live (signed in): Artists → artist → releases → album tracks.
- **Phase 4 (track page), done.** `page_track.go`; the root draws pages implementing
  `splitter` as two panels and takes progress geometry from `progressor`, so clicks seek and the
  wheel scrubs there as on the mini player (hidden on this page). Up next is its own Queue page
  instance (own cursor, shared queue) that opens on the playing track. Cover art and the spectrum
  are not drawn yet (phases 5–6).
- **Phase 5 (cover art), done.** `internal/art`: `URL` (googleusercontent → `=w400-h400-l90-rj`,
  JPEG verified live; `i.ytimg.com/vi/<id>/…` → `hqdefault.jpg`, JPEG), `Fetcher` +
  `HTTPFetcher` (4 MB cap, 15 s timeout, stdlib `image/jpeg`/`image/png`: **no WebP dependency**),
  `Square`, box-filter `Scale`, LRU `Cache` (32, memory only; disk cache left to M7). Kitty via
  `x/ansi/kitty` (already a dependency): query `a=q` at start, `uv.KittyGraphicsEvent` reply
  (Bubble Tea passes unknown ultraviolet events through as messages; `ultraviolet` becomes a
  direct requirement, same version), PNG transmit with a virtual placement (`U=1`) through
  `tea.Raw`, placeholder cells colored with the image id, old image freed on change and quit.
  Cells are drawn by `style.HalfBlocks` / `style.KittyCells` (the only non-token colors).
  Art follows the queue's current track after every Update (`syncArt`). Half blocks need 256+
  colors and color at all; `album_art = true` (old key) now maps to `auto`. Live (tmux, signed
  in): real art fetched and drawn as half blocks. **Kitty placeholders not yet seen in a real
  kitty window** (tmux does not pass graphics); covered by unit tests only.
- **Phase 6 (visualizer), done.** `internal/viz`: `PWCat` source (`pw-cat --record --raw
  --format s16 --rate 48000 --channels 1 -P '{ stream.capture.sink=true }' -`; no `--target`
  needed, the property records the default sink's monitor), in-house radix-2 FFT with Hann
  window, 96 log bands 45 Hz–16 kHz, ×0.86 decay, hop 1600 samples = 30 updates/s (no clock).
  `Visualizer.Run` is the one goroutine (app-owned, ends with ctx, goleak-tested); `SetActive`
  never blocks. The UI activates it only on the track page while playing (`syncSpectrum`).
  Measured: analysis 37 µs/frame, 0 allocs (~0.1 % of a core at 30 fps); live, pw-cat
  appears on the track page, shows 0.0 % CPU, and is gone on leaving and on quit. Bars with
  real music not seen yet (tests played nothing audible).

