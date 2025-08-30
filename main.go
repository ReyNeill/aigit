package main

import (
    "bufio"
    "bytes"
    "errors"
    "flag"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "time"
    "runtime/debug"
)

var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)

func main() {
    // Fill version info from build info when not injected (e.g., go install)
    if version == "dev" {
        if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
            if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
                version = bi.Main.Version
            }
            // Extract VCS info if available
            for _, s := range bi.Settings {
                switch s.Key {
                case "vcs.revision":
                    if commit == "none" && s.Value != "" {
                        commit = s.Value
                    }
                case "vcs.time":
                    if date == "unknown" && s.Value != "" {
                        date = s.Value
                    }
                }
            }
        }
    }
    if len(os.Args) < 2 {
        printHelp()
        return
    }

    cmd := os.Args[1]
    args := os.Args[2:]

    if cmd == "--version" || cmd == "version" || cmd == "-v" {
        fmt.Printf("aigit %s (%s %s)\n", version, commit, date)
        return
    }

    // Autostart background watch daemon if not already running
    if cmd != "watch" {
        _ = maybeAutostartWatch()
    }

    switch cmd {
    case "checkpoint":
        fs := flag.NewFlagSet("checkpoint", flag.ExitOnError)
        msg := fs.String("m", "(auto)", "one-line summary message")
        if err := fs.Parse(args); err != nil {
            fatal(err)
        }
        if err := doCheckpoint(*msg); err != nil {
            fatal(err)
        }
    case "status":
        if err := doStatus(); err != nil {
            fatal(err)
        }
    case "tail":
        fs := flag.NewFlagSet("tail", flag.ExitOnError)
        lines := fs.Int("n", 100, "lines of history before following")
        if err := fs.Parse(args); err != nil { fatal(err) }
        if err := doTail(*lines); err != nil { fatal(err) }
    case "events":
        fs := flag.NewFlagSet("events", flag.ExitOnError)
        sessionID := fs.String("id", "", "unique session id for state (e.g., host:tty:pid)")
        back := fs.Int("n", 80, "lines to show on first run")
        follow := fs.Bool("follow", false, "follow and print events as they arrive")
        if err := fs.Parse(args); err != nil { fatal(err) }
        if strings.TrimSpace(*sessionID) == "" { fatal(errors.New("usage: aigit events -id <session-id> [-n N] [--follow]")) }
        if err := doEvents(*sessionID, *back, *follow); err != nil { fatal(err) }
    case "init-shell":
        fs := flag.NewFlagSet("init-shell", flag.ExitOnError)
        zsh := fs.Bool("zsh", false, "install zsh integration (~/.zshrc)")
        bash := fs.Bool("bash", false, "install bash integration (~/.bashrc)")
        if err := fs.Parse(args); err != nil { fatal(err) }
        if !*zsh && !*bash { fatal(errors.New("usage: aigit init-shell --zsh|--bash")) }
        if err := doInitShell(*zsh, *bash); err != nil { fatal(err) }
    case "id":
        if err := doID(); err != nil { fatal(err) }
    case "list":
        fs := flag.NewFlagSet("list", flag.ExitOnError)
        n := fs.Int("n", 20, "number of checkpoints to show")
        meta := fs.Bool("meta", false, "show metadata trailers")
        if err := fs.Parse(args); err != nil {
            fatal(err)
        }
        if err := doList(*n, *meta); err != nil {
            fatal(err)
        }
    case "sync":
        if len(args) == 0 {
            fmt.Println("usage: aigit sync push|pull [options]")
            return
        }
        sub := args[0]
        subArgs := args[1:]
        switch sub {
        case "push":
            fs := flag.NewFlagSet("sync push", flag.ExitOnError)
            remote := fs.String("remote", defaultStr(getGitConfig("aigit.pushRemote"), "origin"), "remote name")
            if err := fs.Parse(subArgs); err != nil { fatal(err) }
            if err := pushCheckpoints(*remote); err != nil { fatal(err) }
        case "pull":
            fs := flag.NewFlagSet("sync pull", flag.ExitOnError)
            remote := fs.String("remote", defaultStr(getGitConfig("aigit.pullRemote"), "origin"), "remote name")
            if err := fs.Parse(subArgs); err != nil { fatal(err) }
            if err := fetchCheckpoints(*remote); err != nil { fatal(err) }
        default:
            fmt.Println("usage: aigit sync push|pull [options]")
        }
    case "remote-list":
        fs := flag.NewFlagSet("remote-list", flag.ExitOnError)
        remote := fs.String("remote", defaultStr(getGitConfig("aigit.pullRemote"), "origin"), "remote name")
        user := fs.String("user", "", "filter by user id; if empty, list users")
        n := fs.Int("n", 20, "number of entries when listing a user")
        meta := fs.Bool("meta", false, "show metadata trailers when listing a user")
        if err := fs.Parse(args); err != nil { fatal(err) }
        if err := doRemoteList(*remote, *user, *n, *meta); err != nil { fatal(err) }
    case "apply":
        fs := flag.NewFlagSet("apply", flag.ExitOnError)
        from := fs.String("from", "", "user id to apply from (required)")
        remote := fs.String("remote", defaultStr(getGitConfig("aigit.pullRemote"), "origin"), "remote name")
        sha := fs.String("sha", "", "checkpoint sha to apply (default latest)")
        if err := fs.Parse(args); err != nil { fatal(err) }
        if strings.TrimSpace(*from) == "" { fatal(errors.New("--from <user> is required")) }
        if err := applyRemoteCheckpoint(*remote, *from, *sha); err != nil { fatal(err) }
    case "restore":
        fs := flag.NewFlagSet("restore", flag.ExitOnError)
        if err := fs.Parse(args); err != nil {
            fatal(err)
        }
        if fs.NArg() < 1 {
            fatal(errors.New("usage: aigit restore <sha>"))
        }
        sha := fs.Arg(0)
        if err := doRestore(sha); err != nil {
            fatal(err)
        }
    case "watch":
        fs := flag.NewFlagSet("watch", flag.ExitOnError)
        intervalStr := fs.String("interval", defaultStr(getGitConfig("aigit.interval"), "3m"), "checkpoint interval, e.g. 30s, 2m, 1h")
        settleStr := fs.String("settle", defaultStr(getGitConfig("aigit.settle"), "1.5s"), "settle window after changes, e.g. 1s")
        summaryMode := fs.String("summary", defaultStr(getGitConfig("aigit.summary"), "ai"), "summary mode: ai|diff|off")
        aiModel := fs.String("model", defaultStr(getGitConfig("aigit.summaryModel"), "openai/gpt-oss-20b:free"), "OpenRouter model when summary=ai")
        if err := fs.Parse(args); err != nil {
            fatal(err)
        }
        d, err := time.ParseDuration(*intervalStr)
        if err != nil {
            fatal(fmt.Errorf("invalid interval: %w", err))
        }
        settle, err := time.ParseDuration(*settleStr)
        if err != nil {
            fatal(fmt.Errorf("invalid settle: %w", err))
        }
        if err := doWatch(d, settle, *summaryMode, *aiModel); err != nil {
            fatal(err)
        }
    default:
        printHelp()
    }
}

