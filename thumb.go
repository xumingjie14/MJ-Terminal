package main

// ---------- v0.56: Stage Manager window previews ----------
//
// In preview mode （设置 → 桌面与 Dock → 台前调度显示， the
// default) the external-window strip shows a live picture of
// each claimed window instead of an icon card. The daemon
// captures the window with PrintWindow (PW_RENDERFULLCONTENT,
// thumb_windows.go) and serves the PNG from the LOCAL mux only
// (/api/extwin/thumb) -- the remote-assist mux never gets this
// endpoint: a picture of the user's windows is exactly the kind
// of content that must not leave the machine through the
// assistance channel.
//
// test1 field failure (fixed here): EVERY preview 404'd on the
// user's machine, twice in the log, and the strip never showed
// a single picture. The route was registered and the captures
// were fine -- the protocol between daemon and view was broken:
// the handler answered a window's FIRST request with 404 (no
// frame cached yet, capture kicked off async), and the view's
// <img onerror> treated that first 404 as a permanent failure,
// banned the hwnd for the session and never asked again; the
// captured frame then sat in the cache unserved. Both ends are
// fixed:
//   - daemon: a window's first frame is captured SYNCHRONOUSLY
//     in the handler, so a working capture answers 200 on the
//     very first request; later requests keep the
//     stale-while-revalidate behaviour. Every refusal carries an
//     X-Thumb-Reason header (unknown / minimized /
//     capture-failed / unsupported / bad-request) and the
//     reason is logged in this module -- never just a bare
//     [api] 404 line.
//   - view: only the permanent reasons (unknown /
//     capture-failed / unsupported) ban a window for the
//     session; a minimized window simply keeps its identity
//     card for that round and is retried on the next poll.
//
// A window that cannot be captured right now (an app whose
// rendering refuses PrintWindow) keeps its last good frame;
// three consecutive capture failures mark the window dead for
// previews until it re-registers.

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var errNoCapture = errors.New("当前平台不支持窗口抓图")

const (
	thumbMaxWidth = 384
	thumbTTL      = 2500 * time.Millisecond
	// thumbMaxFails: consecutive capture failures after which a
	// window's previews are given up on (the view bans it for
	// the session, the daemon stops retrying).
	thumbMaxFails = 3
)

type thumbEntry struct {
	png       []byte
	at        time.Time
	capturing bool
	fails     int  // consecutive capture failures
	dead      bool // fails exhausted: no more capture attempts
}

var (
	thumbMu    sync.Mutex
	thumbCache = map[uint64]*thumbEntry{}
	// thumbLogSeen dedupes reason logging: one line per
	// (reason, window) per daemon run is signal; the strip
	// polls every few seconds and would drown the console.
	thumbLogSeen = map[string]bool{}
)

// thumbLogOnce logs a preview refusal/failure reason in the
// [thumb] module, once per reason+key per daemon run.
func thumbLogOnce(reason, key, format string, args ...any) {
	thumbMu.Lock()
	k := reason + "|" + key
	if thumbLogSeen[k] {
		thumbMu.Unlock()
		return
	}
	thumbLogSeen[k] = true
	thumbMu.Unlock()
	logf("thumb", "warn", format, args...)
}

// thumbRefuse answers a request that cannot be served a frame:
// an honest status, a machine-readable reason for the view, and
// the reason in the console (once).
func thumbRefuse(w http.ResponseWriter, status int, reason, logKey, format string, args ...any) {
	thumbLogOnce(reason, logKey, format, args...)
	w.Header().Set("X-Thumb-Reason", reason)
	http.Error(w, "thumb: "+reason, status)
}

func thumbEntryFor(hwnd uint64) *thumbEntry {
	ent := thumbCache[hwnd]
	if ent == nil {
		ent = &thumbEntry{}
		thumbCache[hwnd] = ent
	}
	return ent
}

// recordCapture folds one capture attempt's outcome into the
// cache: success installs the frame and resets the failure
// count; failure counts toward the dead threshold and logs the
// reason (once) in the [thumb] module.
func recordCapture(hwnd uint64, png []byte, err error) {
	thumbMu.Lock()
	ent := thumbEntryFor(hwnd)
	if err == nil && len(png) > 0 {
		ent.png, ent.at, ent.fails, ent.dead = png, time.Now(), 0, false
		thumbMu.Unlock()
		return
	}
	ent.fails++
	ent.at = time.Now()
	dead := ent.fails >= thumbMaxFails
	ent.dead = dead
	thumbMu.Unlock()
	extMu.Lock()
	label := ""
	if e, ok := extWins[hwnd]; ok {
		label = e.App
	}
	extMu.Unlock()
	if errors.Is(err, errNoCapture) {
		thumbLogOnce("unsupported", "platform", "台前调度预览：当前平台不支持窗口抓图，预览不可用")
		return
	}
	thumbLogOnce("capture-failed", fmt.Sprint(hwnd), "台前调度预览：窗口抓图失败（%s，hwnd %d）：%v", label, hwnd, err)
	if dead {
		logf("thumb", "warn", "台前调度预览：窗口 %d（%s）连续抓图失败 %d 次，本会话改用标识卡片", hwnd, label, thumbMaxFails)
	}
}

