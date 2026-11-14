package main

// ---------- v0.71 test3, layer 2: 关窗变藏 (close becomes hide) ----------
//
// The conditional-interception layer's first block (the layer
// v0.72's route A -- own title-bar menu + in-terminal Alt+Tab --
// will reuse; THIS version ships exactly one use for it: turning
// a locked web app's close gesture into a hide-to-background, so
// the window and its page stay alive and the Stage card never
// moves).
//
// Mechanism (risk review L2, binding): two low-level hooks,
// WH_MOUSE_LL for the caption close button and WH_KEYBOARD_LL
// for Alt+F4. No DLL, no injection, no foreign WNDPROC -- the
// project's no-injection boundary (winmgr_windows.go) stands;
// the hooks live in the daemon and only ever SWALLOW the one
// input event that lands on a locked MJ window's close gesture.
// Everything else -- Ctrl+W, task-manager kills, another
// process's WM_CLOSE -- is deliberately NOT intercepted; layer
// 1 (the lock-pinned Stage card) plus the patrol relaunch cover
// those paths.
//
// Structure, per the review's callback discipline (L2-B2):
//   - The DECISION is this file's pure decideCloseIntercept:
//     platform files only collect facts and carry out verdicts,
//     exactly the planBridgeLaunch discipline from the test1
//     incident. GOOS behaviour is pinned by its truth table on
//     any OS.
//   - Callbacks read an atomic snapshot (hwnd -> lock URL) that
//     the patrol refreshes wholesale; they never touch a
//     business lock, never log, and hand the actual hide to a
//     worker goroutine through the hook thread's message window
//     (PostMessage), never doing the work inline.
//   - The hooks exist only while the lock set is non-empty
//     (reconciled on the patrol cadence); the daemon exit
//     sequence unhooks before anything else, after the boss key
//     (requestDaemonExit), so an update-handover successor never
//     meets a double hook.

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- pure decision ----------

type interceptEventKind int

const (
	interceptMouseDown interceptEventKind = iota
	interceptMouseUp
	interceptKeyDown
	interceptKeyUp
)

type interceptAction int

const (
	interceptPass interceptAction = iota
	// interceptSwallow eats the event and nothing else happens:
	// an armed mouse DOWN, or the UP that cancels the gesture
	// outside the close zone.
	interceptSwallow
	// interceptSwallowHide eats the event and hides HideHWND.
	interceptSwallowHide
)

// interceptFacts is everything decideCloseIntercept may look
// at. The platform callback fills it from the event struct, two
// cheap Win32 calls (WindowFromPoint + GetWindowRect) and the
// atomic snapshot -- nothing else is permitted on that path.
type interceptFacts struct {
	Kind     interceptEventKind
	Injected bool // synthesized input (accessibility tools, macros, remote control): never ours to eat

	// Mouse path.
	X, Y          int32
	TargetHWND    uint64 // top-level window under the point (0 = none)
	TargetLockURL string // the target's lock key ("" = not a locked app's window)
	RectL         int32  // target window rect, same coordinate space as X/Y
	RectT         int32
	RectR         int32
	RectB         int32
	CloseW        int32  // caption close-button width, from system metrics at runtime
	CaptionH      int32  // caption band height, from system metrics at runtime
	ArmedHWND     uint64 // the window our swallowed DOWN armed (0 = no gesture in flight)

	// Keyboard path.
	AltF4             bool   // VK_F4 with the Alt modifier down
	ForegroundHWND    uint64 // foreground window at the keypress
	ForegroundLockURL string // its lock key ("" = not a locked app's window)

	// Gates. Passing is the default; swallowing is the exception.
	ServicesActive bool // vault open, services running
	Enabled        bool // hooks installed and healthy
}

type interceptDecision struct {
	Action   interceptAction
	HideHWND uint64 // for interceptSwallowHide
}

// interceptInCloseZone is the hit geometry (review's hard
// spec): relative to the window's own rect, the close zone is
// the RIGHTMOST caption cell -- x in [right-closeW, right),
// y in [top, top+captionH). One formula for windowed,
// maximized (rect runs past the screen edge) and negative-
// coordinate (secondary monitor) rects alike; no pixel
// constants, the metrics arrive as facts.
func interceptInCloseZone(x, y, l, t, r, b, closeW, captionH int32) bool {
	if closeW <= 0 || captionH <= 0 || r <= l || b <= t {
		return false
	}
	return x >= r-closeW && x < r && y >= t && y < t+captionH
}