func printHelp() {
    fmt.Println("Aigit commands:")
    fmt.Println("  aigit checkpoint -m \"summary\"    # save a live snapshot (works during merges)")
    fmt.Println("  aigit status                     # show last checkpoint summary + diff")
    fmt.Println("  aigit id                         # show your remote user id and refs")
    fmt.Println("  aigit list [-n 20] [--meta]      # list recent checkpoints for this branch")
    fmt.Println("  aigit restore <sha>              # restore files from a checkpoint")
    fmt.Println("  aigit sync push|pull [options]   # push/pull checkpoint refs via remote")
    fmt.Println("  aigit remote-list [--user id]    # list users or a user's remote checkpoints")
    fmt.Println("  aigit apply --from <user>        # apply a remote user's checkpoint to worktree")
    fmt.Println("  aigit tail [-n 100]              # stream watcher logs (AI summaries + checkpoints)")
    fmt.Println("  aigit watch [-interval 3m] [-summary ai|diff|off]  # background snapshots on change")
    fmt.Println("  aigit init-shell --zsh|--bash     # install shell integration so updates pop up while you work")
    fmt.Println("")
    fmt.Println("AI summaries (OpenRouter): set OPENROUTER_API_KEY, default model openai/gpt-oss-20b:free")
    fmt.Println("")
    fmt.Println("Tips:")
    fmt.Println("  git log --oneline $(git rev-parse --abbrev-ref HEAD | xargs -I{} echo refs/aigit/checkpoints/{})")
    fmt.Println("  git show <sha>")
}

func fatal(err error) {
    fmt.Fprintf(os.Stderr, "aigit: %v\n", err)
    os.Exit(1)
}

// ---- Core helpers ----