func extWinThumbHandler(w http.ResponseWriter, r *http.Request) {
	hwnd, err := strconv.ParseUint(r.URL.Query().Get("hwnd"), 10, 64)
	if err != nil || hwnd == 0 {
		thumbRefuse(w, http.StatusBadRequest, "bad-request", r.URL.RawQuery,
			"台前调度预览：请求参数不对（hwnd=%q）", r.URL.Query().Get("hwnd"))
		return
	}
	extMu.Lock()
	e, ok := extWins[hwnd]
	claimed := ok && !e.Shell
	label := ""
	if ok {
		label = e.App
	}
	extMu.Unlock()
	if !claimed {
		thumbRefuse(w, http.StatusNotFound, "unknown", fmt.Sprint(hwnd),
			"台前调度预览：hwnd %d 不在台前调度的在册窗口里，不给抓图", hwnd)
		return
	}
	thumbMu.Lock()
	ent := thumbEntryFor(hwnd)
	if ent.png != nil {
		// A cached frame is served even on a platform that
		// cannot capture new ones (and, on Windows, while a
		// refresh runs in the background).
		stale := time.Since(ent.at) > thumbTTL
		if stale && !ent.capturing && thumbSupported {
			ent.capturing = true
			go captureThumb(hwnd)
		}
		png := ent.png
		thumbMu.Unlock()
		serveThumbPNG(w, png)
		return
	}
	if ent.dead {
		thumbMu.Unlock()
		thumbRefuse(w, http.StatusNotFound, "capture-failed", fmt.Sprint(hwnd),
			"台前调度预览：窗口 %d（%s）抓图已多次失败，跳过", hwnd, label)
		return
	}
	thumbMu.Unlock()
	if !thumbSupported {
		thumbRefuse(w, http.StatusNotFound, "unsupported", "platform",
			"台前调度预览：当前平台不支持窗口抓图")
		return
	}
	// No frame yet. A minimized window has nothing to capture;
	// that is a normal, retryable state -- NOT a failure, and
	// the view must not ban the window for it.
	if platformWindowIconic(hwnd) {
		w.Header().Set("X-Thumb-Reason", "minimized")
		http.Error(w, "thumb: minimized", http.StatusNotFound)
		return
	}
	// First frame: capture synchronously (test1's async-first-
	// 404 is what killed every preview). PrintWindow on a
	// strip-sized window is fast; the strip's first paint waits
	// a few hundred ms per NEW window at worst.
	png, capErr := platformCaptureWindowPNG(hwnd, thumbMaxWidth)
	recordCapture(hwnd, png, capErr)
	if capErr != nil || len(png) == 0 {
		reason := "capture-failed"
		if errors.Is(capErr, errNoCapture) {
			reason = "unsupported"
		}
		w.Header().Set("X-Thumb-Reason", reason)
		http.Error(w, "thumb: "+reason, http.StatusNotFound)
		return
	}
	serveThumbPNG(w, png)
}

func serveThumbPNG(w http.ResponseWriter, png []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// captureThumb is the async refresher for windows that already
// have a frame (stale-while-revalidate). A minimized window
// keeps its last good frame silently.
func captureThumb(hwnd uint64) {
	defer func() {
		thumbMu.Lock()
		if ent := thumbCache[hwnd]; ent != nil {
			ent.capturing = false
		}
		thumbMu.Unlock()
	}()
	if platformWindowIconic(hwnd) {
		thumbMu.Lock()
		if ent := thumbCache[hwnd]; ent != nil {
			ent.at = time.Now() // do not retry every poll while minimized
		}
		thumbMu.Unlock()
		return
	}
	png, err := platformCaptureWindowPNG(hwnd, thumbMaxWidth)
	recordCapture(hwnd, png, err)
}

// thumbForget drops a window's cached frame (called when the
// window leaves the registry, so a recycled HWND can never
// show the previous window's picture).
func thumbForget(hwnd uint64) {
	thumbMu.Lock()
	delete(thumbCache, hwnd)
	thumbMu.Unlock()
}
