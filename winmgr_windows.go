//go:build windows

package main

// Windows implementation of the unified window management hooks
// declared in winmgr.go: finding the top-level window a browser
// launch produced, bringing it to the front, and stamping the shared
// AppUserModelID on it. Pure stdlib (syscall + LazyDLL), like the rest
// of the project -- no x/sys dependency.

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	procEnumWindows                 = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                    = user32.NewProc("IsWindow")
	procIsWindowVisible             = user32.NewProc("IsWindowVisible")
	procIsIconic                    = user32.NewProc("IsIconic")
	procGetWindowTextW              = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW        = user32.NewProc("GetWindowTextLengthW")
	procGetClassNameW               = user32.NewProc("GetClassNameW")
	procGetWindow                   = user32.NewProc("GetWindow")
	procShowWindow                  = user32.NewProc("ShowWindow")
	procGetWindowRect               = user32.NewProc("GetWindowRect")
	procIsZoomed                    = user32.NewProc("IsZoomed")
	procSetWindowPos                = user32.NewProc("SetWindowPos")
	procSetForegroundWindow         = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow         = user32.NewProc("GetForegroundWindow")
	procBringWindowToTop            = user32.NewProc("BringWindowToTop")
	procAttachThreadInput           = user32.NewProc("AttachThreadInput")
	procGetCurrentThreadId          = kernel32.NewProc("GetCurrentThreadId")
	procOpenProcess                 = kernel32.NewProc("OpenProcess")
	procCloseHandle                 = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW  = kernel32.NewProc("QueryFullProcessImageNameW")
	procSHGetPropertyStoreForWindow = shell32.NewProc("SHGetPropertyStoreForWindow")
	procCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	procCoUninitialize              = ole32.NewProc("CoUninitialize")
)

// EnumWindows takes a C callback; syscall.NewCallback slots are a
// scarce resource, so ONE callback is created for the process and the
// current visitor is handed over through a package variable, with all
// enumerations serialized on enumMu.
var (
	enumMu sync.Mutex
	enumFn func(hwnd uintptr) bool
)

var enumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	if enumFn == nil || enumFn(hwnd) {
		return 1 // continue
	}
	return 0 // stop
})

func enumTopLevel(fn func(hwnd uintptr) bool) {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumFn = fn
	defer func() { enumFn = nil }()
	procEnumWindows.Call(enumCallback, 0)
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// processImageBase returns the lowercased executable base name
// ("chrome.exe") owning pid, "" when it cannot be queried (the
// process is gone or protected -- either way not ours to manage).
func processImageBase(pid uint32) string {
	full := processImagePath(pid)
	if full == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(full))
}

type winCand struct {
	hwnd uint64
	pid  uint32
}

// browserWindows lists the visible top-level windows whose owning
// process image matches the browser executable (chrome.exe/msedge.exe),
// regardless of which profile instance they belong to.
func browserWindows(browserPath string) []winCand {
	base := strings.ToLower(filepath.Base(browserPath))
	var out []winCand
	imgCache := map[uint32]string{}
	enumTopLevel(func(hwnd uintptr) bool {
		if r, _, _ := procIsWindowVisible.Call(hwnd); r == 0 {
			return true
		}
		pid := windowPID(hwnd)
		img, ok := imgCache[pid]
		if !ok {
			img = processImageBase(pid)
			imgCache[pid] = img
		}
		if img == base {
			out = append(out, winCand{hwnd: uint64(hwnd), pid: pid})
		}
		return true
	})
	return out
}

// ---------- v0.36: full inventory collection ----------
//
// The patrol no longer filters at collection time. These functions
// gather the raw facts for EVERY process and EVERY top-level
// window; all ownership decisions happen in the platform-neutral
// classifier (owncheck.go), which is also what keeps the host's own
// windows safe: they are looked at, classified as not ours, and
// left alone.

var (
	ntdll = syscall.NewLazyDLL("ntdll.dll")

	procNtQueryInformationProcess = ntdll.NewProc("NtQueryInformationProcess")
	procReadProcessMemory         = kernel32.NewProc("ReadProcessMemory")
	procIsWow64Process            = kernel32.NewProc("IsWow64Process")
	procCreateToolhelp32Snapshot  = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW           = kernel32.NewProc("Process32FirstW")
	procProcess32NextW            = kernel32.NewProc("Process32NextW")
)

