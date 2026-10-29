//go:build windows

package main

// ---------- v0.50: global summon hotkey, Windows half ----------
//
// RegisterHotKey on a dedicated OS thread behind a message-only
// window -- the same shape as the tray (tray_windows.go), whose
// window/message plumbing this file reuses. The registration is
// process-wide and needs no hook DLL; the OS delivers WM_HOTKEY
// to our window. Unmodified function keys only (the setting is
// F8 / F9 / F7 / off), with MOD_NOREPEAT so holding the key down
// does not summon repeatedly.
//
// Exit hygiene: the daemon never explicitly unregisters at exit
// because every exit path ends in os.Exit -- and Windows drops a
// process's hotkey registrations when the process dies. Nothing
// is installed, hooked or left resident; the portable promise
// holds.

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const hotkeyPlatformSupported = true

var (
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
)

const (
	hkID          = 1
	hkReconfigMsg = 0x0411 // WM_USER + 0x11: us -> us, setting changed
	wmHotkey      = 0x0312
	modNoRepeat   = 0x4000
	errHotkeyBusy = 1409 // ERROR_HOTKEY_ALREADY_REGISTERED
)

var (
	hkStart  sync.Once
	hkHWND   atomic.Uintptr
	hkWantVK atomic.Uint32 // desired virtual-key code; 0 = off
	hkRegVK  uint32        // currently registered vk (hotkey thread only)
)

// hotkeyPlatformApply pushes the configured key toward the OS:
// store the wish, start the thread on first use, and wake it to
// re-register when it is already running.
func hotkeyPlatformApply() {
	key := settingSummonHotkey()
	if key == "off" {
		hkWantVK.Store(0)
		if h := hkHWND.Load(); h != 0 {
			procPostMessageW.Call(h, hkReconfigMsg, 0, 0)
		} else {
			hotkeySetResult(false, "")
		}
		return
	}
	hkWantVK.Store(hotkeyVK[key])
	hkStart.Do(func() { go hotkeyThread() })
	if h := hkHWND.Load(); h != 0 {
		procPostMessageW.Call(h, hkReconfigMsg, 0, 0)
	}
	// If the thread is still starting, its startup applies the
	// wish once the window exists (same contract as the tray).
}

// hkApplyRegistration reconciles the OS registration with
// hkWantVK. Hotkey thread only.
func hkApplyRegistration(hwnd uintptr) {
	want := hkWantVK.Load()
	if hkRegVK != 0 && hkRegVK != want {
		procUnregisterHotKey.Call(hwnd, hkID)
		hkRegVK = 0
	}
	if want == 0 {
		hotkeySetResult(false, "")
		return
	}
	if hkRegVK == want {
		return // already registered as wished
	}
	r, _, errno := procRegisterHotKey.Call(hwnd, hkID, modNoRepeat, uintptr(want))
	if r == 0 {
		hkRegVK = 0
		if errno != nil && errno.(syscall.Errno) == errHotkeyBusy {
			hotkeySetResult(false, "该键已被其他程序占用（游戏、录屏或输入法常见），请改用备选键或关闭")
		} else {
			hotkeySetResult(false, "系统拒绝注册（错误 "+errnoText(errno)+"）")
		}
		return
	}
	hkRegVK = want
	hotkeySetResult(true, "")
}

func errnoText(e error) string {
	if e == nil {
		return "unknown"
	}
	return e.Error()
}

// The window procedure is created lazily inside hotkeyThread for
// the same reason as the tray's: a package-level callback whose
// reference chain reaches the summon action (recall -> shell ->
// state machine) would form an initialization cycle.
var hkWndProcPtr uintptr

func makeHKWndProc() uintptr {
	return syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		switch uint32(msg) {
		case wmHotkey:
			if wParam == hkID {
				go hotkeySummon()
			}
			return 0
		case hkReconfigMsg:
			hkApplyRegistration(hwnd)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	})
}

func hotkeyThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("MJTerminalHotkeyWnd")
	if hkWndProcPtr == 0 {
		hkWndProcPtr = makeHKWndProc()
	}
	wc := trayWndClassEx{
		Style:         0,
		LpfnWndProc:   hkWndProcPtr,
		HInstance:     hInst,
		LpszClassName: className,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	const hwndMessage = ^uintptr(2) // (HWND)-3
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0, hwndMessage, 0, hInst, 0)
	if hwnd == 0 {
		hotkeySetResult(false, "召唤热键窗口创建失败")
		return
	}
	hkHWND.Store(hwnd)
	hkApplyRegistration(hwnd)

	var m trayMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
