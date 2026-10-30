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
	"os"
	"path/filepath"
	"sort"
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
	// v0.52: the boss key hid the whole set while one of our
	// windows was foreground (bosskey.go). Distinct from the tray
	// state in origin only -- the icon is parked and the same
	// evRestoreAll / evShellOpened gestures bring the set back --
	// because the exit and patrol bookkeeping must treat all
	// three hidden states alike, and the boss key must be able
	// to tell "I hid this" from "the view was closed".
	visHiddenBoss
)

type setVisEvent int

const (
	evShellMinimized setVisEvent = iota
	evShellRestored              // the shell is no longer minimized
	evShellClosed                // the shell window is gone (user closed the view)
	evShellOpened                // a (new) desktop view registered
	evRestoreAll                 // tray click / Stage Manager click / switch rescued off
	evBossKeyHide                // v0.52: boss key pressed while our window is foreground
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
		case evBossKeyHide:
			// v0.52: the boss key is its own gesture -- it does
			// NOT ride on the minimize / close-to-tray switches.
			// The tray icon parks unconditionally: it is the
			// anchor the Settings copy promises ("点托盘图标
			// 即可回来") while the set is boss-hidden.
			return setVisStep{Next: visHiddenBoss, HideSet: true, TrayDelta: 1}
		}
	case visHiddenMinimize:
		switch ev {
		case evShellRestored, evRestoreAll, evShellOpened:
			return setVisStep{Next: visShown, RestoreSet: true}
		case evBossKeyHide:
			// v0.52 corner: the shell is minimized and the set
			// lent to it, but a window the user had minimized
			// individually beforehand was restored by hand and
			// is foreground -- the boss key must still hide
			// what is up. The set's home moves to the boss
			// state (and its tray anchor); grpHidden keeps the
			// minimize-lent windows booked throughout.
			return setVisStep{Next: visHiddenBoss, HideSet: true, TrayDelta: 1}
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
	case visHiddenBoss:
		switch ev {
		case evRestoreAll, evShellOpened:
			return setVisStep{Next: visShown, RestoreSet: true, TrayDelta: -1}
		case evShellClosed:
			// The view died while boss-hidden (crash / killed):
			// the set stays hidden, the parked icon stays, only
			// the "home" moves to tray semantics. grpHidden is
			// untouched -- restore still returns the set.
			return setVisStep{Next: visHiddenTray}
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

// grpIsBossHidden reports whether the set is currently tucked
// away by the boss key specifically (v0.52).
func grpIsBossHidden() bool {
	grpMu.Lock()
	defer grpMu.Unlock()
	return grpVis == visHiddenBoss
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
		logf("winmgr", "info", "开窗礼仪：整套窗口已隐藏，托盘图标已停靠（点托盘图标或双击程序恢复）")
	case step.TrayDelta < 0:
		platformTraySetVisible(false)
		logf("winmgr", "info", "开窗礼仪：整套窗口已恢复显示，托盘图标已撤下")
	}
}

// grpShellOpened notes a freshly claimed desktop view: it becomes
// the tracked shell, and if the set was parked (tray above all),
// a new view opening IS the restore the user asked for.
func grpShellOpened(hwnd uint64) {
	trackedShellHWND.Store(hwnd)
	clearShellCreateInFlight()
	grpMu.Lock()
	grpShellIconic = platformWindowIconic(hwnd)
	grpMu.Unlock()
	grpApplyEvent(evShellOpened)
	// v0.38: if the previous view died with a remembered
	// non-maximized frame, this new view lands back in it instead
	// of the launch default (tray memory's geometry half).
	applyRememberedShellGeometry(hwnd)
	// v0.45: a view born while 桌面全屏 is on goes fullscreen
	// (the saved original frame is its landing geometry, just
	// restored -- entering fullscreen re-saves it from there).
	applyShellFullscreenState()
}

// grpShowShell brings a LIVE desktop view back onto the screen:
// un-hides it if the red button tucked it away, un-minimizes it
// if it was lent to the minimize linkage. The view process and
// its page are untouched -- this is the zero-reload return the
// tray-memory fix exists for.
func grpShowShell(hwnd uint64) {
	if hwnd == 0 || !platformWindowExists(hwnd) {
		return
	}
	if !platformWindowVisible(hwnd) {
		platformShowBackWindow(hwnd)
	}
	if platformWindowIconic(hwnd) {
		platformRestoreShellWindow(hwnd)
	}
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
	// v0.45: immersion styles come off FIRST, while every window
	// is still exactly where the set machine left it; the
	// group-restore below then shows them all already back on
	// the host taskbar.
	immerseRestoreAllForExit()
	// v0.54: the isolated-Chrome DWM frame colours come off with
	// the immersion styles -- nothing the daemon restyled may
	// outlive it either.
	chromeFrameRestoreAll()
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

// ---------- v0.48: exit closes the terminal's apps ----------
//
// A user-initiated full exit （设置退出 / 托盘退出 / 顶栏关机） --
// and a vault seal, which needs the files closed -- takes the
// apps the terminal opened DOWN with it instead of leaving them
// running on the host. Choreography: polite WM_CLOSE to every
// registered window first (an app gets its chance to save and
// quit on its own), a ~5 s grace poll, then termination for
// whatever of OURS is still alive.
//
// The red line is the v0.36 ownership discipline: the kill set
// is built ONLY from (a) registered windows' PIDs, (b) the spawn
// ledger's live roots, (c) Sandboxie's in-box PID list, expanded
// DOWNWARD through parent->child links. A host program the
// terminal never launched or claimed can never be in the set,
// whatever it is named.

// computeKillSet expands roots downward over the process facts
// (children, grandchildren, ...). The daemon itself and the
// kernel PIDs are excluded even if listed as roots. Pure: the
// tests pin the tree behaviour.
func computeKillSet(procs []procFact, roots map[uint32]bool, selfPID uint32) map[uint32]bool {
	children := map[uint32][]uint32{}
	alive := map[uint32]bool{}
	for _, p := range procs {
		alive[p.PID] = true
		if p.PPID != 0 && p.PPID != p.PID {
			children[p.PPID] = append(children[p.PPID], p.PID)
		}
	}
	out := map[uint32]bool{}
	queue := []uint32{}
	for pid := range roots {
		if pid == selfPID || pid <= 4 || !alive[pid] {
			continue
		}
		if !out[pid] {
			out[pid] = true
			queue = append(queue, pid)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, ch := range children[cur] {
			if ch == selfPID || ch <= 4 || out[ch] {
				continue
			}
			out[ch] = true
			queue = append(queue, ch)
		}
	}
	return out
}

// closeAppsForExit performs the polite-close + terminate
// sequence over the registered set and the daemon's own process
// trees. Best effort throughout: every step logs what it did
// (module system) and nothing here may block the exit for long.
func closeAppsForExit() {
	// 1) Polite close for every registered window.
	extMu.Lock()
	wins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		wins = append(wins, e)
	}
	extMu.Unlock()
	closed := 0
	for _, e := range wins {
		if platformCloseWindow(e.HWND) {
			closed++
		}
	}
	if closed > 0 {
		logf("system", "info", "退出关应用：已向 %d 个在册窗口发出关闭请求，等它们自己收尾", closed)
	}
	// 2) Grace poll, up to ~5 s: done when every registered
	// window is gone.
	for i := 0; i < 10 && len(wins) > 0; i++ {
		alive := 0
		for _, e := range wins {
			if platformWindowExists(e.HWND) {
				alive++
			}
		}
		if alive == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	// 3) Terminate what is still ours. Roots: live spawn-ledger
	// entries + registered window PIDs + the box PID list.
	procs := platformProcessSnapshot()
	aliveSet := map[uint32]bool{}
	imageByPID := map[uint32]string{}
	for _, p := range procs {
		aliveSet[p.PID] = true
		imageByPID[p.PID] = p.ImagePath
	}
	roots := spawnSeedPIDs(aliveSet)
	for _, e := range wins {
		if e.pid != 0 {
			roots[e.pid] = true
		}
	}
	for _, pid := range sbieBoxPids() {
		roots[uint32(pid)] = true
	}
	kill := computeKillSet(procs, roots, uint32(os.Getpid()))
	// Sandboxie's own terminate-all is the authoritative box
	// cleanup on top of the per-PID pass below.
	sbieTerminateBox()
	// 4) Re-verify before killing: a PID recycled by the host in
	// the meantime shows a different image path -- skip those.
	procs2 := platformProcessSnapshot()
	image2 := map[uint32]string{}
	for _, p := range procs2 {
		image2[p.PID] = p.ImagePath
	}
	var killed []string
	for pid := range kill {
		if img, ok := image2[pid]; !ok || img != imageByPID[pid] {
			continue
		}
		proc, err := os.FindProcess(int(pid))
		if err != nil {
			continue
		}
		if err := proc.Kill(); err == nil {
			name := filepath.Base(imageByPID[pid])
			if name == "" || name == "." {
				name = "未知程序"
			}
			killed = append(killed, name)
		}
	}
	sort.Strings(killed)
	for _, n := range killed {
		logf("system", "info", "退出关应用：已终止 %s", n)
	}
	if len(killed) > 0 {
		waitBoxEmpty(2 * time.Second)
		logf("system", "info", "退出关应用：共终止 %d 个由终端机启动的进程", len(killed))
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
	if !servicesActive() {
		return // v0.48: vault locked (or sealing) -- hands off
	}
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
	// v0.38: keep the view's last good frame on record (its own
	// guard skips hidden/minimized states), so a view that later
	// dies for real can be rebuilt where it stood.
	noteShellGeometry(hwnd)
}

// ---------- birth settlement (placement + backdrop) ----------

// settleNewWindow runs once, at the moment a window enters the
// registry (either claim path). If the set is tucked away the
// newborn follows it into hiding at once -- a terminal window
// must never sit alone on the host desktop while its set is
// hidden.
//
// v0.38 reorders the visible path around a field failure: windows
// born from inside the terminal often never surfaced -- the user
// had to click their Stage Manager entry by hand. The causes, in
// the order they bit: the birth path never presented the window
// at all (landing deliberately leaves activation alone, and a
// patrol claim is registered silently), so a window born
// minimized or behind the desktop stayed there; Windows'
// foreground lock can refuse a background process's raise; and
// the v0.37 backdrop lift could then slide the desktop OVER a
// window that had never actually come forward. The settle order
// is now: land -> PRESENT through platformFocusWindow (the exact
// function a Stage Manager click uses -- one shared raise, no
// parallel version) -> backdrop only when the presentation took
// effect. The window is then marked present-pending; the patrol
// verifies it really is on stage and retries the shared raise
// if not (presentSweep below).
//
// v0.54 completes this story: the backdrop itself was the last
// bug -- platformRaiseBackdrop's z-order direction was inverted,
// so the "backdrop" slid the desktop OVER the newborn instead of
// under it (see winmgr_windows.go) -- and the verification only
// ever checked foreground, which a covered window still holds.
// Verification is now foreground + above-the-shell + not
// minimized, with a bounded budget of two retries.
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
	shell := shellHWND()
	fg := classifyForeground(shell)
	doBirthPlacement(hwnd)
	// Presentation is expected when the user is in terminal
	// context: the daemon opened the window from the desktop UI,
	// or the foreground at claim time was the desktop or another
	// claimed window (the click that spawned a popup happened
	// inside the terminal). In host context the claim stays
	// silent -- no raise, no focus steal, ever.
	expect := launchCtx || fg == fgShell || fg == fgClaimed
	presented := false
	if expect {
		// platformFocusWindow restores a minimized window first,
		// then raises -- the same code path (and the same
		// AttachThreadInput foreground-lock workaround) as the
		// Stage Manager click in extWinFocusHandler.
		presented = platformFocusWindow(hwnd)
		if !presented {
			// v0.54: a refused birth raise used to be silent.
			// Name it in the console; the patrol's verification
			// (presentSweep) owns the bounded retries from here.
			extMu.Lock()
			label := ""
			if e, ok := extWins[hwnd]; ok {
				label = e.App
			}
			extMu.Unlock()
			logf("winmgr", "warn", "开窗礼仪：新窗口出生呈现未即时生效，巡逻将核验补提：%s", label)
		}
	}
	usable := shell != 0 && shell != hwnd && platformWindowExists(shell) && !platformWindowIconic(shell) && platformWindowVisible(shell)
	if presented && usable && decideBackdrop(backdropFacts{
		Enabled:       settingWinPlaceOnClaim(),
		Owned:         platformWindowOwned(hwnd),
		SetHidden:     false,
		ShellUsable:   true,
		LaunchContext: launchCtx,
		Foreground:    fg,
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
	// Mark for the patrol's verification pass regardless of how
	// the raise reported itself: focus APIs can claim success a
	// beat before the window is really up (or fail quietly behind
	// the foreground lock), and the sweep is the honest judge.
	// v0.54: the judge now also checks the window is ABOVE the
	// desktop view in z-order (and not minimized) -- foreground
	// alone let the old backdrop bug pass verification while the
	// window sat covered.
	extMu.Lock()
	if e, ok := extWins[hwnd]; ok {
		e.presentPending = true
		e.presentExpect = expect
		e.presentRetries = 0
	}
	extMu.Unlock()
	// v0.54: an isolated-browser window gets the terminal's dark
	// DWM frame as it settles (launch and patrol claims alike);
	// chromeFrameSweep keeps the book honest afterwards.
	chromeFrameApplyFor(hwnd)
}

// ---------- v0.38: birth-presentation verification ----------

type presentFacts struct {
	Pending       bool // marked at birth, not yet verified
	ExpectPresent bool // the birth context said this window belongs on stage
	RetriesUsed   int  // retries already spent (budget: maxPresentRetries)
	SetHidden     bool // the whole set is tucked away right now
	BossHidden    bool // v0.52: tucked away by the boss key specifically
	Foreground    bool // the window IS the foreground window now
	AboveShell    bool // ... AND sits above the desktop view in z-order
	Iconic        bool // minimized: cannot count as on stage
	// Immersed is carried deliberately as a NON-input (v0.54):
	// immersion (v0.45) restyles the window, but presentation is
	// owed exactly the same with it on or off -- the field
	// exists so the tests can pin that irrelevance.
	Immersed bool
}

type presentAction int

const (
	presentDone  presentAction = iota // verified (or nothing owed): clear the mark
	presentRetry                      // raise it again, through the shared path
	presentWait                       // cannot judge while the set is hidden: keep the mark
)

// maxPresentRetries bounds the sweep's re-raises (v0.54: raised
// from one to two, because the second attempt is what covers the
// slow claim->settle races; the budget applies only while the
// window has NEVER verified on stage).
const maxPresentRetries = 2

// decidePresentRetry is the verification rule. A window that was
// never expected on stage (quiet host-context claim) owes
// nothing. A window hidden along with its set -- boss-hidden
// included: a newborn must never pop out on its own while the
// boss key has the set tucked away -- is judged only after the
// set returns. Otherwise "on stage" means foreground AND above
// the desktop view AND not minimized: v0.54 learned (the hard
// way) that foreground alone is not proof, because the old
// backdrop bug left windows foreground but covered by the shell,
// and the old foreground-only check marked them presented and
// never looked again. Retries are bounded; once the budget is
// spent the window is the user's, not the daemon's.
func decidePresentRetry(f presentFacts) presentAction {
	if !f.Pending || !f.ExpectPresent {
		return presentDone
	}
	if f.SetHidden || f.BossHidden {
		return presentWait
	}
	if f.Foreground && f.AboveShell && !f.Iconic {
		return presentDone
	}
	if f.RetriesUsed >= maxPresentRetries {
		return presentDone
	}
	return presentRetry
}

// presentSweep is the patrol's verification pass (called from
// patrolTick): every present-pending window is judged, and the
// ones owed a retry get it -- through platformFocusWindow, the
// shared raise, never a private variant.
func presentSweep() {
	fg := platformForegroundHWND()
	shell := shellHWND()
	hidden := grpIsHidden()
	boss := grpIsBossHidden()
	type cand struct {
		hwnd uint64
		app  string
		n    int // which retry this is (1-based, for the log)
	}
	var retries []cand
	var gaveUp []string
	extMu.Lock()
	for h, e := range extWins {
		if e.Shell || !e.presentPending {
			continue
		}
		above := true // no shell to be under: nothing can cover it
		if shell != 0 && shell != h {
			above = platformWindowAbove(h, shell)
		}
		facts := presentFacts{
			Pending:       true,
			ExpectPresent: e.presentExpect,
			RetriesUsed:   e.presentRetries,
			SetHidden:     hidden,
			BossHidden:    boss,
			Foreground:    fg != 0 && fg == h,
			AboveShell:    above,
			Iconic:        platformWindowIconic(h),
		}
		switch decidePresentRetry(facts) {
		case presentDone:
			e.presentPending = false
			if facts.ExpectPresent && !hidden && !(facts.Foreground && facts.AboveShell && !facts.Iconic) {
				// Budget spent and STILL not on stage: say so
				// once (the pending flag clears here, so this
				// line fires exactly once per window).
				gaveUp = append(gaveUp, e.App)
			}
		case presentRetry:
			e.presentRetries++
			retries = append(retries, cand{h, e.App, e.presentRetries})
		}
	}
	extMu.Unlock()
	for _, c := range retries {
		if platformFocusWindow(c.hwnd) {
			logf("winmgr", "info", "开窗礼仪：新窗口出生后未露面，已补提呈现（第 %d 次）：%s", c.n, c.app)
		} else {
			logf("winmgr", "warn", "开窗礼仪：新窗口补提呈现仍未生效（系统前台锁定？）（第 %d 次）：%s", c.n, c.app)
		}
	}
	for _, app := range gaveUp {
		logf("winmgr", "warn", "开窗礼仪：新窗口多次补提仍未露面，已停止补提（点台前调度或窗口菜单可手动调出）：%s", app)
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

// onTrayIconRestore is the tray icon's click: the whole set
// comes back and the desktop view is raised DIRECTLY by the
// daemon when it is alive (v0.52 -- the boss key's summon shares
// this exact path, see bosskey.go). Only a genuinely dead view
// still takes the openshell creation door, proxy decision
// included.
func onTrayIconRestore() {
	grpApplyEvent(evRestoreAll)
	if bossRaiseViewDirect() {
		return
	}
	go openShellViaHTTP()
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
