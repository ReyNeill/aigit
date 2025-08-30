package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "os/user"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "syscall"
    "time"
)

// ---- Autostart daemon helpers ----

func aigitDir() (string, error) {
    gd, err := gitDir()
    if err != nil {
        return "", err
    }
    dir := filepath.Join(gd, "aigit")
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return "", err
    }
    return dir, nil
}

func maybeAutostartWatch() error {
    // Only attempt inside a git repo
    if _, err := gitTopLevel(); err != nil {
        return nil
    }
    dir, err := aigitDir()
    if err != nil { return nil }
    pidPath := filepath.Join(dir, "watch.pid")
    if b, err := os.ReadFile(pidPath); err == nil {
        s := strings.TrimSpace(string(b))
        if pid, err := strconv.Atoi(s); err == nil {
            if isAlive(pid) { return nil }
        }
    }
    // Build watch command using config defaults
    interval := defaultStr(getGitConfig("aigit.interval"), "3m")
    settle := defaultStr(getGitConfig("aigit.settle"), "1.5s")
    summary := defaultStr(getGitConfig("aigit.summary"), "ai")
    model := defaultStr(getGitConfig("aigit.summaryModel"), "x-ai/grok-code-fast-1")
    args := []string{"watch", "-interval", interval, "-settle", settle, "-summary", summary, "-model", model}
    cmd := exec.Command(os.Args[0], args...)
    // detach
    if runtime.GOOS != "windows" {
        cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    }
    // Silence output; could redirect to a log file if desired
    devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
    cmd.Stdout = devnull
    cmd.Stderr = devnull
    if err := cmd.Start(); err != nil {
        return err
    }
    return nil
}

func isAlive(pid int) bool {
    p, err := os.FindProcess(pid)
    if err != nil { return false }
    if runtime.GOOS == "windows" {
        // Best effort: FindProcess doesn't error if not running on Windows; assume alive
        return true
    }
    err = p.Signal(syscall.Signal(0))
    return err == nil
}

