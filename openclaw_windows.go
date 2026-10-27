//go:build windows

package main

// Gateway process hosting on Windows: the gateway is assigned to a
// Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE right after it
// starts. Closing the job handle (on stop, or implicitly when the
// daemon itself exits) terminates the WHOLE process tree — node
// spawns children, and killing only the launcher process would leak
// them (the Windows validation DESIGN.md called this out as the
// daemon-side requirement). If the job assignment fails, killTree
// falls back to killing the direct process only.

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ocProc struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	job     windows.Handle
}

func ocProcStart(cmd *exec.Cmd) (*ocProc, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &ocProc{cmd: cmd, done: make(chan struct{})}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		logf("openclaw", "warn", "小龙虾进程组（Job Object）创建失败，停止时只能杀主进程：%v", err)
	} else {
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(job,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			logf("openclaw", "warn", "小龙虾进程组（Job Object）设置失败：%v", err)
			_ = windows.CloseHandle(job)
		} else if ph, err := windows.OpenProcess(
			windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
			false, uint32(cmd.Process.Pid)); err != nil {
			logf("openclaw", "warn", "小龙虾进程组（Job Object）打开进程失败：%v", err)
			_ = windows.CloseHandle(job)
		} else {
			if err := windows.AssignProcessToJobObject(job, ph); err != nil {
				logf("openclaw", "warn", "小龙虾进程组（Job Object）加入失败，停止时只能杀主进程：%v", err)
				_ = windows.CloseHandle(job)
			} else {
				p.job = job
			}
			_ = windows.CloseHandle(ph)
		}
	}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *ocProc) killTree() {
	if p.job != 0 {
		// KILL_ON_JOB_CLOSE: closing the last job handle kills the
		// entire tree, gateway children included.
		_ = windows.CloseHandle(p.job)
		p.job = 0
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}
