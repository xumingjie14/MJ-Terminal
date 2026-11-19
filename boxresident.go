package main

// ---------- v0.71 test7: 锁住后台 for native (sandboxed) apps ----------
//
// The web resident engine (resident.go) locks WEBAPPS-ledger
// entries by URL. This file extends the same promise to the
// apps the user installed into the Sandboxie box (the box
// registry in sandbox.go: WeChat, Telegram desktop, VLC,
// Notepad++, ...), under the binding rulings of the
// independent risk review (mj-terminal-v071-test7/
// RISK-REVIEW.md):
//
//   - B1 attribution: there is no stored "window -> box app"
//     fact anywhere in the daemon (classifyOwnership only
//     proves a process is IN the box). Attribution is computed
//     fresh EVERY TICK from the live snapshot: a registered
//     window whose process is on the box PID list AND whose
//     image path matches a registry entry -- exact Exe match
//     first, InstallDir prefix second, both spellings of the
//     path (virtual as reported, physical shadow converted
//     through physicalToVirtual) -- belongs to that app. An
//     ambiguous match (two registrations sharing one exe, two
//     install dirs claiming one helper) attributes NOTHING:
//     fail-open toward the user, never guess. Attribution is
//     never pinned on extWindow.pid, which is a claim-time
//     stamp that process regeneration strands.
//   - B2 alignment: the lock's unit is the APP (registry id);
//     interception and close-translation act only on ONE
//     aligned main window per app, chosen deterministically
//     (launch-claimed window first, then an exact-exe window,
//     then the lowest handle) and sticky while it lives
//     (chooseAlignedWindow). Secondary windows of a locked
//     app -- settings windows, viewers -- keep their real
//     close; owned dialogs are never claimed at all.
//   - B3 geometry: standard-frame windows reuse the system
//     caption geometry (closeZoneFor). Self-drawn windows get
//     their close zone from a PATROL-SIDE WM_NCHITTEST probe,
//     cached per (hwnd, rect, dpi); the hook callback only
//     ever reads the cache -- asking the window from inside
//     the callback is the test5 death (a hung target eats the
//     LowLevelHooksTimeout and the OS silently unhooks). A
//     self-drawn window with no probed HTCLOSE run keeps its
//     real mouse close; Alt+F4 and the Stage card menu still
//     hide it, and Settings says so per app.
//   - B4 no auto-relaunch by default: from outside, a tray
//     quit, a menu quit, a self-update and a crash are the
//     SAME observation (window gone, process gone) -- the
//     daemon is not the box apps' parent and holds no exit
//     channel. So a vanished locked app is NOT relaunched:
//     its card turns "exited" （已退出） and one click brings
//     it back through launchBoxApp (boxed data persists, so
//     e.g. WeChat returns signed-in). Automatic relaunch is a
//     per-app opt-in in settings, default off.
//   - B5 tray coexistence: a window the APP hid itself (its
//     own tray behaviour) never enters our books -- we only
//     ever book windows daemonHideCore hid for us, and unlock
//     only ever surfaces those. Whether a host tray icon can
//     re-raise a window WE hid is a field question (see the
//     review's gate list), not something presentation code
//     may assume.
//   - B6 fuse: repeated daemon-hide failures on one window
//     drop that window out of the intercept snapshot and say
//     so (console + Settings), instead of swallowing the
//     user's × and then failing to hide -- the worst degrade.
//   - B7 unlock: the closing invariant mirrors the web one
//     (residentUnlockShow) but reads ONLY our own hide book:
//     the aligned window we hid comes back to visibility (or
//     a set ledger owns it); a window the app hid itself is
//     never force-shown by an unlock.
//
// The engine rides the patrol cadence (patrolTick calls
// boxResidentTick with the tick's own process/window/box-PID
// snapshots, after that tick's claims have settled).

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- registry helpers ----------

