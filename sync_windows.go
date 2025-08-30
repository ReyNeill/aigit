//go:build windows

package main

import "os/exec"

func detach(cmd *exec.Cmd) {
    // no-op on Windows
}

