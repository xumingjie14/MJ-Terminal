package main

// v0.45 沉浸模式 (immersion): everything MJ Terminal opens lives
// only on MJ's desktop -- the host lends screen, keyboard and
// mouse, nothing else. Layer 2 of the design (whole-set linked
// hide / tray retreat) shipped in v0.37 (winset.go); this file
// adds layer 1 (the core) and the basic form of layer 3:
//
//   Layer 1 -- a window confirmed registered in the Stage Manager
//   registry is restyled as a tool window (WS_EX_TOOLWINDOW set,
//   WS_EX_APPWINDOW clear), which removes it from the host
//   taskbar AND from Alt-Tab. The Stage Manager strip and the
//   Dock become the only switch entries; the v0.37 tray icon is
//   the anchor back to the host. The desktop view itself is in
//   the same registry (铭杰工作站) and follows the same rule --
//   no exception. Boxed windows are registered through ownBox
//   and follow the same rule too -- no special case.
//
//   Layer 3 (basic) -- the desktop view can go borderless
//   fullscreen on its own monitor (the 「桌面全屏」 switch),
//   reusing shellview's geometry memory for the frame it returns
//   to when the switch goes off.
//
// FAIL-VISIBLE is the user's own acceptance line and is not
// negotiable: a window may be hidden ONLY after its registration
// is confirmed (in the registry, with a live Stage Manager
// entry). An unclaimed or uncertain window ALWAYS stays on the
// host taskbar -- findable beats hidden. The patrol re-verifies
// every tick: anything immersed that is no longer registered is
// restored immediately and logged at warn. Style-call failures
// leave the window visible and are logged; nothing retries in a
// way that could lose a window.
//
// Every immersed window carries an "MJImmersed" window property
// (SetProp) so a daemon that starts after a crash can recognize
// the previous incarnation's leftovers: the startup sweep
// restores and unmarks any marked window that is not in the
// (fresh) registry. prepareWindowsForDaemonExit restores every
// immersed window first, on all four exit paths.
//
// Orthogonality with the v0.37 set-visibility machine: immersion
// changes only the extended window style (taskbar / Alt-Tab
// presence); the set machine owns ShowWindow visibility. Neither
// ever does the other's job -- decideImmerse deliberately
// ignores SetHidden, and the tests pin that the verdict is
// identical with the set hidden or shown. When the two seem to
// conflict, the SET-HIDDEN state wins for visibility and the
// style simply follows registration.

import (
	"sort"
	"sync"
)

// ---------- pure decisions ----------

type immerseAction int

const (
	immNone immerseAction = iota
	immApply
	immRestore
)

type immerseFacts struct {
	Enabled    bool // 沉浸模式 switch on
	Registered bool // window is in the Stage Manager registry
	Immersed   bool // window currently carries the tool-window style
	// SetHidden is the v0.37 whole-set visibility state. It is
	// part of the facts so the tests can pin that it NEVER
	// changes the verdict: show/hide belongs to the set machine,
	// style belongs to immersion, and the two compose.
	SetHidden bool
}

// decideImmerse is the whole layer-1 rule in one place:
//
//   - registered + switch on + not yet immersed -> apply.
//   - immersed + (switch off OR no longer registered) -> restore.
//     The second arm is the fail-visible line: the moment a
//     window leaves the registry it must become findable again.
//   - unregistered + not immersed -> never apply, in every state.
//     Registration is the only door into hiding.
func decideImmerse(f immerseFacts) immerseAction {
	if f.Immersed {
		if !f.Enabled || !f.Registered {
			return immRestore
		}
		return immNone
	}
	if f.Enabled && f.Registered {
		return immApply
	}
	return immNone
}

// immerseRec is the bookkeeping for one immersed window: the
// extended style it had before immersion, restored exactly on
// the way out, and whether the fail-visible restore for it has
// already been warned about (one warn, not one per patrol tick).
type immerseRec struct {
	OrigStyle    uint32
	OrphanWarned bool
}