// processImagePath returns the FULL image path owning pid (""
// when it cannot be queried: the process is gone or protected --
// either way its facts stay empty and only tree descent can claim
// its windows).
func processImagePath(pid uint32) string {
	const processQueryLimitedInformation = 0x1000
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

type processEntry32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

// browserImageNames are the only processes whose command line the
// snapshot collects: the profile fingerprint lives there, and
// reading every process's command line would be both slower and
// more intrusive than the patrol needs to be.
var browserImageNames = map[string]bool{
	"chrome.exe": true, "msedge.exe": true, "firefox.exe": true,
}

// platformProcessSnapshot inventories all processes: pid, parent
// pid, full image path, and -- for browser images only -- the
// command line (the isolated-profile fingerprint).
func platformProcessSnapshot() []procFact {
	const th32csSnapProcess = 0x00000002
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snap == 0 || snap == uintptr(^uint(0)) {
		return nil
	}
	defer procCloseHandle.Call(snap)
	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []procFact
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for r != 0 {
		f := procFact{PID: entry.ProcessID, PPID: entry.ParentProcessID}
		f.ImagePath = processImagePath(entry.ProcessID)
		base := strings.ToLower(filepath.Base(f.ImagePath))
		if f.ImagePath == "" {
			base = strings.ToLower(syscall.UTF16ToString(entry.ExeFile[:]))
		}
		if browserImageNames[base] {
			f.CmdLine = processCmdLine(entry.ProcessID)
		}
		out = append(out, f)
		entry.Size = uint32(unsafe.Sizeof(entry))
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return out
}

// platformAllTopWindows enumerates every top-level window with the
// facts decideClaim needs: owning pid, class, title, visibility,
// owner and size. Unfiltered by design (see the section comment).
func platformAllTopWindows() []windowFact {
	var out []windowFact
	enumTopLevel(func(hwnd uintptr) bool {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		owner, _, _ := procGetWindow.Call(hwnd, 4) // GW_OWNER
		var cls [256]uint16
		n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), uintptr(len(cls)))
		class := ""
		if n > 0 {
			class = syscall.UTF16ToString(cls[:n])
		}
		title, _ := platformWindowTitle(uint64(hwnd))
		var w, h int32
		if l, t, r, b, ok := platformWindowRect(uint64(hwnd)); ok {
			w, h = r-l, b-t
		}
		out = append(out, windowFact{
			HWND: uint64(hwnd), PID: windowPID(hwnd), Class: class, Title: title,
			Visible: visible != 0, Owned: owner != 0, W: w, H: h,
		})
		return true
	})
	return out
}

// ---------- process command line via the PEB ----------
//
// There is no supported Win32 API for another process's command
// line; the standard route is NtQueryInformationProcess ->
// PEB -> RTL_USER_PROCESS_PARAMETERS.CommandLine, read with
// ReadProcessMemory. Pure stdlib syscalls, fail-soft throughout:
// any error yields "", which simply means the profile rule cannot
// fire for that process this tick.
//
// Offsets: a 64-bit reader uses the 64-bit PEB layout for native
// targets and the 32-bit layout for WOW64 targets; a 32-bit reader
// (the 386 build) only ever sees the 32-bit layout.

const daemonIs64Bit = unsafe.Sizeof(uintptr(0)) == 8

func readProcessMem(h uintptr, addr uintptr, buf []byte) bool {
	if addr == 0 || len(buf) == 0 {
		return false
	}
	var read uintptr
	r, _, _ := procReadProcessMemory.Call(h, addr,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&read)))
	return r != 0 && read == uintptr(len(buf))
}

