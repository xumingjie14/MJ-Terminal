package main

// ---------- unified window management (v0.18, 乙+丙) ----------
//
// The desktop's web apps live in INDEPENDENT Chrome/Edge --app windows
// (that isolation is deliberate: separate profile, node proxy, logins
// travel with the folder). Until now those windows were managed only
// by the Windows taskbar, while in-desktop windows are managed by the
// Dock / Stage Manager -- two window systems to switch between.
//
// 乙 (unified switching): the daemon registers every external app
// window it launches, lists them in the Stage Manager strip next to
// the in-system windows, and brings one to the front on click.
//
// 丙 (one taskbar button): every registered window -- external app
// windows AND the desktop shell window -- gets the same
// AppUserModelID stamped on it, so the Windows taskbar groups them
// under a single MJ Terminal button. Best effort: whether grouping
// actually happens depends on the host's taskbar settings.
//
// v0.20 adds one-shot landing placement: a freshly registered
// external window is fitted ONCE into the desktop shell window's
// rectangle (inset so the menu bar and Dock stay visible), because
// apps kept opening maximized on top of the whole desktop. After
// that single placement the window is the user's -- never corrected
// again. True embedding was explicitly rejected by the user: the
// window stays an independent host window, it just starts out
// inside the terminal's territory.
//
// The OS-specific parts (EnumWindows hunting, SetForegroundWindow,
// SHGetPropertyStoreForWindow) live in winmgr_windows.go, with no-op
// stubs in winmgr_other.go so this file stays platform-neutral.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// unifiedAppID is the AppUserModelID stamped on every window the
// daemon owns, so the taskbar treats them as one app （丙）.
const unifiedAppID = "MJTerminal.Workstation"

type extWindow struct {
	HWND  uint64 `json:"hwnd"`
	App   string `json:"app"`           // pinned web-app name (or hostname fallback)
	URL   string `json:"url,omitempty"` // URL it was launched with
	Title string `json:"title"`         // live window title, refreshed on list
	Shell bool   `json:"shell,omitempty"`
	// v0.38: live presentation state, sampled on every list call so
	// the Stage Manager strip can highlight the window the user is
	// actually in (and dim the minimized ones). Computed on the copy
	// in extWinListHandler, never stored as truth.
	Active    bool `json:"active"`
	Minimized bool `json:"minimized"`
	// v0.56: identity facts for the Stage Manager strip's identity
	// mode, also computed on the copy in extWinListHandler:
	// Browser marks a daemon-launched browser window; IconKind
	// names the real icon the view can show for it ("chrome" /
	// "firefox", "" for anything else).
	Browser  bool   `json:"browser,omitempty"`
	IconKind string `json:"icon,omitempty"`

	pid       uint32
	lastTitle string // title at last AppID stamp; a change triggers a re-stamp
	placed    bool   // v0.20: one-shot landing placement already done
	// hiddenClaim marks a register-only claim of a born-hidden
	// window (v0.41 verdict, enforced since v0.43): while the
	// window stays invisible, the prune pass must NOT offer it
	// the one-shot landing — moving a hidden window around is
	// how the v0.42 Telegram installer's silent Setup window
	// got landed inside the desktop mid-install. Its landing
	// belongs to the reveal path (claimWindowByPIDs' settle).
	hiddenClaim bool
	// v0.38: birth-presentation verification (winset.go). A window
	// whose birth context said it should be on stage is marked
	// pending; the patrol verifies a beat later that it really is
	// on stage (v0.54: foreground AND above the desktop view AND
	// not minimized), and retries the shared raise a bounded
	// number of times if not.
	presentPending bool
	presentRetries int
	presentExpect  bool
	// browser marks windows owned by a browser the daemon launched
	// (isolated Chrome / Firefox component, incl. windows the patrol
	// auto-claimed from them). The patrol treats further windows of
	// a browser process as the gated auto-claim class (v0.36).
	browser bool
	// origin records HOW this window entered the registry:
	// "launch" (the daemon opened it and trackLaunch claimed it) or
	// the v0.36 ownership reason the patrol claimed it under
	// (ownProfile / ownExe / ownTree). Audit trail only.
	origin string
	// v0.56: window geometry memory (wingeom.go). geomKey is the
	// launch identity the window's size is remembered under (""
	// = not remembered: the shell, dialogs). placeMode is the
	// per-webapp landing mode from the 调整窗口 menu ("" / "memory"
	// = geometry memory governs; "phone" / "desktop" /
	// "fullscreen" force that landing instead).
	geomKey   string
	placeMode string
}

var (
	extMu   sync.Mutex
	extWins = map[uint64]*extWindow{}
	// extClaimMu serializes window claiming: two launches in quick
	// succession must not race to claim the same fresh window.
	extClaimMu sync.Mutex
)

// ---------- v0.36: spawn ledger ----------
//
// The daemon historically started UI processes with
// exec.Command(...).Start() and threw the handle away, so nothing
// remembered WHICH processes it had brought into being. The ledger
// is that memory: every UI launch site notes the PID it started.
// The patrol uses live ledger entries as ownership seeds (rule
// (a): the daemon's own process trees), alongside the PIDs of
// already-registered windows. Entries are pruned once the process
// leaves the snapshot -- a launcher that handed off and exited has
// done its job; its children keep their ownership through the
// exe/profile rules or through still-living ancestors.

var (
	spawnMu    sync.Mutex
	spawnRoots = map[uint32]string{} // pid -> label (log/audit only)
)

// noteSpawnedProcess records a process the daemon just started.
// Call it right after a successful cmd.Start() at UI launch sites
// (isolated browser, Firefox, store apps, user-run executables).
func noteSpawnedProcess(pid uint32, label string) {
	if pid == 0 {
		return
	}
	spawnMu.Lock()
	spawnRoots[pid] = label
	spawnMu.Unlock()
}

// spawnSeedPIDs returns the ledger PIDs still alive in the current
// process snapshot, pruning the dead ones on the way out.
func spawnSeedPIDs(alive map[uint32]bool) map[uint32]bool {
	spawnMu.Lock()
	defer spawnMu.Unlock()
	out := map[uint32]bool{}
	for pid := range spawnRoots {
		if alive[pid] {
			out[pid] = true
		} else {
			delete(spawnRoots, pid)
		}
	}
	return out
}

// appNameForURL labels a launched window: the pinned web-app's name
// when the URL matches one, else the site's hostname, else generic.
func appNameForURL(u string) string {
	for _, a := range loadWebApps() {
		if a.URL == u {
			return a.Name
		}
	}
	if pu, err := url.Parse(u); err == nil && pu.Hostname() != "" {
		return pu.Hostname()
	}
	return "浏览器"
}

// trackBrowserLaunch registers a just-launched browser window with
// the daemon. It must be handed a snapshot of the browser's existing
// top-level windows taken BEFORE the launch (platformSnapshotWindows):
// Chrome's launcher process hands off to an already-running instance
// of the same profile and exits, so the new window usually does NOT
// belong to the process we started -- "the first new window of this
// browser that was not there before" is the reliable way to find it.
// The window may take a moment to appear, so this polls briefly.
func trackBrowserLaunch(app, targetURL, browserPath string, before map[uint64]bool, shell bool) {
	geomKey := ""
	if !shell {
		geomKey = "app:browser"
	}
	trackLaunch(app, targetURL, browserPath, before, shell, !shell, geomKey, "")
}

