package main

// Windows platform layer for app isolation (v0.39): component
// detection (service/driver state via sc.exe, Start.exe file
// version via version.dll) and the one-UAC elevation bridge
// (ShellExecuteEx with the "runas" verb) used to run the daemon's
// own narrow helper modes (see sandbox.go sbieHelperExitCode).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procShellExecuteExW      = shell32.NewProc("ShellExecuteExW")
	procWaitForSingleObject  = kernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess   = kernel32.NewProc("GetExitCodeProcess")
	versionDll               = syscall.NewLazyDLL("version.dll")
	procGetFileVersionInfoSz = versionDll.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo   = versionDll.NewProc("GetFileVersionInfoW")
	procVerQueryValue        = versionDll.NewProc("VerQueryValueW")
)

// errSbieUACCanceled marks the user pressing "No" on the UAC
// prompt, so callers can fail fast with an honest message instead
// of polling for a state that will never arrive.
var errSbieUACCanceled = errors.New("管理员确认被取消了")

var scStateRe = regexp.MustCompile(`STATE\s*:\s*(\d+)`)

var scStateNames = map[string]string{
	"1": "STOPPED", "2": "START_PENDING", "3": "STOP_PENDING",
	"4": "RUNNING", "5": "CONTINUE_PENDING", "6": "PAUSE_PENDING", "7": "PAUSED",
}

// scQueryState returns the normalized state of a service/driver
// ("RUNNING", "STOPPED", ...) or "" when it does not exist.
func scQueryState(name string) string {
	sc := filepath.Join(os.Getenv("SystemRoot"), "System32", "sc.exe")
	cmd := exec.Command(sc, "query", name)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	m := scStateRe.FindSubmatch(out)
	if m == nil {
		return ""
	}
	if s, ok := scStateNames[string(m[1])]; ok {
		return s
	}
	return ""
}

// fileVersionString reads the FileVersion string resource of an
// exe ("" when unavailable). Trailing ".0" is trimmed so Sandboxie
// reports "1.18.8" rather than "1.18.8.0".
func fileVersionString(path string) string {
	p16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var handle uint32
	size, _, _ := procGetFileVersionInfoSz.Call(uintptr(unsafe.Pointer(p16)), uintptr(unsafe.Pointer(&handle)))
	if size == 0 {
		return ""
	}
	data := make([]byte, size)
	r, _, _ := procGetFileVersionInfo.Call(uintptr(unsafe.Pointer(p16)), 0, size, uintptr(unsafe.Pointer(&data[0])))
	if r == 0 {
		return ""
	}
	langCP := uint32(0x040904B0) // en-US + Unicode, the usual default
	sub, _ := syscall.UTF16PtrFromString(`\VarFileInfo\Translation`)
	var transPtr uintptr
	var transLen uint32
	r, _, _ = procVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(sub)),
		uintptr(unsafe.Pointer(&transPtr)), uintptr(unsafe.Pointer(&transLen)))
	if r != 0 && transLen >= 4 && transPtr != 0 {
		t := (*[2]uint16)(unsafe.Pointer(transPtr))
		langCP = uint32(t[0]) | uint32(t[1])<<16
	}
	q, _ := syscall.UTF16PtrFromString(fmt.Sprintf(`\StringFileInfo\%08x\FileVersion`, langCP))
	var valPtr uintptr
	var valLen uint32
	r, _, _ = procVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(q)),
		uintptr(unsafe.Pointer(&valPtr)), uintptr(unsafe.Pointer(&valLen)))
	if r == 0 || valLen == 0 || valPtr == 0 {
		return ""
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(valPtr)), valLen)
	v := strings.TrimSpace(syscall.UTF16ToString(u16))
	if strings.Count(v, ".") == 3 && strings.HasSuffix(v, ".0") {
		v = v[:len(v)-2]
	}
	return v
}

// sbiePlatformStatus reports (serviceState, driverState, version).
func sbiePlatformStatus() (string, string, string) {
	ver := ""
	if se := sbieStartExePath(); se != "" {
		ver = fileVersionString(se)
	}
	return scQueryState("SbieSvc"), scQueryState("SbieDrv"), ver
}

// ---------- elevation bridge ----------

type shellExecuteInfoW struct {
	cbSize         uint32
	fMask          uint32
	hwnd           uintptr
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       uintptr
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      uintptr
	dwHotKey       uint32
	hIconOrMonitor uintptr
	hProcess       uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	swShowNormal          = 1
	waitInfinite          = 0xFFFFFFFF
	errorCancelled        = 1223
)

// sbieRunElevated runs exe elevated (one UAC prompt) with the
// given args and waits for it to finish, returning its exit code.
// Arguments must not contain double quotes.
func sbieRunElevated(exe string, args ...string) (uint32, error) {
	for _, a := range args {
		if strings.Contains(a, `"`) {
			return 0, fmt.Errorf("参数里不能带引号：%s", a)
		}
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	var params *uint16
	if len(args) > 0 {
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = `"` + a + `"`
		}
		params, err = syscall.UTF16PtrFromString(strings.Join(quoted, " "))
		if err != nil {
			return 0, err
		}
	}
	sei := shellExecuteInfoW{
		fMask:  seeMaskNoCloseProcess,
		lpVerb: verb,
		lpFile: file,
		nShow:  swShowNormal,
	}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	if params != nil {
		sei.lpParameters = params
	}
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r == 0 {
		if callErr != nil && callErr.(syscall.Errno) == errorCancelled {
			return 0, errSbieUACCanceled
		}
		return 0, fmt.Errorf("拉起管理员进程失败：%v", callErr)
	}
	if sei.hProcess == 0 {
		return 0, fmt.Errorf("拉起管理员进程失败：没有进程句柄")
	}
	defer procCloseHandle.Call(sei.hProcess)
	// Installs take seconds to a couple of minutes; the cap only
	// guards a wedged child. INFINITE is avoided on purpose.
	const waitMs = 20 * 60 * 1000
	wr, _, _ := procWaitForSingleObject.Call(sei.hProcess, waitMs)
	if wr != 0 {
		return 0, fmt.Errorf("等管理员进程结束超时")
	}
	var code uint32
	procGetExitCodeProcess.Call(sei.hProcess, uintptr(unsafe.Pointer(&code)))
	return code, nil
}

// sbieProcImagePath reuses the window manager's process-image
// query (QueryFullProcessImageNameW).
func sbieProcImagePath(pid uint32) string { return processImagePath(pid) }

// sbieKillPid force-ends one process via the stock taskkill tool.
func sbieKillPid(pid int) error {
	cmd := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/F")
	hideConsole(cmd)
	return cmd.Run()
}