func processCmdLine(pid uint32) string {
	const processQueryInformation = 0x0400
	const processVMRead = 0x0010
	h, _, _ := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)

	wow64 := false
	if daemonIs64Bit {
		var w uintptr
		if r, _, _ := procIsWow64Process.Call(h, uintptr(unsafe.Pointer(&w))); r != 0 {
			wow64 = w != 0
		}
	}

	var pebAddr uintptr
	if daemonIs64Bit && wow64 {
		// ProcessWow64Information (26) returns the 32-bit PEB.
		var p uintptr
		st, _, _ := procNtQueryInformationProcess.Call(h, 26,
			uintptr(unsafe.Pointer(&p)), unsafe.Sizeof(p), 0)
		if st != 0 {
			return ""
		}
		pebAddr = p
	} else {
		// ProcessBasicInformation (0): PEB address is the second
		// pointer-sized field.
		var pbi [6]uintptr
		st, _, _ := procNtQueryInformationProcess.Call(h, 0,
			uintptr(unsafe.Pointer(&pbi[0])), unsafe.Sizeof(pbi), 0)
		if st != 0 {
			return ""
		}
		pebAddr = pbi[1]
	}
	if pebAddr == 0 {
		return ""
	}

	use32 := !daemonIs64Bit || wow64
	var paramsAddr uintptr
	var cmdOff uintptr // offset of CommandLine within the parameters block
	if use32 {
		buf := make([]byte, 0x14)
		if !readProcessMem(h, pebAddr, buf) {
			return ""
		}
		paramsAddr = uintptr(uint32(buf[0x10]) | uint32(buf[0x11])<<8 | uint32(buf[0x12])<<16 | uint32(buf[0x13])<<24)
		cmdOff = 0x40
	} else {
		buf := make([]byte, 0x28)
		if !readProcessMem(h, pebAddr, buf) {
			return ""
		}
		paramsAddr = uintptr(uint64(buf[0x20]) | uint64(buf[0x21])<<8 | uint64(buf[0x22])<<16 | uint64(buf[0x23])<<24 |
			uint64(buf[0x24])<<32 | uint64(buf[0x25])<<40 | uint64(buf[0x26])<<48 | uint64(buf[0x27])<<56)
		cmdOff = 0x70
	}
	if paramsAddr == 0 {
		return ""
	}

	// UNICODE_STRING at cmdOff: Length u16, then the buffer
	// pointer (u32 at +4 in the 32-bit layout, u64 at +8 in the
	// 64-bit layout).
	hdr := make([]byte, cmdOff+0x18)
	if !readProcessMem(h, paramsAddr, hdr) {
		return ""
	}
	co := int(cmdOff)
	length := int(hdr[co]) | int(hdr[co+1])<<8
	var bufAddr uintptr
	if use32 {
		bufAddr = uintptr(uint32(hdr[co+4]) | uint32(hdr[co+5])<<8 | uint32(hdr[co+6])<<16 | uint32(hdr[co+7])<<24)
	} else {
		var v uint64
		for i := 0; i < 8; i++ {
			v |= uint64(hdr[co+8+i]) << (8 * i)
		}
		bufAddr = uintptr(v)
	}
	if length <= 0 || bufAddr == 0 {
		return ""
	}
	if length > 16384 {
		length = 16384
	}
	raw := make([]byte, length)
	if !readProcessMem(h, bufAddr, raw) {
		return ""
	}
	u16 := make([]uint16, length/2)
	for i := range u16 {
		u16[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return syscall.UTF16ToString(u16)
}

func platformSnapshotWindows(browserPath string) map[uint64]bool {
	out := map[uint64]bool{}
	for _, c := range browserWindows(browserPath) {
		out[c.hwnd] = true
	}
	return out
}

func platformClaimWindow(browserPath string, before, claimed map[uint64]bool) (uint64, uint32, bool) {
	for _, c := range browserWindows(browserPath) {
		if !before[c.hwnd] && !claimed[c.hwnd] {
			return c.hwnd, c.pid, true
		}
	}
	return 0, 0, false
}

// ---------- v0.20: one-shot landing placement ----------

type wmRect struct {
	Left, Top, Right, Bottom int32
}

// platformWindowRect returns the window's screen rectangle.
func platformWindowRect(hwnd uint64) (int32, int32, int32, int32, bool) {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return 0, 0, 0, 0, false
	}
	var rc wmRect
	r, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&rc)))
	if r == 0 {
		return 0, 0, 0, 0, false
	}
	return rc.Left, rc.Top, rc.Right, rc.Bottom, true
}

// platformWindowZoomed reports whether the window is maximized.
// v0.23 reads this only for the desktop shell window, at landing
// time, to decide small-window (phone-width) mode.
func platformWindowZoomed(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	z, _, _ := procIsZoomed.Call(h)
	return z != 0
}

// platformPlaceWindow moves/resizes the window to the given screen
// rectangle. A maximized window is restored first (SetWindowPos on a
// zoomed window only edits its restore geometry, which is not what
// landing means). Z-order and activation are left alone: the window
// has just opened and is already the foreground one; landing only
// fixes WHERE it opened. Called exactly once per registration --
// later user moves/maximizes are never corrected.
func platformPlaceWindow(hwnd uint64, l, t, r, b int32) bool {
	h := uintptr(hwnd)
	if x, _, _ := procIsWindow.Call(h); x == 0 {
		return false
	}
	if z, _, _ := procIsZoomed.Call(h); z != 0 {
		procShowWindow.Call(h, 9) // SW_RESTORE
	}
	const swpNoZOrder = 0x0004
	const swpNoActivate = 0x0010
	res, _, _ := procSetWindowPos.Call(h, 0,
		uintptr(l), uintptr(t), uintptr(r-l), uintptr(b-t),
		swpNoZOrder|swpNoActivate)
	return res != 0
}

