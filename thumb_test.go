package main

import (
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// v0.56 test2: the Stage Manager preview protocol. test1's
// field failure was a first-request 404 the view treated as
// permanent; these tests pin the handler's half of the fixed
// protocol: honest reasons for every refusal, a cached frame
// served as PNG, and the route's registration on the local mux
// (the regression class: a handler that exists but is never
// wired would 404 exactly like test1's symptom).

func TestThumbHandlerUnknownHWND(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/extwin/thumb?hwnd=424242", nil)
	extWinThumbHandler(rec, req)
	if rec.Code != 404 {
		t.Fatalf("unknown hwnd: got status %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("X-Thumb-Reason"); got != "unknown" {
		t.Fatalf("unknown hwnd: X-Thumb-Reason = %q, want %q", got, "unknown")
	}
}

func TestThumbHandlerBadParam(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/extwin/thumb", nil)
	extWinThumbHandler(rec, req)
	if rec.Code != 400 {
		t.Fatalf("missing hwnd: got status %d, want 400", rec.Code)
	}
	if got := rec.Header().Get("X-Thumb-Reason"); got != "bad-request" {
		t.Fatalf("missing hwnd: X-Thumb-Reason = %q, want %q", got, "bad-request")
	}
}

func TestThumbHandlerServesCachedFrame(t *testing.T) {
	const hwnd = 777001
	extMu.Lock()
	old, had := extWins[hwnd]
	extWins[hwnd] = &extWindow{HWND: hwnd, App: "测试窗口"}
	extMu.Unlock()
	thumbMu.Lock()
	thumbCache[hwnd] = &thumbEntry{frame: []byte("\x89PNG-fake-frame"), ver: frameVersion([]byte("\x89PNG-fake-frame")), at: time.Now()}
	thumbMu.Unlock()
	defer func() {
		extMu.Lock()
		if had {
			extWins[hwnd] = old
		} else {
			delete(extWins, hwnd)
		}
		extMu.Unlock()
		thumbForget(hwnd)
	}()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/extwin/thumb?hwnd=777001", nil)
	extWinThumbHandler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cached frame: got status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("cached frame: Content-Type = %q, want image/png", ct)
	}
	if !strings.Contains(rec.Body.String(), "PNG-fake-frame") {
		t.Fatalf("cached frame: body does not carry the cached bytes")
	}
	// test3: the frame carries a version ETag, and a request
	// naming that exact version answers 304 (the view then
	// leaves its DOM untouched -- the anti-flicker protocol).
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("cached frame: no ETag header")
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/extwin/thumb?hwnd=777001", nil)
	req2.Header.Set("If-None-Match", etag)
	extWinThumbHandler(rec2, req2)
	if rec2.Code != 304 {
		t.Fatalf("If-None-Match with the current version: got status %d, want 304", rec2.Code)
	}
}

// test3: a frame's version tracks its bytes -- new pixels get
// a new version (a stale If-None-Match no longer 304s), and
// re-recording identical bytes keeps the version (304 again).
func TestThumbFrameVersionTracksBytes(t *testing.T) {
	const hwnd = 777003
	extMu.Lock()
	extWins[hwnd] = &extWindow{HWND: hwnd, App: "测试窗口"}
	extMu.Unlock()
	defer func() {
		extMu.Lock()
		delete(extWins, hwnd)
		extMu.Unlock()
		thumbForget(hwnd)
	}()
	get := func(inm string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/extwin/thumb?hwnd=777003", nil)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		extWinThumbHandler(rec, req)
		return rec.Code, rec.Header().Get("ETag")
	}
	recordCapture(hwnd, []byte("frame-A"), nil)
	codeA, etagA := get("")
	if codeA != 200 || etagA == "" {
		t.Fatalf("frame A: got %d / ETag %q, want 200 with an ETag", codeA, etagA)
	}
	recordCapture(hwnd, []byte("frame-B"), nil)
	codeB, etagB := get(etagA)
	if codeB != 200 {
		t.Fatalf("frame B with A's version: got %d, want 200 (new frame must not 304)", codeB)
	}
	if etagB == "" || etagB == etagA {
		t.Fatalf("frame B: ETag %q did not change from A's %q", etagB, etagA)
	}
	recordCapture(hwnd, []byte("frame-B"), nil)
	codeB2, _ := get(etagB)
	if codeB2 != 304 {
		t.Fatalf("identical frame B re-recorded: got %d, want 304", codeB2)
	}
}

func TestThumbHandlerClaimedButUnsupported(t *testing.T) {
	if thumbSupported {
		t.Skip("platform supports capture; the unsupported branch is for !windows")
	}
	const hwnd = 777002
	extMu.Lock()
	extWins[hwnd] = &extWindow{HWND: hwnd, App: "测试窗口"}
	extMu.Unlock()
	defer func() {
		extMu.Lock()
		delete(extWins, hwnd)
		extMu.Unlock()
		thumbForget(hwnd)
	}()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/extwin/thumb?hwnd=777002", nil)
	extWinThumbHandler(rec, req)
	if rec.Code != 404 || rec.Header().Get("X-Thumb-Reason") != "unsupported" {
		t.Fatalf("unsupported platform: got %d / %q, want 404 / unsupported", rec.Code, rec.Header().Get("X-Thumb-Reason"))
	}
}

// The route-existence regression: the handler must be wired on
// the local mux in main.go (and, per the security rule, NOT on
// the remote mux in remote.go).
func TestThumbRouteRegisteredLocalOnly(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `mux.HandleFunc("/api/extwin/thumb", extWinThumbHandler)`) {
		t.Fatal("/api/extwin/thumb is not registered on the local mux")
	}
	rb, err := os.ReadFile("remote.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rb), "extwin/thumb") {
		t.Fatal("/api/extwin/thumb leaked into the remote-assist mux -- window pictures must never leave via the assistance channel")
	}
}