// trackBrowserLaunchAs is trackBrowserLaunch with an explicit
// geometry identity and landing mode -- the web-app open path
// uses it (an app-mode window belongs to its SITE, and the
// webapp's 调整窗口 mode rides along to the birth placement).
func trackBrowserLaunchAs(app, targetURL, browserPath string, before map[uint64]bool, shell bool, geomKey, placeMode string) {
	trackLaunch(app, targetURL, browserPath, before, shell, !shell, geomKey, placeMode)
}

// siteGeomKey is the geometry identity of a browser app-mode
// window: "site:<host>".
func siteGeomKey(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Hostname() != "" {
		return "site:" + strings.ToLower(u.Hostname())
	}
	return ""
}

// exeGeomKey is the fallback geometry identity of a window with
// no launch context: "exe:<lowercase image base name>".
func exeGeomKey(imagePath string) string {
	if imagePath == "" {
		return ""
	}
	return "exe:" + strings.ToLower(filepath.Base(imagePath))
}

// trackAppLaunch is the store-app twin (v0.31): a portable app from
// the app store gets its main window claimed and registered exactly
// like a browser window (unified AppID + one-shot landing), so it
// shows up in the Stage Manager strip too. Some portable apps hand
// off to a child process, so claiming keys on "the first new window
// of this exe" (process image match), never on the started PID.
func trackAppLaunch(appName, exePath string, before map[uint64]bool) {
	trackLaunch(appName, "", exePath, before, false, false, "", "")
}

// trackAppLaunchAs is trackAppLaunch with an explicit geometry
// identity (store:<id> / comp:<id> / exe:<base>).
func trackAppLaunchAs(appName, exePath string, before map[uint64]bool, geomKey string) {
	trackLaunch(appName, "", exePath, before, false, false, geomKey, "")
}

// trackLaunch is the shared claiming engine behind both. browser
// marks the registration as browser-owned, which is what puts its
// PID into the v0.31 patrol's watched set.
func trackLaunch(app, targetURL, exePath string, before map[uint64]bool, shell, browser bool, geomKey, placeMode string) {
	if exePath == "" {
		return
	}
	go func() {
		extClaimMu.Lock()
		defer extClaimMu.Unlock()
		deadline := time.Now().Add(9 * time.Second)
		for time.Now().Before(deadline) {
			extMu.Lock()
			claimed := make(map[uint64]bool, len(extWins))
			for h := range extWins {
				claimed[h] = true
			}
			extMu.Unlock()
			if hwnd, pid, ok := platformClaimWindow(exePath, before, claimed); ok {
				extMu.Lock()
				extWins[hwnd] = &extWindow{HWND: hwnd, App: app, URL: targetURL, Shell: shell, pid: pid, browser: browser, origin: "launch", geomKey: geomKey, placeMode: placeMode}
				extMu.Unlock()
				logf("winmgr", "info", "已登记外部窗口：%s", app)
				// 丙: stamp the unified AppUserModelID (best effort).
				platformSetAppID(hwnd)
				if shell {
					// The desktop view itself: it is the frame
					// everything else answers to (v0.37 watcher).
					grpShellOpened(hwnd)
				} else {
					// v0.37: birth settlement -- the one landing
					// plus the backdrop lift, or following the
					// set into hiding (winset.go).
					settleNewWindow(hwnd, true)
				}
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
		logf("winmgr", "warn", "打开的窗口一直没出现，未能登记：%s", app)
	}()
}

// ---------- v0.40: explicit PID-set claiming (boxed apps) ----------
//
// A boxed app's process is created by the Sandboxie SERVICE, not
// by the Start.exe the daemon launches (Start.exe hands off and
// exits), and its image path reports the virtual path -- so the
// two identity lines trackLaunch and the patrol historically had
// (daemon process tree, exe under the terminal folder) both miss
// it, and its window went unclaimed in v0.39. Sandboxie itself
// knows exactly which PIDs live in the box (Start.exe /listpids),
// so v0.40 promotes that list to a first-class identity source:
//
//   - at launch, sandbox.go snapshots the box PID set, polls the
//     difference after Start.exe hands off, and hands the NEW PID
//     set to claimWindowByPIDs below (explicit-PID registration,
//     no process-tree assumption anywhere);
//   - on a SECOND launch (the app is already running in the box)
//     the difference is legitimately empty; the launch then
//     presents the window the EXISTING PID set already owns
//     (presentWindowForPIDs) instead of failing;
//   - the patrol refreshes the box PID set once per tick and the
//     ownership classifier gains the matching "box" signal
//     (owncheck.go), so any boxed window the launch path missed
//     is still claimed as a fallback.

// pidSetFromInts builds a PID lookup set from a /listpids result.
func pidSetFromInts(pids []int) map[uint32]bool {
	out := make(map[uint32]bool, len(pids))
	for _, p := range pids {
		if p > 0 {
			out[uint32(p)] = true
		}
	}
	return out
}

// sortedPIDList renders a PID set deterministically for log lines
// (the console log is the field-diagnosis record, so the raw set
// must read the same way every time).
func sortedPIDList(set map[uint32]bool) []int {
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, int(p))
	}
	sort.Ints(out)
	return out
}

// diffPIDs returns the PIDs present in after but not in before
// (pure): the processes one launch brought into the box.
func diffPIDs(before, after map[uint32]bool) map[uint32]bool {
	out := map[uint32]bool{}
	for p := range after {
		if !before[p] {
			out[p] = true
		}
	}
	return out
}

// pidSetsOverlap reports whether two PID sets share any member
// (pure).
func pidSetsOverlap(a, b map[uint32]bool) bool {
	for p := range a {
		if b[p] {
			return true
		}
	}
	return false
}

// pickWindowForPIDs chooses the window an explicit PID set owns
// (pure). v0.41 widens the candidate pool past v0.40's
// visible-only rule, on the strength of the field evidence: a
// boxed app's main window can exist while hidden (WS_VISIBLE
// off), and a visible-only picker structurally cannot claim it.
// Candidates are ranked in two tiers:
//
//	app windows -- titled, unowned, non-system class (see
//	               isAppWindowShape), VISIBLE OR HIDDEN;
//	fallback    -- any other visible, unowned, non-system-class
//	               window (v0.40's pool, minus the junk classes).
//
// Excluded HWNDs (windows that existed before the launch plus
// everything already registered) are always skipped, and no
// window outside the PID set is ever picked -- the set is the
// identity evidence, everything else is ranking. Within a tier
// the ranking is: PIDs in prefer (the launch difference -- the
// processes THIS launch brought in) first, then image-base
// matches (a boxed app spawns helpers whose windows are not the
// main window), then visible before hidden, then larger area,
// then lowest HWND, so the choice is deterministic.
func pickWindowForPIDs(wins []windowFact, pids, prefer map[uint32]bool, exclude map[uint64]bool, imageByPID map[uint32]string, imageBase string) (windowFact, bool) {
	if len(pids) == 0 {
		return windowFact{}, false
	}
	var app, fallback []windowFact
	for _, w := range wins {
		if !pids[w.PID] || exclude[w.HWND] || w.Owned || isSystemWindowClass(w.Class) {
			continue
		}
		if isAppWindowShape(w) {
			app = append(app, w)
		} else if w.Visible {
			fallback = append(fallback, w)
		}
	}
	cands := app
	if len(cands) == 0 {
		cands = fallback
	}
	if len(cands) == 0 {
		return windowFact{}, false
	}
	rank := func(w windowFact) (pref, img, vis int) {
		if prefer[w.PID] {
			pref = 1
		}
		if imageBase != "" {
			if im := imageByPID[w.PID]; im != "" && strings.EqualFold(filepath.Base(im), imageBase) {
				img = 1
			}
		}
		if w.Visible {
			vis = 1
		}
		return
	}
	sort.Slice(cands, func(i, j int) bool {
		pi, ii, vi := rank(cands[i])
		pj, ij, vj := rank(cands[j])
		if pi != pj {
			return pi > pj
		}
		if ii != ij {
			return ii > ij
		}
		if vi != vj {
			return vi > vj
		}
		ai, aj := int64(cands[i].W)*int64(cands[i].H), int64(cands[j].W)*int64(cands[j].H)
		if ai != aj {
			return ai > aj
		}
		return cands[i].HWND < cands[j].HWND
	})
	return cands[0], true
}