func git(args ...string) (string, error) {
    cmd := exec.Command("git", args...)
    var out bytes.Buffer
    var stderr bytes.Buffer
    cmd.Stdout = &out
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        if stderr.Len() > 0 {
            return "", fmt.Errorf("git %v: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
        }
        return "", fmt.Errorf("git %v: %w", strings.Join(args, " "), err)
    }
    return strings.TrimSpace(out.String()), nil
}

func gitEnv(env map[string]string, args ...string) (string, error) {
    cmd := exec.Command("git", args...)
    cmd.Env = os.Environ()
    for k, v := range env {
        cmd.Env = append(cmd.Env, k+"="+v)
    }
    var out bytes.Buffer
    var stderr bytes.Buffer
    cmd.Stdout = &out
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        if stderr.Len() > 0 {
            return "", fmt.Errorf("git %v: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
        }
        return "", fmt.Errorf("git %v: %w", strings.Join(args, " "), err)
    }
    return strings.TrimSpace(out.String()), nil
}

func currentBranch() (string, error) {
    s, err := git("rev-parse", "--abbrev-ref", "HEAD")
    if err != nil {
        return "", err
    }
    return s, nil
}

func ckRef() (string, error) {
    br, err := currentBranch()
    if err != nil {
        return "", err
    }
    return "refs/aigit/checkpoints/" + br, nil
}

func gitDir() (string, error) {
    return git("rev-parse", "--git-dir")
}

func gitTopLevel() (string, error) {
    return git("rev-parse", "--show-toplevel")
}

func isMerging() bool {
    dir, err := gitDir()
    if err != nil {
        return false
    }
    _, err = os.Stat(filepath.Join(dir, "MERGE_HEAD"))
    return err == nil
}

// ---- Commands ----