// decideCloseIntercept is the whole interception rulebook:
//
//   - gates closed (vault locked, hooks down) or injected
//     input: pass, always.
//   - mouse DOWN on a locked window's close zone: swallow and
//     arm; anywhere else (the minimize/maximize cells, a
//     caption drag start, content, another app): pass.
//   - mouse UP completing an armed gesture on the same window
//     inside the zone: swallow and hide. An armed UP landing
//     anywhere else cancels the gesture -- swallowed (it pairs
//     the swallowed DOWN) but nothing hides, matching Chrome's
//     own "press the X, drag away, release: no close".
//   - Alt+F4 keydown while a locked app's window is foreground:
//     swallow and hide (auto-repeat keydowns included; the
//     executor's hide is idempotent). Alt+F4 anywhere else --
//     host app, the shell view, the daemon's own windows --
//     passes untouched. The keyup always passes.
func decideCloseIntercept(f interceptFacts) interceptDecision {
	pass := interceptDecision{Action: interceptPass}
	if !f.Enabled || !f.ServicesActive || f.Injected {
		return pass
	}
	switch f.Kind {
	case interceptMouseDown:
		if f.TargetHWND == 0 || f.TargetLockURL == "" {
			return pass
		}
		if interceptInCloseZone(f.X, f.Y, f.RectL, f.RectT, f.RectR, f.RectB, f.CloseW, f.CaptionH) {
			return interceptDecision{Action: interceptSwallow}
		}
		return pass
	case interceptMouseUp:
		if f.ArmedHWND == 0 {
			return pass
		}
		if f.TargetHWND == f.ArmedHWND && f.TargetLockURL != "" &&
			interceptInCloseZone(f.X, f.Y, f.RectL, f.RectT, f.RectR, f.RectB, f.CloseW, f.CaptionH) {
			return interceptDecision{Action: interceptSwallowHide, HideHWND: f.ArmedHWND}
		}
		return interceptDecision{Action: interceptSwallow}
	case interceptKeyDown:
		if f.AltF4 && f.ForegroundHWND != 0 && f.ForegroundLockURL != "" {
			return interceptDecision{Action: interceptSwallowHide, HideHWND: f.ForegroundHWND}
		}
		return pass
	default: // interceptKeyUp and anything unknown
		return pass
	}
}

// interceptDesiredFor is the install rule, kept pure like
// bridgeRouteFor so the GOOS gate is pinned by test on any OS:
// hooks exist only on Windows, only while at least one app is
// locked, only while services are active (a locked vault means
// hands off everything).
func interceptDesiredFor(goos string, lockCount int, services bool) bool {
	return goos == "windows" && lockCount > 0 && services
}

// ---------- atomic snapshot (the callback's only data) ----------

// interceptSnapshot is the callback's whole world: which live
// top-level windows currently stand for a locked app. The
// patrol rebuilds it wholesale (atomic pointer swap); the
// callback only reads. Identity comes from the resident
// engine's one function (residentWindowLockURL) -- the hook
// never keeps a second, approximate opinion.
type interceptSnapshot struct {
	locks map[uint64]string // hwnd -> lock URL
}

var interceptSnap atomic.Pointer[interceptSnapshot]

func refreshInterceptSnapshot() {
	locks := webAppLockSet()
	m := map[uint64]string{}
	if len(locks) > 0 {
		names := residentNameIndex(locks)
		extMu.Lock()
		for h, e := range extWins {
			if e.Shell {
				continue
			}
			cp := *e
			if key := residentWindowLockURL(&cp, locks, names); key != "" {
				m[h] = key
			}
		}
		extMu.Unlock()
	}
	interceptSnap.Store(&interceptSnapshot{locks: m})
}

// interceptLockFor is called from the hook callbacks: one map
// read on an immutable snapshot, no locks, no allocation.
func interceptLockFor(hwnd uint64) string {
	s := interceptSnap.Load()
	if s == nil {
		return ""
	}
	return s.locks[hwnd]
}

// ---------- hide execution (worker goroutine, never the hook) ----------

type interceptHideRequest struct {
	HWND    uint64
	Gesture string // console wording: 点击关闭按钮 / Alt+F4
}

var (
	interceptWork       = make(chan interceptHideRequest, 16)
	interceptWorkerOnce sync.Once
)

// interceptQueueHide is the hook thread's hand-off: a buffered,
// non-blocking send. A full queue drops the request -- the hide
// is idempotent and the user's next gesture retries; the hook
// path must never wait on anything.
func interceptQueueHide(hwnd uint64, gesture string) {
	interceptWorkerOnce.Do(func() { go interceptWorker() })
	select {
	case interceptWork <- interceptHideRequest{HWND: hwnd, Gesture: gesture}:
	default:
	}
}

func interceptWorker() {
	for req := range interceptWork {
		executeInterceptHide(req)
	}
}