// windowsOwnedByPIDs is the timeout cross-check (pure): every
// enumerated top-level window whose owning PID is in the set,
// regardless of visibility or shape. It answers the field
// question "the box has processes -- do ANY of them hold a
// window, and under which PID / title?"
func windowsOwnedByPIDs(wins []windowFact, pids map[uint32]bool) []windowFact {
	var out []windowFact
	for _, w := range wins {
		if pids[w.PID] {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HWND < out[j].HWND })
	return out
}

// registerPIDClaimedWindow inserts one window into the registry
// on behalf of an explicit PID set (the testable core of
// claimWindowByPIDs). False means the window was already
// registered (by the patrol, most likely) -- a duplicate claim
// is not an error, just a lost race.
func registerPIDClaimedWindow(app string, wf windowFact, origin string) bool {
	extMu.Lock()
	defer extMu.Unlock()
	if _, dup := extWins[wf.HWND]; dup {
		return false
	}
	extWins[wf.HWND] = &extWindow{
		HWND: wf.HWND, App: app, Title: wf.Title, pid: wf.PID,
		lastTitle: wf.Title, origin: origin,
	}
	return true
}

// claimWindowByPIDs makes ONE attempt to find and register a
// window owned by the given explicit PID set, then runs the same
// birth settlement (landing + presentation) as a launch claim.
// Serialized with trackLaunch through extClaimMu so two launches
// can never race for the same fresh window.
//
// v0.41: when the picked window is not visible -- a boxed app's
// born-hidden main window -- registration alone would leave it
// exactly as invisible as before, so the claim actively reveals
// it (ShowWindow) and re-reads the live visibility, logging both
// values as the field proof. This reveal belongs ONLY to this
// user-clicked launch path: the patrol's hidden claims register
// without ever showing (decideClaim's HiddenClaim verdict).
func claimWindowByPIDs(app string, pids, prefer map[uint32]bool, exclude map[uint64]bool, imageBase, origin string) (uint64, bool) {
	if len(pids) == 0 {
		return 0, false
	}
	extClaimMu.Lock()
	defer extClaimMu.Unlock()
	ex := make(map[uint64]bool, len(exclude))
	for h := range exclude {
		ex[h] = true
	}
	extMu.Lock()
	for h := range extWins {
		ex[h] = true
	}
	extMu.Unlock()
	imageByPID := map[uint32]string{}
	for _, p := range platformProcessSnapshot() {
		if p.ImagePath != "" {
			imageByPID[p.PID] = p.ImagePath
		}
	}
	wf, ok := pickWindowForPIDs(platformAllTopWindows(), pids, prefer, ex, imageByPID, imageBase)
	if !ok {
		return 0, false
	}
	if !registerPIDClaimedWindow(app, wf, origin) {
		return 0, false
	}
	// v0.56: geometry identity for a PID-claimed window is its
	// exe (boxed apps share the patrol's fallback scheme).
	if imageBase != "" {
		extMu.Lock()
		if e, ok2 := extWins[wf.HWND]; ok2 && e.geomKey == "" {
			e.geomKey = "exe:" + strings.ToLower(imageBase)
		}
		extMu.Unlock()
	}
	logf("winmgr", "info", "已登记外部窗口：%s（显式 PID 集认领，PID %d）", app, wf.PID)
	platformSetAppID(wf.HWND)
	if !wf.Visible {
		beforeVis := platformWindowVisible(wf.HWND)
		platformShowAppWindow(wf.HWND)
		afterVis := platformWindowVisible(wf.HWND)
		if afterVis {
			logf("winmgr", "info", "已把盒内应用的隐藏窗口显示出来：%s（hwnd=%d，PID %d，标题=%q，类=%s，显示前可见=%v，显示后可见=%v）",
				app, wf.HWND, wf.PID, wf.Title, wf.Class, beforeVis, afterVis)
		} else {
			logf("winmgr", "warn", "盒内应用的隐藏窗口没能显示出来：%s（hwnd=%d，PID %d，标题=%q，类=%s，显示前可见=%v，显示后可见=%v）",
				app, wf.HWND, wf.PID, wf.Title, wf.Class, beforeVis, afterVis)
		}
	}
	settleNewWindow(wf.HWND, true)
	return wf.HWND, true
}

// pickRegisteredForPIDs finds an already-registered window owned
// by a PID in the set (pure). A registration matches when its app
// label is the app being launched, or when it entered the
// registry through the box signal itself (a patrol-claimed boxed
// window is labelled by its page/exe title, not by the registry
// name the launch path knows). Shell windows never match.
func pickRegisteredForPIDs(wins map[uint64]*extWindow, app string, pids map[uint32]bool) (uint64, bool) {
	hwnds := make([]uint64, 0, len(wins))
	for h := range wins {
		hwnds = append(hwnds, h)
	}
	sort.Slice(hwnds, func(i, j int) bool { return hwnds[i] < hwnds[j] })
	for _, h := range hwnds {
		e := wins[h]
		if e.Shell || e.pid == 0 || !pids[e.pid] {
			continue
		}
		if e.App == app || e.origin == ownBox {
			return h, true
		}
	}
	return 0, false
}

// presentWindowForPIDs raises the window an explicit PID set
// already owns in the registry -- the second-launch path: the app
// was already running in the box, no new PID exists, and the
// correct response to the user's click is to PRESENT its window
// (through the same restore-then-raise path a Stage Manager
// click uses), never to report a failed launch.
func presentWindowForPIDs(app string, pids map[uint32]bool) (uint64, bool) {
	extMu.Lock()
	hwnd, ok := pickRegisteredForPIDs(extWins, app, pids)
	extMu.Unlock()
	if !ok {
		return 0, false
	}
	if _, alive := platformWindowTitle(hwnd); !alive {
		extMu.Lock()
		delete(extWins, hwnd)
		extMu.Unlock()
		return 0, false
	}
	grpRestoreForFocus()
	if !platformFocusWindow(hwnd) {
		return 0, false
	}
	extMu.Lock()
	if e, ok2 := extWins[hwnd]; ok2 {
		e.presentPending = false
	}
	extMu.Unlock()
	return hwnd, true
}

// currentTopWindowHWNDs snapshots the HWNDs of all top-level
// windows that exist right now (the "before" picture a launch
// diffs its new window against).
func currentTopWindowHWNDs() map[uint64]bool {
	out := map[uint64]bool{}
	for _, w := range platformAllTopWindows() {
		out[w.HWND] = true
	}
	return out
}

// ---------- v0.40: patrol box-list refresh ----------

