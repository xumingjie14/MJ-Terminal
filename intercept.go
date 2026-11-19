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
	"fmt"
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
// at. The platform callback fills it from the event struct, a
// few cheap Win32 calls (WindowFromPoint, GetWindowRect,
// DwmGetWindowAttribute for the visible frame, GetDpiForWindow)
// and the atomic snapshot -- nothing else is permitted on that
// path.
//
// Geometry (test6, from the test5 field miss): the daemon
// binary carries NO DPI manifest (verified by building with
// the project's own toolchain: a stock Go build embeds no
// manifest at all), so the process is DPI-UNAWARE and every
// input the layer consumes -- hook point, window rect, DWM
// frame, system metrics -- arrives virtualized into one
// consistent 96-DPI space. Test5's zone was anchored on the
// window rect, whose right edge sits an invisible resize
// border (~8 virtual px) BEYOND the edge Chrome draws its
// caption buttons against, and its metrics were frozen once
// per patrol at the logon system DPI: the zone stopped a few
// px short of the button's left edge and its height was the
// bare caption band, thinner than Chrome's custom caption.
// The hit test now anchors on the DWM VISIBLE frame and the
// metrics are measured fresh per event at the target window's
// DPI (which the platform reports in the same space as every
// other fact; reading them per-window also keeps the geometry
// correct if the process ever gains a manifest). The metric
// fields below are the system quantities measured at
// DpiWindow by the platform layer; the pure closeZoneFor
// combines them.
type interceptFacts struct {
	Kind     interceptEventKind
	Injected bool // synthesized input (accessibility tools, macros, remote control): never ours to eat

	// Mouse path.
	X, Y          int32
	TargetHWND    uint64 // top-level window under the point (0 = none)
	TargetLockURL string // the target's lock key ("" = not a locked app's window)
	// Native path (v0.71 test7): the target is a locked BOX
	// app's ALIGNED window. TargetBoxApp is its lock key (the
	// registry id); the close zone comes from the patrol-
	// probed rect (ProbeOK) for self-drawn frames, from
	// closeZoneFor for standard frames, and from nowhere when
	// NativeNoZone -- a self-drawn window the probe could not
	// locate a close button on, whose mouse path must pass.
	TargetBoxApp string
	ProbeL       int32
	ProbeT       int32
	ProbeR       int32
	ProbeB       int32
	ProbeOK      bool
	NativeNoZone bool
	// Telemetry-only native facts (§8 of the test7 review):
	// how the window was attributed, and what kind of frame
	// it carries. The decision never reads them.
	Basis     string // "exe" | "dir"
	FrameKind string // "standard" | "selfdrawn"
	RectL     int32  // target window rect (GetWindowRect), same space as X/Y
	RectT     int32
	RectR     int32
	RectB     int32
	FrameL    int32 // DWM visible frame bounds (EXTENDED_FRAME_BOUNDS)
	FrameT    int32
	FrameR    int32
	FrameB    int32
	FrameOK   bool  // Frame* are real (DWM answered)
	DpiWindow int32 // target window's DPI (GetDpiForWindow; 0 = unknown)
	// Caption metrics at DpiWindow (system DPI when unknown):
	BtnW      int32  // SM_CXSIZE: one caption button's width
	FrameXW   int32  // SM_CXSIZEFRAME
	FrameYW   int32  // SM_CYSIZEFRAME
	CaptionH  int32  // SM_CYCAPTION
	PaddedB   int32  // SM_CXPADDEDBORDER
	ArmedHWND uint64 // the window our swallowed DOWN armed (0 = no gesture in flight)

	// Keyboard path.
	AltF4             bool   // VK_F4 with the Alt modifier down
	ForegroundHWND    uint64 // foreground window at the keypress
	ForegroundLockURL string // its lock key ("" = not a locked app's window)
	ForegroundBoxApp  string // native lock key of the foreground window (test7)

	// Gates. Passing is the default; swallowing is the exception.
	ServicesActive bool // vault open, services running
	Enabled        bool // hooks installed and healthy
}

type interceptDecision struct {
	Action   interceptAction
	HideHWND uint64 // for interceptSwallowHide
	Reason   string // console wording for the telemetry line (test6)
}

// closeZone is the caption close button's hit rectangle, in
// the same physical coordinate space as the event point.
type closeZone struct{ X0, X1, Y0, Y1 int32 }

func (z closeZone) contains(x, y int32) bool {
	return x >= z.X0 && x < z.X1 && y >= z.Y0 && y < z.Y1
}

