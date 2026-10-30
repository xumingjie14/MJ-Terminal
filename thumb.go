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
//
// test3 (field report: previews flicker, lag ~4 s, and wide /
// narrow windows distort): the daemon half of the rework --
//   - Frames are JPEG (q80, same 384 px width) instead of PNG:
//     a third of the bytes on the wire and to encode.
//   - Every frame carries a version (FNV-1a of the encoded
//     bytes) served as an ETag. The view sends If-None-Match;
//     an unchanged frame answers 304 and the view leaves its
//     DOM untouched -- identical pixels can no longer cause a
//     swap, and the swap itself is double-buffered view-side.
//   - live=1 (sent while the strip is open) shortens the
//     freshness window to ~0.9 s so the ~1 s poll is fed fresh
//     frames; with the strip closed the lazy 2.5 s behaviour
//     returns and nothing captures in the background (requests
//     are the only capture trigger). Mode switches log once in
//     this module.
//   - fresh=1 (strip just opened) and thumbKick (a window was
//     claimed or raised) force the next frame to be recaptured
//     instead of serving staleness.

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errNoCapture = errors.New("当前平台不支持窗口抓图")

const (
	thumbMaxWidth = 384
	thumbTTL      = 2500 * time.Millisecond
	// thumbLiveTTL: freshness window while the strip is open
	// (live=1) -- pairs with the view's ~1 s live poll.
	thumbLiveTTL = 900 * time.Millisecond
	// thumbMaxFails: consecutive capture failures after which a
	// window's previews are given up on (the view bans it for
	// the session, the daemon stops retrying).
	thumbMaxFails = 3
)

type thumbEntry struct {
	frame     []byte // encoded JPEG (PNG only from old test fakes)
	ver       string // frame version (ETag payload): hash+len of frame
	at        time.Time
	capturing bool
	fails     int  // consecutive capture failures
	dead      bool // fails exhausted: no more capture attempts
}

// frameVersion derives a frame's ETag payload from its encoded
// bytes: identical pixels encode to identical bytes, so an
// unchanged window keeps its version and the view's
// If-None-Match answers 304.
func frameVersion(b []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%x-%d", h.Sum64(), len(b))
}

// frameContentType sniffs the encoded frame's real type (the
// capture path emits JPEG; a PNG signature still reads PNG).
func frameContentType(b []byte) string {
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return "image/jpeg"
	}
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return "image/png"
	}
	return "application/octet-stream"
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
func recordCapture(hwnd uint64, frame []byte, err error) {
	thumbMu.Lock()
	ent := thumbEntryFor(hwnd)
	if err == nil && len(frame) > 0 {
		ent.frame, ent.ver, ent.at, ent.fails, ent.dead = frame, frameVersion(frame), time.Now(), 0, false
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
	live := r.URL.Query().Get("live") == "1"
	if live {
		thumbNoteLive()
	}
	fresh := r.URL.Query().Get("fresh") == "1"
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
	// fresh=1 (the strip just opened): recapture synchronously
	// even when a frame is cached, so the strip's first paint is
	// current. A failed attempt falls through to the cached
	// frame below rather than refusing.
	if fresh && thumbSupported && !platformWindowIconic(hwnd) {
		frame, capErr := platformCaptureWindowFrame(hwnd, thumbMaxWidth)
		recordCapture(hwnd, frame, capErr)
		if capErr == nil && len(frame) > 0 {
			serveThumbFrame(w, r, frame, frameVersion(frame))
			return
		}
	}
	thumbMu.Lock()
	ent := thumbEntryFor(hwnd)
	if ent.frame != nil {
		// A cached frame is served even on a platform that
		// cannot capture new ones (and, on Windows, while a
		// refresh runs in the background).
		ttl := thumbTTL
		if live {
			ttl = thumbLiveTTL
		}
		stale := time.Since(ent.at) > ttl
		if stale && !ent.capturing && thumbSupported {
			ent.capturing = true
			go captureThumb(hwnd)
		}
		frame, ver := ent.frame, ent.ver
		thumbMu.Unlock()
		serveThumbFrame(w, r, frame, ver)
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
	frame, capErr := platformCaptureWindowFrame(hwnd, thumbMaxWidth)
	recordCapture(hwnd, frame, capErr)
	if capErr != nil || len(frame) == 0 {
		reason := "capture-failed"
		if errors.Is(capErr, errNoCapture) {
			reason = "unsupported"
		}
		w.Header().Set("X-Thumb-Reason", reason)
		http.Error(w, "thumb: "+reason, http.StatusNotFound)
		return
	}
	serveThumbFrame(w, r, frame, frameVersion(frame))
}

// serveThumbFrame writes a cached frame with its version as an
// ETag. If the view's If-None-Match already names this exact
// version, the answer is 304 and the view leaves its DOM
// untouched (test3 anti-flicker: no identical-pixel swaps).
func serveThumbFrame(w http.ResponseWriter, r *http.Request, frame []byte, ver string) {
	etag := `"mjf-` + ver + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-store")
	if inm := r.Header.Get("If-None-Match"); inm != "" && (inm == "*" || strings.Contains(inm, etag)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", frameContentType(frame))
	_, _ = w.Write(frame)
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
	frame, err := platformCaptureWindowFrame(hwnd, thumbMaxWidth)
	recordCapture(hwnd, frame, err)
}

// thumbKick marks a window's frame stale so the next request
// recaptures: called when a window is newly claimed or raised
// (winmgr), so a window the user just brought forward never
// greets them with its pre-raise picture.
func thumbKick(hwnd uint64) {
	thumbMu.Lock()
	if ent := thumbCache[hwnd]; ent != nil {
		ent.at = time.Time{}
	}
	thumbMu.Unlock()
}

// ---------- live (near-real-time) mode tracking ----------
//
// While the strip is open the view polls with live=1 (~1 s);
// the daemon shortens its freshness window and notes the mode
// switch once per transition in this module. When live
// requests stop, the sweeper notes the return to lazy mode.
// Captures still only ever happen in answer to a request, so
// a closed strip costs no CPU at all.

var thumbLive = struct {
	sync.Mutex
	on   bool
	last time.Time
	once sync.Once
}{}

func thumbNoteLive() {
	thumbLive.Lock()
	turnOn := !thumbLive.on
	thumbLive.on = true
	thumbLive.last = time.Now()
	thumbLive.Unlock()
	if turnOn {
		logf("thumb", "info", "台前调度预览：调度条打开，准实时抓图开启（约 1 秒一帧）")
	}
	thumbLive.once.Do(func() { go thumbLiveSweeper() })
}

func thumbLiveSweeper() {
	for range time.Tick(5 * time.Second) {
		thumbLive.Lock()
		off := thumbLive.on && time.Since(thumbLive.last) > 6*time.Second
		if off {
			thumbLive.on = false
		}
		thumbLive.Unlock()
		if off {
			logf("thumb", "info", "台前调度预览：调度条关闭，准实时抓图停止（回到懒抓图）")
		}
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
