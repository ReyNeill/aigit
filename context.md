Love the idea. You can ship **Aigit** entirely *on top of* Git without forking, and still get “live checkpoints,” summaries, and “save while mid-merge.” Here’s a practical design that stays compatible with vanilla Git and avoids maintenance pain.

# How to model “checkpoints” on top of Git

### Core idea

* Keep normal Git history **clean**.
* Record rapid “live” snapshots as **checkpoint commits** in a **separate ref namespace**:

  * `refs/aigit/checkpoints/<branch>`
* Build each checkpoint from the **working tree** using a **temporary index** (so you don’t touch the user’s real index/staging area).
* Add a **one-line summary** as the commit subject. Put structured metadata (base commit, merge state, mode) as trailers in the message.
* Optionally auto-push these checkpoint refs to a remote namespace like `refs/aigit/<branch>` for collaboration.

### Why this works

* Checkpoints don’t pollute `refs/heads/<branch>`.
* Atomic updates via `git update-ref` make concurrency sane (agents / background process).
* You can **restore** files from any checkpoint without moving `HEAD`.
* **Mid-merge** checkpoints work because you bypass Git’s “can’t commit with conflicts” rule by building a tree from a temporary index (it just snapshots the current files, conflict markers and all).

---

# Minimal plumbing that proves it

Below is a tiny, safe PoC you can drop into a repo as `aigit` (bash). It implements:

* `aigit checkpoint -m "summary"` — save a live snapshot
* `aigit status` — show last checkpoint summary + current diff
* `aigit restore <sha>` — restore files from a checkpoint (doesn’t move `HEAD`)
* (You can extend with `aigit publish` later to squash N checkpoints into one real commit)

> Try it in a test repo first.

```bash
#!/usr/bin/env bash
set -euo pipefail

cmd="${1:-}"
shift || true

branch() { git rev-parse --abbrev-ref HEAD; }
ck_ref() { echo "refs/aigit/checkpoints/$(branch)"; }

checkpoint() {
  # usage: aigit checkpoint -m "one-line summary"
  local summary="(auto)"
  while getopts ":m:" opt; do
    case $opt in
      m) summary="$OPTARG" ;;
    esac
  done

  local ref; ref="$(ck_ref)"
  local tmpidx; tmpidx="$(mktemp)"
  trap 'rm -f "$tmpidx"' EXIT
  : > "$tmpidx"
  export GIT_INDEX_FILE="$tmpidx"

  # Snapshot working tree to temp index without touching user's index
  git add -A >/dev/null

  # Build tree & commit
  local tree; tree="$(git write-tree)"
  local parent; parent="$(git rev-parse -q --verify "$ref^{commit}" 2>/dev/null || true)"

  local base; base="$(git rev-parse HEAD)"
  local merging="no"; [ -f .git/MERGE_HEAD ] && merging="yes"

  local meta=$'Aigit-Base: '"$base"$'\nAigit-When: '"$(date -u +%FT%TZ)"$'\nAigit-Merge: '"$merging"
  local new
  if [ -z "$parent" ]; then
    new="$(printf "%s\n\n%s\n" "$summary" "$meta" | git commit-tree "$tree")"
  else
    new="$(printf "%s\n\n%s\n" "$summary" "$meta" | git commit-tree -p "$parent" "$tree")"
  fi

  # Atomic ref update (creates a separate reflog for time travel)
  if [ -n "$parent" ]; then
    git update-ref -m "aigit: $summary" "$ref" "$new" "$parent" >/dev/null
  else
    git update-ref -m "aigit: $summary" "$ref" "$new" >/dev/null
  fi

  echo "Checkpoint: $new  ($summary)"
}

status() {
  local ref; ref="$(ck_ref)"
  local last_summary; last_summary="$(git log -1 --format=%s "$ref" 2>/dev/null || echo "no checkpoints yet")"
  echo "Branch: $(branch)"
  echo "Last checkpoint: $last_summary"
  echo
  echo "Working tree diff vs HEAD:"
  git --no-pager diff --stat
}

restore() {
  # usage: aigit restore <checkpoint-sha>
  local sha="${1:-}"
  if [ -z "$sha" ]; then echo "usage: aigit restore <sha>"; exit 2; fi
  echo "Restoring worktree from $sha (does not move HEAD)..."
  # Overwrite tracked files in the worktree only; leave index & HEAD alone
  git restore --worktree --source "$sha" -- .
  echo "Done. (Untracked files are left as-is.)"
}

case "$cmd" in
  checkpoint) checkpoint "$@" ;;
  status)     status ;;
  restore)    restore "$@" ;;
  *)
    cat <<EOF
Aigit commands:
  aigit checkpoint -m "summary"    # save a live snapshot (works during merges)
  aigit status                     # show last checkpoint summary + diff
  aigit restore <sha>              # restore files from a checkpoint

Tips:
  git log --oneline $(ck_ref)      # browse checkpoint history
  git show <sha>                   # inspect a checkpoint
EOF
    ;;
esac
```