// platformWindowTitle reports the window's current title and whether
// the window still exists. ("", true) = alive but no title yet.
func platformWindowTitle(hwnd uint64) (string, bool) {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return "", false
	}
	n, _, _ := procGetWindowTextLengthW.Call(h)
	if n == 0 {
		return "", true
	}
	buf := make([]uint16, n+2)
	procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf), true
}

// platformFocusWindow restores (if minimized) and foregrounds the
// window. Windows refuses SetForegroundWindow from a background
// process unless it shares the input queue with the current
// foreground window, so AttachThreadInput is used as the standard
// workaround before asking.
func platformFocusWindow(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	if r, _, _ := procIsIconic.Call(h); r != 0 {
		procShowWindow.Call(h, 9) // SW_RESTORE
	} else {
		procShowWindow.Call(h, 5) // SW_SHOW
	}
	cur, _, _ := procGetCurrentThreadId.Call()
	fg, _, _ := procGetForegroundWindow.Call()
	var fgThread, targetThread uintptr
	if fg != 0 {
		fgThread, _, _ = procGetWindowThreadProcessId.Call(fg, 0)
	}
	targetThread, _, _ = procGetWindowThreadProcessId.Call(h, 0)
	type pair struct{ a, b uintptr }
	var attached []pair
	attach := func(other uintptr) {
		if other != 0 && other != cur {
			procAttachThreadInput.Call(cur, other, 1)
			attached = append(attached, pair{cur, other})
		}
	}
	attach(fgThread)
	attach(targetThread)
	r, _, _ := procSetForegroundWindow.Call(h)
	procBringWindowToTop.Call(h)
	for _, p := range attached {
		procAttachThreadInput.Call(p.a, p.b, 0)
	}
	if r != 0 {
		return true
	}
	now, _, _ := procGetForegroundWindow.Call()
	return now == h
}

// ---------- v0.37: window etiquette platform ops (winset.go) ----------

// platformShowAppWindow shows a window that exists but is not
// visible (SW_SHOW), or restores it when it is minimized
// (SW_RESTORE). v0.41: the reveal for a boxed app's born-hidden
// main window. platformFocusWindow also ends up showing, but the
// claim path logs and verifies the reveal as its own step,
// before any focus work happens.
func platformShowAppWindow(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	if r, _, _ := procIsIconic.Call(h); r != 0 {
		procShowWindow.Call(h, 9) // SW_RESTORE
	} else {
		procShowWindow.Call(h, 5) // SW_SHOW
	}
	return true
}

// platformWindowExists reports whether the handle is still a window.
func platformWindowExists(hwnd uint64) bool {
	r, _, _ := procIsWindow.Call(uintptr(hwnd))
	return r != 0
}

// platformWindowIconic reports whether the window is minimized.
func platformWindowIconic(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	r, _, _ := procIsIconic.Call(h)
	return r != 0
}

// platformWindowVisible reports the window's WS_VISIBLE state. Note
// a minimized window keeps this flag -- iconic is the minimize test.
func platformWindowVisible(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	r, _, _ := procIsWindowVisible.Call(h)
	return r != 0
}

// platformWindowOwned reports whether the window has an owner
// (dialogs, palettes, file pickers): the birth-placement exemption.
func platformWindowOwned(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	owner, _, _ := procGetWindow.Call(h, 4) // GW_OWNER
	return owner != 0
}

// platformForegroundHWND returns the current foreground window.
func platformForegroundHWND() uint64 {
	fg, _, _ := procGetForegroundWindow.Call()
	return uint64(fg)
}

// platformHideWindow hides the window outright (SW_HIDE): it leaves
// the screen AND the host taskbar, which is what a group hide means.
func platformHideWindow(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	procShowWindow.Call(h, 0) // SW_HIDE
	return true
}

// platformShowBackWindow undoes platformHideWindow with
// SW_SHOWNOACTIVATE: the window returns in the size/position/show
// state it had when hidden, without taking activation.
func platformShowBackWindow(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	procShowWindow.Call(h, 8) // SW_SHOWNOACTIVATE
	return true
}

// platformCloseWindow asks a window to close itself with a plain
// WM_CLOSE message -- the same request its own X button sends.
// No hooks, no injection: the window's owner remains free to
// refuse or prompt. Used by the desktop page's red button when
// the user has switched the close-to-tray retreat off (v0.38).
func platformCloseWindow(hwnd uint64) bool {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return false
	}
	r, _, _ := procPostMessageW.Call(h, 0x0010 /* WM_CLOSE */, 0, 0)
	return r != 0
}

