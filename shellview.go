package main

// ---------- v0.38: desktop view singleton + tray memory ----------
//
// Two field complaints about the desktop VIEW (the Edge --app
// window the whole desktop lives in):
//
//  1. Multi-open: double-clicking the terminal on the host
//     desktop could stack up several desktop pages. Diagnosis:
//     the open path had no find-or-create at all -- every
//     /api/openshell (and every tray click, which posts to it)
//     unconditionally spawned a new Edge window, and Edge with
//     the same ShellProfile happily opens one window per launch
//     in the shared profile process. Rapid double-clicks raced
//     the same way, because claiming (trackLaunch) is async and
//     nothing remembered a creation was already in flight.
//
//  2. Tray amnesia: reopening from the tray felt like a page
//     refresh -- because it WAS one. The old view was destroyed
//     (the Edge frame's own X), so the "reopen" built a brand-new
//     page: default geometry, in-system windows gone, the
//     工作站 back at its first-run state.
//
// The fixes: every open entry (EXE launcher -> /api/openshell,
// tray icon, tray menu) funnels into ensureShellView, a
// find-or-create that raises a live view, restores a tucked-away
// one, and only ever creates when the view is truly dead -- with
// an in-flight latch so a burst of clicks still creates at most
// one. And the desktop page grows its own red traffic-light
// button: clicking it HIDES the view (SW_HIDE) instead of
// destroying it, so the return is the same window, same page,
// same place -- zero reload. The Edge frame's native X remains
// uninterceptable without hooks (project rule: no injection), so
// for that path the daemon remembers the view's last frame and
// the page keeps a session ledger of its in-system windows; a
// rebuilt view lands back where it stood, with its windows
// restored (see restoreSession in index.html).

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// ---------- find-or-create verdict (pure) ----------

type shellFacts struct {
	Registered bool // a shell entry exists in the window registry
	Exists     bool // that handle is still a live window
	Visible    bool // WS_VISIBLE (false after the red button's SW_HIDE)
	Iconic     bool // minimized
	SetHidden  bool // the winset state machine has the whole set tucked away
}

type shellOpenVerdict int

const (
	shellRaiseExisting shellOpenVerdict = iota // alive and on screen: un-minimize + shared raise
	shellRestoreTucked                         // alive but tucked away: restore the set, show, raise
	shellCreateNew                             // no live view: launch exactly one
)

// decideShellOpen is the whole singleton rule in one place. A
// view that exists is NEVER joined by a second one; a view that
// is merely hidden or minimized is the user's own desktop coming
// back, not a reason to build another.
func decideShellOpen(f shellFacts) shellOpenVerdict {
	if !f.Registered || !f.Exists {
		return shellCreateNew
	}
	if f.SetHidden || !f.Visible {
		return shellRestoreTucked
	}
	return shellRaiseExisting
}

// ---------- creation in-flight latch ----------
//
// Claiming a spawned view takes a moment (trackLaunch polls), so
// "no registry entry yet" does not mean "nobody is creating
// one". The latch bridges that gap: a creation started less than
// shellCreateWindow ago suppresses further creations; the claim
// (grpShellOpened) clears it the moment the view registers.

const shellCreateWindow = 12 * time.Second

var (
	shellCreateMu sync.Mutex
	shellCreateAt time.Time
)

func shellCreateInFlightAt(start, now time.Time) bool {
	return !start.IsZero() && now.Sub(start) < shellCreateWindow
}

func shellCreateInFlight() bool {
	shellCreateMu.Lock()
	defer shellCreateMu.Unlock()
	return shellCreateInFlightAt(shellCreateAt, time.Now())
}

func markShellCreateStart() {
	shellCreateMu.Lock()
	shellCreateAt = time.Now()
	shellCreateMu.Unlock()
}

func clearShellCreateInFlight() {
	shellCreateMu.Lock()
	shellCreateAt = time.Time{}
	shellCreateMu.Unlock()
}

// ---------- view geometry memory ----------
//
// The watcher samples the view's frame while it is up and
// healthy; noteShellGeometry's guards keep only sane, on-screen
// frames (a minimized or hidden view reports a parking-lot rect
// or nothing at all -- neither is a place worth returning to).

var (
	shellGeomMu     sync.Mutex
	shellLastRect   [4]int32
	shellLastZoomed bool
	shellLastSeen   bool
)

func noteShellGeometry(hwnd uint64) {
	// v0.45: while the view is in immersion fullscreen its frame
	// is the monitor itself -- sampling it would poison the
	// memory with a "remembered" frame that IS the fullscreen,
	// and exiting fullscreen would have nowhere to return to.
	if shellFullscreenActive() {
		return
	}
	if hwnd == 0 || !platformWindowExists(hwnd) || !platformWindowVisible(hwnd) || platformWindowIconic(hwnd) {
		return
	}
	l, t, r, b, ok := platformWindowRect(hwnd)
	if !ok || r-l < 200 || b-t < 200 {
		return
	}
	shellGeomMu.Lock()
	shellLastRect = [4]int32{l, t, r, b}
	shellLastZoomed = platformWindowZoomed(hwnd)
	shellLastSeen = true
	shellGeomMu.Unlock()
}

