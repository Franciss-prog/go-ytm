# ytm-go: working agreements

Go rewrite of ytm-player (YouTube Music TUI). Reference: https://github.com/peternaame-boop/ytm-player (MIT).
Specs live in docs/specs/. Progress in docs/PROGRESS.md. Read both before starting any task.

Module path: `github.com/Franciss-Prog/go-ytm` (product name ytm-go, binary `ytm`).

## Commands
- Build: `make build`   Test: `make test` (`go test -race ./...`)   Lint: `make lint`
- Run: `make run`   Bench: `make bench`

## Rules
- Bubble Tea `Update` must never block. Do I/O in `tea.Cmd`/goroutines and return messages.
- No cgo. mpv via JSON IPC subprocess; SQLite via modernc.org/sqlite.
- Charm v2 modules only (`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`,
  `charm.land/huh/v2`, `charm.land/fang/v2`, `charm.land/glamour/v2`, `charm.land/log/v2`). No v1 imports.
- Every external system (mpv, ytmusic, yt-dlp, filesystem clock) sits behind an interface with a fake in tests.
- Context-propagate cancellation; no goroutine without an owner and a shutdown path.
- Errors: wrap with `%w`, user-facing messages are short and actionable; details go to the log.
- Public behavior (config keys, keybindings, CLI flags) changes only with a spec update.
- All CLI/TUI styling goes through `internal/style` (Charm stack: Bubble Tea, Bubbles, Huh, Lip Gloss).
  Styled output only on a TTY; `--json` and piped output are plain. Spinners to stderr, results to stdout.
- UI code uses semantic theme tokens only, never raw colors. Every new view must render correctly in
  transparent mode (no background SGR codes) and survive a live theme reload. Add it to the
  transparent-render test when you add a page.
- Don't add a dependency without justifying it in the PR description.
- One PR per milestone, branch `feat/<slug>`, Conventional Commits.
- Never claim something works unless you ran it. Say what you could not test.
- Don't guess at library or YouTube API behavior: verify and cite.
- Unix socket paths must stay under 104 bytes (macOS limit); length-check and error clearly.

## Style
- gofmt/goimports, small packages, table-driven tests, golden files in testdata/.
- Doc comments on exported identifiers. No panics outside main.
