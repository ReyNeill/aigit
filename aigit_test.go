package main

import (
    "flag"
    "bytes"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
)

// --- Test helpers ---

func must(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}

func runGit(t *testing.T, dir string, args ...string) string {
    t.Helper()
    cmd := exec.Command("git", args...)
    cmd.Dir = dir
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("git %v: %v: %s", strings.Join(args, " "), err, string(out))
    }
    return strings.TrimSpace(string(out))
}

func withTempRepo(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    runGit(t, dir, "init", "-q")
    runGit(t, dir, "config", "user.name", "Test User")
    runGit(t, dir, "config", "user.email", "test@example.com")
    // initial commit
    os.WriteFile(filepath.Join(dir, "init.txt"), []byte("init\n"), 0o644)
    runGit(t, dir, "add", ".")
    runGit(t, dir, "commit", "-m", "init")
    return dir
}

func chdir(t *testing.T, dir string) func() {
    t.Helper()
    old, _ := os.Getwd()
    must(t, os.Chdir(dir))
    return func() { _ = os.Chdir(old) }
}

func captureOutput(t *testing.T, f func()) string {
    t.Helper()
    old := os.Stdout
    r, w, _ := os.Pipe()
    os.Stdout = w
    f()
    _ = w.Close()
    os.Stdout = old
    var buf bytes.Buffer
    io.Copy(&buf, r)
    return buf.String()
}

// --- Tests ---

func TestStatusCleanWorkspace(t *testing.T) {
    repo := withTempRepo(t)
    defer chdir(t, repo)()
    // switch cwd for our git() helper
    must(t, os.Chdir(repo))
    out := captureOutput(t, func() {
        must(t, doStatus())
    })
    if !strings.Contains(out, "nothing here yet, clean workspace") {
        t.Fatalf("expected clean workspace message, got:\n%s", out)
    }
}

func TestCheckpointListRestore(t *testing.T) {
    repo := withTempRepo(t)
    defer chdir(t, repo)()
    must(t, os.Chdir(repo))

    // Create and checkpoint v1
    os.WriteFile("foo.txt", []byte("v1\n"), 0o644)
    must(t, doCheckpoint("v1"))
    ref, err := ckRef()
    must(t, err)
    sha1 := runGit(t, repo, "rev-list", "-1", ref)

    // Change and checkpoint v2
    os.WriteFile("foo.txt", []byte("v2\n"), 0o644)
    must(t, doCheckpoint("v2"))

    // Restore from first checkpoint
    must(t, doRestore(sha1))
    data, _ := os.ReadFile("foo.txt")
    if string(data) != "v1\n" {
        t.Fatalf("restore failed, foo.txt=%q", string(data))
    }

    // List output should include v2 or v1 subject
    out := captureOutput(t, func() { must(t, doList(5, true)) })
    if !(strings.Contains(out, "v1") || strings.Contains(out, "v2")) {
        t.Fatalf("list missing expected subjects, got:\n%s", out)
    }
}

func TestDiffOneLinerIncludesUntracked(t *testing.T) {
    repo := withTempRepo(t)
    defer chdir(t, repo)()
    must(t, os.Chdir(repo))
    os.WriteFile("newfile.txt", []byte("hello\n"), 0o644)
    s, err := diffOneLiner()
    must(t, err)
    if !strings.Contains(s, "Add newfile.txt") {
        t.Fatalf("expected untracked file in summary, got: %q", s)
    }
}

func TestCheckpointDuringMerge(t *testing.T) {
    repo := withTempRepo(t)
    defer chdir(t, repo)()
    must(t, os.Chdir(repo))
    // Create a conflicting change on a branch
    os.WriteFile("file.txt", []byte("base\n"), 0o644)
    runGit(t, repo, "add", ".")
    runGit(t, repo, "commit", "-m", "base")

    runGit(t, repo, "checkout", "-b", "feature")
    os.WriteFile("file.txt", []byte("feature\n"), 0o644)
    runGit(t, repo, "commit", "-am", "feature change")

    runGit(t, repo, "checkout", "main")
    os.WriteFile("file.txt", []byte("main\n"), 0o644)
    runGit(t, repo, "commit", "-am", "main change")

    // Trigger a merge conflict
    _ = exec.Command("git", "merge", "feature").Run()
    // Now checkpoint during merge
    must(t, doCheckpoint("merge progress"))
    ref, err := ckRef()
    must(t, err)
    body := runGit(t, repo, "log", "-1", "--format=%B", ref)
    if !strings.Contains(body, "Aigit-Merge: yes") {
        t.Fatalf("expected merge metadata, got:\n%s", body)
    }
}

func TestRemoteSyncAndApply(t *testing.T) {
    repo := withTempRepo(t)
    defer chdir(t, repo)()
    must(t, os.Chdir(repo))

    // Create a checkpoint with identifiable content
    os.WriteFile("sync.txt", []byte("remote v1\n"), 0o644)
    must(t, doCheckpoint("remote v1"))

    // Create bare remote and add as origin
    bare := filepath.Join(repo, "remote.git")
    runGit(t, repo, "init", "--bare", bare)
    runGit(t, repo, "remote", "add", "origin", bare)

    // Push checkpoints
    must(t, pushCheckpoints("origin"))

    // Fetch checkpoints back and verify user appears
    must(t, fetchCheckpoints("origin"))
    br, err := currentBranch()
    must(t, err)
    users, err := listRemoteUsers("origin", br)
    must(t, err)
    if len(users) == 0 {
        t.Fatalf("expected at least one remote user")
    }
    // Change local file and apply from remote (self)
    os.WriteFile("sync.txt", []byte("local diverged\n"), 0o644)
    uid := getUserID()
    must(t, applyRemoteCheckpoint("origin", uid, ""))
    data, _ := os.ReadFile("sync.txt")
    if string(data) != "remote v1\n" {
        t.Fatalf("apply did not restore remote content, got %q", string(data))
    }
}

var noSummary = flag.Bool("no_summary", false, "skip AI summary tests")
var offline = flag.Bool("offline", false, "allow offline AI summary test using local fake")

func TestAISummaryCheckpoint(t *testing.T) {
    if *noSummary {
        t.Skip("-no_summary set; skipping AI summary tests")
    }
    repo := withTempRepo(t)
    defer chdir(t, repo)()
    must(t, os.Chdir(repo))
    // Avoid any background autostart interference
    t.Setenv("AIGIT_DISABLE_AUTOSTART", "1")

    // Make a change
    os.WriteFile("ai.txt", []byte("hello\n"), 0o644)

    // By default require network + real OpenRouter key
    if os.Getenv("OPENROUTER_API_KEY") == "" && !*offline {
        t.Fatalf("OPENROUTER_API_KEY not set; set it or run `go test -offline` or `-no_summary`")
    }

    err := maybeCheckpoint("ai", "x-ai/grok-code-fast-1")
    if err != nil && *offline {
        // Fallback to local fake if allowed
        t.Setenv("AIGIT_FAKE_AI_SUMMARY", "1")
        must(t, maybeCheckpoint("ai", "x-ai/grok-code-fast-1"))
    } else {
        must(t, err)
    }

    ref, err := ckRef()
    must(t, err)
    subj := runGit(t, repo, "log", "-1", "--format=%s", ref)
    if os.Getenv("AIGIT_FAKE_AI_SUMMARY") != "" {
        if !strings.HasPrefix(subj, "AI: ") {
            t.Fatalf("expected fake AI summary prefix, got: %q", subj)
        }
    } else {
        if strings.TrimSpace(subj) == "" {
            t.Fatalf("expected non-empty AI summary, got empty")
        }
    }
}