func doCheckpoint(summary string) error {
    ref, err := ckRef()
    if err != nil {
        return err
    }

    // Create a temp dir and point GIT_INDEX_FILE to a path inside it.
    // Do NOT pre-create the file; let Git create it to avoid "index file smaller than expected".
    tmpdir, err := os.MkdirTemp("", "aigit-index-*")
    if err != nil {
        return err
    }
    defer os.RemoveAll(tmpdir)
    idxPath := filepath.Join(tmpdir, "index")

    env := map[string]string{"GIT_INDEX_FILE": idxPath}

    if _, err := gitEnv(env, "add", "-A"); err != nil {
        return err
    }
    tree, err := gitEnv(env, "write-tree")
    if err != nil {
        return err
    }

    // Find parent (previous checkpoint commit if exists)
    var parent string
    if out, err := git("rev-parse", "-q", "--verify", ref+"^{commit}"); err == nil {
        parent = out
    }

    base, err := git("rev-parse", "HEAD")
    if err != nil {
        return err
    }
    merging := "no"
    if isMerging() {
        merging = "yes"
    }

    meta := fmt.Sprintf("Aigit-Base: %s\nAigit-When: %s\nAigit-Merge: %s\n", base, time.Now().UTC().Format(time.RFC3339), merging)

    // Build commit via commit-tree, piping message
    var newSha string
    {
        args := []string{"commit-tree", tree}
        if parent != "" {
            args = append(args, "-p", parent)
        }
        cmd := exec.Command("git", args...)
        cmd.Stdin = strings.NewReader(summary + "\n\n" + meta + "\n")
        var out bytes.Buffer
        var stderr bytes.Buffer
        cmd.Stdout = &out
        cmd.Stderr = &stderr
        if err := cmd.Run(); err != nil {
            if stderr.Len() > 0 {
                return fmt.Errorf("git %v: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
            }
            return fmt.Errorf("git %v: %w", strings.Join(args, " "), err)
        }
        newSha = strings.TrimSpace(out.String())
    }

    // Atomic update-ref
    if parent != "" {
        if _, err := git("update-ref", "-m", fmt.Sprintf("aigit: %s", summary), ref, newSha, parent); err != nil {
            return err
        }
    } else {
        if _, err := git("update-ref", "-m", fmt.Sprintf("aigit: %s", summary), ref, newSha); err != nil {
            return err
        }
    }

    fmt.Printf("update-arrived!\n")
    logLine("update-arrived!")
    fmt.Printf("Summary: %s\n", summary)
    logLine("Summary: %s", summary)
    // Print a concise list of changed files for this checkpoint
    if files, err := git("diff-tree", "--no-commit-id", "--name-status", "-r", newSha); err == nil && strings.TrimSpace(files) != "" {
        fmt.Println("Files:")
        logLine("Files:")
        scanner := bufio.NewScanner(strings.NewReader(files))
        count := 0
        for scanner.Scan() {
            line := scanner.Text()
            fmt.Printf("  %s\n", line)
            logLine("  %s", line)
            count++
            if count >= 20 {
                fmt.Println("  ...")
                logLine("  ...")
                break
            }
        }
    }
    fmt.Printf("Checkpoint: %s  (%s)\n", newSha, summary)
    logLine("Checkpoint: %s  (%s)", newSha, summary)
    // Optional autopush
    if remote := strings.TrimSpace(getGitConfig("aigit.pushRemote")); remote != "" {
        if err := pushCheckpoints(remote); err != nil {
            fmt.Fprintf(os.Stderr, "push checkpoints failed: %v\n", err)
        }
    }
    return nil
}

func doStatus() error {
    ref, err := ckRef()
    if err != nil {
        return err
    }
    br, _ := currentBranch()
    last := "no checkpoints yet"
    if out, err := git("log", "-1", "--format=%s", ref); err == nil && strings.TrimSpace(out) != "" {
        last = out
    }
    fmt.Printf("Branch: %s\n", br)
    fmt.Printf("Last checkpoint: %s\n", last)
    if isMerging() {
        if conflicts, _ := listConflicts(); len(conflicts) > 0 {
            n := len(conflicts)
            preview := joinPreview(conflicts)
            fmt.Printf("Merge in progress: %d conflicted files (%s)\n", n, preview)
        }
    }
    fmt.Println("")
    fmt.Println("Working tree diff vs HEAD:")
    if out, err := git("--no-pager", "diff", "--stat"); err == nil {
        if strings.TrimSpace(out) == "" {
            // Nothing changed vs HEAD
            fmt.Println("nothing here yet, clean workspace")
        } else {
            fmt.Println(out)
            // Also show a suggested one-line summary of pending changes
            mode := strings.ToLower(defaultStr(getGitConfig("aigit.summary"), "ai"))
            model := defaultStr(getGitConfig("aigit.summaryModel"), "openai/gpt-oss-20b:free")
            if s, used := suggestSummary(mode, model); strings.TrimSpace(s) != "" {
                fmt.Printf("Suggested summary (%s): %s\n", used, s)
            }
        }
    } else {
        return err
    }
    return nil
}

func doRestore(sha string) error {
    fmt.Printf("Restoring worktree from %s (does not move HEAD)...\n", sha)
    // Prefer git restore, fallback to checkout for older Git
    if _, err := git("restore", "--worktree", "--source", sha, "--", "."); err != nil {
        // Fallback
        if _, err2 := git("checkout", sha, "--", "."); err2 != nil {
            return fmt.Errorf("restore failed: %v; fallback checkout failed: %v", err, err2)
        }
    }
    fmt.Println("Done. (Untracked files are left as-is.)")
    return nil
}

func doWatch(interval, settle time.Duration, summaryMode, aiModel string) error {
    root, err := gitTopLevel()
    if err != nil {
        return fmt.Errorf("not in a git repo: %w", err)
    }
    fmt.Printf("Watching %s (interval=%s, settle=%s, summary=%s)...\n", root, interval, settle, summaryMode)
    recordWatchPID()
    fmt.Println("Tip: run 'aigit tail' in another terminal to view live summaries and checkpoints.")

    // Start fsnotify-based watcher in a goroutine
    events := make(chan struct{}, 1)
    stop, err := startFsWatch(root, events)
    if err != nil {
        fmt.Fprintf(os.Stderr, "fsnotify unavailable, falling back to timer-only: %v\n", err)
    }
    defer func() {
        if stop != nil {
            _ = stop()
        }
    }()

    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    var lastEvent time.Time
    active := false

    for {
        select {
        case <-events:
            if !active {
                active = true
                fmt.Println("Detected changes; live checkpoints activated.")
            }
            lastEvent = time.Now()
        case <-time.After(settle):
            if active && !lastEvent.IsZero() && time.Since(lastEvent) >= settle {
                if err := maybeCheckpoint(summaryMode, aiModel); err != nil {
                    fmt.Fprintf(os.Stderr, "checkpoint error: %v\n", err)
                }
                lastEvent = time.Time{}
            }
        case <-ticker.C:
            // periodic remote sync and optional apply
            if err := maybePullAndAutoApply(); err != nil {
                fmt.Fprintf(os.Stderr, "remote sync/apply failed: %v\n", err)
            }
            if !active {
                // Stay idle until the first save enables local checkpointing
                continue
            }
            // periodic safety net for local changes
            if err := maybeCheckpoint(summaryMode, aiModel); err != nil {
                fmt.Fprintf(os.Stderr, "checkpoint error: %v\n", err)
            }
        }
    }
}

func maybeCheckpoint(summaryMode, aiModel string) error {
    changed, err := workingTreeChanged()
    if err != nil {
        return err
    }
    if !changed {
        return nil
    }
    // Build summary
    var summary string
    var used string
    switch strings.ToLower(summaryMode) {
    case "off":
        summary = "(auto)"
        used = "off"
    case "ai":
        if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
            if s, err := summarizeWithAI(aiModel); err == nil && strings.TrimSpace(s) != "" {
                summary = s
                used = "AI"
                break
            } else if err != nil {
                fmt.Fprintf(os.Stderr, "AI summary failed, falling back to diff: %v\n", err)
            }
        } else {
            fmt.Fprintln(os.Stderr, "OPENROUTER_API_KEY not set; falling back to diff summaries")
        }
        fallthrough
    default:
        summary, _ = diffOneLiner()
        if summary == "" {
            summary = "(auto)"
        }
        if used == "" { used = "diff" }
    }
    // Echo the summary source for clarity in terminal output
    if used == "AI" {
        fmt.Printf("AI summary: %s\n", summary)
    } else if used == "diff" {
        fmt.Printf("Summary (diff): %s\n", summary)
    }
    return doCheckpoint(summary)
}

