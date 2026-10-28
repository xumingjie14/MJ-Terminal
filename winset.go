package main

// ---------- v0.37: window etiquette （开窗礼仪） ----------
//
// Four behaviours the user signed off on, all standing on the
// v0.36 registry (the patrol's ownership verdicts are NOT touched
// here -- this file only decides what happens to a window once it
// is known to be ours):
//
//  1. Birth placement, equal for everyone: a window is landed
//     exactly once when it FIRST enters the registry -- patrol
//     claims included, not just daemon launches -- using the very
//     same landing geometry as v0.20-v0.23 (desktop rect, 62% x
//     85%, phone width in small-window mode). After that one
//     landing the window is the user's, forever.
//  2. Desktop backdrop: when a window is born while the user is in
//     terminal context (foreground = the desktop view or a claimed
//     window, or the daemon itself just opened it from the desktop
//     UI), the desktop view is slid directly underneath the new
//     window in z-order (NOACTIVATE only -- focus is never stolen),
//     so the window visibly opens ON the terminal's desktop. When
//     the user is busy in a host app, the window is only claimed,
//     quietly.
//  3. Minimize linkage: minimize the desktop view and every
//     currently-visible claimed window hides with it (the desktop
//     itself minimizes normally to the host taskbar); restore the
//     view and exactly the windows we hid come back. Windows the
//     user minimized individually are never touched.
//  4. Close to tray: closing the desktop view hides the whole set
//     and parks a tray icon in the host notification area; clicking
//     it (or double-clicking the EXE, as always) brings the view
//     and the whole set back.
//
// Safety line: every daemon exit path calls
// prepareWindowsForDaemonExit FIRST (see main.go / update.go), so
// a group-hidden set can never be stranded on the machine by a
// daemon that has gone away.
//
// All three decisions (placement, backdrop, visibility) are pure
// functions over plain facts -- winset_test.go pins them down. The
// platform layer only collects facts and performs the resulting
// window operations. Switches live in 设置 -> 桌面与 Dock
// (settings keys winPlaceOnClaim / winHideOnMinimize /
// winCloseToTray, all default on; the backdrop rides on the
// placement switch -- one less switch, one less confusion).

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- birth placement decision ----------

type birthFacts struct {
	Enabled    bool // 「新窗口自动落位」 switch
	Placed     bool // the one-shot landing is already spent
	Owned      bool // has an owner window: a dialog/palette class window
	SetHidden  bool // the whole set is currently group-hidden
	ShellReady bool // the desktop view exists and is not minimized
	// BornMaximized is deliberately NOT an exemption: a window
	// born maximized (Chrome inheriting its parent's state) is
	// the very symptom this version exists to cure. The platform
	// layer restores it first, then lands it like any other.
	BornMaximized bool
}

type birthAction int

const (
	birthPlaceIt birthAction = iota // land it now
	birthDefer                      // not now; retry on a later housekeeping pass
	birthResolve                    // never land it; stop offering (decision made)
)

// decideBirthPlacement rules on a registered window's first landing.
// Dialogs are the hard exception the user never sees a rule for:
// an owned window (file picker, palette) keeps the size and spot
// its owner gave it. A window born while the set is hidden is
// never landed either -- it follows the set into hiding instead
// (its one landing still waits for it when the set returns).
func decideBirthPlacement(f birthFacts) birthAction {
	if f.Placed || f.Owned || !f.Enabled {
		return birthResolve
	}
	if f.SetHidden || !f.ShellReady {
		return birthDefer
	}
	return birthPlaceIt
}

// ---------- backdrop decision ----------

type fgKind int

const (
	fgUnknown fgKind = iota
	fgShell          // foreground is the desktop view itself
	fgClaimed        // foreground is a registered (claimed) window
	fgHost           // foreground belongs to the host
)

type backdropFacts struct {
	Enabled       bool // rides on the placement switch
	Owned         bool // dialogs never get a backdrop
	SetHidden     bool
	ShellUsable   bool // shell exists and is not minimized
	LaunchContext bool // the daemon opened this window from the desktop UI just now
	Foreground    fgKind
}

