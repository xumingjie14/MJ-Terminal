package main

// ---------- v0.52: the boss key （老板键） ----------
//
// v0.50's F8 was summon-only, and the user's field verdict on
// 2026-10-30 redefined it as a boss key:
//
//   - pressed while one of OUR windows is foreground (the
//     desktop view or any Stage-Manager-claimed window): hide
//     the WHOLE set at once. Processes and the proxy core keep
//     running; only the windows leave the screen.
//   - pressed while anything else is foreground (set hidden or
//     merely behind host apps): summon -- restore exactly the
//     pre-hide set plus the desktop view and bring it forward.
//   - vault locked: summon the lock screen only (unchanged
//     from v0.50 -- there is nothing else to hide or show).
//
// The tray icon and the boss key drive ONE state machine (the
// winset.go set-visibility machine, new state visHiddenBoss):
// tray click while boss-hidden summons through the same
// evRestoreAll transition, and every exit path undoes a boss
// hide before anything else (prepareWindowsForDaemonExit
// restores any non-shown state, boss included).
//
// Why the old summon felt slow (code facts, v0.50): the press
// ran onTrayIconRestore, which restored the set in-process but
// then raised the view through an HTTP self-call to
// /api/openshell; that handler first polled the proxy core for
// up to 20 x 300 ms (~6 s) before touching the view at all, and
// the Spotlight feedback additionally waited on the view page's
// SSE connection. And inside the terminal the old press had no
// visible effect whatsoever -- there was no hide branch; it
// re-raised an already-foreground set.
//
// The v0.52 press path therefore does its window work DIRECTLY:
// bossKeyPress is invoked synchronously from the hotkey message
// thread and performs plain Win32 calls (grpHideSet /
// grpRestoreSet / grpShowShell / platformFocusWindow) -- no
// HTTP, no SSE, no polling stands between the key and the
// screen. The only asynchronous remainder is the dead-view
// case: when the view process is gone, creation goes through
// the openshell door in a goroutine (a browser cold start is
// inherently slow; the release notes say so), and the Spotlight
// command keeps riding the sticky view channel
// (viewCmdBroadcast) so it lands when the new view connects --
// after the raise, never instead of it.
//
// Boss hide is orthogonal to immersion (v0.45): it hides via
// the group bookkeeping only and never touches immersion
// styles or marks; the patrol re-verifies immersion after the
// set returns, exactly as it does for minimize / tray hides.

import (
	"mjlight/internal/diag"
	"net/http"
	"time"
)

// ---------- press decision (pure) ----------

type bossFacts struct {
	Locked     bool   // vault sealed: the lock screen is the whole story
	BossHidden bool   // the set is currently boss-hidden
	Foreground fgKind // whose window is foreground right now
}

type bossAction int

const (
	bossActLock   bossAction = iota // summon the lock screen only
	bossActHide                     // our window is foreground: hide the set now
	bossActSummon                   // anything else: bring the terminal forward now
)

// decideBossAction is the whole press contract in one pure
// function. Foreground can never be ours while boss-hidden
// (every our-window is hidden), so a press in that state is
// always a summon; the BossHidden guard is belt and braces for
// the beat where the patrol has not yet re-hidden a window an
// app re-showed on its own.
func decideBossAction(f bossFacts) bossAction {
	if f.Locked {
		return bossActLock
	}
	if !f.BossHidden && (f.Foreground == fgShell || f.Foreground == fgClaimed) {
		return bossActHide
	}
	return bossActSummon
}

// bossKeyPress is the hotkey action. The Windows layer calls it
// SYNCHRONOUSLY from the hotkey message thread: every branch's
// window work is direct platform calls that complete within the
// press. The single exception is the dead-view creation
// fallback, which leaves for a goroutine so the message pump
// never waits on a browser start. Every press logs its branch
// (module system) so a field diagnosis never has to guess.
func bossKeyPress() {
	act := decideBossAction(bossFacts{
		Locked:     vaultLocked(),
		BossHidden: grpIsBossHidden(),
		Foreground: classifyForeground(shellHWND()),
	})
	switch act {
	case bossActLock:
		logf("system", "info", "老板键：保险库锁定中，唤出锁定屏")
		onTrayIconRestore()
	case bossActHide:
		logf("system", "info", "老板键：终端机在最前面，整套隐藏（守护进程与节点继续运行，再按一次热键或点托盘图标回来）")
		bossHideSet()
	case bossActSummon:
		logf("system", "info", "老板键：召唤终端机")
		bossSummon()
	}
}

// ---------- hide ----------

// bossHideSet hides the whole set NOW: the claimed windows go
// through the state machine (booked into grpHidden, tray icon
// parked), and the desktop view itself is hidden directly and
// booked into the same grpHidden set -- so grpRestoreSet, and
// with it every exit path's restore, covers the view too. A
// minimized view (only possible with the minimize linkage off)
// keeps its own state; summon un-minimizes it.
func bossHideSet() {
	shell := shellHWND()
	grpApplyEvent(evBossKeyHide)
	if shell == 0 || !platformWindowExists(shell) || !platformWindowVisible(shell) || platformWindowIconic(shell) {
		return
	}
	if platformHideWindow(shell) {
		grpMu.Lock()
		grpHidden[shell] = true
		grpMu.Unlock()
		logf("winmgr", "info", "老板键：桌面视图已隐藏")
	}
}

// ---------- summon ----------

// bossSummon restores the group set and raises the desktop view
// directly; the Spotlight command follows asynchronously over
// the sticky view channel -- the window never waits for it.
// With no live view, the command is broadcast first (sticky, so
// the view being created still receives it) and creation takes
// the openshell door in the background.
func bossSummon() {
	grpApplyEvent(evRestoreAll)
	if bossRaiseViewDirect() {
		viewCmdBroadcast("spotlight")
		return
	}
	viewCmdBroadcast("spotlight")
	go openShellViaHTTP()
}

// bossRaiseViewDirect raises a LIVE desktop view in-process:
// un-hide / un-minimize through grpShowShell, then the shared
// foreground raise (platformFocusWindow). It reports false when
// there is no live registered view to raise -- the only case
// that still needs the openshell creation door.
func bossRaiseViewDirect() bool {
	hwnd := shellHWND()
	if hwnd == 0 {
		return false
	}
	extMu.Lock()
	_, registered := extWins[hwnd]
	extMu.Unlock()
	if !registered || !platformWindowExists(hwnd) {
		return false
	}
	grpShowShell(hwnd)
	platformFocusWindow(hwnd)
	return true
}

// openShellViaHTTP is the creation door shared by the tray icon
// and the boss key: the daemon's own openshell endpoint, the
// same path a double-click on the EXE takes (proxy decision
// included). Callers run it on a goroutine -- it may wait on
// the auto-starting core inside the handler.
func openShellViaHTTP() {
	if diag.Port <= 0 {
		return
	}
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Post(daemonURLForPort(diag.Port)+"api/openshell", "application/json", nil)
	if err == nil {
		_ = resp.Body.Close()
	}
}