func workingTreeChanged() (bool, error) {
    out, err := git("status", "--porcelain")
    if err != nil {
        return false, err
    }
    return strings.TrimSpace(out) != "", nil
}

func diffOneLiner() (string, error) {
    out, err := git("diff", "--name-status", "-M", "-C")
    if err != nil {
        return "", err
    }
    var adds, mods, dels, renames []string
    scanner := bufio.NewScanner(strings.NewReader(out))
    for scanner.Scan() {
        line := scanner.Text()
        parts := strings.Fields(line)
        if len(parts) == 0 {
            continue
        }
        code := parts[0]
        switch {
        case strings.HasPrefix(code, "A"):
            if len(parts) > 1 { adds = append(adds, parts[len(parts)-1]) }
        case strings.HasPrefix(code, "M"):
            if len(parts) > 1 { mods = append(mods, parts[len(parts)-1]) }
        case strings.HasPrefix(code, "D"):
            if len(parts) > 1 { dels = append(dels, parts[len(parts)-1]) }
        case strings.HasPrefix(code, "R"):
            if len(parts) > 2 { renames = append(renames, parts[1]+"->"+parts[2]) }
        }
    }
    // Include untracked files as adds
    if outU, err := git("ls-files", "--others", "--exclude-standard"); err == nil {
        for _, ln := range strings.Split(strings.TrimSpace(outU), "\n") {
            ln = strings.TrimSpace(ln)
            if ln != "" {
                adds = append(adds, ln)
            }
        }
    }
    var chunks []string
    if len(mods) > 0 {
        chunks = append(chunks, fmt.Sprintf("Edit %s", joinPreview(mods)))
    }
    if len(adds) > 0 {
        chunks = append(chunks, fmt.Sprintf("Add %s", joinPreview(adds)))
    }
    if len(dels) > 0 {
        chunks = append(chunks, fmt.Sprintf("Remove %s", joinPreview(dels)))
    }
    if len(renames) > 0 {
        chunks = append(chunks, fmt.Sprintf("Rename %s", joinPreview(renames)))
    }
    return strings.Join(chunks, "; "), nil
}

func joinPreview(paths []string) string {
    const max = 3
    if len(paths) <= max {
        return strings.Join(paths, ", ")
    }
    return strings.Join(paths[:max], ", ") + fmt.Sprintf(" (+%d)", len(paths)-max)
}