// decideBackdrop: the backdrop lift happens only in terminal
// context. A daemon launch IS terminal context by construction
// (the user just clicked inside the desktop); a patrol claim
// inherits the context of whatever is foreground at claim time --
// which, for a popup the user just clicked open inside a claimed
// window, is that very popup (it is registered before we ask).
func decideBackdrop(f backdropFacts) bool {
	if !f.Enabled || f.Owned || f.SetHidden || !f.ShellUsable {
		return false
	}
	if f.LaunchContext {
		return true
	}
	return f.Foreground == fgShell || f.Foreground == fgClaimed
}

// ---------- whole-set visibility state machine ----------

type setVisState int

const (
	visShown setVisState = iota
	visHiddenMinimize
	visHiddenTray
)

type setVisEvent int

const (
	evShellMinimized setVisEvent = iota
	evShellRestored              // the shell is no longer minimized
	evShellClosed                // the shell window is gone (user closed the view)
	evShellOpened                // a (new) desktop view registered
	evRestoreAll                 // tray click / Stage Manager click / switch rescued off
)

type setVisSwitches struct {
	MinimizeLink bool
	CloseToTray  bool
}

type setVisStep struct {
	Next       setVisState
	HideSet    bool
	RestoreSet bool
	TrayDelta  int // +1 park the tray icon, -1 remove it, 0 leave it
}

// stepSetVis is the whole state machine in one place. Note the two
// asymmetric closes the user specified: minimizing keeps the set
// findable through the shell's own taskbar button, while closing
// the view retreats everything into the notification area.
// Closing from the minimized state keeps the set hidden and just
// moves its "home" from the taskbar to the tray.
func stepSetVis(cur setVisState, ev setVisEvent, sw setVisSwitches) setVisStep {
	stay := setVisStep{Next: cur}
	switch cur {
	case visShown:
		switch ev {
		case evShellMinimized:
			if sw.MinimizeLink {
				return setVisStep{Next: visHiddenMinimize, HideSet: true}
			}
		case evShellClosed:
			if sw.CloseToTray {
				return setVisStep{Next: visHiddenTray, HideSet: true, TrayDelta: 1}
			}
		}
	case visHiddenMinimize:
		switch ev {
		case evShellRestored, evRestoreAll, evShellOpened:
			return setVisStep{Next: visShown, RestoreSet: true}
		case evShellClosed:
			if sw.CloseToTray {
				return setVisStep{Next: visHiddenTray, TrayDelta: 1}
			}
			// No tray retreat: the set was only lent to the
			// minimize, and the minimize is over.
			return setVisStep{Next: visShown, RestoreSet: true}
		}
	case visHiddenTray:
		switch ev {
		case evRestoreAll, evShellOpened:
			return setVisStep{Next: visShown, RestoreSet: true, TrayDelta: -1}
		}
	}
	return stay
}

// ---------- runtime state ----------

var (
	grpMu          sync.Mutex
	grpVis         = visShown
	grpHidden      = map[uint64]bool{} // windows WE hid; only these are ours to restore
	grpExiting     bool                // daemon is on an exit path: the machine freezes
	grpShellIconic bool                // last observed iconic state of the shell window
)

var (
	trackedShellHWND atomic.Uint64 // the current desktop view window, as claimed
	shellGonePending atomic.Bool   // the registry prune saw the shell die; watcher's to consume
)

func grpIsHidden() bool {
	grpMu.Lock()
	defer grpMu.Unlock()
	return grpVis != visShown
}

// shellHWND returns the desktop view's window handle: the tracked
// one when known, else the registry's shell entry (adopted on the
// way out).
func shellHWND() uint64 {
	if h := trackedShellHWND.Load(); h != 0 {
		return h
	}
	extMu.Lock()
	defer extMu.Unlock()
	for h, e := range extWins {
		if e.Shell {
			trackedShellHWND.Store(h)
			return h
		}
	}
	return 0
}

// ---------- set hide / restore ----------

// grpHideSet hides every claimed window that is currently up on
// the host desktop and books exactly those into grpHidden. A
// window the user minimized on their own (iconic) or that is
// already invisible is skipped: its state is its own business,
// and group restore must never resurrect what it did not hide.
func grpHideSet() {
	extMu.Lock()
	hwnds := make([]uint64, 0, len(extWins))
	for h, e := range extWins {
		if !e.Shell {
			hwnds = append(hwnds, h)
		}
	}
	extMu.Unlock()
	n := 0
	for _, h := range hwnds {
		if !platformWindowExists(h) || !platformWindowVisible(h) || platformWindowIconic(h) {
			continue
		}
		if platformHideWindow(h) {
			grpMu.Lock()
			grpHidden[h] = true
			grpMu.Unlock()
			n++
		}
	}
	if n > 0 {
		logf("winmgr", "info", "开窗礼仪：整套窗口联动隐藏（%d 个）", n)
	}
}

