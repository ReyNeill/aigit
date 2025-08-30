# Repository Guidelines

## Project Structure & Module Organization

- `main.go` — CLI entry, commands (status, checkpoint, list, restore, watch, sync/apply, id).
- `ai.go` — OpenRouter summaries (uses `OPENROUTER_API_KEY`).
- `watch.go` — fsnotify watcher; ignores `.git`, `vendor`, `node_modules`.
- `sync.go`, `sync_unix.go`, `sync_windows.go` — autostart, user id, push/pull/apply, OS‑specific helpers.
- `aigit_test.go` — end‑to‑end tests using temporary Git repos.
- `assets/` — branding (logo).
- `.github/workflows/` — CI (build/tests) and release (GoReleaser).
- `LLM.txt` — prompt to brief AI agents on how to use Aigit.

Module: `github.com/ReyNeill/aigit` (Go 1.21+).

## Build, Test, and Development Commands

- Build: `go build` (local) or `go install github.com/ReyNeill/aigit@latest`.
- Run: `aigit status` in a repo (autostarts watcher); stop with `aigit stop`.
- Interval: default 5m; override per repo `git config aigit.interval 2m`.
- Tests (online): `go test` (requires `OPENROUTER_API_KEY`).
- Tests (offline): `go test -offline` (uses a local AI summary fake).
- Skip AI tests: `go test -no_summary`.
- Helpful env for local dev/tests: `AIGIT_DISABLE_AUTOSTART=1`.

## Coding Style & Naming Conventions

- Go standard style; format with `go fmt ./...` (CI expects formatted code).
- Use clear, descriptive names; exported identifiers use `UpperCamelCase`, locals `lowerCamelCase`.
- Keep functions focused; prefer small helpers in topical files (e.g., git helpers in `main.go`).

## Testing Guidelines

- Framework: Go `testing` package.
- Tests live in `*_test.go`; see `aigit_test.go` for patterns.
- The suite creates temp Git repos; it does not touch your working tree.
- Online tests hit OpenRouter; offline mode uses a deterministic fake.
- Run with `-v` when debugging; keep tests fast and hermetic.

## Commit & Pull Request Guidelines

- Commit messages: short, imperative subjects (<= 72 chars), e.g., “Add remote-list command”.
- Scope PRs narrowly; include:
  - What changed and why (1–3 bullets).
  - Screenshots or CLI snippets when UX changes (e.g., `aigit status` output).
  - Test notes: how you validated (`go test`, manual steps).
- Link related issues; keep diffs minimal and aligned with existing structure.

## Security & Configuration Tips

- Do not commit secrets. Export `OPENROUTER_API_KEY` in your shell rc.
- Shell integration: `aigit init-shell --zsh|--bash` to print updates while working. Suppress local echo with `aigit checkpoint -q`.
- CI: online tests run only if `OPENROUTER_API_KEY` is configured; releases require a `BREW_GITHUB_TOKEN` with `repo` scope.