// boxAppsList returns the box registry as a slice sorted by
// id (loadBoxApps hands back a map; every consumer here wants
// a deterministic order).
func boxAppsList() []boxApp {
	reg := loadBoxApps()
	out := make([]boxApp, 0, len(reg))
	for _, a := range reg {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func boxAppByID(id string) (boxApp, bool) {
	a, ok := loadBoxApps()[id]
	return a, ok
}

// boxAppName renders a registry id for log lines and cards:
// the registry name when the app is still registered, else
// the bare id.
func boxAppName(id string) string {
	if a, ok := boxAppByID(id); ok && a.Name != "" {
		return a.Name
	}
	return id
}

// boxRegistryIDSet returns the registry's id set. ok=false
// when the registry dataset itself cannot be read (absent or
// broken): pruning and validation must then stand down,
// because absence proves nothing either way.
func boxRegistryIDSet() (map[string]bool, bool) {
	b, status, err := dsLoad("sandbox-apps")
	if err != nil || status != dsOK {
		return nil, false
	}
	reg := map[string]boxApp{}
	if err := json.Unmarshal(b, &reg); err != nil {
		return nil, false
	}
	out := make(map[string]bool, len(reg))
	for id := range reg {
		out[id] = true
	}
	return out, true
}

// boxAmbiguousExeApps returns the ids of registered apps
// whose main exe is shared with another registration
// (newBoxAppID allows the same exe twice, with a -2 suffix).
// Their windows can never be told apart, so they are not
// lockable and attribution refuses them (review §1).
func boxAmbiguousExeApps(apps []boxApp) map[string]bool {
	byExe := map[string][]string{}
	for _, a := range apps {
		k := normalizeWinPath(a.Exe)
		byExe[k] = append(byExe[k], a.ID)
	}
	out := map[string]bool{}
	for _, ids := range byExe {
		if len(ids) > 1 {
			for _, id := range ids {
				out[id] = true
			}
		}
	}
	return out
}

// ---------- lock sets (settings store) ----------

func boxAppLockSet() map[string]bool {
	out := map[string]bool{}
	for id, on := range settingsGet().BoxAppLocks {
		if on {
			out[id] = true
		}
	}
	return out
}

func boxAutoRelaunchSet() map[string]bool {
	out := map[string]bool{}
	for id, on := range settingsGet().BoxAppAutoRelaunch {
		if on {
			out[id] = true
		}
	}
	return out
}

// pruneBoxAppIDMap keeps only true entries whose id is in
// valid (sanitizeSettings runs it on every load with the box
// registry's ids).
func pruneBoxAppIDMap(m map[string]bool, valid map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	out := make(map[string]bool, len(m))
	for id, on := range m {
		if on && valid[id] {
			out[id] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// settingsSetBoxAppLock persists one native lock flip through
// the settings store. Called by the lock handler only (the
// web twin is settingsSetWebAppLock).
func settingsSetBoxAppLock(id string, on bool) error {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	next := setState.cur
	locks := make(map[string]bool, len(next.BoxAppLocks)+1)
	for k, v := range next.BoxAppLocks {
		locks[k] = v
	}
	if on {
		locks[id] = true
	} else {
		delete(locks, id)
	}
	if ids, ok := boxRegistryIDSet(); ok {
		locks = pruneBoxAppIDMap(locks, ids)
	}
	next.BoxAppLocks = locks
	setState.cur = next
	return settingsSaveLocked()
}

// settingsSetBoxAppAutoRelaunch persists one per-app
// auto-relaunch flip (the B4 opt-in).
func settingsSetBoxAppAutoRelaunch(id string, on bool) error {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	next := setState.cur
	m := make(map[string]bool, len(next.BoxAppAutoRelaunch)+1)
	for k, v := range next.BoxAppAutoRelaunch {
		m[k] = v
	}
	if on {
		m[id] = true
	} else {
		delete(m, id)
	}
	if ids, ok := boxRegistryIDSet(); ok {
		m = pruneBoxAppIDMap(m, ids)
	}
	next.BoxAppAutoRelaunch = m
	setState.cur = next
	return settingsSaveLocked()
}

// ---------- attribution (B1, pure) ----------

// boxAttribution is one window's (or process's) assignment to
// a box app, with the basis that earned it: "exe" (the image
// IS the app's main exe) or "dir" (the image lives under the
// app's install dir -- a helper process of the app).
type boxAttribution struct {
	AppID string
	Basis string
}

// attributeWindow assigns an image path to at most one box
// app. inBox is the Sandboxie PID-list fact for the process
// owning the image: WITHOUT it the same path string could be
// the host's own install of the same program (a host WeChat
// reports the very virtual path the boxed one does), so the
// box fact gates everything and the path only ever picks
// WHICH app. Both path spellings are tried: the image as
// reported, and -- when it is a physical path under the box
// root -- its virtual conversion. Exact exe match beats
// install-dir prefix; any tie attributes nothing.
func attributeWindow(image string, inBox bool, apps []boxApp, boxRoot string) (boxAttribution, bool) {
	if !inBox || image == "" || len(apps) == 0 {
		return boxAttribution{}, false
	}
	cand := image
	if v := physicalToVirtual(boxRoot, image); v != "" {
		cand = v
	}
	nc := normalizeWinPath(cand)
	var exact []boxApp
	for _, a := range apps {
		if normalizeWinPath(a.Exe) == nc {
			exact = append(exact, a)
		}
	}
	if len(exact) == 1 {
		return boxAttribution{AppID: exact[0].ID, Basis: "exe"}, true
	}
	if len(exact) > 1 {
		return boxAttribution{}, false // same exe registered twice: indistinguishable
	}
	var pref []boxApp
	for _, a := range apps {
		if a.InstallDir != "" && pathUnderDir(nc, a.InstallDir) {
			pref = append(pref, a)
		}
	}
	if len(pref) == 1 {
		return boxAttribution{AppID: pref[0].ID, Basis: "dir"}, true
	}
	return boxAttribution{}, false
}

// boxAttrSnapData is the patrol's latest attribution of
// every registered window (hwnd -> assignment). The Stage
// list handler, the close handler and the unlock path read
// it; it is rebuilt wholesale each tick, never edited in
// place.
type boxAttrSnapData struct {
	byHWND map[uint64]boxAttribution
}

var boxAttrSnap atomic.Pointer[boxAttrSnapData]

func boxAttrSnapshot() map[uint64]boxAttribution {
	s := boxAttrSnap.Load()
	if s == nil {
		return nil
	}
	return s.byHWND
}

// ---------- launch claims (B2's first alignment rung) ----------

// boxLaunchClaims remembers which window each box launch
// claimed (hwnd -> app id), written by pollBoxLaunch's claim
// successes in sandbox.go. It is an alignment PREFERENCE
// only -- attribution still has to agree every tick before
// the window counts as the app's.
var boxLaunchClaims = struct {
	sync.Mutex
	m map[uint64]string
}{m: map[uint64]string{}}

func boxNoteLaunchClaim(hwnd uint64, appID string) {
	if hwnd == 0 || appID == "" {
		return
	}
	boxLaunchClaims.Lock()
	boxLaunchClaims.m[hwnd] = appID
	boxLaunchClaims.Unlock()
}

func boxLaunchClaimFor(hwnd uint64) string {
	boxLaunchClaims.Lock()
	defer boxLaunchClaims.Unlock()
	return boxLaunchClaims.m[hwnd]
}

// ---------- alignment (B2, pure) ----------

// alignedCand is one candidate window for an app's aligned
// (main) window: its attribution basis this tick and whether
// a box launch claimed it for this app.
type alignedCand struct {
	HWND          uint64
	Basis         string
	LaunchClaimed bool
}

// chooseAlignedWindow picks the app's ONE aligned window.
// Stickiness first: a live previous alignment never moves --
// the web book's remap rule (residentRemapHiddenBook) voids a
// hide debt the moment alignment changes, so a jittering
// alignment would orphan the very window we hid. Among fresh
// candidates the order is launch-claimed, exact-exe,
// install-dir, lowest handle -- a total order, so the choice
// never depends on map iteration.
func chooseAlignedWindow(prev uint64, cands []alignedCand) uint64 {
	if prev != 0 {
		for _, c := range cands {
			if c.HWND == prev {
				return prev
			}
		}
	}
	rank := func(c alignedCand) int {
		if c.LaunchClaimed {
			return 0
		}
		if c.Basis == "exe" {
			return 1
		}
		return 2
	}
	best := uint64(0)
	bestRank := 0
	for _, c := range cands {
		r := rank(c)
		if best == 0 || r < bestRank || (r == bestRank && c.HWND < best) {
			best, bestRank = c.HWND, r
		}
	}
	return best
}

// ---------- card state (pure) ----------

type boxCardFacts struct {
	HasWindow     bool
	WindowVisible bool // visible AND not minimized
	ProcAlive     bool // a process of the app lives (tray-only counts)
	AutoRelaunch  bool
	Summoning     bool
	Attempts      int
}

// decideBoxCardState is the native card rulebook: the web
// four states plus "exited" (B4) -- no window AND no process,
// and nobody is bringing it back on their own. A live process
// without a window (the app's own tray mode) reads as "back":
// the app is still running in the background, which is the
// truth the card owes.
func decideBoxCardState(f boxCardFacts) string {
	if f.HasWindow {
		if f.WindowVisible {
			return string(residentCardFront)
		}
		return string(residentCardBack)
	}
	if f.ProcAlive {
		return string(residentCardBack)
	}
	if f.Summoning {
		return string(residentCardRestoring)
	}
	if f.AutoRelaunch {
		if f.Attempts >= residentUnstableAttempts {
			return string(residentCardUnstable)
		}
		return string(residentCardRestoring)
	}
	return "exited"
}

// ---------- hide fuse (B6, pure) ----------

// boxHideFuseThreshold is how many consecutive daemon-hide
// failures on one window pause interception of that window.
const boxHideFuseThreshold = 3

func decideHideFuse(consecutiveFails int) bool {
	return consecutiveFails >= boxHideFuseThreshold
}

// ---------- probe cache (B3) ----------

// boxProbeEntry is one window's probed close zone (or its
// failure), keyed by the geometry it was probed under.
type boxProbeEntry struct {
	RectL, RectT, RectR, RectB int32
	DPI                        int32
	Zone                       closeZone
	OK                         bool
	At                         time.Time
}

// boxProbeRetryDelay bounds re-probes of a window that gave
// no HTCLOSE run: the answer is a property of the app, not of
// the moment, so re-asking every tick would just spam a busy
// window with messages.
const boxProbeRetryDelay = 30 * time.Second

// probeCacheValid reports whether a cached probe still
// answers for this geometry: same rect, same DPI, and either
// it found a zone (geometry-keyed, stable) or it failed
// recently enough that re-probing is not due.
func probeCacheValid(e boxProbeEntry, l, t, r, b, dpi int32, now time.Time) bool {
	if e.RectL != l || e.RectT != t || e.RectR != r || e.RectB != b || e.DPI != dpi {
		return false
	}
	if e.OK {
		return true
	}
	return now.Sub(e.At) < boxProbeRetryDelay
}

// hitSample is one WM_NCHITTEST answer along the caption
// band: the x probed and whether the window called it its
// close button (HTCLOSE).
type hitSample struct {
	X     int32
	Close bool
}

// zoneFromHitRun turns a left-to-right row of hit samples
// into a close zone: the RIGHTMOST maximal run of close
// answers wins (the close button is the caption's rightmost
// cell; a stray close answer elsewhere is not it). The zone
// extends half a step past the run's samples on both sides --
// the samples prove points, the button fills between them.
// Runs narrower than 16 px are specks, not buttons.
func zoneFromHitRun(samples []hitSample, step, bandTop, bandH int32) (closeZone, bool) {
	runStart, runEnd := -1, -1
	cur := -1
	flush := func(end int) {
		if cur >= 0 {
			runStart, runEnd = cur, end
			cur = -1
		}
	}
	for i, s := range samples {
		if s.Close {
			if cur < 0 {
				cur = i
			}
		} else {
			flush(i - 1)
		}
	}
	flush(len(samples) - 1)
	if runStart < 0 || step <= 0 || bandH <= 0 {
		return closeZone{}, false
	}
	x0 := samples[runStart].X - (step - 1)
	x1 := samples[runEnd].X + step
	if x1-x0 < 16 {
		return closeZone{}, false
	}
	return closeZone{X0: x0, X1: x1, Y0: bandTop, Y1: bandTop + bandH}, true
}

// ---------- intercept target snapshot ----------

// boxInterceptTarget is everything the hook callback may
// know about one locked native app's aligned window: its lock
// key, the attribution basis and frame kind (telemetry), and
// how its close zone is sourced -- "formula" (standard frame,
// closeZoneFor computes it in the callback from live facts),
// "probe" (self-drawn, Zone carries the patrol-probed rect)
// or "none" (self-drawn, no zone: the mouse path passes).
type boxInterceptTarget struct {
	AppID     string
	Basis     string
	FrameKind string // "standard" | "selfdrawn"
	ZoneKind  string // "formula" | "probe" | "none"
	Zone      closeZone
}

type boxInterceptSnapshot struct {
	targets map[uint64]boxInterceptTarget
}

var boxInterceptSnap atomic.Pointer[boxInterceptSnapshot]

// interceptNativeTarget is called from the hook callbacks:
// one map read on an immutable snapshot, no locks.
func interceptNativeTarget(hwnd uint64) (boxInterceptTarget, bool) {
	s := boxInterceptSnap.Load()
	if s == nil {
		return boxInterceptTarget{}, false
	}
	t, ok := s.targets[hwnd]
	return t, ok
}

// boxInterceptResolve re-verifies, against CURRENT state,
// that hwnd is still the aligned window of a still-locked
// box app (the executor-side second check, mirroring
// executeInterceptHide's web re-verification).
func boxInterceptResolve(hwnd uint64) (string, bool) {
	locks := boxAppLockSet()
	if len(locks) == 0 {
		return "", false
	}
	a, ok := boxAttrSnapshot()[hwnd]
	if !ok || !locks[a.AppID] {
		return "", false
	}
	st := boxBookSnapshot()[a.AppID]
	if st == nil || st.AlignedHWND != hwnd {
		return "", false
	}
	return a.AppID, true
}

// boxAlignedLockFor reports whether hwnd is the aligned
// window of a locked box app (the Stage close handler's
// translation test, B2: only the aligned window translates;
// every other window of the app really closes).
func boxAlignedLockFor(hwnd uint64) (string, bool) {
	return boxInterceptResolve(hwnd)
}

// ---------- engine ----------

// boxResidentAppState is one locked native app's runtime
// book (never persisted; the lock SET is the durable fact).
type boxResidentAppState struct {
	AlignedHWND  uint64 // the app's one aligned main window (0 = none this tick)
	AlignedBasis string // attribution basis of the aligned window ("exe"/"dir")
	HiddenHWND   uint64 // the window WE hid (0 = none / user is looking at it)
	MissingSince time.Time
	Attempts     int
	NextTry      time.Time
	HidePending  bool // an auto-relaunch is owed a hide when its window lands
	Summoning    bool // a user-summoned restore launch is in flight
	Launching    bool // an auto-relaunch launch is in flight
	Warned       bool
	ProcAlive    bool   // last tick: a process of this app lives
	CardState    string // last tick's decideBoxCardState verdict
	ProbeMode    string // aligned window's close-zone source: "formula"/"probe"/"none"/"fused"
	HideFails    int    // consecutive daemon-hide failures on the aligned window
	FusedHWND    uint64 // window the B6 fuse dropped from interception (0 = none)
}

var boxSt = struct {
	sync.Mutex
	bootedAt   time.Time
	apps       map[string]*boxResidentAppState
	probeCache map[uint64]boxProbeEntry
	attrPrev   map[uint64]boxAttribution
}{
	apps:       map[string]*boxResidentAppState{},
	probeCache: map[uint64]boxProbeEntry{},
	attrPrev:   map[uint64]boxAttribution{},
}

func boxBookSnapshot() map[string]*boxResidentAppState {
	boxSt.Lock()
	defer boxSt.Unlock()
	out := make(map[string]*boxResidentAppState, len(boxSt.apps))
	for id, st := range boxSt.apps {
		cp := *st
		out[id] = &cp
	}
	return out
}

// boxProbeZone returns the probed close zone for a self-drawn
// window, through the per-geometry cache. Patrol goroutine
// only (it may message the target window; see boxprobe_*).
func boxProbeZone(hwnd uint64) (closeZone, bool) {
	l, t, r, b, ok := platformWindowRect(hwnd)
	if !ok {
		return closeZone{}, false
	}
	dpi := platformWindowDPI(hwnd)
	now := time.Now()
	boxSt.Lock()
	e, have := boxSt.probeCache[hwnd]
	boxSt.Unlock()
	if have && probeCacheValid(e, l, t, r, b, dpi, now) {
		return e.Zone, e.OK
	}
	zone, zok := platformProbeCloseZone(hwnd)
	boxSt.Lock()
	boxSt.probeCache[hwnd] = boxProbeEntry{
		RectL: l, RectT: t, RectR: r, RectB: b, DPI: dpi,
		Zone: zone, OK: zok, At: now,
	}
	boxSt.Unlock()
	return zone, zok
}

// boxDaemonHide is the native engine's daemon-intentional
// hide: the shared core (daemonHideCore) plus this engine's
// book and the B6 fuse accounting.
func boxDaemonHide(hwnd uint64, appID, via string) bool {
	registered, hid := daemonHideCore(hwnd)
	if !hid {
		if registered {
			logf("sandbox", "warn", "锁住后台：%s 的窗口隐藏失败（%s），将在下个巡逻周期重试", boxAppName(appID), via)
			boxNoteHideResult(appID, hwnd, false)
		}
		return false
	}
	boxNoteHideResult(appID, hwnd, true)
	boxSt.Lock()
	if st := boxSt.apps[appID]; st != nil {
		st.HiddenHWND = hwnd
		st.HidePending = false
	}
	boxSt.Unlock()
	logf("sandbox", "info", "锁住后台：%s 已转入后台运行（%s）", boxAppName(appID), via)
	return true
}

// boxNoteHideResult folds one daemon-hide outcome into the
// fuse book: success clears the streak (and a fused window is
// readmitted, with a line); failure advances it, and at the
// threshold the window leaves the intercept snapshot until
// its alignment changes or a hide succeeds again.
func boxNoteHideResult(appID string, hwnd uint64, ok bool) {
	boxSt.Lock()
	st := boxSt.apps[appID]
	if st == nil {
		boxSt.Unlock()
		return
	}
	if ok {
		st.HideFails = 0
		if st.FusedHWND == hwnd {
			st.FusedHWND = 0
			boxSt.Unlock()
			logf("sandbox", "info", "锁住后台：%s 的窗口隐藏已恢复，关窗变藏重新生效", boxAppName(appID))
			return
		}
		boxSt.Unlock()
		return
	}
	if st.AlignedHWND != hwnd {
		boxSt.Unlock()
		return // failures count only against the aligned window
	}
	st.HideFails++
	fuse := decideHideFuse(st.HideFails) && st.FusedHWND != hwnd
	if fuse {
		st.FusedHWND = hwnd
	}
	fails := st.HideFails
	boxSt.Unlock()
	if fuse {
		logf("sandbox", "warn", "锁住后台：%s 的窗口连续 %d 次隐藏失败，已暂停对这个窗口的关窗变藏（点 × 将按应用原本的方式关闭）；解除锁住后重新锁住可恢复", boxAppName(appID), fails)
	}
}

// boxAutoRelaunchDue is the opt-in relaunch rule (pure): the
// web engine's disappearance discipline (boot grace, confirm
// window, backoff gate) with the native precondition that the
// app is FULLY gone -- a live tray process is never chased.
func boxAutoRelaunchDue(st boxResidentAppState, procAlive bool, now, bootedAt time.Time) bool {
	if procAlive || st.Summoning || st.Launching {
		return false
	}
	if now.Before(bootedAt.Add(residentBootGrace)) {
		return false
	}
	if st.MissingSince.IsZero() || now.Sub(st.MissingSince) < residentConfirm {
		return false
	}
	return !now.Before(st.NextTry)
}

// boxResidentTick runs on the patrol cadence (patrolTick,
// after this tick's claims have settled): rebuild attribution
// from the live snapshot, advance every locked app's book,
// maintain the probe cache and the intercept snapshot.
func boxResidentTick(procs []procFact, wins []windowFact, boxPIDs map[uint32]bool) {
	if runtime.GOOS != "windows" {
		return
	}
	apps := boxAppsList()
	locks := boxAppLockSet()
	auto := boxAutoRelaunchSet()
	boxRoot := sandboxBoxRoot()
	byPID := make(map[uint32]procFact, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	liveWin := make(map[uint64]windowFact, len(wins))
	for _, wf := range wins {
		liveWin[wf.HWND] = wf
	}

	// --- attribution over the registered windows (B1) ---
	extMu.Lock()
	regWins := make([]*extWindow, 0, len(extWins))
	for _, e := range extWins {
		cp := *e
		regWins = append(regWins, &cp)
	}
	extMu.Unlock()
	attr := map[uint64]boxAttribution{}
	for _, e := range regWins {
		if e.Shell || e.browser {
			continue
		}
		wf, ok := liveWin[e.HWND]
		if !ok {
			continue
		}
		pf, ok := byPID[wf.PID]
		if !ok {
			continue
		}
		if a, ok := attributeWindow(pf.ImagePath, boxPIDs[wf.PID], apps, boxRoot); ok {
			attr[e.HWND] = a
		}
	}
	boxAttrSnap.Store(&boxAttrSnapData{byHWND: attr})

	// Attribution telemetry: one line when a LOCKED app's
	// window is newly attributed or its basis changes -- the
	// evidence the next field diagnosis reads (§8).
	boxSt.Lock()
	for h, a := range attr {
		if !locks[a.AppID] {
			continue
		}
		if prev, ok := boxSt.attrPrev[h]; !ok || prev != a {
			logf("winmgr", "info", "锁住后台：窗口 %d 归属应用「%s」（依据：%s）", h, boxAppName(a.AppID), boxBasisText(a.Basis))
		}
	}
	boxSt.attrPrev = attr
	// Probe cache entries for dead windows leave with them.
	for h := range boxSt.probeCache {
		if _, ok := liveWin[h]; !ok {
			delete(boxSt.probeCache, h)
		}
	}
	boxSt.Unlock()
	// Launch-claim ledger: dead handles leave it.
	boxLaunchClaims.Lock()
	for h := range boxLaunchClaims.m {
		if _, ok := liveWin[h]; !ok {
			delete(boxLaunchClaims.m, h)
		}
	}
	boxLaunchClaims.Unlock()

	// --- process presence per app (tray-only counts) ---
	procAlive := map[string]bool{}
	for _, p := range procs {
		if a, ok := attributeWindow(p.ImagePath, boxPIDs[p.PID], apps, boxRoot); ok {
			procAlive[a.AppID] = true
		}
	}

	now := time.Now()
	targets := map[uint64]boxInterceptTarget{}
	var hides []struct {
		hwnd uint64
		id   string
	}
	var launches []boxApp
	boxSt.Lock()
	if boxSt.bootedAt.IsZero() {
		boxSt.bootedAt = now
	}
	bootedAt := boxSt.bootedAt
	for id := range boxSt.apps {
		if !locks[id] {
			delete(boxSt.apps, id)
		}
	}
	for id := range locks {
		app, ok := boxAppByID(id)
		if !ok {
			continue
		}
		st := boxSt.apps[id]
		if st == nil {
			st = &boxResidentAppState{}
			boxSt.apps[id] = st
		}
		// Candidates: registered, attributed, main-window shape.
		var cands []alignedCand
		for _, e := range regWins {
			a, is := attr[e.HWND]
			if !is || a.AppID != id {
				continue
			}
			if !isAppWindowShape(liveWin[e.HWND]) {
				continue
			}
			cands = append(cands, alignedCand{
				HWND:          e.HWND,
				Basis:         a.Basis,
				LaunchClaimed: boxLaunchClaimFor(e.HWND) == id,
			})
		}
		aligned := chooseAlignedWindow(st.AlignedHWND, cands)
		if aligned != st.AlignedHWND {
			st.AlignedHWND = aligned
			st.HideFails = 0
			if st.FusedHWND != 0 && st.FusedHWND != aligned {
				st.FusedHWND = 0
			}
		}
		st.AlignedBasis = ""
		if aligned != 0 {
			st.AlignedBasis = attr[aligned].Basis
		}
		st.ProcAlive = procAlive[id]
		if aligned != 0 {
			st.MissingSince = time.Time{}
			st.Attempts = 0
			st.Warned = false
			st.NextTry = time.Time{}
			// The alignment may have moved under the book: a
			// hide debt naming any other window is void (the
			// web book's L1-I2 rule, shared).
			st.HiddenHWND = residentRemapHiddenBook(st.HiddenHWND, aligned)
			visNow := platformWindowVisible(aligned)
			if st.HiddenHWND == aligned && visNow {
				// The user raised it (a Stage click shows as
				// part of raising): residency steps back.
				st.HiddenHWND = 0
				logf("sandbox", "info", "锁住后台：%s 的窗口已回到台前", app.Name)
			}
			if st.HidePending && !st.Summoning {
				st.HidePending = false
				hides = append(hides, struct {
					hwnd uint64
					id   string
				}{aligned, id})
			}
			st.CardState = decideBoxCardState(boxCardFacts{
				HasWindow:     true,
				WindowVisible: visNow && !platformWindowIconic(aligned),
			})
		} else {
			if st.HiddenHWND != 0 {
				st.HiddenHWND = 0 // the hidden window is the one that vanished
			}
			if st.ProcAlive {
				st.MissingSince = time.Time{}
			} else {
				if st.MissingSince.IsZero() {
					st.MissingSince = now
				}
				if auto[id] && boxAutoRelaunchDue(*st, false, now, bootedAt) {
					st.Attempts++
					st.NextTry = now.Add(residentBackoff(st.Attempts))
					st.HidePending = true
					st.Launching = true
					launches = append(launches, app)
					if st.Attempts >= 4 && !st.Warned {
						st.Warned = true
						logf("sandbox", "warn", "锁住后台：%s 多次自动重新打开仍未稳定（第 %d 次），已进入退避（最长 5 分钟一次）", app.Name, st.Attempts)
					} else {
						logf("sandbox", "info", "锁住后台：%s 已退出，按你的设置自动重新打开（第 %d 次）", app.Name, st.Attempts)
					}
				}
			}
			st.CardState = decideBoxCardState(boxCardFacts{
				ProcAlive:    st.ProcAlive,
				AutoRelaunch: auto[id],
				Summoning:    st.Summoning,
				Attempts:     st.Attempts,
			})
		}
		// --- intercept target + probe maintenance ---
		st.ProbeMode = ""
		if aligned != 0 && st.FusedHWND != aligned {
			tgt := boxInterceptTarget{AppID: id, Basis: attr[aligned].Basis}
			if style, sok := platformWindowStyle(aligned); sok && style&boxWsCaption == boxWsCaption {
				tgt.FrameKind = "standard"
				tgt.ZoneKind = "formula"
			} else {
				tgt.FrameKind = "selfdrawn"
				if zone, zok := boxProbeZone(aligned); zok {
					tgt.ZoneKind = "probe"
					tgt.Zone = zone
				} else {
					tgt.ZoneKind = "none"
				}
			}
			st.ProbeMode = tgt.ZoneKind
			targets[aligned] = tgt
		}
		if st.FusedHWND != 0 && st.FusedHWND == aligned {
			st.ProbeMode = "fused"
		}
	}
	boxSt.Unlock()
	boxInterceptSnap.Store(&boxInterceptSnapshot{targets: targets})

	for _, hd := range hides {
		if boxDaemonHide(hd.hwnd, hd.id, "常驻引擎") {
			continue
		}
		boxSt.Lock()
		if st := boxSt.apps[hd.id]; st != nil {
			st.HidePending = true
		}
		boxSt.Unlock()
	}
	for _, app := range launches {
		app := app
		go func() {
			err := launchBoxApp(app)
			boxSt.Lock()
			if st := boxSt.apps[app.ID]; st != nil {
				st.Launching = false
				if err != nil {
					// No window is coming: the armed hide must
					// not stay pending (the web engine's rule).
					st.HidePending = false
					st.MissingSince = time.Now()
				}
			}
			boxSt.Unlock()
			if err != nil {
				logf("sandbox", "warn", "锁住后台：%s 自动重新打开失败：%v", app.Name, err)
			}
		}()
	}
}

// boxWsCaption is the WS_CAPTION style pair (WS_BORDER |
// WS_DLGFRAME): a window carrying it draws the system caption
// buttons the closeZoneFor formula describes. (winmgr_windows.go
// has its own wsCaption for the immersion surgery; this file
// is platform-neutral and needs the value on every build.)
const boxWsCaption = 0x00C00000

// boxBasisText renders an attribution basis for console lines.
func boxBasisText(basis string) string {
	switch basis {
	case "exe":
		return "主程序精确匹配"
	case "dir":
		return "安装目录"
	}
	return basis
}

// ---------- immediate restore ----------

// boxResidentRestore performs the windowless card's click for
// a native app whose window is gone: a user-summoned bring-up
// through launchBoxApp (whose own second-launch path presents
// a still-running instance instead of starting a second one).
// Any armed auto-relaunch hide is disarmed -- a window the
// user asked for arrives PRESENTED.
func boxResidentRestore(id string) {
	app, ok := boxAppByID(id)
	if !ok {
		return
	}
	boxSt.Lock()
	st := boxSt.apps[id]
	if st == nil {
		st = &boxResidentAppState{}
		boxSt.apps[id] = st
	}
	if st.Summoning {
		boxSt.Unlock()
		return
	}
	st.Summoning = true
	st.HidePending = false
	st.Attempts = 0
	st.Warned = false
	st.NextTry = time.Time{}
	boxSt.Unlock()
	go func() {
		err := launchBoxApp(app)
		boxSt.Lock()
		if st := boxSt.apps[id]; st != nil {
			st.Summoning = false
			if err != nil {
				st.MissingSince = time.Now()
			}
		}
		boxSt.Unlock()
		if err != nil {
			logf("sandbox", "warn", "锁住后台：%s 立即恢复启动失败：%v", app.Name, err)
		}
	}()
	logf("sandbox", "info", "锁住后台：%s 已从台前调度立即恢复", app.Name)
}

// ---------- unlock closing invariant (B7) ----------

// boxUnlockShow performs the native unlock closing invariant:
// BEFORE the lock drops, the window WE hid for this app must
// end up visible or owned by a window-set ledger -- never an
// unbooked invisible orphan. It reads ONLY our own hide book:
// a window the app hid itself (its tray behaviour) is not in
// the book and is never force-shown by an unlock.
func boxUnlockShow(id string) {
	boxSt.Lock()
	var hidden uint64
	if st := boxSt.apps[id]; st != nil {
		hidden = st.HiddenHWND
	}
	boxSt.Unlock()
	if hidden == 0 {
		return
	}
	if a, ok := boxAttrSnapshot()[hidden]; !ok || a.AppID != id {
		return // no longer provably this app's window: hands off
	}
	if platformWindowVisible(hidden) {
		return
	}
	if grpOwnsHidden(hidden) || soloOwns(hidden) {
		return
	}
	if platformShowBackWindow(hidden) {
		extMu.Lock()
		if e, ok := extWins[hidden]; ok {
			e.hiddenClaim = false
		}
		extMu.Unlock()
		boxSt.Lock()
		if st := boxSt.apps[id]; st != nil && st.HiddenHWND == hidden {
			st.HiddenHWND = 0
		}
		boxSt.Unlock()
		logf("sandbox", "info", "锁住后台：解除锁住时，%s 的窗口已从后台显示回来", boxAppName(id))
	}
}

// ---------- HTTP ----------

// boxLockResponse builds the lock endpoints' shared answer:
// every registered box app with its lock state (the Settings
// list renders from it), the apps that can never be locked
// (same-exe registrations, review §1) with their reason, and
// the close-becomes-hide layer's live state.
func boxLockResponse() boxAppLockResponse {
	apps := boxAppsList()
	locks := boxAppLockSet()
	auto := boxAutoRelaunchSet()
	book := boxBookSnapshot()
	amb := boxAmbiguousExeApps(apps)
	resp := boxAppLockResponse{OK: true, Apps: []boxAppLockInfo{}}
	resp.CloseHide, resp.CloseHideDegraded = interceptCloseHideState()
	for _, a := range apps {
		info := boxAppLockInfo{
			ID: a.ID, Name: a.Name,
			Locked: locks[a.ID], AutoRelaunch: auto[a.ID],
			Lockable: !amb[a.ID],
		}
		if locks[a.ID] {
			if st := book[a.ID]; st != nil {
				info.State = st.CardState
				info.CloseHideMode = st.ProbeMode
			}
		}
		resp.Apps = append(resp.Apps, info)
		if amb[a.ID] {
			resp.Unavailable = append(resp.Unavailable, boxAppUnavailable{
				ID: a.ID, Name: a.Name,
				Reason: "与另一个登记共用同一主程序，无法区分窗口归属",
			})
		}
	}
	return resp
}

// boxAppLockHandler serves /api/boxapps/lock:
//
//	GET -- every registered box app with its lock state.
//	POST {id, on} -- flip one app's 锁住后台 state. The Stage
//	  card menu and 设置 → 通知 → 锁住后台 share it.
func boxAppLockHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, boxLockResponse())
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		ID string `json:"id"`
		On bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	app, ok := boxAppByID(body.ID)
	if !ok {
		apiErrBadRequest(w, errCodeBadRequest, "这个应用不在隔离安装的登记里")
		return
	}
	if body.On && boxAmbiguousExeApps(boxAppsList())[body.ID] {
		apiErrBadRequest(w, errCodeBadRequest, "这个应用与另一个登记共用同一主程序，暂时无法区分它的窗口，先不能锁住后台")
		return
	}
	if !body.On {
		// B7 closing invariant, while the lock still stands.
		boxUnlockShow(body.ID)
	}
	if err := settingsSetBoxAppLock(body.ID, body.On); err != nil {
		logf("sandbox", "error", "锁住后台：保存失败（%s）：%v", body.ID, err)
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "保存失败")
		return
	}
	if body.On {
		boxSt.Lock()
		boxSt.apps[body.ID] = &boxResidentAppState{
			MissingSince: time.Now().Add(-residentConfirm - time.Second),
		}
		boxSt.Unlock()
		logf("sandbox", "info", "锁住后台：已锁住 %s——关闭窗口会转入后台继续运行；应用退出后不会自动重开，点台前调度的卡片即可重新打开", app.Name)
	} else {
		boxSt.Lock()
		delete(boxSt.apps, body.ID)
		boxSt.Unlock()
		logf("sandbox", "info", "锁住后台：已解除 %s 的锁住——之后关窗即真关", app.Name)
	}
	resp := boxLockResponse()
	resp.ID = body.ID
	resp.Locked = body.On
	writeJSON(w, resp)
}

// boxAppAutoRelaunchHandler serves POST
// /api/boxapps/autorelaunch {id, on}: the B4 per-app opt-in.
func boxAppAutoRelaunchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		ID string `json:"id"`
		On bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	app, ok := boxAppByID(body.ID)
	if !ok {
		apiErrBadRequest(w, errCodeBadRequest, "这个应用不在隔离安装的登记里")
		return
	}
	if err := settingsSetBoxAppAutoRelaunch(body.ID, body.On); err != nil {
		logf("sandbox", "error", "锁住后台：保存「退出后自动重新打开」失败（%s）：%v", body.ID, err)
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "保存失败")
		return
	}
	if body.On {
		logf("sandbox", "info", "锁住后台：%s 已开启「退出后自动重新打开」——它退出或停止运行后会被自动拉回", app.Name)
	} else {
		logf("sandbox", "info", "锁住后台：%s 已关闭「退出后自动重新打开」", app.Name)
	}
	resp := boxLockResponse()
	resp.ID = body.ID
	writeJSON(w, resp)
}

// boxAppRestoreHandler serves POST /api/boxapps/restore {id}:
// the windowless Stage card's click -- an immediate,
// user-summoned bring-up of a locked native app whose window
// is gone. LOCAL mux only, exactly like the web restore: the
// remote-assist surface must never gain the power to launch
// apps on this machine.
func boxAppRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadJSON, "请求格式不对")
		return
	}
	if _, ok := boxAppByID(body.ID); !ok {
		apiErrBadRequest(w, errCodeBadRequest, "这个应用不在隔离安装的登记里")
		return
	}
	if !boxAppLockSet()[body.ID] {
		apiErrBadRequest(w, errCodeBadRequest, "这个应用没有锁住后台")
		return
	}
	// A window may exist after all (hidden, ours or the app's
	// own doing): then the click is a plain raise, no launch.
	if st := boxBookSnapshot()[body.ID]; st != nil && st.AlignedHWND != 0 {
		extMu.Lock()
		_, registered := extWins[st.AlignedHWND]
		extMu.Unlock()
		if registered {
			focusClaimedWindow(st.AlignedHWND, boxAppName(body.ID))
			writeJSON(w, boxAppRestoreResponse{OK: true, Present: true})
			return
		}
	}
	boxResidentRestore(body.ID)
	writeJSON(w, boxAppRestoreResponse{OK: true})
}
