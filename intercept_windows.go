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
	procGetDpiForWindow        = user32.NewProc("GetDpiForWindow")
	procDwmGetWindowAttribute  = dwmapi.NewProc("DwmGetWindowAttribute")
)

const dwmwaExtendedFrameBounds = 9 // DWMWA_EXTENDED_FRAME_BOUNDS

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
	icTeleMsg = 0x0414 // WM_USER+0x14: callback -> us, one telemetry record (lParam=*icTeleBox)

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
	icHWND        atomic.Uintptr
	icWant        atomic.Bool
	icInstalled   atomic.Bool
	icFailed      atomic.Bool
	icThreadAlive atomic.Bool
	icPrevWant    atomic.Bool
	// Per-hook liveness stamps (UnixNano). Test5 shared one
	// stamp across both hooks, which let a live hook mask a
	// silently-removed sibling forever; the watchdog in
	// intercept.go now reads them separately.
	icLastMouse atomic.Int64
	icLastKey   atomic.Int64

	icStartMu sync.Mutex

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

// ensureICThread starts the hook thread if it is not running.
// Test5 started it once per process (sync.Once): if the
// thread ever died, its hooks died with it while icInstalled
// stayed true forever -- the layer reported itself active and
// intercepted nothing. The thread now clears the installed
// flag on exit (see interceptThread's defer) and the patrol's
// per-tick SetWanted respawns it.
func ensureICThread() {
	if icThreadAlive.Load() {
		return
	}
	icStartMu.Lock()
	defer icStartMu.Unlock()
	if !icThreadAlive.Load() {
		icThreadAlive.Store(true)
		go interceptThread()
	}
}

