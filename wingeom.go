package main

// ---------- v0.56: window geometry memory ----------
//
// The user resizes an opened app's window once and expects the
// terminal to remember: the next launch of the same app should
// come back at the size (and spot) he left it, not at the same
// default landing rectangle every time. This file is the store
// and the sampler; the restore side lives in doBirthPlacement
// (winset.go), which consults the memory INSTEAD of the default
// rectangle whenever an entry exists for the launch identity.
//
// Identity keys (assigned at launch/claim time, see winmgr.go):
//   app:<id>    -- desktop apps / isolated browser main windows
//   store:<id>  -- app-store portable apps
//   comp:<id>   -- productivity components
//   box:<id>    -- Sandboxie-registered apps
//   site:<host> -- browser app-mode windows (Chrome --app, Firefox
//                  app mode), keyed by the site's host
//   exe:<base>  -- patrol-claimed windows with no launch context
//
// The store is Data/wingeom.json. It travels with the folder; a
// rectangle remembered on a big monitor is clamped into the
// current desktop area at restore time, so it can never land
// off-screen on a smaller machine (clampWinGeom is pure and
// unit-tested). Recording needs no app cooperation: the patrol
// samples the live rectangles of tracked, already-placed windows
// and persists a rectangle once it has been stable for ~800 ms,
// plus a final flush when a tracked window closes.

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

type winGeom struct {
	L         int32 `json:"l"`
	T         int32 `json:"t"`
	R         int32 `json:"r"`
	B         int32 `json:"b"`
	UpdatedAt int64 `json:"updatedAt"`
}

func (g winGeom) width() int32  { return g.R - g.L }
func (g winGeom) height() int32 { return g.B - g.T }
func (g winGeom) valid() bool   { return g.width() >= 80 && g.height() >= 60 }

var (
	geomMu     sync.Mutex
	geomLoaded bool
	geomMap    = map[string]winGeom{}
	// Sampler state, per HWND: the last rectangle seen, when it
	// last changed, and what was last persisted for it.
	geomSamples = map[uint64]*geomSample{}
	// Windows that vanished in pruneExtWindowsLocked, queued
	// under extMu and drained (final flush) outside the lock.
	geomGone []goneGeom
)

type geomSample struct {
	rect      winGeom
	changedAt time.Time
	persisted winGeom
	hasPers   bool
}

type goneGeom struct {
	hwnd uint64
	key  string
}

func winGeomPath() string { return filepath.Join(dataDir(), "wingeom.json") }

func loadWinGeomLocked() {
	if geomLoaded {
		return
	}
	geomLoaded = true
	b, status, err := dsLoad("wingeom")
	if err != nil || status == dsMissing {
		return
	}
	var m map[string]winGeom
	if json.Unmarshal(b, &m) == nil {
		for k, g := range m {
			if g.valid() {
				geomMap[k] = g
			}
		}
	}
}

func saveWinGeomLocked() {
	b, err := json.MarshalIndent(geomMap, "", " ")
	if err != nil {
		return
	}
	_ = dsSave("wingeom", b)
}

// winGeomGet returns the remembered rectangle for a launch key.
func winGeomGet(key string) (winGeom, bool) {
	if key == "" {
		return winGeom{}, false
	}
	geomMu.Lock()
	defer geomMu.Unlock()
	loadWinGeomLocked()
	g, ok := geomMap[key]
	return g, ok
}

func winGeomPut(key string, g winGeom) {
	if key == "" || !g.valid() {
		return
	}
	geomMu.Lock()
	loadWinGeomLocked()
	old, had := geomMap[key]
	if had && old.L == g.L && old.T == g.T && old.R == g.R && old.B == g.B {
		geomMu.Unlock()
		return
	}
	g.UpdatedAt = time.Now().Unix()
	geomMap[key] = g
	saveWinGeomLocked()
	geomMu.Unlock()
	logf("winmgr", "info", "窗口记忆：已记住 %s 的窗口大小 %d×%d", key, g.width(), g.height())
}

// winGeomDrop forgets one identity (store app / component
// uninstall paths call it, so a reinstalled app starts from the
// default landing again).
func winGeomDrop(key string) {
	if key == "" {
		return
	}
	geomMu.Lock()
	loadWinGeomLocked()
	if _, ok := geomMap[key]; ok {
		delete(geomMap, key)
		saveWinGeomLocked()
	}
	geomMu.Unlock()
}

func winGeomCount() int {
	geomMu.Lock()
	defer geomMu.Unlock()
	loadWinGeomLocked()
	return len(geomMap)
}

