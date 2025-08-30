<p align="center">
  <img src="assets/aigit-logo.png" alt="Aigit logo" width="200"/>
</p>

# Aigit — Git but updated for AI coding agents (optional human collaboration)

Aigit overlays live, restorable "checkpoint" commits on top of Git without touching your normal branch history. It snapshots your working tree (even during merges), writes to a separate ref (`refs/aigit/checkpoints/<branch>`), and generates concise one‑line summaries via OpenRouter. Optionally sync checkpoints to a remote and opt‑in to auto‑apply teammates’ updates — all without moving `HEAD`.

## Features

- No commits, all code changes happen live (including remotely)
- Commits are now checkpoints one can restore and share with others, stored under `refs/aigit/checkpoints/<branch>`.
- You can checkpoint while merging! So you can save your progress while resolving a big conflict.
- Code updates and checkpoints come with one-sentence summaries via OpenRouter (default model `openai/gpt-oss-20b:free`) or a heuristic from `git diff`.
- Our service (auto-code updates, etc) auto-starts after the first file save, indicating the code is been worked on, so you don't have to possible forget to `aigit status`
- Optional remote sync: push your checkpoints to a per‑user namespace; fetch/accept others; opt‑in auto‑apply.

## Install

```sh
# Option 1: go install
go install github.com/ReyNeill/aigit@latest

# Option 2: Homebrew
brew update && brew tap ReyNeill/homebrew-tap && brew install aigit && aigit version

# Option 3: build (downloading repo)
go build

# If `aigit` is not found after go install, ensure Go bin is on PATH:
# zsh
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
# bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

This produces an `aigit` binary.

## Quick Start (Intended Use)

1) Set your OpenRouter API key (optional but recommended):

- zsh: add to `~/.zshrc`

```sh
export OPENROUTER_API_KEY="sk-or-v1-..."
```

- bash: add to `~/.bashrc` or `~/.bash_profile`

```sh
export OPENROUTER_API_KEY="sk-or-v1-..."
```

Reload your shell or open a new terminal.

2) Enable live terminal updates (recommended):

- For zsh:
```
aigit init-shell --zsh
# then follow the printed instruction to add a single `source` line to ~/.zshrc
```

- For bash:
```
aigit init-shell --bash
# then follow the printed instruction to add a single `source` line to ~/.bashrc
```

3) In a Git repo, run any `aigit` command (e.g., `aigit status`). The watcher autostarts; updates (checkpoints, AI summaries, applies) pop up in your terminal while you work.

4) Edit and save a file. You’ll see:

```
Detected changes; live checkpoints activated.
Checkpoint: <sha>  (<summary>)
```

Notes
- The shell integration runs a lightweight background follower per repository, printing new events as they arrive.
- Prefer a second pane with `aigit tail` if you want a dedicated continuous view.

## Note: You can teach LLMs how to use aigit pasting them the LLM.txt file!

## Commands

- `aigit status` — last checkpoint summary + diffstat vs HEAD.
- `aigit version` — print the version (set by GoReleaser in releases).
- `aigit id` — show your computed user id and the local/remote ref mapping.
- `aigit checkpoint -m "msg"` — manual snapshot with custom summary.
- `aigit list [-n 20] [--meta]` — list recent checkpoints for this branch.
- `aigit restore <sha>` — restore files from a checkpoint into the worktree.
- `aigit watch` — manual start of the watcher (auto‑started on first use).
- `aigit sync push [-remote origin]` — push your local checkpoints to a remote namespace.
- `aigit sync pull [-remote origin]` — fetch checkpoint refs from the remote.
- `aigit remote-list [--remote origin] [--user id] [-n 20] [--meta]` — list users with checkpoints, or show a user's remote checkpoints for the current branch.
- `aigit apply --from <user> [--remote origin] [--sha <sha>]` — apply a remote user’s checkpoint to your worktree (latest if `--sha` omitted).

## Configuration (git config)

Set per‑repo in `.git/config` or globally with `--global`.

- `aigit.summary` — `ai` (default) | `diff` | `off`
- `aigit.summaryModel` — default `openai/gpt-oss-20b:free`
- `aigit.interval` — checkpoint cadence when active (e.g., `30s`, `2m`, `1h`)
- `aigit.settle` — debounce window after saves (default `1.5s`)
- `aigit.user` — override your user id for remote namespaces (defaults to `user.email`)
  - By default, Aigit uses your `git user.email` as the user id (safe for ref names). You can override via `aigit.user`.

Remote sync (optional):

- `aigit.pushRemote` — remote name to auto‑push after checkpoints (e.g., `origin`)
- `aigit.pullRemote` — remote name to fetch from periodically (e.g., `origin`)
- `aigit.autoApply` — `true|false` enable auto‑apply of remote checkpoints
- `aigit.autoApplyFrom` — comma list of user ids or `*` for all (excluding yourself)

## Remote Namespace

Your local checkpoints live at `refs/aigit/checkpoints/<branch>`.

When pushing, Aigit maps them to a per‑user namespace on the remote:

```
refs/aigit/users/<user>/checkpoints/<branch>
```

Fetching pulls these into local tracking refs under:

```
refs/remotes/<remote>/aigit/users/<user>/checkpoints/<branch>
```

You can list and apply from those using `aigit apply`.
To discover users and browse their checkpoints use `aigit remote-list`.

## Why It’s Different
- Clean history: Doesn’t touch `refs/heads/<branch>`; writes to `refs/aigit/...`.
- Mid‑merge safe: Snapshot the working files (including conflict markers) via a temporary index.
- No HEAD moves: Restore files from any checkpoint without moving `HEAD`.
- Live collaboration: Optional push/pull of checkpoint refs; opt‑in auto‑apply.

## How It Works
- On save, Aigit builds a snapshot using a temporary Git index (leaves your index alone).
- Creates a tree and commit via `git commit-tree`.
- Updates `refs/aigit/checkpoints/<branch>` atomically with `git update-ref`.
- Summaries come from OpenRouter (or a diff heuristic fallback).

## Requirements
- Git ≥ 2.23 (uses `git restore`; falls back to `checkout` when needed)
- Go ≥ 1.21 to build from source
- macOS, Linux, or Windows

## Privacy & Security
- No telemetry.
- AI summaries call OpenRouter only when enabled. Keep `OPENROUTER_API_KEY` in your shell rc (e.g., `~/.zshrc`, `~/.bashrc`).

## Troubleshooting
- Missing remote on push: `git remote add origin <url>` then `aigit sync push`.
- Status shows nothing: You’ll see “nothing here yet, clean workspace” on a clean tree.
 - Watcher didn’t start: Run `aigit status` once; ensure files are saved to trigger activation.
- macOS heavy projects: Increase settle window: `aigit watch -settle 3s` or `git config aigit.settle 3s`.
- OpenRouter key missing: Aigit falls back to diff‑based summaries.
 - Homebrew on pre‑release macOS: If Xcode/CLT mismatch errors appear, use `go install github.com/ReyNeill/aigit@latest` until CLT updates.

## Testing

Run the test suite:

```
go test
```

By default, AI summary tests call OpenRouter (requires `OPENROUTER_API_KEY`). To run without network, pass `-offline` to use a local fake. To skip AI tests entirely, use `-no_summary`. Tests run in temp repos and won’t affect your working repo.

```
go test                    # requires OPENROUTER_API_KEY for AI tests
go test -offline           # run AI tests with local fake (no network)
go test -no_summary        # skip AI tests entirely
```

## Merge‑Friendly

Checkpoints work during merges because Aigit builds a tree from a temporary index and snapshots the working files (including conflict markers). `aigit status` shows a preview of conflicted paths.

## Notes & Limits

- Aigit does not move `HEAD`. It writes separate checkpoint commits and updates an internal ref.
- Checkpoints include all files (tracked or previously untracked) in your worktree.
- For team sync, ensure your remote allows pushing custom refs (most hosts do). The first push may require `aigit sync push`.
- Auto‑apply writes files into your working tree. Enable it only if you want live updates from selected users.

## For Agents

If you’re using an AI coding agent, share the `LLM.txt` file in this repo. It explains Aigit’s model, guardrails, and the exact commands to use (checkpoint, list, restore, status, sync/apply), including merge‑time behavior and summary style.

## License

MIT

## Release & CI

- CI (offline tests) runs on pushes/PRs.
- Optional online AI tests run if you add a repository secret `OPENROUTER_API_KEY`.
- Releases: Tag with `vX.Y.Z` to trigger GoReleaser and publish archives.
- Homebrew tap: Create `https://github.com/ReyNeill/homebrew-tap` and add a repo secret `BREW_GITHUB_TOKEN` (a Personal Access Token with `repo` scope). The release action uses it to publish the formula to your tap.