// immerseExitList is the daemon-exit restore list: EVERY window
// immersion still tracks, registered or not, sorted for a
// deterministic restore order. Exit restores everything --
// nothing the daemon hid may outlive the daemon.
func immerseExitList(book map[uint64]immerseRec) []uint64 {
	out := make([]uint64, 0, len(book))
	for h := range book {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// isOrphanMark is the startup-sweep verdict: a window carrying
// the MJImmersed property that is NOT in the current registry is
// a leftover of a previous daemon (which exited through the
// restore-all path would have unmarked it, so a surviving mark
// means a crash or a kill) and is restored at once. A marked
// window that IS registered belongs to this daemon's live set
// and is never swept.
func isOrphanMark(marked, registered bool) bool {
	return marked && !registered
}

// ---------- desktop fullscreen (layer 3, basic) ----------

type fsAction int

const (
	fsNone fsAction = iota
	fsEnter
	fsExit
	fsReenter // shell window changed under an active fullscreen
)

type fsFacts struct {
	Want         bool // 桌面全屏 switch on
	Active       bool // fullscreen currently applied
	ShellPresent bool // a desktop view is registered
	SameShell    bool // the active fullscreen is on the current shell
}

// decideFullscreen reconciles the switch with the applied state:
// enter when wanted and not applied, exit when unwanted, and
// re-enter (exit old, enter new) when the view was rebuilt while
// fullscreen was on.
func decideFullscreen(f fsFacts) fsAction {
	if !f.Want {
		if f.Active {
			return fsExit
		}
		return fsNone
	}
	if !f.ShellPresent {
		return fsNone
	}
	if !f.Active {
		return fsEnter
	}
	if !f.SameShell {
		return fsReenter
	}
	return fsNone
}

// fsState is the saved frame of the fullscreened shell window.
// begin/end are the pure save/restore round-trip the settings
// copy promises: what goes in at enter comes back out verbatim
// at exit.
type fsState struct {
	Active bool
	HWND   uint64
	Style  int32
	Rect   [4]int32
	Zoomed bool
}

func (s *fsState) begin(hwnd uint64, style int32, rect [4]int32, zoomed bool) {
	*s = fsState{Active: true, HWND: hwnd, Style: style, Rect: rect, Zoomed: zoomed}
}

// end returns the saved frame and clears the state; ok=false
// means no fullscreen was active (nothing to restore).
func (s *fsState) end() (hwnd uint64, style int32, rect [4]int32, zoomed bool, ok bool) {
	if !s.Active {
		return 0, 0, [4]int32{}, false, false
	}
	hwnd, style, rect, zoomed = s.HWND, s.Style, s.Rect, s.Zoomed
	*s = fsState{}
	return hwnd, style, rect, zoomed, true
}

// ---------- runtime state ----------

var (
	immMu        sync.Mutex
	immBook      = map[uint64]immerseRec{}
	immFailWarn  = map[uint64]bool{} // apply failures already logged (anti-spam)
	immOnSeen    bool                // last switch state the sweep observed
	immOnLogged  bool                // whether immOnSeen has been logged yet
	immFsMu      sync.Mutex
	immFs        fsState
	immKickCh    = make(chan struct{}, 1)
	immSweepBusy sync.Mutex // serializes sweep runs (patrol + kick)
)

// immerseKick asks the immersion worker to reconcile right away
// (settings flips apply immediately, not at the next patrol).
// Non-blocking: a pending kick already covers this one.
func immerseKick() {
	select {
	case immKickCh <- struct{}{}:
	default:
	}
}

// startImmerse runs the crash-leftover sweep once, then serves
// kick requests for the daemon's lifetime. The patrol also calls
// immerseSweep every tick as the standing fail-visible check, so
// the kick is a latency optimization, never the only path.
func startImmerse() {
	immerseStartupSweep()
	go func() {
		for range immKickCh {
			immerseSweep()
		}
	}()
}

// registeredSnapshot copies the registry as hwnd -> display label
// for one sweep pass.
func registeredSnapshot() map[uint64]string {
	extMu.Lock()
	defer extMu.Unlock()
	out := make(map[uint64]string, len(extWins))
	for h, e := range extWins {
		label := e.App
		if e.Title != "" {
			label = e.Title
		}
		if e.Shell {
			label = "铭杰工作站（桌面）"
		}
		out[h] = label
	}
	return out
}

// immerseSweep is one full reconcile pass, run by the patrol
// every tick and on every settings kick:
//
//   - the switch flipping is logged once (winmgr module);
//   - every registered window is immersed while the switch is on;
//   - every immersed window that left the registry is restored
//     at once (fail-visible, warn) -- or forgotten when it died;
//   - an immersed window whose window died is dropped silently;
//   - the desktop-fullscreen state is reconciled too.
func immerseSweep() {
	immSweepBusy.Lock()
	defer immSweepBusy.Unlock()

	on := settingImmersiveMode()
	immMu.Lock()
	// The first observation only sets the baseline (a daemon
	// that starts with the switch on has not "flipped" anything);
	// every later flip is logged exactly once.
	flipped := immOnLogged && immOnSeen != on
	immOnSeen, immOnLogged = on, true
	immMu.Unlock()
	if flipped {
		if on {
			logf("winmgr", "info", "沉浸模式已打开：在册窗口将离开宿主任务栏与 Alt-Tab，切换只走台前调度与 Dock")
		} else {
			logf("winmgr", "info", "沉浸模式已关闭：在册窗口的任务栏按钮全部还原")
		}
	}

	registered := registeredSnapshot()

	immMu.Lock()
	book := make(map[uint64]immerseRec, len(immBook))
	for h, r := range immBook {
		book[h] = r
	}
	immMu.Unlock()

	// Registered windows: apply or (switch off) restore.
	setHidden := grpIsHidden()
	for h, label := range registered {
		rec, immersed := book[h]
		switch decideImmerse(immerseFacts{
			Enabled: on, Registered: true, Immersed: immersed,
			SetHidden: setHidden,
		}) {
		case immApply:
			orig, ok := platformImmerseWindow(h)
			if !ok {
				immMu.Lock()
				already := immFailWarn[h]
				immFailWarn[h] = true
				immMu.Unlock()
				if !already {
					logf("winmgr", "warn", "沉浸：窗口样式设置失败，窗口保持可见（%s）", label)
				}
				continue
			}
			immMu.Lock()
			immBook[h] = immerseRec{OrigStyle: orig}
			delete(immFailWarn, h)
			immMu.Unlock()
			logf("winmgr", "info", "沉浸：窗口已离开宿主任务栏与 Alt-Tab（%s）", label)
		case immRestore:
			if platformUnimmerseWindow(h, rec.OrigStyle, true) {
				immMu.Lock()
				delete(immBook, h)
				immMu.Unlock()
				logf("winmgr", "info", "沉浸关闭：窗口已回到宿主任务栏（%s）", label)
			} else {
				logf("winmgr", "warn", "沉浸关闭：窗口样式还原失败，下一轮巡逻再试（%s）", label)
			}
		}
	}

	// Immersed but no longer registered: the fail-visible line.
	for h, rec := range book {
		if _, ok := registered[h]; ok {
			continue
		}
		if !platformWindowExists(h) {
			// Window died while immersed: nothing to restore,
			// the style died with it. Drop the bookkeeping.
			immMu.Lock()
			delete(immBook, h)
			immMu.Unlock()
			continue
		}
		if platformUnimmerseWindow(h, rec.OrigStyle, true) {
			immMu.Lock()
			delete(immBook, h)
			immMu.Unlock()
			logf("winmgr", "warn", "沉浸：窗口已不在台前调度注册表内，已立即还原到宿主任务栏（hwnd=%d）", h)
		} else if !rec.OrphanWarned {
			rec.OrphanWarned = true
			immMu.Lock()
			immBook[h] = rec
			immMu.Unlock()
			logf("winmgr", "warn", "沉浸：窗口已不在册且样式还原失败，将在下一轮巡逻重试（hwnd=%d）", h)
		}
	}

	applyShellFullscreenState()
}

// immerseRestoreAllForExit restores every immersed window's
// original style and removes the marks. Called FIRST by
// prepareWindowsForDaemonExit, which all four exit paths share,
// before the v0.37 hidden-set restore runs.
func immerseRestoreAllForExit() {
	immMu.Lock()
	list := immerseExitList(immBook)
	book := immBook
	immBook = map[uint64]immerseRec{}
	immMu.Unlock()
	n := 0
	for _, h := range list {
		if !platformWindowExists(h) {
			continue
		}
		if platformUnimmerseWindow(h, book[h].OrigStyle, true) {
			n++
		} else {
			logf("winmgr", "warn", "沉浸：退出时窗口样式还原失败（hwnd=%d），窗口关闭即自愈", h)
		}
	}
	if n > 0 {
		logf("winmgr", "info", "沉浸：守护进程退出前已还原 %d 个窗口的任务栏样式", n)
	}
}

// immerseStartupSweep is the crash safety net: any top-level
// window still carrying the MJImmersed mark but absent from the
// fresh registry is a leftover of a daemon that died without its
// exit restore (crash / kill). It is restored to the host
// taskbar and unmarked at once. The original style is gone with
// the old daemon, so the platform falls back to the standard
// app-window style for orphans.
func immerseStartupSweep() {
	registered := registeredSnapshot()
	for _, w := range platformAllTopWindows() {
		_, inRegistry := registered[w.HWND]
		if !isOrphanMark(platformWindowHasImmerseMark(w.HWND), inRegistry) {
			continue
		}
		if platformUnimmerseWindow(w.HWND, 0, false) {
			label := w.Title
			if label == "" {
				label = "未命名窗口"
			}
			logf("winmgr", "info", "沉浸启动清扫：上次异常退出遗留的窗口已还原到宿主任务栏（%s）", label)
		} else {
			logf("winmgr", "warn", "沉浸启动清扫：遗留窗口样式还原失败（hwnd=%d），窗口关闭即自愈", w.HWND)
		}
	}
}

// ---------- desktop fullscreen driver ----------

// shellFullscreenActive reports whether the shell is currently
// in immersion fullscreen. shellview's geometry memory consults
// it: the fullscreen frame must never be sampled as the frame to
// return to.
func shellFullscreenActive() bool {
	immFsMu.Lock()
	defer immFsMu.Unlock()
	return immFs.Active
}

// applyShellFullscreenState reconciles the 桌面全屏 switch with
// the applied state (fresh views, switch flips, view rebuilds).
func applyShellFullscreenState() {
	want := settingDesktopFullscreen()
	shell := shellHWND()
	immFsMu.Lock()
	st := immFs
	immFsMu.Unlock()

	switch decideFullscreen(fsFacts{
		Want: want, Active: st.Active,
		ShellPresent: shell != 0,
		SameShell:    st.Active && st.HWND == shell,
	}) {
	case fsEnter:
		enterShellFullscreen(shell)
	case fsExit:
		exitShellFullscreen()
	case fsReenter:
		exitShellFullscreen()
		enterShellFullscreen(shell)
	}
}

func enterShellFullscreen(hwnd uint64) {
	style, rect, zoomed, ok := platformEnterShellFullscreen(hwnd)
	if !ok {
		logf("winmgr", "warn", "桌面全屏：进入全屏失败，桌面保持原窗口状态")
		return
	}
	immFsMu.Lock()
	immFs.begin(hwnd, style, rect, zoomed)
	immFsMu.Unlock()
	logf("winmgr", "info", "桌面全屏：桌面视图已无边框全屏（所在显示器）")
}

func exitShellFullscreen() {
	immFsMu.Lock()
	hwnd, style, rect, zoomed, ok := immFs.end()
	immFsMu.Unlock()
	if !ok {
		return
	}
	if platformWindowExists(hwnd) &&
		platformExitShellFullscreen(hwnd, style, rect, zoomed) {
		logf("winmgr", "info", "桌面全屏：已还原桌面视图原来的窗口大小与位置")
	}
}
