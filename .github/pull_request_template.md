## Summary

<!-- One or two sentences: what this PR delivers and why. -->

**Milestone:** M?  ·  **Spec:** [docs/specs/NN-name.md](../docs/specs/)

## What changed

-

## Dependencies added

<!-- Module, version, why it's needed, what was verified (go doc / source / maintenance). "None" is fine. -->

## How to test

```sh
make test
make lint
# manual steps:
```

## Screenshots / asciinema

<!-- Required if UI or styled CLI output changed. Include transparent + opaque, and NO_COLOR when relevant. -->

## Performance

<!-- Required if rendering, playback, or startup changed. Paste benchstat / timing numbers vs budget. -->

## Checklist

- [ ] Linked to a spec section; spec updated if behavior changed
- [ ] Tests added; `go test -race ./...` passes locally and in CI
- [ ] Lint clean; no new TODOs without an issue reference
- [ ] Benchmarks/perf numbers included if this touches rendering, playback, or startup
- [ ] New views added to the transparent-render test
- [ ] `docs/PROGRESS.md` updated
- [ ] Manual test steps listed above, and I actually ran them (say what could not be tested)
