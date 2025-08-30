package main

import (
    "bufio"
    "bytes"
    "errors"
    "flag"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
)

func main() {
    if len(os.Args) < 2 {
        printHelp()
        return
    }

    cmd := os.Args[1]
    args := os.Args[2:]

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
        intervalStr := fs.String("interval", "3m", "checkpoint interval, e.g. 30s, 2m, 1h")
        if err := fs.Parse(args); err != nil {
            fatal(err)
        }
        d, err := time.ParseDuration(*intervalStr)
        if err != nil {
            fatal(fmt.Errorf("invalid interval: %w", err))
        }
        if err := doWatch(d); err != nil {
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
    fmt.Println("  aigit restore <sha>              # restore files from a checkpoint")
    fmt.Println("  aigit watch [-interval 3m]       # background snapshots when there are changes")
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

    // Create temporary index file and snapshot working tree into it
    tmp, err := os.CreateTemp("", "aigit-index-*")
    if err != nil {
        return err
    }
    tmp.Close()
    defer os.Remove(tmp.Name())

    env := map[string]string{"GIT_INDEX_FILE": tmp.Name()}

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

    fmt.Printf("Checkpoint: %s  (%s)\n", newSha, summary)
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
    fmt.Printf("Last checkpoint: %s\n\n", last)
    fmt.Println("Working tree diff vs HEAD:")
    // Show diff stat without paging
    if err := runStream("git", "--no-pager", "diff", "--stat"); err != nil {
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

func doWatch(interval time.Duration) error {
    fmt.Printf("Watching for changes every %s... (Ctrl+C to stop)\n", interval)
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        // On each tick, if there are changes, create a checkpoint with a heuristic summary
        changed, err := workingTreeChanged()
        if err != nil {
            return err
        }
        if changed {
            summary, _ := diffOneLiner()
            if summary == "" {
                summary = "(auto) periodic checkpoint"
            }
            if err := doCheckpoint(summary); err != nil {
                // Log and continue
                fmt.Fprintf(os.Stderr, "checkpoint error: %v\n", err)
            }
        }
        <-ticker.C
    }
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
    if strings.TrimSpace(out) == "" {
        return "", nil
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