func runStream(name string, args ...string) error {
    cmd := exec.Command(name, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.Stdin = os.Stdin
    return cmd.Run()
}

func doTail(lines int) error {
    dir, err := aigitDir()
    if err != nil { return err }
    logp := filepath.Join(dir, "aigit.log")
    // If tail is available, use it. Otherwise, simple follow loop.
    if _, err := exec.LookPath("tail"); err == nil {
        args := []string{"-n", fmt.Sprint(lines), "-f", logp}
        return runStream("tail", args...)
    }
    // Minimal fallback: print last N lines, then poll file for appends.
    b, _ := os.ReadFile(logp)
    out := string(b)
    rows := strings.Split(out, "\n")
    if len(rows) > lines {
        rows = rows[len(rows)-lines:]
    }
    for _, r := range rows { fmt.Println(r) }
    // Poll every second for new data
    lastLen := len(b)
    for {
        time.Sleep(time.Second)
        nb, _ := os.ReadFile(logp)
        if len(nb) > lastLen {
            os.Stdout.Write(nb[lastLen:])
            lastLen = len(nb)
        }
    }
}

func doInitShell(zsh, bash bool) error {
    home, err := os.UserHomeDir()
    if err != nil { return err }
    cfgDir := filepath.Join(home, ".config", "aigit")
    if err := os.MkdirAll(cfgDir, 0o755); err != nil { return err }
    if zsh {
        path := filepath.Join(cfgDir, "aigit-shell.zsh")
        content := `# Installed by aigit init-shell --zsh
typeset -g AIGIT_EVENTS_PID=""
typeset -g AIGIT_EVENTS_ROOT=""
function _aigit_watch_repo() {
  local top
  top=$(git rev-parse --show-toplevel 2>/dev/null) || top=""
  if [[ -z "$top" ]]; then
    if [[ -n "$AIGIT_EVENTS_PID" ]]; then kill "$AIGIT_EVENTS_PID" 2>/dev/null; AIGIT_EVENTS_PID=""; AIGIT_EVENTS_ROOT=""; fi
    return 0
  fi
  if [[ "$top" == "$AIGIT_EVENTS_ROOT" ]] && kill -0 "$AIGIT_EVENTS_PID" 2>/dev/null; then
    return 0
  fi
  if [[ -n "$AIGIT_EVENTS_PID" ]]; then kill "$AIGIT_EVENTS_PID" 2>/dev/null; fi
  AIGIT_EVENTS_ROOT="$top"
  local sid
  sid="zsh:${HOST}:${TTY}:${$}:$top"
  (cd "$top" && aigit events -id "$sid" -n 50 --follow) &!
  AIGIT_EVENTS_PID=$!
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd _aigit_watch_repo
`
        if err := os.WriteFile(path, []byte(content), 0o644); err != nil { return err }
        fmt.Printf("Installed zsh hook at %s\nAdd to ~/.zshrc: source %s\n", path, path)
    }
    if bash {
        path := filepath.Join(cfgDir, "aigit-shell.bash")
        content := `# Installed by aigit init-shell --bash
_AIGIT_EVENTS_PID=""
_AIGIT_EVENTS_ROOT=""
_aigit_watch_repo() {
  local top
  top=$(git rev-parse --show-toplevel 2>/dev/null) || top=""
  if [[ -z "$top" ]]; then
    if [[ -n "$_AIGIT_EVENTS_PID" ]]; then kill "$_AIGIT_EVENTS_PID" 2>/dev/null; _AIGIT_EVENTS_PID=""; _AIGIT_EVENTS_ROOT=""; fi
    return 0
  fi
  if [[ "$top" == "$_AIGIT_EVENTS_ROOT" ]] && kill -0 "$_AIGIT_EVENTS_PID" 2>/dev/null; then
    return 0
  fi
  if [[ -n "$_AIGIT_EVENTS_PID" ]]; then kill "$_AIGIT_EVENTS_PID" 2>/dev/null; fi
  _AIGIT_EVENTS_ROOT="$top"
  local sid
  sid="bash:${HOSTNAME}:${TTY}:$$:$top"
  (cd "$top" && aigit events -id "$sid" -n 50 --follow) &
  _AIGIT_EVENTS_PID=$!
}
if [[ -n "$PROMPT_COMMAND" ]]; then
  PROMPT_COMMAND="_aigit_watch_repo; $PROMPT_COMMAND"
else
  PROMPT_COMMAND="_aigit_watch_repo"
fi
`
        if err := os.WriteFile(path, []byte(content), 0o644); err != nil { return err }
        fmt.Printf("Installed bash hook at %s\nAdd to ~/.bashrc: source %s\n", path, path)
    }
    return nil
}

func doEvents(sessionID string, back int, follow bool) error {
    dir, err := aigitDir()
    if err != nil { return err }
    logp := filepath.Join(dir, "aigit.log")
    // State dir
    sdir := filepath.Join(dir, "events")
    if err := os.MkdirAll(sdir, 0o755); err != nil { return err }
    sid := strings.ReplaceAll(sessionID, string(os.PathSeparator), "_")
    spos := filepath.Join(sdir, sid+".pos")
    // Read last position
    var pos int64 = 0
    if b, err := os.ReadFile(spos); err == nil {
        if p, perr := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); perr == nil {
            pos = p
        }
    }
    fi, err := os.Stat(logp)
    if err != nil {
        // No log yet; nothing to print
        return nil
    }
    size := fi.Size()
    if pos == 0 {
        // First run: print last 'back' lines
        data, _ := os.ReadFile(logp)
        lines := strings.Split(string(data), "\n")
        if back > 0 && len(lines) > back {
            lines = lines[len(lines)-back:]
        }
        for _, ln := range lines { if strings.TrimSpace(ln) != "" { fmt.Println(ln) } }
        // Advance to end
        _ = os.WriteFile(spos, []byte(fmt.Sprint(size)), 0o644)
        if !follow { return nil }
    }
    if pos > size {
        // Log rotated or truncated; reset
        pos = 0
    }
    // Follow mode: print new data as it arrives
    for {
        f, err := os.Open(logp)
        if err != nil { time.Sleep(500 * time.Millisecond); continue }
        if pos > 0 { if _, err := f.Seek(pos, io.SeekStart); err != nil { pos = 0; f.Seek(0, io.SeekStart) } }
        n, _ := io.Copy(os.Stdout, f)
        _ = f.Close()
        if n > 0 { pos += n; _ = os.WriteFile(spos, []byte(fmt.Sprint(pos)), 0o644) }
        time.Sleep(500 * time.Millisecond)
    }
}