// grpRestoreSet brings back exactly the windows grpHideSet hid
// and clears the book. Dead windows are skipped silently -- a
// window that closed while hidden simply never comes back, which
// is what its owner wanted anyway.
func grpRestoreSet() {
	grpMu.Lock()
	set := make([]uint64, 0, len(grpHidden))
	for h := range grpHidden {
		set = append(set, h)
	}
	grpHidden = map[uint64]bool{}
	grpMu.Unlock()
	n := 0
	for _, h := range set {
		if platformWindowExists(h) && platformShowBackWindow(h) {
			n++
		}
	}
	if n > 0 {
		logf("winmgr", "info", "开窗礼仪：整套窗口恢复显示（%d 个）", n)
	}
}

// grpEnforceHidden is the patrol's consistency pass: while the set
// is tucked away, any registered window found visible on the host
// desktop (claimed a beat before the hide, or re-shown by its own
// app) is hidden and booked. Findability is unaffected -- the set
// comes back as a whole.
func grpEnforceHidden() {
	if !grpIsHidden() {
		return
	}
	extMu.Lock()
	hwnds := make([]uint64, 0, len(extWins))
	for h, e := range extWins {
		if !e.Shell {
			hwnds = append(hwnds, h)
		}
	}
	extMu.Unlock()
	for _, h := range hwnds {
		grpMu.Lock()
		_, booked := grpHidden[h]
		grpMu.Unlock()
		if booked {
			continue
		}
		if platformWindowExists(h) && platformWindowVisible(h) && !platformWindowIconic(h) && platformHideWindow(h) {
			grpMu.Lock()
			grpHidden[h] = true
			grpMu.Unlock()
		}
	}
}

// ---------- state machine driver ----------

func grpApplyEvent(ev setVisEvent) {
	sw := setVisSwitches{
		MinimizeLink: settingWinHideOnMinimize(),
		CloseToTray:  settingWinCloseToTray(),
	}
	grpMu.Lock()
	if grpExiting {
		grpMu.Unlock()
		return
	}
	step := stepSetVis(grpVis, ev, sw)
	grpVis = step.Next
	grpMu.Unlock()
	if step.HideSet {
		grpHideSet()
	}
	if step.RestoreSet {
		grpRestoreSet()
	}
	switch {
	case step.TrayDelta > 0:
		platformTraySetVisible(true)
		logf("winmgr", "info", "开窗礼仪：桌面关闭，整套窗口已退到系统托盘（点托盘图标或双击程序恢复）")
	case step.TrayDelta < 0:
		platformTraySetVisible(false)
		logf("winmgr", "info", "开窗礼仪：已离开托盘隐藏状态")
	}
}

// grpShellOpened notes a freshly claimed desktop view: it becomes
// the tracked shell, and if the set was parked (tray above all),
// a new view opening IS the restore the user asked for.
func grpShellOpened(hwnd uint64) {
	trackedShellHWND.Store(hwnd)
	grpMu.Lock()
	grpShellIconic = platformWindowIconic(hwnd)
	grpMu.Unlock()
	grpApplyEvent(evShellOpened)
}

// noteShellWindowGone is called by the registry prune when the
// shell's entry dies. The prune owns the alive checks, so this
// only records the fact; the watcher consumes it (a beat later,
// and only if no new shell has taken over meanwhile).
func noteShellWindowGone(hwnd uint64) {
	if trackedShellHWND.CompareAndSwap(hwnd, 0) {
		shellGonePending.Store(true)
	}
}

// grpRestoreForFocus implements the Stage Manager click rule the
// user picked: clicking a listed window while the set is tucked
// away restores the WHOLE set first (and un-minimizes the
// desktop), then the caller raises the target. One predictable
// gesture instead of a window floating alone on the host desktop.
func grpRestoreForFocus() {
	grpMu.Lock()
	vis := grpVis
	grpMu.Unlock()
	if vis == visShown {
		return
	}
	grpApplyEvent(evRestoreAll)
	if vis == visHiddenMinimize {
		if shell := shellHWND(); shell != 0 {
			platformRestoreShellWindow(shell)
		}
	}
}