// Write the PID of the watcher to file
func recordWatchPID() {
    dir, err := aigitDir()
    if err != nil { return }
    pidPath := filepath.Join(dir, "watch.pid")
    _ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

// ---- Remote sync / apply ----

func getUserID() string {
    if v := strings.TrimSpace(getGitConfig("aigit.user")); v != "" { return sanitizeID(v) }
    if v := strings.TrimSpace(getGitConfig("user.email")); v != "" { return sanitizeID(v) }
    if u, err := user.Current(); err == nil && u.Username != "" { return sanitizeID(u.Username) }
    return "anon"
}

func sanitizeID(s string) string {
    s = strings.ToLower(s)
    s = strings.ReplaceAll(s, "@", "_")
    s = strings.ReplaceAll(s, "/", "_")
    s = strings.ReplaceAll(s, " ", "_")
    return s
}

func userRemoteRef(user, branch string) string {
    return "refs/aigit/users/" + user + "/checkpoints/" + branch
}

func remoteTrackingRef(remote, user, branch string) string {
    // Where fetched refs will appear locally
    return "refs/remotes/" + remote + "/aigit/users/" + user + "/checkpoints/" + branch
}

func pushCheckpoints(remote string) error {
    br, err := currentBranch()
    if err != nil { return err }
    localRef, err := ckRef()
    if err != nil { return err }
    user := getUserID()
    remoteRef := userRemoteRef(user, br)
    _, err = git("push", "-f", remote, localRef+":"+remoteRef)
    return err
}

func fetchCheckpoints(remote string) error {
    // Fetch all aigit refs under remote into refs/remotes/<remote>/aigit/*
    // Use a refspec to ensure they are fetched.
    _, err := git("fetch", remote, "+refs/aigit/*:refs/remotes/"+remote+"/aigit/*")
    return err
}

func latestRemoteCheckpoint(remote, user, branch string) (string, string, error) {
    ref := remoteTrackingRef(remote, user, branch)
    sha, err := git("rev-parse", "-q", "--verify", ref+"^{commit}")
    if err != nil { return "", "", err }
    subj, _ := git("log", "-1", "--format=%s", ref)
    return sha, subj, nil
}

func applyRemoteCheckpoint(remote, user, sha string) error {
    br, err := currentBranch()
    if err != nil { return err }
    if strings.TrimSpace(sha) == "" {
        tip, _, err := latestRemoteCheckpoint(remote, user, br)
        if err != nil { return err }
        sha = tip
    }
    fmt.Printf("Applying %s from %s/%s to worktree...\n", short(sha), remote, user)
    if _, err := git("restore", "--worktree", "--source", sha, "--", "."); err != nil {
        if _, err2 := git("checkout", sha, "--", "."); err2 != nil {
            return fmt.Errorf("apply failed: %v; fallback checkout failed: %v", err, err2)
        }
    }
    // Record last applied
    _ = markApplied(remote, user, br, sha)
    return nil
}

// ---- Auto-apply state ----

type appliedState struct {
    Items map[string]string `json:"items"` // key -> sha
}

func statePath() (string, error) {
    dir, err := aigitDir()
    if err != nil { return "", err }
    return filepath.Join(dir, "applied.json"), nil
}

func loadState() (*appliedState, error) {
    p, err := statePath()
    if err != nil { return nil, err }
    b, err := os.ReadFile(p)
    if err != nil { return &appliedState{Items: map[string]string{}}, nil }
    var st appliedState
    if err := json.Unmarshal(b, &st); err != nil { return &appliedState{Items: map[string]string{}}, nil }
    if st.Items == nil { st.Items = map[string]string{} }
    return &st, nil
}

func saveState(st *appliedState) error {
    p, err := statePath()
    if err != nil { return err }
    b, _ := json.MarshalIndent(st, "", "  ")
    return os.WriteFile(p, b, 0o644)
}

func key(remote, user, branch string) string { return remote+"|"+user+"|"+branch }

func markApplied(remote, user, branch, sha string) error {
    st, err := loadState()
    if err != nil { return err }
    st.Items[key(remote, user, branch)] = sha
    return saveState(st)
}

func lastApplied(remote, user, branch string) (string, error) {
    st, err := loadState()
    if err != nil { return "", err }
    return st.Items[key(remote, user, branch)], nil
}

// maybePullAndAutoApply fetches remote checkpoint refs and applies latest from configured users
func maybePullAndAutoApply() error {
    remote := strings.TrimSpace(getGitConfig("aigit.pullRemote"))
    if remote == "" { return nil }
    if err := fetchCheckpoints(remote); err != nil { return err }
    br, err := currentBranch()
    if err != nil { return err }
    auto := strings.EqualFold(strings.TrimSpace(getGitConfig("aigit.autoApply")), "true")
    if !auto { return nil }
    allow := strings.TrimSpace(getGitConfig("aigit.autoApplyFrom"))
    var users []string
    if allow == "*" || allow == "" {
        // Apply from all users except self by enumerating refs under remote path
        list, _ := listRemoteUsers(remote, br)
        users = list
    } else {
        users = splitComma(allow)
    }
    self := getUserID()
    for _, u := range users {
        if u == "" || u == self { continue }
        tip, _, err := latestRemoteCheckpoint(remote, u, br)
        if err != nil { continue }
        last, _ := lastApplied(remote, u, br)
        if tip != "" && tip != last {
            // apply
            if err := applyRemoteCheckpoint(remote, u, tip); err != nil {
                fmt.Fprintf(os.Stderr, "auto-apply from %s failed: %v\n", u, err)
            } else {
                fmt.Printf("Auto-applied %s from %s/%s\n", short(tip), remote, u)
            }
        }
    }
    return nil
}

func listRemoteUsers(remote, branch string) ([]string, error) {
    // Enumerate refs under refs/remotes/<remote>/aigit/users/*/checkpoints/<branch>
    prefix := "refs/remotes/"+remote+"/aigit/users/"
    out, err := git("for-each-ref", "--format=%(refname)", prefix)
    if err != nil { return nil, err }
    var set = map[string]struct{}{}
    for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
        if line == "" { continue }
        // Expect refs/remotes/<remote>/aigit/users/<user>/checkpoints/<branch>
        parts := strings.Split(line, "/")
        if len(parts) < 7 { continue }
        user := parts[5]
        if parts[6] == "checkpoints" && len(parts) >= 8 && parts[7] == branch {
            set[user] = struct{}{}
        }
    }
    var users []string
    for u := range set { users = append(users, u) }
    return users, nil
}

func splitComma(s string) []string {
    var out []string
    for _, p := range strings.Split(s, ",") {
        p = strings.TrimSpace(p)
        if p != "" { out = append(out, p) }
    }
    return out
}

