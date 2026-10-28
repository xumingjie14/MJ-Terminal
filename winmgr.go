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

	pid       uint32
	lastTitle string // title at last AppID stamp; a change triggers a re-stamp
	placed    bool   // v0.20: one-shot landing placement already done
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
	trackLaunch(app, targetURL, browserPath, before, shell, !shell)
}

// trackAppLaunch is the store-app twin (v0.31): a portable app from
// the app store gets its main window claimed and registered exactly
// like a browser window (unified AppID + one-shot landing), so it
// shows up in the Stage Manager strip too. Some portable apps hand
// off to a child process, so claiming keys on "the first new window
// of this exe" (process image match), never on the started PID.
func trackAppLaunch(appName, exePath string, before map[uint64]bool) {
	trackLaunch(appName, "", exePath, before, false, false)
}

// trackLaunch is the shared claiming engine behind both. browser
// marks the registration as browser-owned, which is what puts its
// PID into the v0.31 patrol's watched set.
func trackLaunch(app, targetURL, exePath string, before map[uint64]bool, shell, browser bool) {
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
				extWins[hwnd] = &extWindow{HWND: hwnd, App: app, URL: targetURL, Shell: shell, pid: pid, browser: browser, origin: "launch"}
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
	writeJSON(w, map[string]any{"ok": true})
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
// terminal-owned main window that is not registered yet.
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

	owned := classifyOwnership(procs, seedPIDs, ownContext{
		RootDir:     exeDir(),
		ProfileDirs: []string{filepath.Join(dataDir(), "ChromeProfile"), ffProfileDir()},
		SelfPID:     selfPID,
	})
	opts := claimOpts{
		AutoClaimBrowser: settingStageAutoClaimBrowser(),
		ShellPIDs:        shellPIDs,
		BrowserPIDs:      browserPIDs,
	}

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
			label := claimLabel(wf, byPID[wf.PID], v.Browser)
			extMu.Lock()
			if _, dup := extWins[wf.HWND]; dup {
				extMu.Unlock()
				continue
			}
			extWins[wf.HWND] = &extWindow{
				HWND: wf.HWND, App: label, Title: wf.Title, pid: wf.PID,
				browser: v.Browser, placed: false, lastTitle: wf.Title, origin: v.Reason,
			}
			extMu.Unlock()
			registered[wf.HWND] = true
			auditMu.Lock()
			delete(pendingOwned, wf.HWND)
			auditMu.Unlock()
			platformSetAppID(wf.HWND)
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

	// v0.37: while the set is tucked away, keep it tucked (a
	// window claimed a beat before the hide, or re-shown by its
	// own app, rejoins the hidden set here).
	grpEnforceHidden()
}