func rememberedShellGeometry() (rect [4]int32, zoomed bool, seen bool) {
	shellGeomMu.Lock()
	defer shellGeomMu.Unlock()
	return shellLastRect, shellLastZoomed, shellLastSeen
}

// applyRememberedShellGeometry lands a freshly claimed view back
// in the frame its predecessor died in -- only when that frame
// was a floating one. A predecessor that was maximized needs
// nothing: the launch itself starts maximized, exactly as
// remembered.
func applyRememberedShellGeometry(hwnd uint64) {
	rect, zoomed, seen := rememberedShellGeometry()
	if !seen || zoomed {
		return
	}
	if platformPlaceWindow(hwnd, rect[0], rect[1], rect[2], rect[3]) {
		logf("winmgr", "info", "托盘记忆：新桌面视图已回到上次的位置（%d×%d）", rect[2]-rect[0], rect[3]-rect[1])
	}
}

// ---------- the single door ----------

var shellEnsureMu sync.Mutex // serializes find-or-create decisions

// ensureShellView is the ONE way a desktop view is ever opened:
// EXE launcher, tray icon and tray menu all arrive here (through
// /api/openshell). It returns what it did, for the log and the
// caller: "raised" / "restored" / "created" / "creating".
func ensureShellView(url string, proxy bool) (string, error) {
	shellEnsureMu.Lock()
	defer shellEnsureMu.Unlock()

	hwnd := shellHWND()
	registered := false
	if hwnd != 0 {
		extMu.Lock()
		_, registered = extWins[hwnd]
		extMu.Unlock()
	}
	f := shellFacts{Registered: registered, SetHidden: grpIsHidden()}
	if registered {
		f.Exists = platformWindowExists(hwnd)
		f.Visible = platformWindowVisible(hwnd)
		f.Iconic = platformWindowIconic(hwnd)
	}
	switch decideShellOpen(f) {
	case shellRaiseExisting:
		if f.Iconic {
			platformRestoreShellWindow(hwnd)
		}
		platformFocusWindow(hwnd)
		logf("winmgr", "info", "桌面视图单例：视图已在运行，已提到最前（未新建）")
		return "raised", nil
	case shellRestoreTucked:
		// Set first (it un-hides the claimed windows and retires
		// the tray icon), then the view itself, then the raise --
		// the same shared focus function as everywhere else.
		grpApplyEvent(evRestoreAll)
		grpShowShell(hwnd)
		platformFocusWindow(hwnd)
		logf("winmgr", "info", "桌面视图单例：已唤回藏起的视图（同一扇窗，未新建、未重载）")
		return "restored", nil
	}
	if shellCreateInFlight() {
		logf("winmgr", "info", "桌面视图单例：上一扇视图正在打开中，本次点击已合并")
		return "creating", nil
	}
	markShellCreateStart()
	browser, before, err := spawnShellWindow(url, proxy)
	if err != nil {
		clearShellCreateInFlight()
		return "", err
	}
	if browser == "" {
		// The OS default browser took the URL (no shell browser
		// installed): no view exists to claim, nothing in flight.
		clearShellCreateInFlight()
		return "created", nil
	}
	// Claim it like every shell before it (unified AppID, and
	// grpShellOpened on registration clears the in-flight latch,
	// restores the set if it was parked, and lands the view back
	// in its remembered frame).
	trackBrowserLaunch("铭杰工作站", "", browser, before, true)
	return "created", nil
}

// ---------- local endpoints (127.0.0.1 mux only) ----------

// shellHideHandler serves POST /api/shell/hide: the desktop
// page's own red traffic-light button. With the close-to-tray
// switch on (the default), the view is HIDDEN, not destroyed --
// the Edge process and the page live on untouched, the whole set
// follows it into the tray state (v0.37 machinery), and the
// return is the same window in the same place. With the switch
// off the user opted out of the tray retreat, so the button does
// the honest old thing and really closes the view (a plain
// WM_CLOSE, no hooks); the geometry/session memory still makes
// its eventual return faithful.
func shellHideHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "POST only"})
		return
	}
	hwnd := shellHWND()
	if hwnd == 0 || !platformWindowExists(hwnd) {
		writeJSON(w, map[string]any{"error": "桌面视图现在不在"})
		return
	}
	if settingWinCloseToTray() {
		platformHideWindow(hwnd)
		grpApplyEvent(evShellClosed)
		logf("winmgr", "info", "桌面视图已藏到托盘（红钮：藏而不关，进程与页面原样保留）")
	} else {
		platformCloseWindow(hwnd)
		logf("winmgr", "info", "桌面视图已关闭（关闭到托盘开关为关，红钮走真实关闭）")
	}
	writeJSON(w, map[string]any{"ok": true})
}

// shellRestoredHandler serves POST /api/shell/restored {wins:n}:
// the rebuilt desktop page reports how many in-system windows
// its session ledger brought back, purely so the restore lands
// in the diagnostics hub like everything else.
func shellRestoredHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "POST only"})
		return
	}
	var body struct {
		Wins int `json:"wins"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Wins < 0 || body.Wins > 64 {
		writeJSON(w, map[string]any{"error": "参数不对"})
		return
	}
	if body.Wins > 0 {
		logf("browser", "info", "托盘记忆：重建的桌面已按会话账本恢复系统内窗口 %d 个", body.Wins)
	}
	writeJSON(w, map[string]any{"ok": true})
}