// logLine appends a formatted line to the repo-scoped log file.
func logLine(format string, a ...any) {
    dir, err := aigitDir()
    if err != nil { return }
    logp := filepath.Join(dir, "aigit.log")
    f, err := os.OpenFile(logp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil { return }
    defer f.Close()
    fmt.Fprintf(f, format+"\n", a...)
}

// suggestSummary returns a one-line summary and the mode used (AI or diff).
func suggestSummary(summaryMode, aiModel string) (summary string, used string) {
    switch strings.ToLower(summaryMode) {
    case "ai":
        if os.Getenv("OPENROUTER_API_KEY") != "" {
            if s, err := summarizeWithAI(aiModel); err == nil && strings.TrimSpace(s) != "" {
                return s, "AI"
            }
        }
        fallthrough
    case "diff", "":
        s, _ := diffOneLiner()
        return s, "diff"
    default:
        return "", strings.ToLower(summaryMode)
    }
}

func doID() error {
    uid := getUserID()
    br, _ := currentBranch()
    local, _ := ckRef()
    pushRemote := strings.TrimSpace(getGitConfig("aigit.pushRemote"))
    pullRemote := strings.TrimSpace(getGitConfig("aigit.pullRemote"))
    fmt.Printf("User ID: %s\n", uid)
    fmt.Printf("Branch: %s\n", br)
    fmt.Printf("Local ref: %s\n", local)
    if pushRemote != "" {
        fmt.Printf("Push target: %s %s\n", pushRemote, userRemoteRef(uid, br))
    } else {
        fmt.Printf("Push target: (not configured)\n")
    }
    if pullRemote != "" {
        fmt.Printf("Pull source: %s %s\n", pullRemote, remoteTrackingRef(pullRemote, uid, br))
    } else {
        fmt.Printf("Pull source: (not configured)\n")
    }
    return nil
}

func doRemoteList(remote, user string, limit int, showMeta bool) error {
    if strings.TrimSpace(remote) == "" { remote = "origin" }
    // Ensure we have latest checkpoint refs
    _ = fetchCheckpoints(remote)
    br, err := currentBranch()
    if err != nil { return err }
    if strings.TrimSpace(user) == "" {
        users, err := listRemoteUsers(remote, br)
        if err != nil { return err }
        if len(users) == 0 {
            fmt.Println("No remote checkpoint users found.")
            return nil
        }
        fmt.Println("Remote users with checkpoints:")
        for _, u := range users {
            fmt.Println(" -", u)
        }
        return nil
    }
    ref := remoteTrackingRef(remote, user, br)
    if _, err := git("rev-parse", "-q", "--verify", ref); err != nil {
        return fmt.Errorf("no checkpoints for user %s on %s", user, br)
    }
    format := "%h%x09%ct%x09%s"
    out, err := git("--no-pager", "log", "-n", strconv.Itoa(limit), "--format="+format, ref)
    if err != nil { return err }
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.TrimSpace(line) == "" { continue }
        parts := strings.SplitN(line, "\t", 3)
        if len(parts) < 3 { continue }
        sha, ctStr, subj := parts[0], parts[1], parts[2]
        ct, _ := strconv.ParseInt(ctStr, 10, 64)
        rel := relTime(time.Since(time.Unix(ct, 0)))
        fmt.Printf("%s  %6s  %s\n", sha, rel, subj)
        if showMeta {
            body, _ := git("show", "-s", "--format=%B", sha)
            m := parseMeta(body)
            line := ""
            if m.Base != "" { line += fmt.Sprintf("base=%s ", short(m.Base)) }
            if m.Merge != "" { line += fmt.Sprintf("merge=%s ", m.Merge) }
            if m.When != "" { line += fmt.Sprintf("when=%s", m.When) }
            if strings.TrimSpace(line) != "" { fmt.Printf("    %s\n", strings.TrimSpace(line)) }
        }
    }
    return nil
}

