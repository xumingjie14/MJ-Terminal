//go:build !windows

package main

// Gateway process hosting outside Windows: the gateway starts in
// its own process group and killTree kills the whole group — the
// unix counterpart of the Windows Job Object (see
// openclaw_windows.go). Used on macOS/Linux, including the local
// validation runs of the component lifecycle.

import (
	"os/exec"
	"syscall"
)

type ocProc struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
}

func ocProcStart(cmd *exec.Cmd) (*ocProc, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &ocProc{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *ocProc) killTree() {
	if p.cmd.Process != nil {
		// Negative pid: the whole process group.
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		_ = p.cmd.Process.Kill()
	}
}