// ---------- v0.71 test3: the thumb noise rules ----------
//
// Field evidence (2026-11-14 22:49): one hidden Telegram
// window produced 50+ identical failure lines at ~1 Hz. The
// rules pinned below are the review's §3: a per-window log
// budget (first failure + the alive->dead transition, then
// silence), dead checked before fresh with at most one
// capture attempt per request, hidden windows as a transient
// no-capture state, and exactly three revival ports.

func thumbTestSetup(t *testing.T, hwnd uint64, capture func(uint64, int) ([]byte, error), visible bool) func() {
	t.Helper()
	oldCap, oldVis, oldSup := thumbCaptureFn, thumbVisibleFn, thumbSupportedDyn
	thumbCaptureFn = capture
	thumbVisibleFn = func(uint64) bool { return visible }
	thumbSupportedDyn = true
	extMu.Lock()
	extWins[hwnd] = &extWindow{HWND: hwnd, App: "测试窗口"}
	extMu.Unlock()
	return func() {
		thumbCaptureFn, thumbVisibleFn, thumbSupportedDyn = oldCap, oldVis, oldSup
		extMu.Lock()
		delete(extWins, hwnd)
		extMu.Unlock()
		thumbForget(hwnd)
	}
}

func thumbGet(hwnd uint64, fresh bool) (int, string) {
	rec := httptest.NewRecorder()
	q := "/api/extwin/thumb?hwnd=" + strconv.FormatUint(hwnd, 10)
	if fresh {
		q += "&fresh=1"
	}
	req := httptest.NewRequest("GET", q, nil)
	extWinThumbHandler(rec, req)
	return rec.Code, rec.Header().Get("X-Thumb-Reason")
}

func TestThumbFailStepTruthTable(t *testing.T) {
	fails, dead, diedCount := 0, false, 0
	for i := 0; i < 10; i++ {
		var died bool
		fails, dead, died = thumbFailStep(fails, dead)
		if died {
			diedCount++
		}
	}
	if !dead {
		t.Fatal("10 consecutive failures must end dead")
	}
	if fails != thumbMaxFails {
		t.Fatalf("fails = %d, want capped at %d", fails, thumbMaxFails)
	}
	if diedCount != 1 {
		t.Fatalf("died transition fired %d times over 10 failures, want exactly 1 (the log budget)", diedCount)
	}
	// A success-shaped reset is recordCapture's job, not the
	// step's; the step on a dead book is a pure no-op.
	f2, d2, died2 := thumbFailStep(thumbMaxFails, true)
	if f2 != thumbMaxFails || !d2 || died2 {
		t.Fatalf("dead book step = (%d, %v, %v), want (%d, true, false)", f2, d2, died2, thumbMaxFails)
	}
}

// The spam regression, handler level: ten fresh=1 requests
// against an always-failing target must produce at most
// thumbMaxFails capture attempts in total (before the fix,
// EACH request attempted twice and the window never stayed
// dead under fresh).
func TestThumbHandlerFreshBurstCapsAttempts(t *testing.T) {
	const hwnd = 777101
	attempts := 0
	restore := thumbTestSetup(t, hwnd, func(uint64, int) ([]byte, error) {
		attempts++
		return nil, errTestCapture
	}, true)
	defer restore()
	for i := 0; i < 10; i++ {
		code, reason := thumbGet(hwnd, true)
		if code != 404 || reason != "capture-failed" {
			t.Fatalf("request %d: got %d / %q, want 404 / capture-failed", i, code, reason)
		}
		if i == 0 && attempts != 1 {
			t.Fatalf("first request attempted %d captures, want exactly 1 (T-B2: one request, one attempt)", attempts)
		}
	}
	if attempts != thumbMaxFails {
		t.Fatalf("10 fresh requests attempted %d captures, want %d (the dead threshold, then refusals)", attempts, thumbMaxFails)
	}
	thumbMu.Lock()
	ent := thumbCache[hwnd]
	thumbMu.Unlock()
	if ent == nil || !ent.dead || ent.fails != thumbMaxFails {
		t.Fatalf("entry after burst = %+v, want dead with fails capped at %d", ent, thumbMaxFails)
	}
}