var boxPatrolState struct {
	sync.Mutex
	lastWarn time.Time
	warned   bool
}

// decideBoxWarn throttles /listpids failure warnings (pure): the
// first failure logs, repeats are silent for a minute -- a broken
// box must be visible in the console, never a 2-second spam loop.
func decideBoxWarn(last time.Time, warned bool, now time.Time) bool {
	if !warned {
		return true
	}
	return now.Sub(last) >= time.Minute
}

// refreshBoxPIDsForPatrol fetches the box PID set for this patrol
// tick. It is called exactly once per tick and the result feeds
// the whole tick's classification. Any failure (component absent,
// box not placed, /listpids error) degrades to an empty set --
// the box rule just does not fire this tick -- with failures
// warned about at most once a minute.
func refreshBoxPIDsForPatrol() map[uint32]bool {
	if sbieStartExePath() == "" || !iniPlacedCurrent() {
		return nil
	}
	pids, err := sbieBoxPidsE()
	if err != nil {
		boxPatrolState.Lock()
		if decideBoxWarn(boxPatrolState.lastWarn, boxPatrolState.warned, time.Now()) {
			boxPatrolState.lastWarn = time.Now()
			boxPatrolState.warned = true
			boxPatrolState.Unlock()
			logf("winmgr", "warn", "台前调度：读取盒内进程名单失败（%v），本轮按无盒内进程处理", err)
		} else {
			boxPatrolState.Unlock()
		}
		return nil
	}
	boxPatrolState.Lock()
	boxPatrolState.warned = false
	boxPatrolState.Unlock()
	return pidSetFromInts(pids)
}

// pruneExtWindowsLocked drops registrations whose window is gone and
// refreshes titles. A title change usually means the page finished
// loading (or a popup took over the window), so the AppID is
// re-stamped for those windows -- Chrome can replace a window's
// identity as it navigates. It also collects external windows whose
// one-shot landing placement (v0.20) has not happened yet -- e.g. the
// window was claimed a moment before the desktop shell registered,
// so there was no frame to land in. Caller must hold extMu.
func pruneExtWindowsLocked() (restamp, place []uint64) {
	for h, e := range extWins {
		title, alive := platformWindowTitle(h)
		if !alive {
			if e.Shell {
				// v0.37: the watcher turns this into the
				// close-to-tray transition (winset.go).
				noteShellWindowGone(h)
			}
			// v0.56: the geometry sampler gets a final flush
			// for this window (drained outside the lock).
			noteGeomGone(h, e.geomKey)
			thumbForget(h)
			delete(extWins, h)
			continue
		}
		if title != "" {
			e.Title = title
			if title != e.lastTitle {
				e.lastTitle = title
				restamp = append(restamp, h)
			}
		}
		if !e.Shell && !e.placed {
			if e.hiddenClaim && !platformWindowVisible(h) {
				// Register-only claims stay unplaced while
				// hidden (see the field comment): the landing
				// happens when the window is revealed.
				continue
			}
			place = append(place, h)
		}
	}
	return restamp, place
}

// ---------- v0.20/v0.21: one-shot landing placement ----------
//
// External apps are launched with --start-maximized and Chrome also
// restores its own remembered geometry, so a window opened from the
// desktop used to splat over the WHOLE screen, hiding the menu bar
// and the Dock. The user asked for the window to open "inside the
// terminal" while staying a real external window. So: exactly once,
// when a window is first registered, it is fitted into the desktop
// shell window's rectangle, sized per the constants below. After
// that the user may drag or maximize it freely -- the daemon never
// touches its geometry again. A window that is closed and reopened
// is a new registration and lands again.

// Landing size as fractions of the desktop rectangle (v0.21),
// measured from the user's reference photo of the effect he wants:
// the landed window takes about 62% of the desktop width and 85%
// of its height, horizontally centered, with a ring of desktop
// showing on all four sides -- menu bar and wallpaper on top, the
// Stage Manager strip on the left, the desktop icon column on the
// right, the Dock at the bottom. The top gap is 6.5% of the
// desktop height, which keeps the menu bar clear; the rest of the
// vertical slack falls to the bottom so the Dock shows too.
const (
	placeWidthFrac  = 0.62  // of desktop width
	placeHeightFrac = 0.85  // of desktop height
	placeTopFrac    = 0.065 // of desktop height, above the window
)

// Minimum usable landed size. If the proportional size would come
// out smaller than this (small desktop window), placement falls
// back to the v0.20 fixed-inset algorithm below so the app window
// is not squeezed into something unusable.
const (
	placeMinWidth  = 760
	placeMinHeight = 520
)

// Landing insets in physical pixels for the small-desktop fallback,
// sized from the desktop's own chrome (index.html): the menu bar is
// 38px tall, the floating Dock occupies roughly the bottom 90px once
// its hover magnification is counted. The insets leave a breathing
// gap on top of that.
const (
	placeTopInset    = 50  // menu bar (38) + gap
	placeBottomInset = 106 // Dock zone (~90) + gap
	placeSideInset   = 28
)

// ---------- v0.23: phone-width landing in small-window mode ----------
//
// The user often runs the MJ desktop as a small floating window next
// to other apps (QQ and friends). A web app opened at the v0.21
// proportional size is then squeezed into the little desktop, and
// its desktop-layout page is barely usable. So when the desktop
// shell is in "small-window mode" at the moment of landing, the new
// window lands at phone width instead: most sites key their mobile
// layout off the viewport width, so ~400px gets the phone version of
// the page. Height and vertical position keep the usual proportions
// (0.85 / 0.065) so the menu bar and Dock stay clear exactly as
// before. This is still launch-time only -- the placed flag rules
// everything after -- and it does NOT touch the in-system UI or the
// browser's user agent; those are separate, later decisions.

// phoneModeMaxDesktopWidth: a desktop shell narrower than this is in
// small-window mode even when maximized (a maximized-but-narrow
// desktop, e.g. snapped to half a small screen, is still small).
// Since v0.30 this is only the DEFAULT: the live values (and the
// on/off switch) are user preferences in Data/settings.json, read
// through settingPhone() on every landing.
const phoneModeMaxDesktopWidth = 1200

// phoneWidth is the landed width in phone mode, matching a large
// phone's CSS viewport. Default only, see above.
const phoneWidth = 400

// placePhoneMode reports whether a landing should use the phone
// width: the desktop shell is not maximized (the user's floating-
// window way of working), or it is narrower than the configured
// trigger width even though maximized. Phone mode can also be
// switched off entirely in 设置 -> 桌面与 Dock.
func placePhoneMode(l, r int32, shellZoomed bool) bool {
	on, trigger, _ := settingPhone()
	if !on {
		return false
	}
	return !shellZoomed || (r-l) < int32(trigger)
}

// placeTargetRectPhone computes the phone-mode landing rectangle:
// phoneWidth wide, horizontally centered on the desktop, with the
// usual height/top proportions. On a desktop narrower than the
// phone width plus a small margin, the width degrades to the
// desktop width minus 32, floored at the smaller of 280 and 90% of
// the desktop width, so the result can never be zero, negative or
// wider than the desktop.
func placeTargetRectPhone(l, t, r, b int32) (int32, int32, int32, int32) {
	w, h := r-l, b-t
	_, _, prefWidth := settingPhone()
	tw := int32(prefWidth)
	if w < tw+32 {
		tw = w - 32
		floor := int32(280)
		if m := w * 90 / 100; m < floor {
			floor = m
		}
		if tw < floor {
			tw = floor
		}
		if tw < 1 {
			tw = 1
		}
		if tw > w {
			tw = w
		}
	}
	th := int32(float64(h) * placeHeightFrac)
	tt := t + int32(float64(h)*placeTopFrac)
	tl := l + (w-tw)/2
	return tl, tt, tl + tw, tt + th
}