// interceptPlatformSetWanted pushes the reconcile wish toward
// the hook thread (starting it on first use) and reports the
// install state. On the false->true transition it waits
// briefly (bounded, transitions only) for the hook thread's
// verdict, so the patrol's arm log and degraded state describe
// THIS attempt instead of a previous tick's: in test5 the
// reconcile itself ran on the hook thread, but the patrol only
// OBSERVED the installed flag on its next 2 s tick (and ticks
// slip further whenever the shared patrol body is busy), so
// the lock -> armed gap stretched to 18 s in the field and the
// console's "armed" line never proved the hooks were live at
// any given click.
func interceptPlatformSetWanted(want bool) (bool, bool) {
	icWant.Store(want)
	if want {
		ensureICThread()
	}
	if h := icHWND.Load(); h != 0 {
		procPostMessageW.Call(h, icSyncMsg, 0, 0)
	}
	if want && icPrevWant.CompareAndSwap(false, true) {
		deadline := time.Now().Add(400 * time.Millisecond)
		for time.Now().Before(deadline) {
			if icInstalled.Load() || icFailed.Load() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !want {
		icPrevWant.Store(false)
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

// interceptPlatformHookEvents returns the per-hook liveness
// stamps the watchdog reconciles against.
func interceptPlatformHookEvents() (mouse, key time.Time) {
	if n := icLastMouse.Load(); n != 0 {
		mouse = time.Unix(0, n)
	}
	if n := icLastKey.Load(); n != 0 {
		key = time.Unix(0, n)
	}
	return mouse, key
}

// interceptPlatformReinstall forces an unhook+rehook cycle on
// the hook thread (the watchdog's answer to a suspected silent
// removal).
func interceptPlatformReinstall() {
	if h := icHWND.Load(); h != 0 {
		procPostMessageW.Call(h, icSyncMsg, 0, 1)
	}
}

// icCaptionMetrics measures the caption quantities at the
// TARGET WINDOW's dpi (test6): test5 measured them once per
// patrol into atomics at the logon system DPI and the
// callbacks consumed the frozen pair. Measuring per event, at
// the window the event is about, keeps the metric set in the
// same coordinate space as the event's other facts whatever
// the process's DPI awareness (today: none -- no manifest, so
// every value arrives virtualized at 96) and however the
// display configuration changes between events. Leaf user32
// calls only; the callbacks invoke this solely for events on
// locked windows, never on the mouse-move path.
func icCaptionMetrics(dpi int32) (btnW, frameX, frameY, caption, padded int32) {
	if dpi <= 0 {
		if r, _, _ := procGetDpiForSystem.Call(); r != 0 {
			dpi = int32(r)
		}
	}
	if dpi <= 0 {
		dpi = 96
	}
	get := func(idx int) int32 {
		r, _, _ := procGetSystemMetricsForDpi.Call(uintptr(idx), uintptr(dpi))
		return int32(r)
	}
	return get(smCxSize), get(smCxSizeFrame), get(smCySizeFrame), get(smCyCaption), get(smCxPaddedBorder)
}

// icWindowDPI reads the target window's own DPI
// (GetDpiForWindow): the value Chrome sized the window's
// caption for. 0 = the call failed (facts carry it honestly;
// the metrics then fall back to the system DPI).
func icWindowDPI(hwnd uint64) int32 {
	r, _, _ := procGetDpiForWindow.Call(uintptr(hwnd))
	return int32(r)
}

// icFrameBounds reads the window's VISIBLE frame
// (DWMWA_EXTENDED_FRAME_BOUNDS): the border Chrome draws its
// caption buttons against, without GetWindowRect's invisible
// resize overhang.
func icFrameBounds(hwnd uint64) (int32, int32, int32, int32, bool) {
	var rc wmRect
	hr, _, _ := procDwmGetWindowAttribute.Call(uintptr(hwnd), dwmwaExtendedFrameBounds,
		uintptr(unsafe.Pointer(&rc)), unsafe.Sizeof(rc))
	if hr != 0 {
		return 0, 0, 0, 0, false
	}
	return rc.Left, rc.Top, rc.Right, rc.Bottom, true
}

// icFillGeometry completes the mouse/keyboard facts for one
// locked target window: rect, visible frame, window DPI and
// the caption metrics at that DPI. Callback-legal calls only.
func icFillGeometry(f *interceptFacts, hwnd uint64) {
	f.RectL, f.RectT, f.RectR, f.RectB, _ = platformWindowRect(hwnd)
	f.FrameL, f.FrameT, f.FrameR, f.FrameB, f.FrameOK = icFrameBounds(hwnd)
	f.DpiWindow = icWindowDPI(hwnd)
	f.BtnW, f.FrameXW, f.FrameYW, f.CaptionH, f.PaddedB = icCaptionMetrics(f.DpiWindow)
}

// ---------- telemetry hand-off (callback -> wndproc -> worker) ----------

// icTeleBox carries one decision record from a callback to
// the hook window. The box is held in icTeleInFlight from the
// post until the dispatch, so the GC always sees a live
// reference while the only other copy is a uintptr inside the
// OS message queue (which the GC does not scan).
type icTeleBox struct{ t interceptTelemetry }

var (
	icTeleMu       sync.Mutex
	icTeleInFlight = map[*icTeleBox]bool{}
)

func icPostTelemetry(t interceptTelemetry) {
	h := icHWND.Load()
	if h == 0 {
		return
	}
	box := &icTeleBox{t: t}
	icTeleMu.Lock()
	icTeleInFlight[box] = true
	icTeleMu.Unlock()
	procPostMessageW.Call(h, icTeleMsg, 0, uintptr(unsafe.Pointer(box)))
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
		now := time.Now().UnixNano()
		icLastMouse.Store(now)
		icLastKey.Store(now)
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
		icLastMouse.Store(time.Now().UnixNano())
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
		kind := interceptMouseDown
		if msg == wmLButtonUpIC {
			kind = interceptMouseUp
		}
		facts := interceptFacts{
			Kind:           kind,
			Injected:       ms.Flags&llmhfInjected != 0,
			X:              ms.Pt.X,
			Y:              ms.Pt.Y,
			ArmedHWND:      icArmed,
			ServicesActive: servicesActive(),
			Enabled:        icInstalled.Load(),
		}
		if hw := icTopFromPoint(ms.Pt); hw != 0 {
			facts.TargetHWND = hw
			facts.TargetLockURL = interceptLockFor(hw)
			if facts.TargetLockURL != "" {
				icFillGeometry(&facts, hw)
			}
		}
		dec := decideCloseIntercept(facts)
		// Telemetry (test6): every button decision about a
		// locked window -- plus an armed release whose target
		// stopped being locked mid-gesture -- is posted to the
		// worker's console line. A plain struct copy and a
		// PostMessage; the callback still never logs.
		if facts.TargetLockURL != "" || (kind == interceptMouseUp && icArmed != 0 && facts.TargetHWND == icArmed) {
			zone, zoneOK := closeZoneFor(facts)
			phase := "按下"
			if kind == interceptMouseUp {
				phase = "抬起"
			}
			icPostTelemetry(interceptTelemetry{
				Gesture: "点击关闭按钮", Phase: phase,
				Facts: facts, Zone: zone, ZoneOK: zoneOK, Dec: dec,
			})
		}
		switch dec.Action {
		case interceptSwallowHide:
			icArmed = 0
			icPostHide(dec.HideHWND, gestureMouseIC)
			return 1
		case interceptSwallow:
			if kind == interceptMouseDown {
				icArmed = facts.TargetHWND
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
		icLastKey.Store(time.Now().UnixNano())
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
		facts := interceptFacts{
			Kind:           kind,
			Injected:       kb.Flags&llkhfInjected != 0,
			AltF4:          altF4,
			ServicesActive: servicesActive(),
			Enabled:        icInstalled.Load(),
		}
		if kind == interceptKeyDown && altF4 && kb.Flags&llkhfInjected == 0 {
			// Target binding (L2-I1): the foreground window,
			// resolved against the snapshot -- the only lookup
			// the keyboard path ever makes.
			fg := platformForegroundHWND()
			if fg != 0 {
				facts.ForegroundHWND = fg
				facts.ForegroundLockURL = interceptLockFor(fg)
				if facts.ForegroundLockURL != "" {
					facts.TargetHWND = fg
					icFillGeometry(&facts, fg)
				}
			}
		}
		dec := decideCloseIntercept(facts)
		// Telemetry (test6): every Alt+F4 verdict against a
		// locked foreground window reaches the console.
		if kind == interceptKeyDown && altF4 && facts.ForegroundLockURL != "" {
			zone, zoneOK := closeZoneFor(facts)
			icPostTelemetry(interceptTelemetry{
				Gesture: "Alt+F4", Phase: "按键",
				Facts: facts, Zone: zone, ZoneOK: zoneOK, Dec: dec,
			})
		}
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
		case icTeleMsg:
			box := (*icTeleBox)(unsafe.Pointer(lParam))
			icTeleMu.Lock()
			delete(icTeleInFlight, box)
			icTeleMu.Unlock()
			interceptQueueTelemetry(box.t)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	})
}

func interceptThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// On ANY exit the hooks die with this thread (the OS
	// removes hooks whose installing thread is gone): clear
	// the state the patrol and Settings read, so the layer
	// never reports itself active while it is dead (test5's
	// write-once-true flag could). ensureICThread respawns
	// the thread on the next patrol tick.
	defer func() {
		icInstalled.Store(false)
		icHWND.Store(0)
		icThreadAlive.Store(false)
	}()

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