// clampWinGeom fits a remembered rectangle into the desktop
// area (the shell window's rectangle): the size is capped at
// the area's size, then the position is pulled so the whole
// rectangle sits inside the area. Pure; wingeom_test.go pins
// the off-screen cases (moved folder, smaller monitor).
func clampWinGeom(g winGeom, al, at, ar, ab int32) winGeom {
	aw, ah := ar-al, ab-at
	if aw <= 0 || ah <= 0 {
		return g
	}
	w, h := g.width(), g.height()
	if w > aw {
		w = aw
	}
	if h > ah {
		h = ah
	}
	l, t := g.L, g.T
	if l < al {
		l = al
	}
	if t < at {
		t = at
	}
	if l+w > ar {
		l = ar - w
	}
	if t+h > ab {
		t = ab - h
	}
	if l < al {
		l = al
	}
	if t < at {
		t = at
	}
	return winGeom{L: l, T: t, R: l + w, B: t + h, UpdatedAt: g.UpdatedAt}
}

// geomSampleTick is the recording half, called from the patrol
// (every ~2 s). It samples every tracked, already-landed window
// that carries a geometry key; a rectangle that stopped moving
// for ~800 ms is written to the store. Minimized and maximized
// windows are skipped: their live rectangles are not the user's
// chosen normal geometry (a maximized window keeps whatever
// normal rectangle it last had).
func geomSampleTick() {
	type cand struct {
		hwnd uint64
		key  string
	}
	extMu.Lock()
	var cands []cand
	live := map[uint64]bool{}
	for h, e := range extWins {
		live[h] = true
		if e.Shell || e.geomKey == "" || !e.placed {
			continue
		}
		cands = append(cands, cand{h, e.geomKey})
	}
	extMu.Unlock()
	now := time.Now()
	seen := map[uint64]bool{}
	for _, c := range cands {
		seen[c.hwnd] = true
		if platformWindowIconic(c.hwnd) || platformWindowZoomed(c.hwnd) {
			continue
		}
		l, t, r, b, ok := platformWindowRect(c.hwnd)
		if !ok {
			continue
		}
		g := winGeom{L: l, T: t, R: r, B: b}
		if !g.valid() {
			continue
		}
		geomMu.Lock()
		s := geomSamples[c.hwnd]
		if s == nil {
			s = &geomSample{rect: g, changedAt: now}
			if old, had := geomMap[c.key]; had {
				s.persisted, s.hasPers = old, true
			}
			geomSamples[c.hwnd] = s
			geomMu.Unlock()
			continue
		}
		if s.rect != g {
			s.rect, s.changedAt = g, now
			geomMu.Unlock()
			continue
		}
		settled := now.Sub(s.changedAt) >= 800*time.Millisecond
		dirty := !s.hasPers || s.persisted.L != g.L || s.persisted.T != g.T || s.persisted.R != g.R || s.persisted.B != g.B
		geomMu.Unlock()
		if settled && dirty {
			winGeomPut(c.key, g)
			geomMu.Lock()
			s.persisted, s.hasPers = g, true
			geomMu.Unlock()
		}
	}
	// Forget sampler state for windows that left the registry
	// without a prune note (defensive; the normal path is the
	// gone-queue drained by geomFlushGone).
	geomMu.Lock()
	for h := range geomSamples {
		if !live[h] && !seen[h] {
			delete(geomSamples, h)
		}
	}
	geomMu.Unlock()
}

// noteGeomGone is called (under extMu) from pruneExtWindowsLocked
// when a tracked window disappears; geomFlushGone persists the
// sampler's last rectangle for it, outside the lock.
func noteGeomGone(hwnd uint64, key string) {
	if key == "" {
		return
	}
	geomGone = append(geomGone, goneGeom{hwnd, key})
}

func geomFlushGone() {
	extMu.Lock()
	gone := geomGone
	geomGone = nil
	extMu.Unlock()
	for _, gg := range gone {
		geomMu.Lock()
		s := geomSamples[gg.hwnd]
		delete(geomSamples, gg.hwnd)
		geomMu.Unlock()
		if s != nil {
			winGeomPut(gg.key, s.rect)
		}
	}
}

// winGeomClearHandler serves POST /api/wingeom/clear （设置 →
// 桌面与 Dock → 清除窗口记忆）: the whole store goes, every app
// falls back to the default landing until it is resized again.
func winGeomClearHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"count": winGeomCount()})
		return
	}
	geomMu.Lock()
	loadWinGeomLocked()
	n := len(geomMap)
	geomMap = map[string]winGeom{}
	saveWinGeomLocked()
	geomMu.Unlock()
	logf("winmgr", "info", "窗口记忆已清除（%d 条）", n)
	writeJSON(w, map[string]any{"ok": true, "cleared": n})
}
