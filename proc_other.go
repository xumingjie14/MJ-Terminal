//go:build !windows

package main

import "os/exec"

// hideConsole is a no-op outside Windows (no console windows there).
func hideConsole(cmd *exec.Cmd) {}