// closeZoneFor is the hit geometry (review's hard spec, test6
// revision): the zone is the RIGHTMOST caption cell of the
// window's VISIBLE frame -- x in [frameR-zoneW, frameR),
// y in [frameT, frameT+zoneH). The width is one caption button
// plus one frame width of slack when anchored on the visible
// frame (the frame edge IS the button's edge there); when DWM
// has no frame answer, the window rect anchors instead and
// the width grows by a second frame width, absorbing the
// invisible resize border the rect includes and the frame
// does not. The height is the caption band plus its frame and
// padded border. All metrics arrive as facts measured at the
// target window's DPI; one formula serves windowed, maximized
// (rect runs past the screen edge) and negative-coordinate
// (secondary monitor) rects alike.
func closeZoneFor(f interceptFacts) (closeZone, bool) {
	l, t, r := f.RectL, f.RectT, f.RectR
	zoneW := f.BtnW + 2*f.FrameXW
	if f.FrameOK {
		l, t, r = f.FrameL, f.FrameT, f.FrameR
		zoneW = f.BtnW + f.FrameXW
	}
	zoneH := f.CaptionH + f.FrameYW + f.PaddedB
	if f.BtnW <= 0 || zoneH <= 0 || r <= l {
		return closeZone{}, false
	}
	return closeZone{X0: r - zoneW, X1: r, Y0: t, Y1: t + zoneH}, true
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
	pass := func(reason string) interceptDecision {
		return interceptDecision{Action: interceptPass, Reason: reason}
	}
	if f.Injected {
		return pass("注入事件，不拦截")
	}
	if !f.Enabled {
		return pass("监听未生效")
	}
	if !f.ServicesActive {
		return pass("服务未运行")
	}
	// The event's close zone (test7): a probed rect when the
	// patrol probed one for this (self-drawn, native) window,
	// the system-metric formula otherwise.
	zoneForEvent := func() (closeZone, bool) {
		if f.ProbeOK {
			return closeZone{X0: f.ProbeL, Y0: f.ProbeT, X1: f.ProbeR, Y1: f.ProbeB}, true
		}
		return closeZoneFor(f)
	}
	lockedTarget := f.TargetLockURL != "" || f.TargetBoxApp != ""
	switch f.Kind {
	case interceptMouseDown:
		if f.TargetHWND == 0 || !lockedTarget {
			return pass("不是已锁应用的窗口")
		}
		if f.NativeNoZone {
			return pass("这个应用的关闭按钮无法定位，鼠标不拦截")
		}
		z, ok := zoneForEvent()
		if !ok {
			return pass("窗口度量不可用")
		}
		if z.contains(f.X, f.Y) {
			return interceptDecision{Action: interceptSwallow, Reason: "按下命中关闭区，吞下并武装"}
		}
		return pass("按下不在关闭区")
	case interceptMouseUp:
		if f.ArmedHWND == 0 {
			return pass("未武装（按下未被拦截）")
		}
		cancel := func(reason string) interceptDecision {
			return interceptDecision{Action: interceptSwallow, Reason: reason}
		}
		if f.TargetHWND != f.ArmedHWND {
			return cancel("抬起时已不在原窗口，取消手势")
		}
		if !lockedTarget {
			return cancel("抬起时应用已解除锁住，取消手势")
		}
		if f.NativeNoZone {
			return cancel("抬起时关闭区已不可用，取消手势")
		}
		z, ok := zoneForEvent()
		if !ok {
			return cancel("窗口度量不可用，取消手势")
		}
		if z.contains(f.X, f.Y) {
			return interceptDecision{Action: interceptSwallowHide, HideHWND: f.ArmedHWND, Reason: "命中关闭区，转入后台"}
		}
		return cancel("抬起时已拖出关闭区，取消手势")
	case interceptKeyDown:
		if f.AltF4 && f.ForegroundHWND != 0 && (f.ForegroundLockURL != "" || f.ForegroundBoxApp != "") {
			return interceptDecision{Action: interceptSwallowHide, HideHWND: f.ForegroundHWND, Reason: "Alt+F4 命中已锁窗口，转入后台"}
		}
		return pass("前台不是已锁窗口")
	default: // interceptKeyUp and anything unknown
		return pass("按键抬起")
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
// down, auto-repeat) is a silent no-op. A re-verification DROP
// is logged once with its reason (test6): in test5 a swallowed
// verdict that died here would have left no trace anywhere,
// and an untraceable chain cannot be field-diagnosed.
func executeInterceptHide(req interceptHideRequest) {
	if !servicesActive() {
		return
	}
	locks := webAppLockSet()
	if len(locks) == 0 && len(boxAppLockSet()) == 0 {
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
		logf("winmgr", "info", "关窗变藏：收到隐藏请求，但窗口 %d 已不在登记中，忽略（手势：%s）", req.HWND, req.Gesture)
		return
	}
	key := residentWindowLockURL(&cp, locks, names)
	if key == "" {
		// v0.71 test7: the native twin -- re-verify against the
		// attribution snapshot and the aligned-window book.
		if appID, ok := boxInterceptResolve(req.HWND); ok {
			if !platformWindowVisible(req.HWND) {
				return
			}
			boxDaemonHide(req.HWND, appID, req.Gesture)
			return
		}
		logf("winmgr", "info", "关窗变藏：收到隐藏请求，但窗口 %d 的应用已解除锁住，忽略（手势：%s）", req.HWND, req.Gesture)
		return
	}
	if !platformWindowVisible(req.HWND) {
		return // already in the background: nothing to do, nothing to log
	}
	daemonHideWindow(req.HWND, key, req.Gesture)
}

// ---------- decision telemetry (test6) ----------
//
// In test5 the whole chain failed open with NO record: the
// callback decided something (or never ran) and the console
// held no line either way, so the field paste could not
// convict the broken link. Now every decision the layer makes
// about a LOCKED app's window -- a caption-area button press
// or release, an Alt+F4 with a locked window in front -- is
// posted by the callback (a plain struct copy + PostMessage;
// the callback still never logs) to a worker that writes one
// console line with the full fact set: the point, the window
// rect, the visible frame, the window DPI, the computed close
// zone and the verdict with its reason. The next field round
// reads the verdict straight off the console.

type interceptTelemetry struct {
	Gesture string // 点击关闭按钮 / Alt+F4
	Phase   string // 按下 / 抬起 / 按键
	Facts   interceptFacts
	Zone    closeZone
	ZoneOK  bool
	Dec     interceptDecision
	// Native targets (test7, review §8): the attributed app,
	// the attribution basis, the frame kind and where the
	// close zone came from -- without them a native miss is as
	// undiagnosable as test5's web miss was.
	BoxApp    string
	Basis     string
	FrameKind string
	Source    string // "formula" | "probe" | "keyboard"
}

var (
	interceptTeleWork = make(chan interceptTelemetry, 32)
	interceptTeleOnce sync.Once
)

// interceptQueueTelemetry is the hook thread's hand-off for a
// telemetry record: buffered, non-blocking; a full queue drops
// the record (a dropped diagnosis line must never stall the
// input path it describes).
func interceptQueueTelemetry(t interceptTelemetry) {
	interceptTeleOnce.Do(func() { go interceptTelemetryWorker() })
	select {
	case interceptTeleWork <- t:
	default:
	}
}

func interceptTelemetryWorker() {
	for t := range interceptTeleWork {
		f := t.Facts
		verdict := "放行"
		switch t.Dec.Action {
		case interceptSwallowHide:
			verdict = "拦截（转入后台）"
		case interceptSwallow:
			verdict = "吞下"
		}
		frame := "无"
		if f.FrameOK {
			frame = fmt.Sprintf("(%d,%d)-(%d,%d)", f.FrameL, f.FrameT, f.FrameR, f.FrameB)
		}
		zone := "不可用"
		if t.ZoneOK {
			zone = fmt.Sprintf("(%d,%d)-(%d,%d)", t.Zone.X0, t.Zone.Y0, t.Zone.X1, t.Zone.Y1)
		}
		extra := ""
		if t.BoxApp != "" {
			extra = fmt.Sprintf("，归属应用 %s（依据：%s），窗口角色：主窗口，边框：%s，关闭区来源：%s",
				boxAppName(t.BoxApp), boxBasisText(t.Basis), frameKindText(t.FrameKind), zoneSourceText(t.Source))
		}
		logf("winmgr", "info", "关窗变藏判定（%s·%s）：窗口 %d，点 (%d,%d)，窗口矩形 (%d,%d)-(%d,%d)，可见边框 %s，窗口 DPI %d，关闭区 %s，结论 %s——%s%s",
			t.Gesture, t.Phase, f.TargetHWND, f.X, f.Y,
			f.RectL, f.RectT, f.RectR, f.RectB, frame, f.DpiWindow, zone, verdict, t.Dec.Reason, extra)
	}
}

// ---------- reconcile + health (patrol cadence) ----------

// interceptHookSilence bounds the watchdog's suspicion: a hook
// that has delivered NO event for this long while the
// foreground window changed inside the same window is presumed
// silently removed by the OS (LowLevelHooksTimeout removals
// come with no notification). Deliberately conservative -- a
// false reinstall costs a log line, a missed one costs the
// feature, but a hair-trigger would spam both. The stamps are
// PER HOOK (test6): test5 shared one stamp across both hooks,
// so a live keyboard hook could mask a dead mouse hook (and
// vice versa) indefinitely. Mouse input flows constantly in
// active use, so its suspicion threshold is tighter; keyboard
// input is bursty, so its threshold stays long and its
// reinstall is correspondingly rarer and cheaper to be wrong
// about.
const (
	interceptMouseSilence = 45 * time.Second
	interceptKeySilence   = 90 * time.Second
)

var interceptCtl = struct {
	sync.Mutex
	installed    bool // hooks up as of the last reconcile
	degraded     bool // install wanted but failing (settings shows it)
	fg           uint64
	fgChangedAt  time.Time
	reinstalled  time.Time
	everDegraded bool // the failure line logs once per failure streak
}{}

// interceptGapWarned remembers which live windows the patrol
// has already reported as missing from the hook snapshot, so
// the line fires once per window. Patrol-goroutine only.
var interceptGapWarned = map[uint64]bool{}

// interceptSnapshotGapCheck cross-checks the hook snapshot
// against the resident engine's own window matching (test6):
// if the engine can see a locked app's live window but the
// snapshot the callbacks read does not carry it, clicks on
// that window can never be intercepted -- say so once, on the
// console, instead of letting the miss stay invisible (the
// test5 failure mode).
func interceptSnapshotGapCheck(locks map[string]bool) {
	if len(locks) == 0 {
		interceptGapWarned = map[uint64]bool{}
		return
	}
	snap := interceptSnap.Load()
	names := residentNameIndex(locks)
	extMu.Lock()
	wins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		cp := *e
		wins = append(wins, &cp)
	}
	extMu.Unlock()
	seen := map[uint64]bool{}
	for u := range locks {
		w := findResidentWindow(wins, locks, names, u)
		if w == nil {
			continue
		}
		seen[w.HWND] = true
		if snap != nil {
			if _, ok := snap.locks[w.HWND]; ok {
				continue
			}
		}
		if !interceptGapWarned[w.HWND] {
			interceptGapWarned[w.HWND] = true
			logf("winmgr", "warn", "关窗变藏：已锁应用「%s」的窗口 %d 未进入监听快照，这个窗口的关闭动作不会被拦截（已记录待查）", residentName(u), w.HWND)
		}
	}
	for h := range interceptGapWarned {
		if !seen[h] {
			delete(interceptGapWarned, h)
		}
	}
}

// frameKindText / zoneSourceText render the native telemetry
// fact codes for the console line (test7, review §8).
func frameKindText(kind string) string {
	switch kind {
	case "standard":
		return "标准边框"
	case "selfdrawn":
		return "自绘边框"
	}
	return kind
}

func zoneSourceText(source string) string {
	switch source {
	case "formula":
		return "系统度量公式"
	case "probe":
		return "巡逻预探"
	case "keyboard":
		return "键盘路径（无几何）"
	}
	return source
}

// interceptPatrol runs at the top of every patrol tick (before
// the vault gate, so locking the vault also stands the hooks
// down): refresh the snapshot, reconcile the install with the
// lock set, watch for a silently-removed hook.
func interceptPatrol() {
	refreshInterceptSnapshot()
	locks := webAppLockSet()
	interceptSnapshotGapCheck(locks)
	want := interceptDesiredFor(runtime.GOOS, len(locks)+len(boxAppLockSet()), servicesActive())
	installed, failed := interceptPlatformSetWanted(want)
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
	// Watchdog: a hook silent past its threshold while the
	// user is plainly active (the foreground window changed
	// inside the silent window) -- presume the OS dropped
	// that hook and reinstall once. Per-hook stamps and
	// thresholds (see the constants above).
	if want && installed && !failed {
		fg := platformForegroundHWND()
		if fg != interceptCtl.fg {
			interceptCtl.fg = fg
			interceptCtl.fgChangedAt = now
		}
		mouseLast, keyLast := interceptPlatformHookEvents()
		suspect := ""
		if !mouseLast.IsZero() && now.Sub(mouseLast) > interceptMouseSilence &&
			!interceptCtl.fgChangedAt.IsZero() && now.Sub(interceptCtl.fgChangedAt) < interceptMouseSilence {
			suspect = "鼠标"
		} else if !keyLast.IsZero() && now.Sub(keyLast) > interceptKeySilence &&
			!interceptCtl.fgChangedAt.IsZero() && now.Sub(interceptCtl.fgChangedAt) < interceptKeySilence {
			suspect = "键盘"
		}
		if suspect != "" && now.Sub(interceptCtl.reinstalled) > interceptKeySilence {
			interceptCtl.reinstalled = now
			interceptCtl.Unlock()
			interceptPlatformReinstall()
			logf("winmgr", "warn", "关窗变藏：%s监听长时间无事件但前台窗口在变化，疑似被系统摘除，已重新安装", suspect)
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
