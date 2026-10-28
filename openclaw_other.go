//go:build !windows

package main

// Gateway process hosting outside Windows: the gateway starts in
// its own process group and killTree kills the whole group — the
// unix counterpart of the Windows Job Object (see
// openclaw_windows.go). Used on macOS/Linux, including the local
// validation runs of the component lifecycle.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// ocTreeRSS sums VmRSS over the gateway process tree (rooted at the
// launcher pid) from /proc.
func ocTreeRSS(rootPid int) uint64 {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	ppid := map[int]int{}
	rss := map[int]uint64{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		if i := strings.LastIndex(s, ")"); i >= 0 && i+2 < len(s) {
			f := strings.Fields(s[i+2:])
			if len(f) > 1 {
				ppid[pid], _ = strconv.Atoi(f[1])
			}
		}
		if sb, err := os.ReadFile("/proc/" + e.Name() + "/status"); err == nil {
			for _, ln := range strings.Split(string(sb), "\n") {
				if strings.HasPrefix(ln, "VmRSS:") {
					var kb uint64
					fmt.Sscanf(ln, "VmRSS: %d kB", &kb)
					rss[pid] = kb * 1024
				}
			}
		}
	}
	var total uint64
	queue := []int{rootPid}
	seen := map[int]bool{}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		total += rss[p]
		for pid, pp := range ppid {
			if pp == p {
				queue = append(queue, pid)
			}
		}
	}
	return total
}
