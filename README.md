# Aigit — Live Git Checkpoints with AI Summaries

Aigit overlays live, restorable "checkpoint" commits on top of Git without touching your normal branch history. It snapshots your working tree (even during merges), writes to a separate ref (`refs/aigit/checkpoints/<branch>`), and generates concise one‑line summaries via OpenRouter. Optionally sync checkpoints to a remote and opt‑in to auto‑apply teammates’ updates — all without moving `HEAD`.

## Features

- Live checkpoints on save, stored under `refs/aigit/checkpoints/<branch>`.
- Safe during merges — snapshot the worktree with conflict markers, and keep normal history clean.
- One‑line summaries via OpenRouter (default model `x-ai/grok-code-fast-1`) or a heuristic from `git diff`.
- Background watcher auto‑starts; it activates only after your first file save.
- Optional remote sync: push your checkpoints to a per‑user namespace; fetch/accept others; opt‑in auto‑apply.

## Install

```sh
# Option 1: from source (local)
go build

# Option 2: go install (after pushing to GitHub)
# Replace YOUR_GH_USER with your GitHub handle once the repo is public
go install github.com/YOUR_GH_USER/aigit@latest
```

This produces an `aigit` binary.

## Quick Start

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

2) In a Git repo, run any `aigit` command (e.g., `./aigit status`). A background watcher auto‑starts and waits (it activates after your first save).

3) Edit and save a file. You’ll see:

```
Detected changes; live checkpoints activated.
Checkpoint: <sha>  (<summary>)
```

4) Browse checkpoints:

```
./aigit list
./aigit list -n 10 --meta
```

5) Restore any checkpoint to the worktree (HEAD not moved):

```
./aigit restore <sha>
```

## Commands

- `aigit status` — last checkpoint summary + diffstat vs HEAD.
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
- `aigit.summaryModel` — default `x-ai/grok-code-fast-1`
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
- Watcher didn’t start: Run `./aigit status` once; ensure files are saved to trigger activation.
- macOS heavy projects: Increase settle window: `aigit watch -settle 3s` or `git config aigit.settle 3s`.
- OpenRouter key missing: Aigit falls back to diff‑based summaries.

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

## License

MIT
