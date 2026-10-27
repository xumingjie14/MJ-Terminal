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
	if browserPath == "" {
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
			if hwnd, pid, ok := platformClaimWindow(browserPath, before, claimed); ok {
				extMu.Lock()
				extWins[hwnd] = &extWindow{HWND: hwnd, App: app, URL: targetURL, Shell: shell, pid: pid}
				extMu.Unlock()
				// 丙: stamp the unified AppUserModelID (best effort).
				platformSetAppID(hwnd)
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
		fmt.Println("winmgr: launched window never appeared, not registered:", app, targetURL)
	}()
}

// pruneExtWindowsLocked drops registrations whose window is gone and
// refreshes titles. A title change usually means the page finished
// loading (or a popup took over the window), so the AppID is
// re-stamped for those windows -- Chrome can replace a window's
// identity as it navigates. Caller must hold extMu.
func pruneExtWindowsLocked() (restamp []uint64) {
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
	}
	return restamp
}

// extWinListHandler serves GET /api/extwin: every registered external
// window that is still alive (dead ones are pruned on the way out).
// The desktop shell window is included with shell=true; the frontend
// does not render it as a card (you are already looking at it), but it
// is registered so it carries the unified AppID like the others.
func extWinListHandler(w http.ResponseWriter, r *http.Request) {
	extMu.Lock()
	restamp := pruneExtWindowsLocked()
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

// extWinNote is a tiny helper for log lines in platform files.
func extWinNote(format string, args ...any) {
	fmt.Println("winmgr: " + strings.TrimSpace(fmt.Sprintf(format, args...)))
}
