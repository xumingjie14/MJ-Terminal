package main

import (
	"net/http/httptest"
	"os"
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
	thumbCache[hwnd] = &thumbEntry{png: []byte("\x89PNG-fake-frame"), at: time.Now()}
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
