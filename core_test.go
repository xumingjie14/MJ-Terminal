package main

// v0.70 characterization tests for the sing-box core lifecycle.
// These were written BEFORE the lifecycle code moved out of
// main.go into core.go, against the code as it stood in main.go,
// and the split must keep every assertion passing unmodified:
// they pin the externally visible behaviour of the moved surface
// (the running-state predicate, the missing-binary guards and
// the /api/core handler responses in a bare environment).
//
// The path-derived cases assume the test process runs bare (no
// sing-box.exe and no core config next to the test binary --
// true under `go test`, where the binary lives in a build temp
// dir); each skips itself if the environment is not bare.

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinCoreState snapshots the core lifecycle globals and restores
// them at test end, holding coreMu for the whole test: for tests
// that drive the *Locked functions directly, exactly as
// production callers do. Handler tests must use
// snapshotCoreState instead -- coreControl takes coreMu itself.
func pinCoreState(t *testing.T) {
	t.Helper()
	coreMu.Lock()
	savedCmd := coreCmd
	savedExited := coreExited.Load()
	savedStopReq := coreStopReq.Load()
	savedExitErr, _ := coreExitErr.Load().(string)
	t.Cleanup(func() {
		coreCmd = savedCmd
		coreExited.Store(savedExited)
		coreStopReq.Store(savedStopReq)
		coreExitErr.Store(savedExitErr)
		coreMu.Unlock()
	})
}

// snapshotCoreState snapshots the core lifecycle globals under a
// briefly held coreMu and restores them at test end, WITHOUT
// holding the lock in between: for tests that go through
// coreControl, which locks coreMu itself (the mutex is not
// re-entrant).
func snapshotCoreState(t *testing.T) {
	t.Helper()
	coreMu.Lock()
	savedCmd := coreCmd
	savedExited := coreExited.Load()
	savedStopReq := coreStopReq.Load()
	savedExitErr, _ := coreExitErr.Load().(string)
	coreMu.Unlock()
	t.Cleanup(func() {
		coreMu.Lock()
		coreCmd = savedCmd
		coreExited.Store(savedExited)
		coreStopReq.Store(savedStopReq)
		coreExitErr.Store(savedExitErr)
		coreMu.Unlock()
	})
}

func TestCoreRunningLockedTruthTable(t *testing.T) {
	pinCoreState(t)

	coreCmd = nil
	coreExited.Store(false)
	if coreRunningLocked() {
		t.Fatal("no coreCmd must read as not running")
	}

	coreCmd = &exec.Cmd{} // never started: Process is nil
	if coreRunningLocked() {
		t.Fatal("coreCmd without a Process must read as not running")
	}

	coreCmd = &exec.Cmd{Process: &os.Process{}}
	coreExited.Store(true)
	if coreRunningLocked() {
		t.Fatal("an exited core must read as not running")
	}

	coreExited.Store(false)
	if !coreRunningLocked() {
		t.Fatal("a live core process must read as running")
	}
}

func coreEnvBare(t *testing.T) bool {
	t.Helper()
	if _, err := os.Stat(corePath()); err == nil {
		return false
	}
	for _, name := range []string{"config.generated.json", "config.json"} {
		if _, err := os.Stat(filepath.Join(coreDir(), name)); err == nil {
			return false
		}
	}
	return true
}

func TestStartCoreLockedMissingBinary(t *testing.T) {
	if !coreEnvBare(t) {
		t.Skip("core binary/config present next to the test binary; bare-environment case only")
	}
	pinCoreState(t)
	coreCmd = nil
	err := startCoreLocked()
	if err == nil {
		t.Fatal("starting without a core binary must fail")
	}
	if !strings.Contains(err.Error(), "未找到 core/sing-box.exe") {
		t.Fatalf("missing-binary error changed: %v", err)
	}
}

func TestCoreConfigPathBareEnv(t *testing.T) {
	if !coreEnvBare(t) {
		t.Skip("core config present next to the test binary; bare-environment case only")
	}
	if got := coreConfigPath(); got != "" {
		t.Fatalf("coreConfigPath() = %q, want \"\" with no config files", got)
	}
}

func coreControlCall(t *testing.T, action string) coreStatusResponse {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/core", strings.NewReader(`{"action":"`+action+`"}`))
	rec := httptest.NewRecorder()
	coreControl(rec, req)
	var resp coreStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("coreControl(%q) response is not a coreStatusResponse: %v (body %q)", action, err, rec.Body.String())
	}
	return resp
}

func TestCoreControlStatusMissingBinary(t *testing.T) {
	if !coreEnvBare(t) {
		t.Skip("core binary present next to the test binary; bare-environment case only")
	}
	resp := coreControlCall(t, "status")
	if resp.Running {
		t.Fatal("status must report not running without a core binary")
	}
	if !strings.Contains(resp.Message, "未找到 core/sing-box.exe") {
		t.Fatalf("status message changed: %q", resp.Message)
	}
}

func TestCoreControlStopIdempotent(t *testing.T) {
	snapshotCoreState(t)
	coreMu.Lock()
	coreCmd = nil
	coreMu.Unlock()
	resp := coreControlCall(t, "stop")
	if resp.Running {
		t.Fatal("stop must report not running")
	}
	if resp.Message != "已停止" {
		t.Fatalf("stop message changed: %q", resp.Message)
	}
}

func TestCoreControlUnknownAction(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/core", strings.NewReader(`{"action":"bogus"}`))
	rec := httptest.NewRecorder()
	coreControl(rec, req)
	if rec.Code != 400 {
		t.Fatalf("unknown action: HTTP %d, want 400", rec.Code)
	}
}
