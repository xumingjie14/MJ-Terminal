package main

// ---------- v0.56: Stage Manager window previews ----------
//
// In preview mode （设置 → 桌面与 Dock → 台前调度显示， the
// default) the external-window strip shows a live picture of
// each claimed window instead of an icon card. The daemon
// captures the window with PrintWindow (PW_RENDERFULLCONTENT,
// thumb_windows.go) at most every few seconds per window and
// serves the PNG from the LOCAL mux only (/api/extwin/thumb) --
// the remote-assist mux never gets this endpoint: a picture of
// the user's windows is exactly the kind of content that must
// not leave the machine through the assistance channel.
//
// Serving is stale-while-revalidate: a request returns the
// last good frame immediately and kicks off an async refresh
// when the frame is older than thumbTTL. A window that cannot
// be captured right now (minimized, or an app whose rendering
// refuses PrintWindow) keeps its last good frame; a window
// with no frame yet 404s and the view falls back to the
// identity card for that round. Capture failures are logged
// once per window, not on every poll.

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var errNoCapture = errors.New("当前平台不支持窗口抓图")

const (
	thumbMaxWidth = 384
	thumbTTL      = 2500 * time.Millisecond
)

type thumbEntry struct {
	png        []byte
	at         time.Time
	capturing  bool
	failLogged bool
}

var (
	thumbMu    sync.Mutex
	thumbCache = map[uint64]*thumbEntry{}
)

func extWinThumbHandler(w http.ResponseWriter, r *http.Request) {
	hwnd, err := strconv.ParseUint(r.URL.Query().Get("hwnd"), 10, 64)
	if err != nil || hwnd == 0 {
		http.NotFound(w, r)
		return
	}
	extMu.Lock()
	e, ok := extWins[hwnd]
	registered := ok && !e.Shell
	extMu.Unlock()
	if !registered {
		http.NotFound(w, r)
		return
	}
	thumbMu.Lock()
	ent := thumbCache[hwnd]
	if ent == nil {
		ent = &thumbEntry{}
		thumbCache[hwnd] = ent
	}
	stale := ent.png == nil || time.Since(ent.at) > thumbTTL
	if stale && !ent.capturing {
		ent.capturing = true
		go captureThumb(hwnd)
	}
	png := ent.png
	thumbMu.Unlock()
	if png == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func captureThumb(hwnd uint64) {
	defer func() {
		thumbMu.Lock()
		if ent := thumbCache[hwnd]; ent != nil {
			ent.capturing = false
		}
		thumbMu.Unlock()
	}()
	// A minimized window has no drawable frame; keep the last
	// good one silently (this is the normal case, not a failure).
	if platformWindowIconic(hwnd) {
		thumbMu.Lock()
		if ent := thumbCache[hwnd]; ent != nil {
			ent.at = time.Now() // do not retry every poll while minimized
		}
		thumbMu.Unlock()
		return
	}
	png, err := platformCaptureWindowPNG(hwnd, thumbMaxWidth)
	thumbMu.Lock()
	ent := thumbCache[hwnd]
	if ent == nil {
		ent = &thumbEntry{}
		thumbCache[hwnd] = ent
	}
	if err == nil && len(png) > 0 {
		ent.png, ent.at, ent.failLogged = png, time.Now(), false
		thumbMu.Unlock()
		return
	}
	firstFail := !ent.failLogged
	ent.failLogged = true
	ent.at = time.Now()
	thumbMu.Unlock()
	if firstFail {
		extMu.Lock()
		label := ""
		if e, ok := extWins[hwnd]; ok {
			label = e.App
		}
		extMu.Unlock()
		logf("winmgr", "warn", "台前调度预览：窗口抓图失败，本窗口改用标识卡片（%s）：%v", label, err)
	}
}

// thumbForget drops a window's cached frame (called when the
// window leaves the registry, so a recycled HWND can never
// show the previous window's picture).
func thumbForget(hwnd uint64) {
	thumbMu.Lock()
	delete(thumbCache, hwnd)
	thumbMu.Unlock()
}