// platformRestoreShellWindow un-minimizes the desktop view (the
// user asked for the set back, e.g. via a Stage Manager click).
func platformRestoreShellWindow(hwnd uint64) {
	h := uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(h); r == 0 {
		return
	}
	if r, _, _ := procIsIconic.Call(h); r != 0 {
		procShowWindow.Call(h, 9) // SW_RESTORE
	}
}

// platformRaiseBackdrop slides the desktop view directly underneath
// the newborn window in z-order: first the shell is lifted to the
// top of the stack, then the new window is inserted directly above
// it. Both moves are NOACTIVATE -- z-order only, focus is never
// taken; where the user's focus sits afterwards is the system's
// natural result of their own click.
func platformRaiseBackdrop(hwnd uint64, shell uint64) bool {
	sh, nw := uintptr(shell), uintptr(hwnd)
	if r, _, _ := procIsWindow.Call(sh); r == 0 {
		return false
	}
	if r, _, _ := procIsWindow.Call(nw); r == 0 {
		return false
	}
	const swpNoSize = 0x0001
	const swpNoMove = 0x0002
	const swpNoActivate = 0x0010
	flags := uintptr(swpNoSize | swpNoMove | swpNoActivate)
	procSetWindowPos.Call(sh, 0 /* HWND_TOP */, 0, 0, 0, 0, flags)
	r, _, _ := procSetWindowPos.Call(nw, sh, 0, 0, 0, 0, flags)
	return r != 0
}

// ---------- 丙: shared AppUserModelID via IPropertyStore ----------

type wmGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// IID_IPropertyStore {886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99}
var iidIPropertyStore = wmGUID{0x886D8EEB, 0x8CF2, 0x4446,
	[8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}

// PKEY_AppUserModel_ID: fmtid {9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3}, pid 5.
var fmtidAppUserModelID = wmGUID{0x9F4C2855, 0x9F79, 0x4B39,
	[8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}}

type wmPropertyKey struct {
	fmtid wmGUID
	pid   uint32
}

// PROPVARIANT holding a VT_LPWSTR. The trailing field pads the struct
// to the real PROPVARIANT size (24 bytes on amd64, 16 on 386) so the
// native side never reads past the allocation.
type wmPropVariant struct {
	vt         uint16
	wReserved1 uint16
	wReserved2 uint16
	wReserved3 uint16
	pwszVal    *uint16
	_          uintptr
}

// platformSetAppID stamps unifiedAppID on the window so the taskbar
// groups it with the rest of the MJ windows. Best effort by design:
// any failure is only logged -- unified switching （乙） must not
// depend on it. Runs the COM calls on a locked OS thread, as COM
// apartment rules require.
func platformSetAppID(hwnd uint64) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitMultithreaded = 0x0
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	// S_OK (0) / S_FALSE (1): this call holds a COM reference.
	// RPC_E_CHANGED_MODE (0x80010106): thread already initialized with
	// another model -- proceed without a matching uninitialize.
	if hr == 0 || hr == 1 {
		defer procCoUninitialize.Call()
	}
	var store uintptr
	hr, _, _ = procSHGetPropertyStoreForWindow.Call(uintptr(hwnd),
		uintptr(unsafe.Pointer(&iidIPropertyStore)), uintptr(unsafe.Pointer(&store)))
	if hr != 0 || store == 0 {
		extWinNote("stamp AppID: SHGetPropertyStoreForWindow hr=%#x hwnd=%d", hr, hwnd)
		return
	}
	vtbl := *(*uintptr)(unsafe.Pointer(store))
	method := func(idx int) uintptr {
		return *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	}
	defer syscall.SyscallN(method(2), store) // IUnknown::Release
	id16, err := syscall.UTF16PtrFromString(unifiedAppID)
	if err != nil {
		return
	}
	key := wmPropertyKey{fmtid: fmtidAppUserModelID, pid: 5}
	pv := wmPropVariant{vt: 31 /* VT_LPWSTR */, pwszVal: id16}
	hr, _, _ = syscall.SyscallN(method(6), store, // IPropertyStore::SetValue
		uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&pv)))
	if hr != 0 {
		extWinNote("stamp AppID: SetValue hr=%#x hwnd=%d", hr, hwnd)
		return
	}
	syscall.SyscallN(method(7), store) // IPropertyStore::Commit
}
