//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideConsole starts a console child process (the sing-box core,
// cmd.exe) without giving it a visible console window. The daemon is
// a GUI process with no console of its own, so without this Windows
// allocates a fresh black console window for every console child --
// most visibly the sing-box core window that popped up on the
// desktop whenever the node started (e.g. when opening the browser).
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
