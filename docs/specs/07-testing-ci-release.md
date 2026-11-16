# 07 — Testing, CI, release

Status: **draft** · Milestones: M0 (CI, lint, Makefile), every PR (tests), M12 (release)

## Goals

- Fast, hermetic unit tests (no network, no mpv, no yt-dlp) that run with `-race` on 3 OSes.
- Opt-in integration tests for real mpv / yt-dlp / InnerTube.
- Golden files for UI and CLI output; benchmarks tracked in PRs.
- Reproducible releases via GoReleaser for linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`.

## Non-goals

- 100 % coverage targets. Coverage is reported, not gated (gate: no PR lowers package coverage of
  `player`, `queue`, `keys`, `config`, `theme` by > 2 pts).

## Test layers

| Layer | Tooling | Runs in CI |
|---|---|---|
| Unit (pure logic: queue, keys, config, theme mapping, contrast, LRC parse, normalizers) | `testing`, table tests, fuzz for parsers | yes, all OSes, `-race` |
| Component (pages/components) | `teatest/v2` + golden snapshots; direct `Update` calls | yes |
| Service w/ fakes | `fakempv` (socket server), `fakeytm`, fake `yt-dlp` script, `httptest` | yes |
| CLI | run cobra root in-process with fake IO; golden styled/nocolor/json | yes |
| Integration `//go:build mpv` | real mpv playing lavfi-generated audio | linux job (apt mpv) |
| Live `//go:build live` | real InnerTube + yt-dlp | manual / weekly scheduled job, non-blocking |
| Benchmarks | `go test -bench -benchmem`, `benchstat` vs base branch | PRs touching ui/player/theme (job posts table) |

Golden update: `go test ./... -update`. Goldens normalize: fixed width/height, fixed clock,
fixed color profile (TrueColor for styled, Ascii for nocolor).

## Lint

`golangci-lint` v2 with: `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `revive`
(exported docs), `gocritic` (diagnostic tags), `misspell`, `bodyclose`, `contextcheck`, `noctx`,
`forbidigo` (forbid `fmt.Print*` outside `cmd/` and `internal/style`, forbid `lipgloss.Color(`
outside `internal/style`/`internal/theme` → enforces "no raw colors"), `depguard`
(forbid `github.com/charmbracelet/bubbletea` v1 imports, cgo packages). `gofumpt` + `goimports`.

Local tools pinned via `go tool` directives in `go.mod` (Go ≥ 1.24 tool deps) so `make lint`
needs nothing preinstalled. (golangci-lint as a `go tool` dep vs the official binary action: CI
uses `golangci/golangci-lint-action`; local `make lint` falls back to `go run` pinned version.)

## CI (`.github/workflows/ci.yml`)

- Triggers: push to `master`, PRs.
- Matrix: `ubuntu-latest`, `macos-latest`, `windows-latest` × Go `stable` and the `go.mod` minimum.
- Steps: checkout → setup-go (cache) → `go mod verify` → `go vet ./...` → `go build ./...`
  (`CGO_ENABLED=0`) → `go test -race -count=1 ./...` (race needs cgo: `CGO_ENABLED=1` for the test
  step only; the shipped binary stays cgo-free) → lint job (ubuntu) → cross-compile job builds all 6
  GOOS/GOARCH targets with `CGO_ENABLED=0` to prove no cgo crept in.
- Integration job (ubuntu): `apt-get install mpv`, `go test -tags mpv ./internal/player/...`.
- Before any Go code exists (Phase 0) the workflow no-ops gracefully when `go.mod` is absent.

## Release (M12)

- GoReleaser: archives (tar.gz / zip), checksums, SBOM, `-ldflags "-s -w -X buildinfo.Version=…"`,
  `-trimpath`.
- Packaging: Homebrew tap (`Franciss-Prog/homebrew-tap`), Scoop bucket, AUR `ytm-go-bin`
  (and `ytm-go-git` PKGBUILD in `contrib/aur/`), Nix flake (stretch). Formula/manifests declare
  `mpv` and `yt-dlp` as dependencies.
- Changelog from Conventional Commits; `ytm changelog` embeds `CHANGELOG.md` via `go:embed`.
- Tag `v0.1.0` after M12 checklist; release notes include perf budget table with measured numbers.

## Edge cases

- Windows CI: Unix-socket tests skipped/swapped for named-pipe variants; path separators in goldens
  normalized.
- macOS CI: no D-Bus; MPRIS tests build-tagged linux.
- Flaky timing tests forbidden: everything time-based uses `clock.Fake`.

## Test plan (for this spec)

- M0 PR proves CI green on all 3 OSes with a trivial test + cross-compile.

## Open questions

1. Homebrew tap / Scoop bucket repos — create under your account at M12? (default: yes, ask then)
