package main

// resident.go -- v0.71 (detail-fix ⑧ lower half, block 2):
// 锁住后台 -- resident web apps (route A).
//
// The promise: a web app the user locks into the background
// keeps RUNNING in the isolated browser, so its page stays
// alive and its notifications keep flowing through the
// bridge into the Notification Center. The mechanics are
// deliberately built from parts the daemon already trusts:
//
//   - Identity is the WEBAPPS ledger's identity: the URL.
//     The lock set lives in the settings store (mjSettings
//     .WebAppLocks, url -> true) -- settings travel with the
//     folder like everything else. It is written ONLY via
//     POST /api/webapps/lock below (Stage card menu and the
//     Settings page share it), never via the settings POST.
//     A window stands for a locked app on one of two rungs
//     (residentWindowLockURL): its launch URL when it was
//     registered by a launch, or -- for a window the patrol
//     claimed after a daemon restart, which carries no URL --
//     its claim label, the page title the ledger itself
//     captured as the app's name. Without the second rung a
//     daemon restart (every in-app update) orphaned every
//     still-open locked window and the engine relaunched a
//     hidden duplicate beside it.
//   - The patrol (winmgr.go patrolTick, 2 s) calls
//     residentTick: a locked app with no live window is
//     relaunched in app mode after a disappearance is
//     confirmed (~4 s, so a window the user is merely moving
//     between states is never chased), and the newborn window
//     is hidden as soon as it registers. A locked app whose
//     window exists is left exactly alone.
//   - Showing is the Stage strip's existing job: clicking the
//     card raises the window through /api/extwin/focus, whose
//     focus dance shows a hidden window as part of raising
//     it. residentTick notices the window is visible again
//     and simply stops treating it as hidden.
//   - Relaunches back off exponentially (15 s doubling, 5 min
//     cap) so a site that crashes its window on load cannot
//     spin the browser; every lock / unlock / hide / show /
//     relaunch / backoff step is logged (module "browser").
//
// Honest boundaries:
//   - A relaunch reloads the page (login survives via the
//     profile, in-flight page state does not) -- that is the
//     route-A trade the design accepted.
//   - If the user quits the WHOLE browser, the patrol will
//     bring the locked apps (and with them the browser) back:
//     that is what "notifications never stop" costs, the
//     Stage menu and Settings both say a locked app keeps a
//     browser window resident, and unlocking is one click.
//     The only thing that stops residency is unlocking, a
//     locked vault (the patrol does not run), or exit.
//   - A window relaunched hidden may be visible for a beat
//     between its birth settlement and the patrol's hide
//     (one tick at worst); hiding rides the same patrol
//     cadence as everything else in the window manager.

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"time"
)

// ---------- lock set (settings store) ----------

// sameWebAppURL is the ledger's URL identity rule (mirrors
// webAppExists): exact match, tolerating one trailing slash.
func sameWebAppURL(a, b string) bool {
	trim := func(s string) string {
		for len(s) > 0 && s[len(s)-1] == '/' {
			s = s[:len(s)-1]
		}
		return s
	}
	return trim(a) == trim(b)
}

// webAppLockSet returns a copy of the locked URL set.
func webAppLockSet() map[string]bool {
	out := map[string]bool{}
	for u, on := range settingsGet().WebAppLocks {
		if on {
			out[u] = true
		}
	}
	return out
}

// winURLLocked reports whether rawURL is in the lock set
// under the ledger's identity rule.
func winURLLocked(set map[string]bool, rawURL string) bool {
	if rawURL == "" {
		return false
	}
	for u := range set {
		if sameWebAppURL(u, rawURL) {
			return true
		}
	}
	return false
}

