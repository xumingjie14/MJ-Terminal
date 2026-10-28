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

// ocTreeRSS sums the working set over the gateway process tree:
// snapshot all processes, walk the parent chain down from the
// gateway root pid, and add each process's working set.
func ocTreeRSS(rootPid int) uint64 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)
	ppid := map[uint32]uint32{}
	var pids []uint32
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return 0
	}
	for {
		ppid[pe.ProcessID] = pe.ParentProcessID
		pids = append(pids, pe.ProcessID)
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	inTree := map[uint32]bool{uint32(rootPid): true}
	for changed := true; changed; {
		changed = false
		for _, pid := range pids {
			if !inTree[pid] && inTree[ppid[pid]] {
				inTree[pid] = true
				changed = true
			}
		}
	}
	var total uint64
	for pid := range inTree {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue
		}
		var counters ocProcMemCounters
		counters.Size = uint32(unsafe.Sizeof(counters))
		r1, _, _ := procGetProcessMemoryInfo.Call(uintptr(h),
			uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
		if r1 != 0 {
			total += uint64(counters.WorkingSetSize)
		}
		_ = windows.CloseHandle(h)
	}
	return total
}

type ocProcMemCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var procGetProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
