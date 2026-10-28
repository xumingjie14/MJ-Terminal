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
	// auto-claimed from them in v0.31). The patrol only watches the
	// PIDs of browser windows; store apps and the shell never enter
	// the watched set.
	browser bool
}

var (
	extMu   sync.Mutex
	extWins = map[uint64]*extWindow{}
	// extClaimMu serializes window claiming: two launches in quick
	// succession must not race to claim the same fresh window.
	extClaimMu sync.Mutex
)

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
				extWins[hwnd] = &extWindow{HWND: hwnd, App: app, URL: targetURL, Shell: shell, pid: pid, browser: browser}
				extMu.Unlock()
				logf("winmgr", "info", "已登记外部窗口：%s", app)
				// 丙: stamp the unified AppUserModelID (best effort).
				platformSetAppID(hwnd)
				// v0.20: land the new window inside the desktop's
				// rectangle, once. The shell window itself is the
				// frame everything lands in, so it is never placed.
				if !shell {
					tryPlaceExtWindow(hwnd)
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

// tryPlaceExtWindow performs the one-shot placement for a registered
// external window. It ALWAYS moves the window to the computed target
// rectangle (rather than keeping a geometry that happens to fit):
// launch geometry is inconsistent by construction -- --start-
// maximized, Chrome's remembered position, per-app quirks -- and one
// predictable landing spot is what the user asked for. When the
// desktop shell is not registered (view closed) placement is skipped
// silently; prune retries it on the next /api/extwin poll, and if
// the view never opens the window simply stays where it opened.
func tryPlaceExtWindow(hwnd uint64) {
	extMu.Lock()
	e, ok := extWins[hwnd]
	if !ok || e.Shell || e.placed {
		extMu.Unlock()
		return
	}
	var shell uint64
	for h, o := range extWins {
		if o.Shell {
			shell = h
			break
		}
	}
	extMu.Unlock()
	if shell == 0 {
		return
	}
	sl, st, sr, sb, ok := platformWindowRect(shell)
	if !ok {
		return
	}
	// v0.23: the shell's maximized state is read here, at landing
	// time, and only for the shell -- whether the external window
	// itself is maximized stays platformPlaceWindow's business (it
	// restores it first, as before).
	tl, tt, tr, tb, phone := placeTargetRectAuto(sl, st, sr, sb, platformWindowZoomed(shell))
	if !platformPlaceWindow(hwnd, tl, tt, tr, tb) {
		return
	}
	extMu.Lock()
	if e2, ok := extWins[hwnd]; ok {
		e2.placed = true
	}
	extMu.Unlock()
	if phone {
		name := e.App
		if e.Title != "" {
			name = e.Title
		}
		logf("winmgr", "info", "外部窗口落位（小窗手机宽）：%s → %d×%d", name, tr-tl, tb-tt)
	} else {
		logf("winmgr", "info", "外部窗口落位：%s → %d×%d", e.App, tr-tl, tb-tt)
	}
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

// ---------- v0.31: patrol -- auto-claim browser-spawned windows ----------
//
// trackBrowserLaunch claims exactly ONE window per launch: the one
// the daemon asked for. Windows the browser then creates on its own
// -- page popups, Ctrl+N, a tab dragged out into its own window, an
// undocked DevTools -- were invisible to the Stage Manager. The
// patrol closes that gap: every ~2s it enumerates the top-level
// windows owned by the PIDs of already-registered browser windows
// and claims the new ones.
//
// The PID keying is the safety boundary. A browser process owns all
// of its windows, and the isolated Chrome/Firefox the daemon
// launched is a different process instance from anything the user
// runs on the host (the host's own Chrome is another PID), so host
// windows can never match. The desktop shell's PID is excluded: the
// shell is registered with browser=false, so Edge's own process
// never enters the watched set either.
//
// Auto-claimed windows are registered with placed=true: they join
// the Stage Manager strip (listed, clickable to raise) but are
// NEVER landed or moved -- a page popup has its own idea of where
// it belongs. The watched PID set is derived from the live registry
// on every tick, so once a browser's last window is pruned its PID
// leaves the set on its own.
//
// The on/off switch is 设置 -> 桌面与 Dock ->
// 「浏览器新窗口自动纳入台前调度」 (settings key
// stageAutoClaimBrowser, default on), re-read every tick.

// patrolCandidate is one top-level window as the platform layer saw
// it, in platform-neutral form so the filter stays unit-testable.
type patrolCandidate struct {
	HWND    uint64
	PID     uint32
	Class   string
	Title   string
	Visible bool
	Owned   bool // has an owner window (dialogs, palettes) -- not a main window
	W, H    int32
}

// patrolMinW / patrolMinH filter out toasts, bubbles and other
// transient specks that share the browser's window class.
const (
	patrolMinW = 200
	patrolMinH = 100
)

// isBrowserWindowClass reports whether a window class is a
// browser's MAIN window class: Chromium browsers (Chrome, Edge)
// use Chrome_WidgetWin_1, Firefox uses MozillaWindowClass.
func isBrowserWindowClass(class string) bool {
	return class == "Chrome_WidgetWin_1" || class == "MozillaWindowClass"
}

// patrolFilter keeps only the candidates that are genuinely new
// browser main windows of watched processes.
func patrolFilter(cands []patrolCandidate, pids map[uint32]bool, registered map[uint64]bool) []patrolCandidate {
	var out []patrolCandidate
	for _, c := range cands {
		if !pids[c.PID] || registered[c.HWND] {
			continue
		}
		if !c.Visible || c.Owned {
			continue
		}
		if !isBrowserWindowClass(c.Class) {
			continue
		}
		if c.W < patrolMinW || c.H < patrolMinH {
			continue
		}
		out = append(out, c)
	}
	return out
}

// deriveBrowserPIDs is the watched set: PIDs of registered browser
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

// startExtPatrol launches the auto-claim patrol. Off Windows the
// platform candidate source is always empty, so the loop is inert.
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
	// view is closed, which is also what retires a PID from the
	// watched set once its last window is gone.
	extMu.Lock()
	restamp, place := pruneExtWindowsLocked()
	pids := deriveBrowserPIDs(extWins)
	registered := make(map[uint64]bool, len(extWins))
	for h := range extWins {
		registered[h] = true
	}
	extMu.Unlock()
	for _, h := range restamp {
		platformSetAppID(h)
	}
	for _, h := range place {
		tryPlaceExtWindow(h)
	}

	if len(pids) == 0 || !settingStageAutoClaimBrowser() {
		return
	}
	fresh := patrolFilter(platformPatrolCandidates(pids), pids, registered)
	for _, c := range fresh {
		app := cleanAutoClaimTitle(c.Title)
		extMu.Lock()
		if _, dup := extWins[c.HWND]; dup {
			extMu.Unlock()
			continue
		}
		extWins[c.HWND] = &extWindow{
			HWND: c.HWND, App: app, Title: c.Title, pid: c.PID,
			browser: true, placed: true, lastTitle: c.Title,
		}
		extMu.Unlock()
		platformSetAppID(c.HWND)
		logf("winmgr", "info", "台前调度自动认领浏览器新窗口：%s", app)
	}
}