// executeInterceptHide re-verifies everything against CURRENT
// state before touching the window: the snapshot the callback
// read can be a patrol old, the user may have unlocked the app
// in between, and hiding an already-hidden window (Alt+F4 held
// down, auto-repeat) is a silent no-op.
func executeInterceptHide(req interceptHideRequest) {
	if !servicesActive() {
		return
	}
	locks := webAppLockSet()
	if len(locks) == 0 {
		return
	}
	names := residentNameIndex(locks)
	extMu.Lock()
	e, ok := extWins[req.HWND]
	var cp extWindow
	if ok {
		cp = *e
	}
	extMu.Unlock()
	if !ok {
		return
	}
	key := residentWindowLockURL(&cp, locks, names)
	if key == "" {
		return
	}
	if !platformWindowVisible(req.HWND) {
		return // already in the background: nothing to do, nothing to log
	}
	daemonHideWindow(req.HWND, key, req.Gesture)
}

// ---------- reconcile + health (patrol cadence) ----------

// interceptHookSilence bounds the watchdog's suspicion: a hook
// that has delivered NO event for this long while the
// foreground window changed inside the same window is presumed
// silently removed by the OS (LowLevelHooksTimeout removals
// come with no notification). Deliberately conservative -- a
// false reinstall costs a log line, a missed one costs the
// feature, but a hair-trigger would spam both.
const interceptHookSilence = 90 * time.Second

var interceptCtl = struct {
	sync.Mutex
	installed    bool // hooks up as of the last reconcile
	degraded     bool // install wanted but failing (settings shows it)
	fg           uint64
	fgChangedAt  time.Time
	reinstalled  time.Time
	everDegraded bool // the failure line logs once per failure streak
}{}

// interceptPatrol runs at the top of every patrol tick (before
// the vault gate, so locking the vault also stands the hooks
// down): refresh the snapshot, reconcile the install with the
// lock set, watch for a silently-removed hook.
func interceptPatrol() {
	refreshInterceptSnapshot()
	locks := webAppLockSet()
	want := interceptDesiredFor(runtime.GOOS, len(locks), servicesActive())
	installed, failed := interceptPlatformSetWanted(want)
	if want {
		interceptPlatformRefreshMetrics()
	}
	now := time.Now()
	interceptCtl.Lock()
	if installed && !interceptCtl.installed {
		logf("winmgr", "info", "关窗变藏：已开始留意已锁应用的关闭动作（点关闭按钮或 Alt+F4 将转入后台）")
	}
	if !installed && interceptCtl.installed && !want {
		logf("winmgr", "info", "关窗变藏：已无锁住的应用，监听已停下")
	}
	degraded := want && failed
	if degraded && !interceptCtl.everDegraded {
		logf("winmgr", "warn", "关窗变藏：系统监听安装失败，本功能未生效——已锁应用关窗后将按原方式自动重新打开")
	}
	interceptCtl.everDegraded = degraded
	// Watchdog: hook silent for a long stretch while the user
	// is plainly active (the foreground window changed inside
	// the silent window) -- presume the OS dropped the hook
	// and reinstall it once.
	if want && installed && !failed {
		fg := platformForegroundHWND()
		if fg != interceptCtl.fg {
			interceptCtl.fg = fg
			interceptCtl.fgChangedAt = now
		}
		last := interceptPlatformLastEvent()
		if !last.IsZero() && now.Sub(last) > interceptHookSilence &&
			!interceptCtl.fgChangedAt.IsZero() && now.Sub(interceptCtl.fgChangedAt) < interceptHookSilence &&
			now.Sub(interceptCtl.reinstalled) > interceptHookSilence {
			interceptCtl.reinstalled = now
			interceptCtl.Unlock()
			interceptPlatformReinstall()
			logf("winmgr", "warn", "关窗变藏：监听长时间无事件但前台窗口在变化，疑似被系统摘除，已重新安装")
			interceptCtl.Lock()
		}
	}
	interceptCtl.installed = installed
	interceptCtl.degraded = degraded
	interceptCtl.Unlock()
}

// interceptCloseHideState reports the layer's live state for
// the Settings lock list: active = the close-becomes-hide
// behaviour is in force right now; degraded = it should be but
// the install failed, which the page must say out loud (no
// silent pretending -- the v0.71 F4 lesson).
func interceptCloseHideState() (active, degraded bool) {
	interceptCtl.Lock()
	defer interceptCtl.Unlock()
	return interceptCtl.installed, interceptCtl.degraded
}

// interceptStop stands the hooks down at daemon exit. It runs
// at the very front of the exit sequence (requestDaemonExit,
// right after the boss key) so an update-handover successor
// never starts against a double hook.
func interceptStop() {
	interceptPlatformSetWanted(false)
	interceptPlatformStop()
}
