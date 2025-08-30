//go:build windows

package main

import "os/exec"

func detach(cmd *exec.Cmd) {
    // no-op on Windows
}

func isAlive(pid int) bool {
    // Best effort on Windows: assume alive to avoid duplicate autostarts.
    // Users can remove .git/aigit/watch.pid to force a restart.
    return true
}