// pruneWebAppLocks keeps only well-formed http(s) URLs with
// a true value (sanitizeSettings runs it on every load).
func pruneWebAppLocks(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	out := make(map[string]bool, len(m))
	for u, on := range m {
		if !on {
			continue
		}
		if _, ok := normalizeWebURL(u); ok {
			out[u] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// settingsSetWebAppLock persists one lock flip through the
// settings store. Called by the lock handler only.
func settingsSetWebAppLock(rawURL string, on bool) error {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	next := setState.cur
	locks := make(map[string]bool, len(next.WebAppLocks)+1)
	for u, v := range next.WebAppLocks {
		locks[u] = v
	}
	if on {
		locks[rawURL] = true
	} else {
		delete(locks, rawURL)
	}
	next.WebAppLocks = pruneWebAppLocks(locks)
	setState.cur = next
	return settingsSaveLocked()
}

// ---------- decision core (pure, unit-tested) ----------

// residentBootGrace is how long after services start the
// engine waits before its first relaunch: the user (and the
// autostart core) get a head start, and a daemon that is
// about to be handed over by an update never races a browser
// launch of its own.
const residentBootGrace = 20 * time.Second

// residentConfirm is how long a locked app's window must be
// continuously missing before a relaunch is owed.
const residentConfirm = 4 * time.Second

type residentAction int

const (
	residentNone residentAction = iota
	residentRelaunch
	residentHide
)

// residentAppState is one locked app's runtime book (never
// persisted; the lock SET is the durable fact).
type residentAppState struct {
	MissingSince time.Time // first tick the window was seen gone; zero = present
	Attempts     int       // consecutive relaunches since the window was last present
	NextTry      time.Time // backoff gate for the next relaunch
	HidePending  bool      // a relaunch happened: hide its window when it registers
	HiddenHWND   uint64    // the window we hid (0 = none / user is looking at it)
	Warned       bool      // the deep-backoff warn already logged for this streak
	Summoning    bool      // a user-summoned restore launch is in flight (v0.71 test3)
}

// residentBackoff is the delay before the NEXT relaunch
// after `attempts` consecutive ones: 15 s doubling, capped
// at 5 minutes.
func residentBackoff(attempts int) time.Duration {
	d := 15 * time.Second
	for i := 1; i < attempts && d < 5*time.Minute; i++ {
		d *= 2
	}
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// residentDecide is the engine's whole rulebook, kept pure:
//   - window present and a hide is owed -> residentHide
//   - window present otherwise -> residentNone
//   - window missing inside the boot grace -> residentNone
//   - window missing for less than residentConfirm -> residentNone
//   - window missing but the backoff has not elapsed -> residentNone
//   - otherwise -> residentRelaunch
func residentDecide(st residentAppState, present bool, missingFor time.Duration, now, bootedAt time.Time) residentAction {
	if present {
		if st.HidePending {
			return residentHide
		}
		return residentNone
	}
	if now.Before(bootedAt.Add(residentBootGrace)) {
		return residentNone
	}
	if missingFor < residentConfirm {
		return residentNone
	}
	if now.Before(st.NextTry) {
		return residentNone
	}
	return residentRelaunch
}

// ---------- Stage card state (v0.71 test3, layer 1) ----------

// residentCardState is a locked app's Stage card state, one of
// four -- the fourth exists so a crash-looping app never shows
// a lying "restoring" forever (risk review §1).
type residentCardState string

const (
	residentCardFront     residentCardState = "front"     // aligned window, on stage
	residentCardBack      residentCardState = "back"      // aligned window, in the background
	residentCardRestoring residentCardState = "restoring" // no window, engine bringing it back
	residentCardUnstable  residentCardState = "unstable"  // no window, relaunches keep failing
)

// residentUnstableAttempts is the Attempts count at which a
// windowless card stops saying 恢复中 and tells the truth --
// the same threshold as the engine's deep-backoff warning.
const residentUnstableAttempts = 4

type residentCardFacts struct {
	HasWindow     bool
	WindowVisible bool // visible AND not minimized
	Attempts      int
}

// decideResidentCardState is the card state rulebook, pure:
// the Stage list handler derives every locked app's state from
// ONE snapshot (lock set + registry + this book) through this
// function -- no second source of truth may grow beside it
// (L1-B1: two cadences would make cards flicker between
// states, the same disjointedness this layer exists to kill).
func decideResidentCardState(f residentCardFacts) residentCardState {
	if f.HasWindow {
		if f.WindowVisible {
			return residentCardFront
		}
		return residentCardBack
	}
	if f.Attempts >= residentUnstableAttempts {
		return residentCardUnstable
	}
	return residentCardRestoring
}

// residentRemapHiddenBook folds the hidden-window book across
// an alignment change (L1-I2): the aligned window is picked by
// identity and CAN change under the book (the window we hid
// died; a sibling window of the same app took over the
// alignment). A hide debt naming any other window is void --
// it must never be pinned on a window we cannot prove we hid.
func residentRemapHiddenBook(hiddenHWND, alignedHWND uint64) uint64 {
	if hiddenHWND == alignedHWND {
		return hiddenHWND
	}
	return 0
}

// residentBookSnapshot copies the runtime book for readers
// that must not hold the lock while sampling the OS (the Stage
// list handler).
func residentBookSnapshot() map[string]residentAppState {
	residentSt.Lock()
	defer residentSt.Unlock()
	out := make(map[string]residentAppState, len(residentSt.apps))
	for u, st := range residentSt.apps {
		out[u] = *st
	}
	return out
}

// residentHiddenSet returns the windows the engine currently
// books as daemon-hidden (presentSweep's DaemonHidden fact).
func residentHiddenSet() map[uint64]bool {
	residentSt.Lock()
	defer residentSt.Unlock()
	out := map[uint64]bool{}
	for _, st := range residentSt.apps {
		if st.HiddenHWND != 0 {
			out[st.HiddenHWND] = true
		}
	}
	return out
}

// daemonHideWindow is THE daemon-intentional hide of a locked
// app's window, shared by every trigger: the patrol's relaunch
// hide, the close interception (× / Alt+F4) and the Stage
// menu's translated close. L2-B1/HS-13: a deliberate hide is a
// first-class fact for the presentation bookkeeping -- the
// birth-presentation marks are cleared, the anti-re-cover
// sweep stood down and the solo book dropped IN THE SAME
// BREATH, so no other mechanism can "helpfully" re-raise or
// resurrect a window the daemon just tucked away. via names
// the trigger for the console line.
func daemonHideWindow(hwnd uint64, lockURL, via string) bool {
	extMu.Lock()
	e, ok := extWins[hwnd]
	if ok {
		e.presentPending = false
		e.presentExpect = false
	}
	extMu.Unlock()
	if !ok {
		return false
	}
	raiseSweepCancel(hwnd)
	soloForget(hwnd)
	if !platformHideWindow(hwnd) {
		logf("browser", "warn", "锁住后台：%s 的窗口隐藏失败（%s），将在下个巡逻周期重试", residentName(lockURL), via)
		return false
	}
	extMu.Lock()
	if e, ok := extWins[hwnd]; ok {
		e.hiddenClaim = true
	}
	extMu.Unlock()
	residentSt.Lock()
	if st := residentSt.apps[lockURL]; st != nil {
		st.HiddenHWND = hwnd
		st.HidePending = false
	}
	residentSt.Unlock()
	logf("browser", "info", "锁住后台：%s 已转入后台运行（%s）", residentName(lockURL), via)
	return true
}

// ---------- engine ----------

var residentSt = struct {
	sync.Mutex
	bootedAt time.Time
	apps     map[string]*residentAppState
}{apps: map[string]*residentAppState{}}

// residentInit stamps the boot time the grace window is
// measured from (startServicesOnce).
func residentInit() {
	residentSt.Lock()
	if residentSt.bootedAt.IsZero() {
		residentSt.bootedAt = time.Now()
	}
	residentSt.Unlock()
}

// residentNoteLocked makes a freshly locked app eligible for
// bring-up on the next tick (its disappearance clock starts
// already confirmed) and clears any stale book.
func residentNoteLocked(rawURL string) {
	residentSt.Lock()
	residentSt.apps[rawURL] = &residentAppState{MissingSince: time.Now().Add(-residentConfirm - time.Second)}
	residentSt.Unlock()
}

// residentNameIndex maps each locked app's display name
// (appNameForURL: the ledger name, else the host) to its lock
// key. A name shared by two locked apps maps to "" -- the
// name rung cannot tell those apps apart, so it aligns
// nothing for them and the URL rung remains their only
// identity. Computed once per tick / list call.
func residentNameIndex(locks map[string]bool) map[string]string {
	idx := make(map[string]string, len(locks))
	for u := range locks {
		n := appNameForURL(u)
		if _, dup := idx[n]; dup {
			idx[n] = ""
		} else {
			idx[n] = u
		}
	}
	return idx
}

// residentWindowLockURL returns the lock-set key a window
// stands for, or "" when it stands for no locked app. Two
// rungs, in order:
//   - the window's own launch URL, matched under the
//     ledger's identity rule (launch-registered windows);
//   - a patrol-claimed browser window carries no URL (the
//     daemon restarted under a still-running browser), so it
//     aligns by its claim label: claimLabel stored the page
//     title with the browser's suffix stripped, and the
//     ledger's name for the app is that same title captured
//     at pin time (addapp.go cleanPageTitle). Only an
//     unambiguous name aligns (see residentNameIndex), and
//     the generic 浏览器 fallback label never does.
func residentWindowLockURL(e *extWindow, locks map[string]bool, names map[string]string) string {
	if e == nil || e.Shell {
		return ""
	}
	if e.URL != "" {
		for u := range locks {
			if sameWebAppURL(e.URL, u) {
				return u
			}
		}
		return ""
	}
	if !e.browser || e.App == "" || e.App == "浏览器" {
		return ""
	}
	return names[e.App]
}

// findResidentWindow picks the live window standing for a
// locked URL. A launch-registered window carrying the URL
// itself always wins; patrol-claimed windows (no URL) align
// through residentWindowLockURL's name rung.
func findResidentWindow(wins []*extWindow, locks map[string]bool, names map[string]string, rawURL string) *extWindow {
	for _, e := range wins {
		if e.Shell || e.URL == "" {
			continue
		}
		if sameWebAppURL(e.URL, rawURL) {
			return e
		}
	}
	for _, e := range wins {
		if residentWindowLockURL(e, locks, names) == rawURL {
			return e
		}
	}
	return nil
}

// residentTick runs on the patrol cadence (winmgr.go). It
// only ever touches locked apps' windows: hide one it
// relaunched, relaunch one that is gone.
func residentTick() {
	if runtime.GOOS != "windows" {
		return
	}
	locks := webAppLockSet()
	residentSt.Lock()
	if residentSt.bootedAt.IsZero() {
		residentSt.bootedAt = time.Now()
	}
	bootedAt := residentSt.bootedAt
	// Forget the books of apps no longer locked.
	for u := range residentSt.apps {
		if !winURLLocked(locks, u) {
			delete(residentSt.apps, u)
		}
	}
	residentSt.Unlock()
	if len(locks) == 0 {
		return
	}
	extMu.Lock()
	wins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		cp := *e
		wins = append(wins, &cp)
	}
	extMu.Unlock()

	now := time.Now()
	names := residentNameIndex(locks)
	var launches []string
	var hides []struct {
		hwnd uint64
		url  string
	}
	residentSt.Lock()
	for u := range locks {
		st := residentSt.apps[u]
		if st == nil {
			st = &residentAppState{}
			residentSt.apps[u] = st
		}
		win := findResidentWindow(wins, locks, names, u)
		if win != nil {
			st.MissingSince = time.Time{}
			st.Attempts = 0
			st.Warned = false
			st.NextTry = time.Time{}
			st.Summoning = false // a summoned window has landed
			// L1-I2: the alignment may have moved to a sibling
			// window under the book -- a hide debt naming any
			// other window is void, never pinned on this one.
			st.HiddenHWND = residentRemapHiddenBook(st.HiddenHWND, win.HWND)
			if st.HiddenHWND == win.HWND && platformWindowVisible(win.HWND) {
				// The user raised it (Stage card click shows
				// as part of the raise): residency steps back.
				st.HiddenHWND = 0
				logf("browser", "info", "锁住后台：%s 的窗口已回到台前", residentName(u))
			}
			if residentDecide(*st, true, 0, now, bootedAt) == residentHide {
				st.HidePending = false
				hides = append(hides, struct {
					hwnd uint64
					url  string
				}{win.HWND, u})
			}
			continue
		}
		if st.HiddenHWND != 0 {
			st.HiddenHWND = 0 // the hidden window is the one that vanished
		}
		if st.MissingSince.IsZero() {
			st.MissingSince = now
		}
		if st.Summoning {
			// A user-summoned restore launch is in flight: its
			// window is on its way and must be PRESENTED when
			// it lands, so the engine neither launches a
			// second copy nor arms a hide for it.
			continue
		}
		if residentDecide(*st, false, now.Sub(st.MissingSince), now, bootedAt) == residentRelaunch {
			st.Attempts++
			st.NextTry = now.Add(residentBackoff(st.Attempts))
			st.HidePending = true
			launches = append(launches, u)
			if st.Attempts >= 4 && !st.Warned {
				st.Warned = true
				logf("browser", "warn", "锁住后台：%s 多次重拉仍未稳定（第 %d 次），已进入退避（最长 5 分钟一次）", residentName(u), st.Attempts)
			} else {
				logf("browser", "info", "锁住后台：%s 的窗口已关闭，正在后台重新拉起（第 %d 次）", residentName(u), st.Attempts)
			}
		}
	}
	residentSt.Unlock()
	// Hides run outside the book lock through the one shared
	// hide path (daemonHideWindow), so a patrol hide carries
	// the same presentation bookkeeping as an intercepted
	// close. A failed hide re-arms HidePending for the next
	// tick, as before.
	for _, hd := range hides {
		if daemonHideWindow(hd.hwnd, hd.url, "常驻引擎") {
			continue
		}
		residentSt.Lock()
		if st := residentSt.apps[hd.url]; st != nil {
			st.HidePending = true
		}
		residentSt.Unlock()
	}
	for _, u := range launches {
		if residentLaunch(u) {
			continue
		}
		// The relaunch failed: no window is coming, so the
		// hide it armed must not stay pending -- left armed,
		// it would hide a window the user opens by hand later.
		residentSt.Lock()
		if st := residentSt.apps[u]; st != nil {
			residentNoteLaunchResult(st, false)
		}
		residentSt.Unlock()
		logf("browser", "warn", "锁住后台：%s 重拉失败，已撤销待隐藏标记——之后手动打开该应用不会被误藏", residentName(u))
	}
}

// residentNoteLaunchResult folds a relaunch outcome into the
// book. Success changes nothing (the armed hide is owed by a
// window that is on its way); failure disarms HidePending,
// because the hide was owed by a window that never came.
func residentNoteLaunchResult(st *residentAppState, ok bool) {
	if !ok {
		st.HidePending = false
	}
}

// residentName renders a locked URL for log lines: the
// ledger's display name when the app is in it, else the host.
func residentName(rawURL string) string { return appNameForURL(rawURL) }

// residentLaunch relaunches one locked app in app mode,
// through the node when in-app acceleration is on (the same
// rule openWebAppHandler applies), direct as the fallback.
// It reports whether a launch actually went out: the caller
// rolls the book back on false (see residentNoteLaunchResult).
func residentLaunch(rawURL string) bool {
	if accelEnabled() && len(nodes) > 0 {
		if err := launchIsolatedBrowser(true, rawURL, true); err == nil {
			return true
		} else {
			logf("browser", "warn", "锁住后台：%s 走节点重拉失败，降级直连：%v", residentName(rawURL), err)
		}
	}
	if err := launchIsolatedBrowser(false, rawURL, true); err != nil {
		logf("browser", "error", "锁住后台：%s 重拉失败：%v", residentName(rawURL), err)
		return false
	}
	return true
}

// ---------- immediate restore (v0.71 test3, HS-4) ----------

// residentRestore performs the windowless card's click: an
// immediate, user-summoned bring-up of a locked app whose
// window is gone. It clears the backoff gate and any armed
// HidePending (an in-flight patrol relaunch then simply lands
// PRESENTED instead of hidden -- the v0.71 F3 lesson: a stale
// hide must never eat a window the user asked for), resets the
// failure streak (a human retry is a fresh start), and
// launches at most one window per URL at a time.
func residentRestore(rawURL string) {
	residentSt.Lock()
	st := residentSt.apps[rawURL]
	if st == nil {
		st = &residentAppState{}
		residentSt.apps[rawURL] = st
	}
	if st.Summoning {
		residentSt.Unlock()
		return // a summon is already in flight for this app
	}
	if st.HidePending {
		// A patrol relaunch is already on its way: the whole
		// job is disarming its hide.
		st.HidePending = false
		st.NextTry = time.Time{}
		residentSt.Unlock()
		logf("browser", "info", "锁住后台：%s 正在恢复的窗口已改为直接显示（撤销待隐藏）", residentName(rawURL))
		return
	}
	st.Attempts = 0
	st.Warned = false
	st.NextTry = time.Time{}
	st.MissingSince = time.Now().Add(-residentConfirm - time.Second)
	st.Summoning = true
	residentSt.Unlock()
	go func() {
		ok := residentLaunch(rawURL)
		residentSt.Lock()
		if st := residentSt.apps[rawURL]; st != nil {
			st.Summoning = false
			if !ok {
				// Restart the disappearance clock: the patrol
				// retries by the book instead of spinning.
				st.MissingSince = time.Now()
			}
		}
		residentSt.Unlock()
		if !ok {
			logf("browser", "warn", "锁住后台：%s 立即恢复启动失败，常驻引擎将按退避重试", residentName(rawURL))
		}
	}()
	logf("browser", "info", "锁住后台：%s 已从台前调度立即恢复", residentName(rawURL))
}

// ---------- unlock closing invariant (v0.71 test3, HS-6) ----------

// residentUnlockShow performs the unlock closing invariant
// (L1-I5): BEFORE the lock is dropped, the app's aligned
// window must end up visible or owned by a window-set ledger
// (the group set book / the solo book) -- never an unbooked
// invisible orphan. That hole exists in test2 today: patrol
// hides a window, the user unlocks, the book is deleted, and
// the hidden window belongs to nobody. Called with the lock
// still in place (alignment needs it); the Stage menu and the
// Settings page share this one closing path through
// webAppLockHandler.
func residentUnlockShow(rawURL string) {
	locks := webAppLockSet()
	names := residentNameIndex(locks)
	extMu.Lock()
	wins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		cp := *e
		wins = append(wins, &cp)
	}
	extMu.Unlock()
	win := findResidentWindow(wins, locks, names, rawURL)
	if win == nil {
		return
	}
	if platformWindowVisible(win.HWND) {
		return // already on the desktop (minimized counts: WS_VISIBLE stands)
	}
	if grpOwnsHidden(win.HWND) || soloOwns(win.HWND) {
		return // a set ledger owns it; its restore paths surface it
	}
	if platformShowBackWindow(win.HWND) {
		extMu.Lock()
		if e, ok := extWins[win.HWND]; ok {
			e.hiddenClaim = false
		}
		extMu.Unlock()
		residentSt.Lock()
		if st := residentSt.apps[rawURL]; st != nil && st.HiddenHWND == win.HWND {
			st.HiddenHWND = 0
		}
		residentSt.Unlock()
		logf("browser", "info", "锁住后台：解除锁住时，%s 的窗口已从后台显示回来", residentName(rawURL))
	}
}

// ---------- HTTP ----------

// webAppLockHandler serves /api/webapps/lock:
//
//	POST {url, on} -- flip one app's 锁住后台 state. The
//	  Stage card menu and 设置 → 通知 → 锁住后台 share it;
//	  the response carries the full sorted lock set so
//	  either surface can re-render without a second fetch.
//	GET -- the current lock set (the Settings list renders
//	  names from the ledger and falls back to the host for
//	  a locked URL that is not a ledger entry).
//
// lockResponse builds the lock endpoints' shared answer: the
// sorted lock set plus the close-becomes-hide layer's live
// state, so the Settings list can show "关窗变藏未生效"
// honestly when the hook install failed (v0.71 test3, HS-14).
func lockResponse() webAppLockResponse {
	set := webAppLockSet()
	locks := make([]string, 0, len(set))
	for k := range set {
		locks = append(locks, k)
	}
	sort.Strings(locks)
	active, degraded := interceptCloseHideState()
	return webAppLockResponse{OK: true, Locks: locks, CloseHide: active, CloseHideDegraded: degraded}
}

func webAppLockHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, lockResponse())
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		URL string `json:"url"`
		On  bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	u, ok := normalizeWebURL(body.URL)
	if !ok {
		apiErrBadRequest(w, errCodeBadRequest, "网址不对")
		return
	}
	if !body.On {
		// HS-6 closing invariant: while the lock still stands
		// (alignment needs it), bring the app's window back to
		// visibility or confirm a set ledger owns it -- the
		// unlock must never strand an invisible orphan window.
		residentUnlockShow(u)
	}
	if err := settingsSetWebAppLock(u, body.On); err != nil {
		logf("browser", "error", "锁住后台：保存失败（%s）：%v", u, err)
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "保存失败")
		return
	}
	name := residentName(u)
	if body.On {
		residentNoteLocked(u)
		logf("browser", "info", "锁住后台：已锁住 %s（%s）——将常驻一个浏览器窗口在后台运行，关窗会自动重拉", name, u)
	} else {
		residentSt.Lock()
		for k := range residentSt.apps {
			if sameWebAppURL(k, u) {
				delete(residentSt.apps, k)
			}
		}
		residentSt.Unlock()
		logf("browser", "info", "锁住后台：已解除 %s（%s）的锁住——之后关窗即真关", name, u)
	}
	resp := lockResponse()
	resp.URL = u
	resp.Locked = body.On
	writeJSON(w, resp)
}

// webAppRestoreHandler serves POST /api/webapps/restore
// {url}: the windowless Stage card's click (v0.71 test3,
// HS-4) -- an immediate, user-summoned bring-up of a locked
// app whose window is gone. Registered on the LOCAL mux only:
// the remote-assist surface must never gain the power to raise
// browser windows on this machine (the same rule the thumb
// endpoint follows).
func webAppRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	u, ok := normalizeWebURL(body.URL)
	if !ok {
		apiErrBadRequest(w, errCodeBadRequest, "网址不对")
		return
	}
	locks := webAppLockSet()
	if !winURLLocked(locks, u) {
		apiErrBadRequest(w, errCodeBadRequest, "这个应用没有锁住后台")
		return
	}
	// A window may have landed a beat after the card painted:
	// then there is nothing to summon, and the strip re-derives
	// the card as a window card on its next poll.
	names := residentNameIndex(locks)
	extMu.Lock()
	wins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		cp := *e
		wins = append(wins, &cp)
	}
	extMu.Unlock()
	if findResidentWindow(wins, locks, names, u) != nil {
		writeJSON(w, webAppRestoreResponse{OK: true, Present: true})
		return
	}
	residentRestore(u)
	writeJSON(w, webAppRestoreResponse{OK: true})
}
