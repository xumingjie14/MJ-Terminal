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
	return strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:size])))
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
