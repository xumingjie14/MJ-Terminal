//go:build windows

package main

// ---------- v0.71 test3, layer 2: hook chassis (Windows half) ----------
//
// Two low-level hooks (WH_MOUSE_LL + WH_KEYBOARD_LL) on a
// dedicated OS thread behind a message-only window -- the
// hotkeyThread chassis (hotkey_windows.go) cloned, NOT shared:
// the boss key and the interception layer have different
// lifecycles and must never stall each other's pump.
//
// Callback discipline (risk review L2-B2, binding):
//   - A callback reads the event struct, the atomic snapshot
//     (intercept.go) and, on the mouse path, only
//     WindowFromPoint / GetAncestor / GetWindowRect. No
//     business locks, no logging, no waiting -- a slow callback
//     makes the WHOLE machine's input queue wait and, past
//     LowLevelHooksTimeout, gets the hook silently removed.
//   - Mouse-move events stamp the liveness clock and pass on
//     the first lines (the stamp is what the watchdog in
//     intercept.go reconciles against; HS-14 requires it in
//     the callback).
//   - A swallow verdict that carries work (the hide) is posted
//     to this thread's own window and handed to a worker
//     goroutine (interceptQueueHide); the pump itself does no
//     business work.
//   - syscall.NewCallback procs are created lazily inside the
//     thread function, for the same init-cycle reason as the
//     hotkey's (see hotkey_windows.go).

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const interceptPlatformSupported = true

var (
	procSetWindowsHookExW      = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx    = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx         = user32.NewProc("CallNextHookEx")
	procWindowFromPoint        = user32.NewProc("WindowFromPoint")
	procGetAncestor            = user32.NewProc("GetAncestor")
	procGetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
)

const (
	whMouseLL    = 14 // WH_MOUSE_LL
	whKeyboardLL = 13 // WH_KEYBOARD_LL

	wmMouseMoveIC   = 0x0200
	wmLButtonDownIC = 0x0201
	wmLButtonUpIC   = 0x0202
	wmKeyDownIC     = 0x0100
	wmKeyUpIC       = 0x0101
	wmSysKeyDownIC  = 0x0104
	wmSysKeyUpIC    = 0x0105

	llmhfInjected = 0x00000001 // LLMHF_INJECTED
	llkhfInjected = 0x00000010 // LLKHF_INJECTED
	llkhfAltdown  = 0x00000020 // LLKHF_ALTDOWN

	vkF4IC = 0x73 // VK_F4
	gaRoot = 2    // GA_ROOT

	icHideMsg = 0x0412 // WM_USER+0x12: callback -> us, queue a hide (wParam=hwnd, lParam=gesture)
	icSyncMsg = 0x0413 // WM_USER+0x13: reconcile hooks with icWant (lParam=1 forces a reinstall cycle)

	gestureMouseIC uintptr = 1
	gestureKeyIC   uintptr = 2
)

// Win32 metric indices (GetSystemMetricsForDpi).
const (
	smCxSize         = 30 // SM_CXSIZE: caption button width
	smCyCaption      = 4  // SM_CYCAPTION: caption band height
	smCxSizeFrame    = 32 // SM_CXSIZEFRAME
	smCySizeFrame    = 33 // SM_CYSIZEFRAME
	smCxPaddedBorder = 92 // SM_CXPADDEDBORDER
)

type icPoint struct{ X, Y int32 }