// placeTargetRectAuto picks the landing rectangle for a new window:
// phone mode (and its rectangle) when placePhoneMode says so, the
// untouched v0.21 logic otherwise. Phone mode takes priority over
// the v0.21 small-desktop inset fallback -- a floating desktop that
// is small is exactly the case phone mode exists for.
func placeTargetRectAuto(l, t, r, b int32, shellZoomed bool) (int32, int32, int32, int32, bool) {
	if placePhoneMode(l, r, shellZoomed) {
		pl, pt, pr, pb := placeTargetRectPhone(l, t, r, b)
		return pl, pt, pr, pb, true
	}
	pl, pt, pr, pb := placeTargetRect(l, t, r, b)
	return pl, pt, pr, pb, false
}

// placeTargetRect computes where a landed window goes. Normally
// that is the proportional rectangle described above: 62% x 85% of
// the desktop, horizontally centered, with 6.5% of the desktop
// height above it. When the desktop is small enough that the
// proportional window would drop below the usable minimum size, it
// falls back to the v0.20 fixed-inset algorithm.
func placeTargetRect(l, t, r, b int32) (int32, int32, int32, int32) {
	w, h := r-l, b-t
	tw := int32(float64(w) * placeWidthFrac)
	th := int32(float64(h) * placeHeightFrac)
	if tw >= placeMinWidth && th >= placeMinHeight {
		tl := l + (w-tw)/2
		tt := t + int32(float64(h)*placeTopFrac)
		return tl, tt, tl + tw, tt + th
	}
	return placeTargetRectInset(l, t, r, b)
}

// placeTargetRectInset is the original v0.20 algorithm, now only
// the small-desktop fallback: the shell rectangle shrunk by the
// landing insets. On a small desktop window the insets converge
// proportionally so they can never eat the whole area; if the
// result would still be degenerate, the window simply gets 92% of
// the shell, centered.
func placeTargetRectInset(l, t, r, b int32) (int32, int32, int32, int32) {
	w, h := r-l, b-t
	side := int32(placeSideInset)
	if m := w / 12; m < side {
		side = m
	}
	if side < 0 {
		side = 0
	}
	top := int32(placeTopInset)
	if m := h / 10; m < top {
		top = m
	}
	bottom := int32(placeBottomInset)
	if m := h / 5; m < bottom {
		bottom = m
	}
	tw, th := w-2*side, h-top-bottom
	if tw < 240 || th < 180 {
		tw, th = w*92/100, h*92/100
		return l + (w-tw)/2, t + (h-th)/2, l + (w+tw)/2, t + (h+th)/2
	}
	return l + side, t + top, r - side, b - bottom
}

// tryPlaceExtWindow is the housekeeping retry of the one-shot
// landing: a window whose birth placement was deferred (no desktop
// frame to land in yet) is offered again on every prune pass, so
// its one landing still happens once a frame exists -- the same
// contract launched windows have had since v0.20. The placement
// itself (and the v0.37 rules around it: equal for patrol claims,
// dialogs exempt, never while the set is hidden) lives in
// doBirthPlacement (winset.go); the birth-moment settlement with
// the backdrop lift is settleNewWindow, called by the claim paths.
func tryPlaceExtWindow(hwnd uint64) {
	// v0.43 belt-and-braces: a window whose process belongs to
	// the running install session is never landed by housekeep-
	// ing, even if it was registered a beat before the session
	// tracked its PID. The install flow owns it until the
	// session ends.
	if installSessionActive() {
		extMu.Lock()
		e, ok := extWins[hwnd]
		var pid uint32
		if ok {
			pid = e.pid
		}
		extMu.Unlock()
		if ok && pid != 0 {
			for _, sp := range installSessionPIDs() {
				if uint32(sp) == pid {
					return
				}
			}
		}
	}
	doBirthPlacement(hwnd)
}

// extWinListHandler serves GET /api/extwin: every registered external
// window that is still alive (dead ones are pruned on the way out).
// The desktop shell window is included with shell=true; the frontend
// does not render it as a card (you are already looking at it), but it
// is registered so it carries the unified AppID like the others.
func extWinListHandler(w http.ResponseWriter, r *http.Request) {
	extMu.Lock()
	restamp, place := pruneExtWindowsLocked()
	list := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		cp := *e
		list = append(list, &cp)
	}
	extMu.Unlock()
	// v0.38: sample the live presentation state onto the copies --
	// which window is foreground right now (the same handle the
	// v0.37 backdrop logic samples), and which are minimized -- so
	// the strip can paint the current window bright and dim the
	// rest. Sampled per call (the strip polls every 4 s), so the
	// highlight follows the user as they move between the host and
	// the terminal.
	fgNow := platformForegroundHWND()
	for _, cp := range list {
		cp.Active = fgNow != 0 && cp.HWND == fgNow
		cp.Minimized = platformWindowIconic(cp.HWND)
		// v0.56: identity facts for the strip (see the field
		// comments on extWindow).
		cp.Browser = cp.browser
		switch platformProcessImageBase(cp.pid) {
		case "chrome.exe", "msedge.exe":
			cp.IconKind = "chrome"
		case "firefox.exe":
			cp.IconKind = "firefox"
		}
	}
	geomFlushGone()
	// Re-stamp outside the lock: the COM call can be slowish.
	for _, h := range restamp {
		platformSetAppID(h)
	}
	// Same for the one-shot landing placement: window moves do not
	// belong inside the registry lock.
	for _, h := range place {
		tryPlaceExtWindow(h)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].App != list[j].App {
			return list[i].App < list[j].App
		}
		return list[i].Title < list[j].Title
	})
	writeJSON(w, map[string]any{"windows": list})
}

// extWinFocusHandler serves POST /api/extwin/focus {"hwnd": N}:
// bring that external window to the front （乙）.
func extWinFocusHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HWND uint64 `json:"hwnd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.HWND == 0 {
		writeJSON(w, map[string]any{"error": "参数不对"})
		return
	}
	if _, alive := platformWindowTitle(body.HWND); !alive {
		extMu.Lock()
		delete(extWins, body.HWND)
		extMu.Unlock()
		writeJSON(w, map[string]any{"error": "这个窗口已经关了"})
		return
	}
	// v0.37: while the set is tucked away (minimize linkage /
	// tray), a Stage Manager click restores the WHOLE set first,
	// then raises the target (grpRestoreForFocus, winset.go).
	grpRestoreForFocus()
	if !platformFocusWindow(body.HWND) {
		writeJSON(w, map[string]any{"error": "窗口置前失败"})
		return
	}
	// The user just presented this window by hand: any pending
	// birth-presentation verification for it is moot.
	extMu.Lock()
	if e, ok := extWins[body.HWND]; ok {
		e.presentPending = false
	}
	extMu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

// extWinPost is the shared shape of the Stage Manager card
// actions (v0.56 test2: the strip's per-window right-click menu
// -- 切换到该窗口 is extWinFocusHandler; these two are its
// siblings): decode {"hwnd": N}, verify the window is alive,
// act, log in the winmgr module.
func extWinPost(w http.ResponseWriter, r *http.Request, act func(hwnd uint64) bool, verb string) {
	var body struct {
		HWND uint64 `json:"hwnd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.HWND == 0 {
		writeJSON(w, map[string]any{"error": "参数不对"})
		return
	}
	if _, alive := platformWindowTitle(body.HWND); !alive {
		extMu.Lock()
		delete(extWins, body.HWND)
		extMu.Unlock()
		writeJSON(w, map[string]any{"error": "这个窗口已经关了"})
		return
	}
	extMu.Lock()
	label := ""
	if e, ok := extWins[body.HWND]; ok {
		label = e.App
	}
	extMu.Unlock()
	if !act(body.HWND) {
		writeJSON(w, map[string]any{"error": verb + "失败"})
		return
	}
	logf("winmgr", "info", "台前调度卡片菜单：%s窗口（%s）", verb, label)
	writeJSON(w, map[string]any{"ok": true})
}

