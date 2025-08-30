//go:build !windows

package main

import (
    "os/exec"
    "syscall"
)

func detach(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