// prepareWindowsForDaemonExit is this feature's hard safety line,
// called FIRST on every daemon exit path （设置退出 / 托盘退出 /
// 空闲自动退出 / 更新交接）: whatever the set's state, every
// group-hidden window is made visible again before the process
// goes, and the tray icon is removed. Nothing the daemon hid may
// outlive the daemon.
func prepareWindowsForDaemonExit() {
	grpMu.Lock()
	grpExiting = true
	was := grpVis
	grpVis = visShown
	grpMu.Unlock()
	platformTraySetVisible(false)
	if was != visShown {
		grpRestoreSet()
		logf("winmgr", "info", "开窗礼仪：守护进程退出前已恢复全部联动隐藏的窗口")
	}
}

// ---------- shell watcher ----------

// startShellWatcher drives components 3 and 4: it watches the
// desktop view's own state (minimize / restore / death) at a
// snappier beat than the 2 s patrol, because linkage lag is
// something the user can feel. Off Windows the tracked shell
// never registers and every tick is a no-op.
func startShellWatcher() {
	go func() {
		for range time.Tick(300 * time.Millisecond) {
			shellWatchTick()
		}
	}()
}

func shellWatchTick() {
	grpMu.Lock()
	exiting := grpExiting
	vis := grpVis
	grpMu.Unlock()
	if exiting {
		return
	}
	// A switch rescued off while its state is active brings the
	// set straight back. (The desktop view may itself be the
	// hidden thing; remote-assist settings can still reach this.)
	if vis == visHiddenTray && !settingWinCloseToTray() {
		grpApplyEvent(evRestoreAll)
		return
	}
	if vis == visHiddenMinimize && !settingWinHideOnMinimize() {
		grpApplyEvent(evRestoreAll)
		return
	}
	// Shell death arrives via the registry prune. Consume it only
	// while no new shell has registered in the meantime -- a quick
	// close-and-reopen must not hide the set under the new view.
	if shellGonePending.Load() {
		shellGonePending.Store(false)
		if trackedShellHWND.Load() == 0 {
			grpApplyEvent(evShellClosed)
		}
		return
	}
	hwnd := trackedShellHWND.Load()
	if hwnd == 0 {
		return
	}
	// While minimized the shell's registry entry persists, so it
	// is a stable identity witness; a missing entry means the
	// death path above owns the story.
	extMu.Lock()
	e, ok := extWins[hwnd]
	isShell := ok && e.Shell
	extMu.Unlock()
	if !isShell {
		return
	}
	iconic := platformWindowIconic(hwnd)
	grpMu.Lock()
	was := grpShellIconic
	grpShellIconic = iconic
	grpMu.Unlock()
	if iconic && !was {
		grpApplyEvent(evShellMinimized)
	} else if !iconic && was {
		grpApplyEvent(evShellRestored)
	}
}

// ---------- birth settlement (placement + backdrop) ----------

// settleNewWindow runs once, at the moment a window enters the
// registry (either claim path). If the set is tucked away the
// newborn follows it into hiding at once -- a terminal window
// must never sit alone on the host desktop while its set is
// hidden. Otherwise it gets its one landing, and the backdrop
// lift when the context calls for it.
func settleNewWindow(hwnd uint64, launchCtx bool) {
	if grpIsHidden() {
		if platformWindowExists(hwnd) && platformWindowVisible(hwnd) && !platformWindowIconic(hwnd) && platformHideWindow(hwnd) {
			grpMu.Lock()
			grpHidden[hwnd] = true
			grpMu.Unlock()
			extMu.Lock()
			label := ""
			if e, ok := extWins[hwnd]; ok {
				label = e.App
			}
			extMu.Unlock()
			logf("winmgr", "info", "开窗礼仪：整套隐藏期间的新窗口已跟随隐藏：%s", label)
		}
		return
	}
	doBirthPlacement(hwnd)
	shell := shellHWND()
	usable := shell != 0 && shell != hwnd && platformWindowExists(shell) && !platformWindowIconic(shell)
	if usable && decideBackdrop(backdropFacts{
		Enabled:       settingWinPlaceOnClaim(),
		Owned:         platformWindowOwned(hwnd),
		SetHidden:     false,
		ShellUsable:   true,
		LaunchContext: launchCtx,
		Foreground:    classifyForeground(shell),
	}) {
		if platformRaiseBackdrop(hwnd, shell) {
			extMu.Lock()
			label := ""
			if e, ok := extWins[hwnd]; ok {
				label = e.App
			}
			extMu.Unlock()
			logf("winmgr", "info", "开窗礼仪：桌面已垫到新窗口之后：%s", label)
		}
	}
}