func doList(limit int, showMeta bool) error {
    ref, err := ckRef()
    if err != nil {
        return err
    }
    if _, err := git("rev-parse", "-q", "--verify", ref); err != nil {
        fmt.Println("No checkpoints yet.")
        return nil
    }
    format := "%h%x09%ct%x09%s"
    out, err := git("--no-pager", "log", "-n", strconv.Itoa(limit), "--format="+format, ref)
    if err != nil {
        return err
    }
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.TrimSpace(line) == "" {
            continue
        }
        parts := strings.SplitN(line, "\t", 3)
        if len(parts) < 3 {
            continue
        }
        sha, ctStr, subj := parts[0], parts[1], parts[2]
        ct, _ := strconv.ParseInt(ctStr, 10, 64)
        rel := relTime(time.Since(time.Unix(ct, 0)))
        fmt.Printf("%s  %6s  %s\n", sha, rel, subj)
        if showMeta {
            body, _ := git("show", "-s", "--format=%B", sha)
            m := parseMeta(body)
            line := ""
            if m.Base != "" {
                line += fmt.Sprintf("base=%s ", short(m.Base))
            }
            if m.Merge != "" {
                line += fmt.Sprintf("merge=%s ", m.Merge)
            }
            if m.When != "" {
                line += fmt.Sprintf("when=%s", m.When)
            }
            if strings.TrimSpace(line) != "" {
                fmt.Printf("    %s\n", strings.TrimSpace(line))
            }
        }
    }
    return nil
}

type metaInfo struct{ Base, When, Merge string }

func parseMeta(body string) metaInfo {
    var m metaInfo
    scanner := bufio.NewScanner(strings.NewReader(body))
    for scanner.Scan() {
        s := scanner.Text()
        if strings.HasPrefix(s, "Aigit-Base:") {
            m.Base = strings.TrimSpace(strings.TrimPrefix(s, "Aigit-Base:"))
        } else if strings.HasPrefix(s, "Aigit-When:") {
            m.When = strings.TrimSpace(strings.TrimPrefix(s, "Aigit-When:"))
        } else if strings.HasPrefix(s, "Aigit-Merge:") {
            m.Merge = strings.TrimSpace(strings.TrimPrefix(s, "Aigit-Merge:"))
        }
    }
    return m
}

func short(sha string) string {
    if len(sha) > 7 {
        return sha[:7]
    }
    return sha
}

func relTime(d time.Duration) string {
    if d < time.Minute {
        return "now"
    }
    if d < time.Hour {
        return fmt.Sprintf("%dm", int(d.Minutes()))
    }
    if d < 24*time.Hour {
        return fmt.Sprintf("%dh", int(d.Hours()))
    }
    return fmt.Sprintf("%dd", int(d.Hours())/24)
}

func listConflicts() ([]string, error) {
    out, err := git("diff", "--name-only", "--diff-filter=U")
    if err != nil {
        return nil, err
    }
    var files []string
    for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
        line = strings.TrimSpace(line)
        if line != "" {
            files = append(files, line)
        }
    }
    return files, nil
}

func getGitConfig(key string) string {
    out, err := git("config", "--get", key)
    if err != nil {
        return ""
    }
    return strings.TrimSpace(out)
}

func defaultStr(v string, def string) string {
    if strings.TrimSpace(v) == "" {
        return def
    }
    return v
}
    case "events":
        fs := flag.NewFlagSet("events", flag.ExitOnError)
        sessionID := fs.String("id", "", "unique session id for state (e.g., host:tty:pid)")
        back := fs.Int("n", 80, "lines to show on first run")
        follow := fs.Bool("follow", false, "follow and print events as they arrive")
        if err := fs.Parse(args); err != nil { fatal(err) }
        if strings.TrimSpace(*sessionID) == "" { fatal(errors.New("usage: aigit events -id <session-id> [-n N] [--follow]")) }
        if err := doEvents(*sessionID, *back, *follow); err != nil { fatal(err) }