type msllHookStruct struct {
	Pt        icPoint
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type kbdllHookStruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

var (
	icStartOnce sync.Once
	icHWND      atomic.Uintptr
	icWant      atomic.Bool
	icInstalled atomic.Bool
	icFailed    atomic.Bool
	icLastEvent atomic.Int64 // UnixNano of the last event any callback saw
	icCloseW    atomic.Int32
	icCaptionH  atomic.Int32

	// Hook-thread-only state (the callbacks and the reconcile
	// run on that thread because it installed the hooks and
	// pumps their messages).
	icMouseHook  uintptr
	icKeyHook    uintptr
	icArmed      uint64 // window armed by our swallowed mouse DOWN
	icMouseProc  uintptr
	icKeyProc    uintptr
	icWndProcPtr uintptr
)

// interceptPlatformSetWanted pushes the reconcile wish toward
// the hook thread (starting it on first use) and reports the
// last known install state; the reconcile itself completes on
// the hook thread, and interceptPatrol reads the fresh state
// on its next tick.
func interceptPlatformSetWanted(want bool) (bool, bool) {
	icWant.Store(want)
	if want {
		icStartOnce.Do(func() { go interceptThread() })
	}
	if h := icHWND.Load(); h != 0 {
		procPostMessageW.Call(h, icSyncMsg, 0, 0)
	}
	return icInstalled.Load(), icFailed.Load()
}

// interceptPlatformStop drops the hooks at daemon exit: the
// wish is cleared and the thread reconciles immediately; the
// OS would reclaim the hooks at process death regardless, so
// the portable promise holds either way -- doing it first is
// what keeps an update-handover successor single-hooked.
func interceptPlatformStop() {
	icWant.Store(false)
	if h := icHWND.Load(); h != 0 {
		procPostMessageW.Call(h, icSyncMsg, 0, 0)
	}
}

func interceptPlatformLastEvent() time.Time {
	n := icLastEvent.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// interceptPlatformReinstall forces an unhook+rehook cycle on
// the hook thread (the watchdog's answer to a suspected silent
// removal).
func interceptPlatformReinstall() {
	if h := icHWND.Load(); h != 0 {
		procPostMessageW.Call(h, icSyncMsg, 0, 1)
	}
}

// interceptPlatformRefreshMetrics re-reads the caption metrics
// the hit geometry consumes. The values are system quantities
// (per the review: never literal pixels); the callbacks only
// read the atomics.
func interceptPlatformRefreshMetrics() {
	dpi := uintptr(96)
	if r, _, _ := procGetDpiForSystem.Call(); r != 0 {
		dpi = r
	}
	get := func(idx int) int32 {
		r, _, _ := procGetSystemMetricsForDpi.Call(uintptr(idx), dpi)
		return int32(r)
	}
	if w := get(smCxSize) + 2*get(smCxSizeFrame); w > 0 {
		icCloseW.Store(w)
	}
	if h := get(smCyCaption) + get(smCySizeFrame) + get(smCxPaddedBorder); h > 0 {
		icCaptionH.Store(h)
	}
}

// icReconcileHooks makes the OS hook state match icWant. Hook
// thread only.
func icReconcileHooks() {
	want := icWant.Load()
	if want && icMouseHook == 0 {
		hInst, _, _ := procGetModuleHandleW.Call(0)
		mh, _, _ := procSetWindowsHookExW.Call(whMouseLL, icMouseProc, hInst, 0)
		kh, _, _ := procSetWindowsHookExW.Call(whKeyboardLL, icKeyProc, hInst, 0)
		if mh == 0 || kh == 0 {
			if mh != 0 {
				procUnhookWindowsHookEx.Call(mh)
			}
			if kh != 0 {
				procUnhookWindowsHookEx.Call(kh)
			}
			icFailed.Store(true)
			icInstalled.Store(false)
			return
		}
		icMouseHook, icKeyHook = mh, kh
		icFailed.Store(false)
		icInstalled.Store(true)
		icLastEvent.Store(time.Now().UnixNano())
	}
	if !want && icMouseHook != 0 {
		procUnhookWindowsHookEx.Call(icMouseHook)
		procUnhookWindowsHookEx.Call(icKeyHook)
		icMouseHook, icKeyHook = 0, 0
		icInstalled.Store(false)
	}
}

// icTopFromPoint resolves an event point to its top-level
// window: WindowFromPoint + GetAncestor(GA_ROOT). One of the
// two cheap call families the callback is allowed.
func icTopFromPoint(pt icPoint) uint64 {
	packed := uint64(uint32(pt.Y))<<32 | uint64(uint32(pt.X))
	r, _, _ := procWindowFromPoint.Call(uintptr(packed))
	if r == 0 {
		return 0
	}
	root, _, _ := procGetAncestor.Call(r, gaRoot)
	if root == 0 {
		return uint64(r)
	}
	return uint64(root)
}

func icPostHide(hwnd uint64, gesture uintptr) {
	if h := icHWND.Load(); h != 0 {
		procPostMessageW.Call(h, icHideMsg, uintptr(hwnd), gesture)
	}
}

func icNext(nCode int32, wParam, lParam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

func makeICMouseProc() uintptr {
	return syscall.NewCallback(func(nCode int32, wParam, lParam uintptr) uintptr {
		icLastEvent.Store(time.Now().UnixNano())
		if nCode < 0 {
			return icNext(nCode, wParam, lParam)
		}
		msg := uint32(wParam)
		if msg == wmMouseMoveIC {
			return icNext(nCode, wParam, lParam)
		}
		if msg != wmLButtonDownIC && msg != wmLButtonUpIC {
			return icNext(nCode, wParam, lParam)
		}
		ms := (*msllHookStruct)(unsafe.Pointer(lParam))
		var (
			target     uint64
			lockURL    string
			l, t, r, b int32
		)
		if hw := icTopFromPoint(ms.Pt); hw != 0 {
			target = hw
			lockURL = interceptLockFor(hw)
			if lockURL != "" {
				l, t, r, b, _ = platformWindowRect(hw)
			}
		}
		kind := interceptMouseDown
		if msg == wmLButtonUpIC {
			kind = interceptMouseUp
		}
		dec := decideCloseIntercept(interceptFacts{
			Kind:           kind,
			Injected:       ms.Flags&llmhfInjected != 0,
			X:              ms.Pt.X,
			Y:              ms.Pt.Y,
			TargetHWND:     target,
			TargetLockURL:  lockURL,
			RectL:          l,
			RectT:          t,
			RectR:          r,
			RectB:          b,
			CloseW:         icCloseW.Load(),
			CaptionH:       icCaptionH.Load(),
			ArmedHWND:      icArmed,
			ServicesActive: servicesActive(),
			Enabled:        icInstalled.Load(),
		})
		switch dec.Action {
		case interceptSwallowHide:
			icArmed = 0
			icPostHide(dec.HideHWND, gestureMouseIC)
			return 1
		case interceptSwallow:
			if kind == interceptMouseDown {
				icArmed = target
			} else {
				icArmed = 0
			}
			return 1
		}
		if kind == interceptMouseDown {
			icArmed = 0
		}
		return icNext(nCode, wParam, lParam)
	})
}

func makeICKeyProc() uintptr {
	return syscall.NewCallback(func(nCode int32, wParam, lParam uintptr) uintptr {
		icLastEvent.Store(time.Now().UnixNano())
		if nCode < 0 {
			return icNext(nCode, wParam, lParam)
		}
		msg := uint32(wParam)
		kind := interceptKeyUp
		if msg == wmKeyDownIC || msg == wmSysKeyDownIC {
			kind = interceptKeyDown
		} else if msg != wmKeyUpIC && msg != wmSysKeyUpIC {
			return icNext(nCode, wParam, lParam)
		}
		kb := (*kbdllHookStruct)(unsafe.Pointer(lParam))
		altF4 := kb.VkCode == vkF4IC && kb.Flags&llkhfAltdown != 0
		var fg uint64
		var fgLock string
		if kind == interceptKeyDown && altF4 && kb.Flags&llkhfInjected == 0 {
			// Target binding (L2-I1): the foreground window,
			// resolved against the snapshot -- the only lookup
			// the keyboard path ever makes.
			fg = platformForegroundHWND()
			if fg != 0 {
				fgLock = interceptLockFor(fg)
			}
		}
		dec := decideCloseIntercept(interceptFacts{
			Kind:              kind,
			Injected:          kb.Flags&llkhfInjected != 0,
			AltF4:             altF4,
			ForegroundHWND:    fg,
			ForegroundLockURL: fgLock,
			ServicesActive:    servicesActive(),
			Enabled:           icInstalled.Load(),
		})
		if dec.Action == interceptSwallowHide {
			icPostHide(dec.HideHWND, gestureKeyIC)
			return 1
		}
		return icNext(nCode, wParam, lParam)
	})
}

func makeICWndProc() uintptr {
	return syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		switch uint32(msg) {
		case icHideMsg:
			gesture := "点击关闭按钮"
			if lParam == gestureKeyIC {
				gesture = "Alt+F4"
			}
			interceptQueueHide(uint64(wParam), gesture)
			return 0
		case icSyncMsg:
			if lParam == 1 && icMouseHook != 0 {
				procUnhookWindowsHookEx.Call(icMouseHook)
				procUnhookWindowsHookEx.Call(icKeyHook)
				icMouseHook, icKeyHook = 0, 0
				icInstalled.Store(false)
			}
			icReconcileHooks()
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	})
}

func interceptThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	icMouseProc = makeICMouseProc()
	icKeyProc = makeICKeyProc()
	icWndProcPtr = makeICWndProc()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("MJTerminalInterceptWnd")
	wc := trayWndClassEx{
		Style:         0,
		LpfnWndProc:   icWndProcPtr,
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
		icFailed.Store(true)
		return
	}
	icHWND.Store(hwnd)
	icReconcileHooks()

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