---

# How this maps to your requirements

* **“All changes happen live; users don’t need to commit/push.”**
  Run a background **watch** mode (daemon) that calls `aigit checkpoint`:

  * Trigger on file saves (fs events) *and/or* every X minutes (configurable).
  * Each checkpoint has a 1-line summary. For AI agents, pipe their action summary in; otherwise auto-generate from `git diff --name-status` (and optionally an LLM).
  * Optional: auto-push `refs/aigit/checkpoints/<branch>` to `origin` for team visibility.

* **“Restore to a saved checkpoint.”**
  `aigit restore <sha>` writes those files into the worktree without touching `HEAD` or the user’s index.

* **“Status shows last summary + diffs.”**
  `aigit status` prints the last checkpoint’s subject and a diffstat of the current working tree.

* **“Passive every X minutes, same locally.”**
  The watch/daemon mode handles cadence. Locally it behaves identically: checkpoints are just commits on the special ref.

* **“Checkpoints during an open merge.”**
  Works: we don’t rely on the real index. We snapshot the **working files**, even with conflict markers present. We also mark `Aigit-Merge: yes` in metadata and could store `MERGE_MSG` if you want.

---

# Recommended architecture (clean, scalable)

* **CLI + daemon** in Go or Node (your stack):

  * `aigit watch` (fs events + timer)
  * `aigit checkpoint -m <msg>`
  * `aigit restore <sha>`
  * `aigit status`
  * `aigit publish [--since <sha>|--last N]` (squash N checkpoints into a real Git commit on `refs/heads/<branch>` with a composed message)
  * `aigit push` / `pull` for `refs/aigit/*` (optional collaboration)
* **Ref layout**

  * `refs/aigit/checkpoints/<branch>` – linear chain of snapshots
  * (optional) `refs/aigit/published/<branch>` – anchors to detect what’s already squashed
* **Metadata**

  * Commit subject = your one-liner summary.
  * Trailers in the commit message, e.g.:

    ```
    Aigit-Base: <HEAD sha at checkpoint time>
    Aigit-When: 2025-08-29T21:45:00Z
    Aigit-Merge: yes|no
    Aigit-Agent: name/id (optional)
    Aigit-Mode: manual|passive
    ```
* **Atomicity & concurrency**

  * Use `git update-ref -m ... <ref> <new> <old>` to ensure no lost updates when multiple writers run.
* **Publishing (make “real” Git commits)**

  * Gather checkpoint summaries since last publish.
  * Create a normal commit on `refs/heads/<branch>` from the current worktree.
  * Compose a clean message (top line + bullets from summaries).
  * Optionally delete or keep old checkpoints (TTL).

---

# Edge cases & guardrails

* **Repo bloat**: lots of small commits are okay; Git packs them well. Add retention: prune checkpoints older than N days (`git for-each-ref` + `update-ref -d`) or keep the last M per branch.
* **Untracked files**: the PoC snapshots everything you add; you might want to include untracked by default. Current script adds them; tweak ignore behavior as needed.
* **Binary/large files**: let users opt-out via `.gitignore` or Aigit filters; consider Git LFS if necessary.
* **Conflicting restores**: by default we overwrite tracked files. If safety is needed, write to a worktree (`git worktree add`) or require a clean tree.

---

# Implementation notes for production

* **Language**: Go (great for single static binary + fsnotify) or Node/Bun (fits your stack; use `chokidar`).
* **Config**: `.git/config` section `[aigit] interval=3m summary=ai|diff pruneDays=14 push=origin`.
* **Summaries**:

  * Heuristic fallback: derive a one-liner from `git diff --name-status -M -C` (e.g., “Edit foo.ts; add bar.test.ts; rm baz.js”).
  * If an AI agent is driving, accept a summary string via CLI/HTTP.
* **Mid-merge UX**:

  * Include `Aigit-Merge: yes` and list conflicted paths (`git diff --name-only --diff-filter=U`) in the message body so “status” can show “resolving X files…”.

---

## TL;DR

Don’t fork Git. Implement **Aigit** as a porcelain + daemon that writes **checkpoint commits** to `refs/aigit/checkpoints/<branch>` using a **temporary index**. This gives you live, restorable snapshots (even during merges), one-line summaries, and a clean path to “publish” into conventional Git history when ready.

If you want, I can turn this into a Go or Bun CLI scaffold next (with `watch`, `publish`, remote push of checkpoint refs, and AI-summary hooks).