// classifyForeground says whose context the user is in right now:
// the desktop's, a claimed window's, or the host's.
func classifyForeground(shell uint64) fgKind {
	fg := platformForegroundHWND()
	if fg == 0 {
		return fgUnknown
	}
	if fg == shell {
		return fgShell
	}
	extMu.Lock()
	_, ok := extWins[fg]
	extMu.Unlock()
	if ok {
		return fgClaimed
	}
	return fgHost
}

// doBirthPlacement performs (or resolves) the one-shot landing
// for a registered window. It is also the prune retry path
// (tryPlaceExtWindow), so a landing deferred by a missing or
// minimized desktop still happens once a frame exists -- the
// same contract the launched windows have had since v0.20.
func doBirthPlacement(hwnd uint64) bool {
	extMu.Lock()
	e, ok := extWins[hwnd]
	if !ok || e.Shell {
		extMu.Unlock()
		return false
	}
	app, title, already := e.App, e.Title, e.placed
	extMu.Unlock()
	shell := shellHWND()
	shellReady := shell != 0 && shell != hwnd && platformWindowExists(shell) && !platformWindowIconic(shell)
	act := decideBirthPlacement(birthFacts{
		Enabled:       settingWinPlaceOnClaim(),
		Placed:        already,
		Owned:         platformWindowOwned(hwnd),
		SetHidden:     grpIsHidden(),
		ShellReady:    shellReady,
		BornMaximized: platformWindowZoomed(hwnd),
	})
	switch act {
	case birthResolve:
		// "Never" is a decision too: mark it spent so the
		// housekeeping retries stop offering this window.
		extMu.Lock()
		if e2, ok := extWins[hwnd]; ok {
			e2.placed = true
		}
		extMu.Unlock()
		return false
	case birthDefer:
		return false
	}
	sl, st, sr, sb, ok := platformWindowRect(shell)
	if !ok {
		return false
	}
	tl, tt, tr, tb, phone := placeTargetRectAuto(sl, st, sr, sb, platformWindowZoomed(shell))
	if !platformPlaceWindow(hwnd, tl, tt, tr, tb) {
		return false
	}
	extMu.Lock()
	if e2, ok := extWins[hwnd]; ok {
		e2.placed = true
	}
	extMu.Unlock()
	name := app
	if title != "" {
		name = title
	}
	if phone {
		logf("winmgr", "info", "外部窗口落位（小窗手机宽）：%s → %d×%d", name, tr-tl, tb-tt)
	} else {
		logf("winmgr", "info", "外部窗口落位：%s → %d×%d", name, tr-tl, tb-tt)
	}
	return true
}

// ---------- tray hooks (called from tray_windows.go) ----------

// onTrayIconRestore is the tray icon's click: the whole set comes
// back, and the desktop view reopens through the daemon's own
// openshell endpoint (the same path a double-click on the EXE
// takes, proxy decision included).
func onTrayIconRestore() {
	grpApplyEvent(evRestoreAll)
	if daemonPort <= 0 {
		return
	}
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Post(daemonURLForPort(daemonPort)+"api/openshell", "application/json", nil)
	if err == nil {
		_ = resp.Body.Close()
	}
}

// onTrayIconExit quits the daemon through /api/shutdown itself --
// literally the same path the Settings app's 退出系统 uses, so the
// exit choreography (unhide-all first, then core/openclaw/remote
// shutdown) can never drift apart between the two entries.
func onTrayIconExit() {
	if daemonPort <= 0 {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(daemonURLForPort(daemonPort)+"api/shutdown", "application/json", nil)
	if err == nil {
		_ = resp.Body.Close()
	}
}
