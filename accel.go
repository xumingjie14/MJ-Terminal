package main

// In-app global acceleration (v0.8).
//
// When ON, everything that lives INSIDE the terminal environment
// routes through the node:
//   * the desktop shell window itself (launched with --proxy-server),
//   * links opened from inside the system (isolated Chrome, proxied),
//   * commands run in the built-in terminal (proxy env vars for the
//     child process -- curl/git/pip/... honor them).
//
// The host PC outside the environment is never touched: no system
// proxy change, no TUN, nothing. The switch persists in
// Data/accel.txt and defaults to ON.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func accelFile() string { return filepath.Join(dataDir(), "accel.txt") }

func accelEnabled() bool {
	b, err := os.ReadFile(accelFile())
	if err != nil {
		return true // default ON
	}
	return strings.TrimSpace(string(b)) != "0"
}

func setAccel(on bool) {
	v := "1"
	if !on {
		v = "0"
	}
	_ = os.WriteFile(accelFile(), []byte(v), 0o600)
}

func coreRunningQuick() bool {
	coreMu.Lock()
	defer coreMu.Unlock()
	return coreRunningLocked()
}

// proxyActive reports whether in-app traffic can go through the node
// right now: switch on AND core really running.
func proxyActive() bool { return accelEnabled() && coreRunningQuick() }

func accelStatus() map[string]any {
	en := accelEnabled()
	running := coreRunningQuick()
	return map[string]any{"enabled": en, "coreRunning": running, "active": en && running}
}

func accelHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			setAccel(body.Enabled)
			// Turning it on should also bring the node up, so the
			// acceleration is real immediately, not just a flag.
			if body.Enabled && len(nodes) > 0 {
				coreMu.Lock()
				_ = startCoreLocked()
				coreMu.Unlock()
			}
		}
	}
	writeJSON(w, accelStatus())
}