// A dead entry refuses fresh BEFORE any capture: the verdict
// precedes the fresh branch.
func TestThumbHandlerDeadRefusesBeforeFresh(t *testing.T) {
	const hwnd = 777102
	attempts := 0
	restore := thumbTestSetup(t, hwnd, func(uint64, int) ([]byte, error) {
		attempts++
		return nil, errTestCapture
	}, true)
	defer restore()
	for i := 0; i < thumbMaxFails; i++ {
		recordCapture(hwnd, nil, errTestCapture)
	}
	for i := 0; i < 5; i++ {
		thumbGet(hwnd, true)
	}
	if attempts != 0 {
		t.Fatalf("dead entry: %d capture attempts across 5 fresh requests, want 0", attempts)
	}
}

// T-I1: a hidden window is a transient no-capture state --
// zero attempts, zero fails, reason "hidden"; with a cached
// frame the frame is served instead.
func TestThumbHandlerHiddenWindow(t *testing.T) {
	const hwnd = 777103
	attempts := 0
	restore := thumbTestSetup(t, hwnd, func(uint64, int) ([]byte, error) {
		attempts++
		return nil, errTestCapture
	}, false)
	defer restore()
	code, reason := thumbGet(hwnd, true)
	if code != 404 || reason != "hidden" {
		t.Fatalf("hidden, no frame: got %d / %q, want 404 / hidden", code, reason)
	}
	thumbGet(hwnd, false)
	if attempts != 0 {
		t.Fatalf("hidden window: %d capture attempts, want 0", attempts)
	}
	thumbMu.Lock()
	ent := thumbCache[hwnd]
	thumbMu.Unlock()
	if ent != nil && ent.fails != 0 {
		t.Fatalf("hidden window accumulated %d fails, want 0", ent.fails)
	}
	recordCapture(hwnd, []byte("\x89PNG-hidden-frame"), nil)
	code, _ = thumbGet(hwnd, false)
	if code != 200 {
		t.Fatalf("hidden with a cached frame: got %d, want 200 (last frame served)", code)
	}
	if attempts != 0 {
		t.Fatalf("hidden window with frame: %d capture attempts, want 0", attempts)
	}
}

// Revival port 2: thumbKick (the user's own summon gesture)
// clears the dead book and the next request may try again.
func TestThumbKickRevivesDead(t *testing.T) {
	const hwnd = 777104
	attempts := 0
	failMode := true
	restore := thumbTestSetup(t, hwnd, func(uint64, int) ([]byte, error) {
		attempts++
		if failMode {
			return nil, errTestCapture
		}
		return []byte("\xff\xd8\xff-revived"), nil
	}, true)
	defer restore()
	for i := 0; i < thumbMaxFails; i++ {
		recordCapture(hwnd, nil, errTestCapture)
	}
	thumbKick(hwnd)
	failMode = false
	code, _ := thumbGet(hwnd, true)
	if code != 200 {
		t.Fatalf("after kick: got %d, want 200 (revived capture served)", code)
	}
	if attempts != 1 {
		t.Fatalf("after kick: %d attempts, want 1", attempts)
	}
	thumbMu.Lock()
	ent := thumbCache[hwnd]
	thumbMu.Unlock()
	if ent == nil || ent.dead || ent.fails != 0 {
		t.Fatalf("entry after successful revival = %+v, want alive with a clean book", ent)
	}
}

// T-I2: failures inside the newborn grace are counted but
// never condemn; the first failure after the grace can.
func TestThumbNewbornGrace(t *testing.T) {
	const hwnd = 777105
	restore := thumbTestSetup(t, hwnd, func(uint64, int) ([]byte, error) {
		return nil, errTestCapture
	}, true)
	defer restore()
	thumbKick(hwnd) // stamps the grace clock, as a claim does
	for i := 0; i < thumbMaxFails+2; i++ {
		recordCapture(hwnd, nil, errTestCapture)
	}
	thumbMu.Lock()
	ent := thumbCache[hwnd]
	thumbMu.Unlock()
	if ent == nil || ent.dead {
		t.Fatalf("inside grace: entry = %+v, want counted but NOT dead", ent)
	}
	thumbMu.Lock()
	thumbBornAt[hwnd] = time.Now().Add(-time.Minute)
	thumbMu.Unlock()
	recordCapture(hwnd, nil, errTestCapture)
	thumbMu.Lock()
	ent = thumbCache[hwnd]
	thumbMu.Unlock()
	if ent == nil || !ent.dead {
		t.Fatalf("after grace: entry = %+v, want dead", ent)
	}
}

var errTestCapture = errorString("test capture failure")

type errorString string

func (e errorString) Error() string { return string(e) }