// extWinMinimizeHandler serves POST /api/extwin/minimize.
func extWinMinimizeHandler(w http.ResponseWriter, r *http.Request) {
	extWinPost(w, r, platformMinimizeWindow, "最小化")
}

// extWinCloseHandler serves POST /api/extwin/close: the plain
// WM_CLOSE path (the window's own X-button request -- the app
// may still prompt or refuse, exactly as when its user clicks
// the X).
func extWinCloseHandler(w http.ResponseWriter, r *http.Request) {
	extWinPost(w, r, platformCloseWindow, "关闭")
}

// extWinNote is a tiny helper for log lines in platform files; as of
// v0.22 they go into the diagnostics hub like everything else.
func extWinNote(format string, args ...any) {
	logf("winmgr", "warn", "%s", strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// ---------- v0.36: patrol -- full-inventory ownership claiming ----------
//
// v0.31's patrol watched only the PIDs of already-registered
// browser windows. That model leaked whenever a window's process
// was not a watched PID: Firefox was never registered at launch at
// all (no trackLaunch -- its PID could never enter the watched
// set), a browser that crashed and respawned came back under a new
// PID, a store app relaunched itself through a stub, a page handed
// a file to an external viewer. The user's field report was exact:
// windows opened from inside the terminal sat on the HOST taskbar
// with no Stage Manager entry, unfindable from inside.
//
// The v0.36 patrol inverts the model (decision logic in
// owncheck.go): every ~2s it inventories ALL visible top-level
// windows and a full process snapshot, classifies every process by
// durable facts (isolated-profile command line / image under the
// terminal folder / descent from a daemon seed), and claims every
// terminal-owned main window that is not registered yet. v0.40
// adds a fourth fact: membership in Sandboxie's in-box process
// list (refreshed once per tick, see refreshBoxPIDsForPatrol) --
// the only fact that identifies a boxed app's process at all.
//
// Claimed windows join the Stage Manager strip (listed,
// clickable to raise) and are never focus-stolen by the patrol.
// v0.37 changed the landing rule the user originally set for
// v0.31 ("claims never land"): birth placement is now equal for
// everyone, so a patrol claim runs the same one-shot settlement
// as a launch claim (settleNewWindow, winset.go) -- landed once,
// backdrop-lifted in terminal context, or hidden along with the
// set when the set is tucked away.
//
// The on/off switch is 设置 -> 桌面与 Dock ->
// 「浏览器新窗口自动纳入台前调度」 (settings key
// stageAutoClaimBrowser, default on), re-read every tick. Its scope
// is the BROWSER auto-claim class only: profile-fingerprint windows
// and further windows of registered browser processes. Store apps
// and other terminal programs (exe/tree rules) are the Stage
// Manager's core job and are claimed regardless -- previously they
// entered only through their launch claim, and a second window or
// a relaunched process fell out of the registry entirely.

// isBrowserWindowClass reports whether a window class is a
// browser's MAIN window class: Chromium browsers (Chrome, Edge)
// use Chrome_WidgetWin_1, Firefox uses MozillaWindowClass.
func isBrowserWindowClass(class string) bool {
	return class == "Chrome_WidgetWin_1" || class == "MozillaWindowClass"
}

// deriveBrowserPIDs is the set of PIDs owning registered browser
// windows. Shell windows and store apps are excluded by their flags.
func deriveBrowserPIDs(wins map[uint64]*extWindow) map[uint32]bool {
	pids := map[uint32]bool{}
	for _, e := range wins {
		if e.browser && !e.Shell && e.pid != 0 {
			pids[e.pid] = true
		}
	}
	return pids
}

// cleanAutoClaimTitle turns a browser window title into the app
// label shown in the Stage Manager strip: drop the browser's own
// suffix ("GitHub - Google Chrome" -> "GitHub"), fall back to
// 「浏览器」 when nothing is left.
func cleanAutoClaimTitle(title string) string {
	t := strings.TrimSpace(title)
	suffixes := []string{
		" - Google Chrome", " - Google Chrome Beta", " - Chromium",
		" - Mozilla Firefox", " — Mozilla Firefox", " - Microsoft Edge",
	}
	for i := 0; i < 3; i++ {
		changed := false
		for _, s := range suffixes {
			// The bare form covers a title that IS just the suffix
			// (its leading space was trimmed above).
			if t == strings.TrimSpace(s) {
				t, changed = "", true
				continue
			}
			if strings.HasSuffix(t, s) {
				t = strings.TrimSpace(strings.TrimSuffix(t, s))
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	if t == "" {
		return "浏览器"
	}
	return t
}

// claimLabel picks the Stage Manager label for a patrol-claimed
// window: browsers get their cleaned page title (so the user finds
// the window by the page they opened); other programs prefer their
// store-catalog name, then their window title, then the image name.
func claimLabel(w windowFact, p procFact, browser bool) string {
	if browser {
		return cleanAutoClaimTitle(w.Title)
	}
	if name := storeAppNameForExe(p.ImagePath); name != "" {
		return name
	}
	if t := strings.TrimSpace(w.Title); t != "" {
		return t
	}
	if base := filepath.Base(p.ImagePath); base != "" && base != "." {
		return strings.TrimSuffix(base, ".exe")
	}
	return "应用"
}

// procDisplayName renders a process for log lines.
func procDisplayName(p procFact) string {
	if base := filepath.Base(p.ImagePath); base != "" && base != "." {
		return base
	}
	return fmt.Sprintf("pid %d", p.PID)
}

// ---------- self-audit ----------
//
// An owned window that cannot be registered yet (today: its title
// has not materialized) is remembered here. One tick of grace is
// normal -- windows are routinely enumerated a beat before their
// title lands. Past the grace tick the window is registered anyway
// under a fallback label AND a warn goes to the console, so a leak
// is visible in the diagnostics hub instead of being discovered by
// the user. The warn fires once per window (at that claim), never
// per tick.

type pendingClaim struct {
	title  string
	pid    uint32
	reason string
}

var (
	auditMu      sync.Mutex
	pendingOwned = map[uint64]*pendingClaim{}
)

// startExtPatrol launches the inventory patrol. Off Windows the
// platform sources are always empty, so the loop is inert.
func startExtPatrol() {
	go func() {
		for range time.Tick(2 * time.Second) {
			patrolTick()
		}
	}()
}

func patrolTick() {
	if !servicesActive() {
		return // v0.48: vault locked (or sealing) -- hands off
	}
	// Prune + housekeeping first (same as the list handler does on
	// poll): dead registrations disappear even when the desktop
	// view is closed.
	extMu.Lock()
	restamp, place := pruneExtWindowsLocked()
	registered := make(map[uint64]bool, len(extWins))
	shellPIDs := map[uint32]bool{}
	seedPIDs := map[uint32]bool{}
	for h, e := range extWins {
		registered[h] = true
		if e.pid == 0 {
			continue
		}
		if e.Shell {
			shellPIDs[e.pid] = true
		} else {
			seedPIDs[e.pid] = true
		}
	}
	browserPIDs := deriveBrowserPIDs(extWins)
	extMu.Unlock()
	for _, h := range restamp {
		platformSetAppID(h)
	}
	for _, h := range place {
		tryPlaceExtWindow(h)
	}
	// v0.56: window geometry memory -- flush the final rects of
	// windows that just closed, then sample the live ones.
	geomFlushGone()
	geomSampleTick()

	procs := platformProcessSnapshot()
	wins := platformAllTopWindows()
	if len(procs) == 0 || len(wins) == 0 {
		return // non-Windows / collection unavailable: patrol inert
	}
	alive := make(map[uint32]bool, len(procs))
	byPID := make(map[uint32]procFact, len(procs))
	for _, p := range procs {
		alive[p.PID] = true
		byPID[p.PID] = p
	}
	for pid := range spawnSeedPIDs(alive) {
		seedPIDs[pid] = true
	}
	selfPID := uint32(os.Getpid())
	delete(seedPIDs, selfPID)
	delete(shellPIDs, selfPID)

	// v0.40: one box-list refresh per tick, shared by the whole
	// tick's classification (the "box" ownership signal).
	boxPIDs := refreshBoxPIDsForPatrol()

	owned := classifyOwnership(procs, seedPIDs, ownContext{
		RootDir:     exeDir(),
		ProfileDirs: []string{filepath.Join(dataDir(), "ChromeProfile"), ffProfileDir()},
		SelfPID:     selfPID,
		BoxPIDs:     boxPIDs,
	})
	opts := claimOpts{
		AutoClaimBrowser: settingStageAutoClaimBrowser(),
		ShellPIDs:        shellPIDs,
		BrowserPIDs:      browserPIDs,
		// v0.43: while a box install session runs, the patrol
		// does not claim box-owned windows at all.
		BoxExempt: installSessionActive(),
	}
	// v0.43: lazy one-per-tick live re-read of the box PID list,
	// fetched only if a system-binary box claim needs verifying.
	var boxRecheck map[uint32]bool
	boxRecheckTried := false

	liveHwnds := make(map[uint64]bool, len(wins))
	for _, wf := range wins {
		liveHwnds[wf.HWND] = true
		if registered[wf.HWND] {
			continue
		}
		auditMu.Lock()
		_, seenBefore := pendingOwned[wf.HWND]
		auditMu.Unlock()
		o := opts
		o.EmptySeenBefore = seenBefore
		v := decideClaim(wf, owned, registered, o)
		switch {
		case v.Claim:
			if v.Reason == ownBox && isSystemBinaryImage(filepath.Base(byPID[wf.PID].ImagePath)) {
				// v0.43: a box-list claim for a system binary
				// must survive a LIVE re-read of the list. The
				// tick's snapshot can be a beat stale, and the
				// pre-v0.43 parser could invent PIDs outright
				// (it ate the count header). Failing the re-check
				// skips the claim and leaves a warn line with
				// the evidence, so the next field occurrence
				// says which side was lying.
				if !boxRecheckTried {
					boxRecheckTried = true
					if cur, err := sbieBoxPidsE(); err == nil {
						boxRecheck = pidSetFromInts(cur)
					}
				}
				if boxRecheck != nil && !boxRecheck[wf.PID] {
					logf("winmgr", "warn", "台前调度：盒名单认领复核未通过，已跳过：%s（PID %d，镜像 %s，实时盒名单里没有这个进程）",
						claimLabel(wf, byPID[wf.PID], v.Browser), wf.PID, byPID[wf.PID].ImagePath)
					continue
				}
			}
			label := claimLabel(wf, byPID[wf.PID], v.Browser)
			extMu.Lock()
			if _, dup := extWins[wf.HWND]; dup {
				extMu.Unlock()
				continue
			}
			extWins[wf.HWND] = &extWindow{
				HWND: wf.HWND, App: label, Title: wf.Title, pid: wf.PID,
				browser: v.Browser, placed: false, lastTitle: wf.Title, origin: v.Reason,
				hiddenClaim: v.HiddenClaim,
				// v0.56: a patrol-claimed window has no launch
				// context; its geometry identity is its exe.
				geomKey: exeGeomKey(byPID[wf.PID].ImagePath),
			}
			extMu.Unlock()
			registered[wf.HWND] = true
			auditMu.Lock()
			delete(pendingOwned, wf.HWND)
			auditMu.Unlock()
			platformSetAppID(wf.HWND)
			if v.HiddenClaim {
				// v0.41: register-only. The patrol never reveals
				// or raises a hidden boxed window on its own --
				// no settle runs, so the entry is never marked
				// present-pending and decidePresentRetry owes it
				// nothing; presenting it is the user's launch
				// click's job (claimWindowByPIDs' reveal).
				logf("winmgr", "info", "台前调度认领窗口：%s（归属：%s；盒内应用窗口处于隐藏状态，仅登记，用户从桌面打开时呈现）",
					label, ownReasonText(v.Reason))
				continue
			}
			// v0.37 birth settlement: the one landing (patrol
			// claims are equal citizens since this version), the
			// backdrop lift in terminal context, or following
			// the set into hiding.
			settleNewWindow(wf.HWND, false)
			if v.NoTitle {
				logf("winmgr", "warn", "台前调度自检：窗口超过一轮盘点仍无标题，已先行登记：%s（进程 %s，归属：%s）",
					label, procDisplayName(byPID[wf.PID]), ownReasonText(v.Reason))
			} else {
				logf("winmgr", "info", "台前调度认领窗口：%s（归属：%s）", label, ownReasonText(v.Reason))
			}
		case v.Defer:
			// Owned but not ready (no title yet): remember it so
			// the next tick knows the grace is spent.
			auditMu.Lock()
			pendingOwned[wf.HWND] = &pendingClaim{title: wf.Title, pid: wf.PID, reason: v.Reason}
			auditMu.Unlock()
		}
		// v.Gated (browser auto-claim switched off) and plain
		// no-claims need no bookkeeping: gated is a user choice,
		// never an alarm.
	}
	// Retire audit entries whose windows are gone.
	auditMu.Lock()
	for h := range pendingOwned {
		if !liveHwnds[h] {
			delete(pendingOwned, h)
		}
	}
	auditMu.Unlock()

	// v0.38: verify birth presentations -- a window whose birth
	// said it should be on stage but that still is not foreground
	// gets exactly one retry through the shared raise (winset.go).
	presentSweep()

	// v0.37: while the set is tucked away, keep it tucked (a
	// window claimed a beat before the hide, or re-shown by its
	// own app, rejoins the hidden set here).
	grpEnforceHidden()

	// v0.54: keep the isolated-Chrome DWM frame book honest --
	// frame newly eligible windows, restore the ones that left.
	chromeFrameSweep(byPID, shellPIDs)

	// v0.45: immersion reconcile + the standing fail-visible
	// check -- anything immersed that left the registry during
	// this tick is restored to the host taskbar at once.
	immerseSweep()
}

// ---------- v0.54: isolated-Chrome frame unification ----------
//
// The registry half of the DWM frame work (the platform half is
// platformApplyChromeFrame / platformRestoreChromeFrame). Only
// windows of the browser the DAEMON launched for the isolated
// profile are eligible; the desktop view itself never is, and
// neither is any window whose process belongs to the shell view's
// own family (the shell is usually Edge -- the same image name as
// a fallback isolated browser, so the PID family is the guard,
// not the name alone). Applied at settle (chromeFrameApplyFor),
// kept honest by the patrol (chromeFrameSweep), restored when a
// booked window leaves the registry alive and for every booked
// window on daemon exit (chromeFrameRestoreAll, called from
// prepareWindowsForDaemonExit). Every transition logs under
// module winmgr.

// chromeFrameMask bits report which attributes a platform apply
// actually landed, so the sweep can log honestly (border+corner
// are the visible parts on Win11; the caption colour may not
// show on Chrome's self-drawn frame).
const (
	chromeFrameCorner uint = 1 << iota
	chromeFrameBorder
	chromeFrameCaption
	chromeFrameText
)

type chromeFrameFacts struct {
	Browser      bool   // registry entry belongs to a daemon-launched browser
	Shell        bool   // the entry IS the desktop view
	ShellPID     bool   // the window's PID is in the shell view's process family
	ImageBase    string // the window process's image base name (lowercase)
	IsolatedBase string // the isolated browser's image base name (lowercase); "" = unknown
}

// chromeFrameEligible is the pure verdict (unit-tested): an
// isolated-browser window gets the terminal's frame; everything
// else keeps the frame its own app chose.
func chromeFrameEligible(f chromeFrameFacts) bool {
	if !f.Browser || f.Shell || f.ShellPID || f.IsolatedBase == "" {
		return false
	}
	return f.ImageBase == f.IsolatedBase &&
		(f.ImageBase == "chrome.exe" || f.ImageBase == "msedge.exe")
}

var (
	chromeFrameMu     sync.Mutex
	chromeFrameBook   = map[uint64]bool{} // HWNDs we have framed
	chromeFrameNoSupp bool                // the "OS accepted nothing" line is logged
)

func isolatedBrowserBase() string {
	p := findChrome()
	if p == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(p))
}

func chromeFrameFactsFor(e *extWindow, imageBase string, shellPIDs map[uint32]bool, isoBase string) chromeFrameFacts {
	return chromeFrameFacts{
		Browser:      e.browser,
		Shell:        e.Shell,
		ShellPID:     e.pid != 0 && shellPIDs[e.pid],
		ImageBase:    imageBase,
		IsolatedBase: isoBase,
	}
}

// chromeFrameNoteLocked records one apply outcome in the console:
// full/partial applications say which parts landed; a zero mask
// (unsupported OS -- the Win10 case) is logged once per process,
// not once per window, and leaves the seeded dark theme as the
// delivered layer. Caller holds chromeFrameMu.
func chromeFrameNoteLocked(hwnd uint64, mask uint, app string) {
	if mask == 0 {
		if !chromeFrameNoSupp {
			chromeFrameNoSupp = true
			logf("winmgr", "warn", "隔离 Chrome 窗口框：DWM 颜色/圆角属性未被系统接受（多为 Windows 10），已降级——仅保留配置里的深色主题播种")
		}
		return
	}
	var parts []string
	if mask&chromeFrameBorder != 0 {
		parts = append(parts, "边框色")
	}
	if mask&chromeFrameCaption != 0 {
		parts = append(parts, "标题栏色")
	}
	if mask&chromeFrameText != 0 {
		parts = append(parts, "标题文字色")
	}
	if mask&chromeFrameCorner != 0 {
		parts = append(parts, "圆角")
	}
	logf("winmgr", "info", "隔离 Chrome 窗口已套用终端深色框（%s）：%s", strings.Join(parts, "、"), app)
}

// chromeFrameApplyFor frames one window at settle time when it
// is eligible and not yet booked.
func chromeFrameApplyFor(hwnd uint64) {
	iso := isolatedBrowserBase()
	if iso == "" {
		return
	}
	extMu.Lock()
	e, ok := extWins[hwnd]
	if !ok {
		extMu.Unlock()
		return
	}
	shellPIDs := map[uint32]bool{}
	for _, x := range extWins {
		if x.Shell && x.pid != 0 {
			shellPIDs[x.pid] = true
		}
	}
	pid, app := e.pid, e.App
	facts := chromeFrameFactsFor(e, "", shellPIDs, iso)
	extMu.Unlock()
	for _, p := range platformProcessSnapshot() {
		if p.PID == pid {
			facts.ImageBase = strings.ToLower(filepath.Base(p.ImagePath))
			break
		}
	}
	if !chromeFrameEligible(facts) {
		return
	}
	chromeFrameMu.Lock()
	defer chromeFrameMu.Unlock()
	if chromeFrameBook[hwnd] {
		return
	}
	mask := platformApplyChromeFrame(hwnd)
	chromeFrameBook[hwnd] = true
	chromeFrameNoteLocked(hwnd, mask, app)
}

// chromeFrameSweep is the patrol pass: frame eligible windows
// that settled before the book knew them (or whose facts only
// became readable later), and restore + unbook windows that left
// the registry while still alive (an unclaim must hand the
// window its own frame back).
func chromeFrameSweep(byPID map[uint32]procFact, shellPIDs map[uint32]bool) {
	iso := isolatedBrowserBase()
	if iso == "" {
		return
	}
	extMu.Lock()
	type ent struct {
		h    uint64
		f    chromeFrameFacts
		app  string
		live bool
	}
	ents := make([]ent, 0, len(extWins))
	registered := make(map[uint64]bool, len(extWins))
	for h, e := range extWins {
		registered[h] = true
		img := ""
		if pf, ok := byPID[e.pid]; ok {
			img = strings.ToLower(filepath.Base(pf.ImagePath))
		}
		ents = append(ents, ent{h, chromeFrameFactsFor(e, img, shellPIDs, iso), e.App, true})
	}
	extMu.Unlock()
	chromeFrameMu.Lock()
	defer chromeFrameMu.Unlock()
	for h := range chromeFrameBook {
		if !registered[h] {
			if platformWindowExists(h) {
				platformRestoreChromeFrame(h)
				logf("winmgr", "info", "隔离 Chrome 窗口已离册，终端深色框已还原为系统默认")
			}
			delete(chromeFrameBook, h)
		}
	}
	for _, en := range ents {
		if chromeFrameBook[en.h] || !chromeFrameEligible(en.f) {
			continue
		}
		mask := platformApplyChromeFrame(en.h)
		chromeFrameBook[en.h] = true
		chromeFrameNoteLocked(en.h, mask, en.app)
	}
}

// chromeFrameRestoreAll hands every booked window its own frame
// back. Called from prepareWindowsForDaemonExit before the
// process goes (winset.go's exit safety line).
func chromeFrameRestoreAll() {
	chromeFrameMu.Lock()
	defer chromeFrameMu.Unlock()
	n := 0
	for h := range chromeFrameBook {
		if platformWindowExists(h) {
			platformRestoreChromeFrame(h)
			n++
		}
		delete(chromeFrameBook, h)
	}
	if n > 0 {
		logf("winmgr", "info", "开窗礼仪：守护进程退出前已还原 %d 个隔离 Chrome 窗口的边框样式", n)
	}
}
