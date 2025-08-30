//go:build !windows

package main

import (
    "os"
    "os/exec"
    "syscall"
)

func detach(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func isAlive(pid int) bool {
    p, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    // Signal 0 checks existence without killing
    if err := p.Signal(syscall.Signal(0)); err != nil {
        return false
    }
    return true
}
