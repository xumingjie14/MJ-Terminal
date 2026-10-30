//go:build windows

package main

// ---------- global hotkey (boss key), Windows half ----------
//
// RegisterHotKey on a dedicated OS thread behind a message-only
// window -- the same shape as the tray (tray_windows.go), whose
// window/message plumbing this file reuses. The registration is
// process-wide and needs no hook DLL; the OS delivers WM_HOTKEY
// to our window. v0.52: the registration carries the combo's
// Ctrl/Alt/Shift modifiers (hotkey.go), still with
// MOD_NOREPEAT so holding the key down does not fire
// repeatedly. A delivered press runs bossKeyPress
// SYNCHRONOUSLY in the window procedure -- the hide / raise is
// a handful of Win32 calls and must be on screen within the
// press, not after a goroutine hop (only the dead-view
// creation fallback leaves the thread, inside bosskey.go).
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
	hkStart    sync.Once
	hkHWND     atomic.Uintptr
	hkWantMods atomic.Uint32 // desired modifier bits; with hkWantVK
	hkWantVK   atomic.Uint32 // desired virtual-key code; 0 = off
	hkRegMods  uint32        // currently registered pair (hotkey thread only)
	hkRegVK    uint32
)

// hotkeyPlatformApply pushes the configured combo toward the
// OS: store the wish, start the thread on first use, and wake
// it to re-register when it is already running.
func hotkeyPlatformApply(combo hotkeyCombo) {
	if combo.Off {
		hotkeyConflict.Store(false)
		hkWantMods.Store(0)
		hkWantVK.Store(0)
		if h := hkHWND.Load(); h != 0 {
			procPostMessageW.Call(h, hkReconfigMsg, 0, 0)
		} else {
			hotkeySetResult(false, "")
		}
		return
	}
	hkWantMods.Store(combo.Mods)
	hkWantVK.Store(combo.VK)
	hkStart.Do(func() { go hotkeyThread() })
	if h := hkHWND.Load(); h != 0 {
		procPostMessageW.Call(h, hkReconfigMsg, 0, 0)
	}
	// If the thread is still starting, its startup applies the
	// wish once the window exists (same contract as the tray).
}

// hkApplyRegistration reconciles the OS registration with the
// wanted (modifiers, vk) pair. Hotkey thread only.
func hkApplyRegistration(hwnd uintptr) {
	wantMods, wantVK := hkWantMods.Load(), hkWantVK.Load()
	if hkRegVK != 0 && (hkRegVK != wantVK || hkRegMods != wantMods) {
		procUnregisterHotKey.Call(hwnd, hkID)
		hkRegVK, hkRegMods = 0, 0
	}
	if wantVK == 0 {
		hotkeyConflict.Store(false)
		hotkeySetResult(false, "")
		return
	}
	if hkRegVK == wantVK && hkRegMods == wantMods {
		return // already registered as wished
	}
	r, _, errno := procRegisterHotKey.Call(hwnd, hkID, modNoRepeat|uintptr(wantMods), uintptr(wantVK))
	if r == 0 {
		hkRegVK, hkRegMods = 0, 0
		if errno != nil && errno.(syscall.Errno) == errHotkeyBusy {
			hotkeyConflict.Store(true)
			hotkeySetResult(false, "该组合已被其他程序占用（游戏、录屏或输入法常见），请换一个组合或关闭")
		} else {
			hotkeyConflict.Store(false)
			hotkeySetResult(false, "系统拒绝注册（错误 "+errnoText(errno)+"）")
		}
		return
	}
	hkRegVK, hkRegMods = wantVK, wantMods
	hotkeyConflict.Store(false)
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
				// v0.52: synchronous on purpose -- the boss
				// key's hide / raise is direct Win32 work and
				// lands within the press (bosskey.go). Only
				// the dead-view creation fallback backgrounds
				// itself, inside bossSummon.
				bossKeyPress()
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
