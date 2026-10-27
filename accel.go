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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

// dlClient serves the daemon's own downloads (app updates, the
// Firefox component): while in-app acceleration is active they go
// through the local node like the rest of the app's traffic —
// direct connections to GitHub are painfully slow on some networks.
func dlClient() *http.Client {
	if proxyActive() {
		proxyURL := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", proxyPort)}
		return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	}
	return http.DefaultClient
}

// dlGet GETs a URL through dlClient; if the proxied attempt fails
// at transport level it retries once directly, so a misbehaving
// node can never block a download outright.
func dlGet(ctx context.Context, rawurl, ua string) (*http.Response, error) {
	try := func(c *http.Client) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
		if err != nil {
			return nil, err
		}
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		return c.Do(req)
	}
	proxied := proxyActive()
	resp, err := try(dlClient())
	if err != nil && proxied {
		return try(http.DefaultClient)
	}
	return resp, err
}

// ensureCoreForDownload makes a best effort to have the node up
// before a big download, so it can take the fast path through the
// node instead of a slow direct connection. It respects the
// acceleration switch: with the switch off, downloads stay direct.
func ensureCoreForDownload() {
	if !accelEnabled() || proxyActive() || len(nodes) == 0 {
		return
	}
	coreMu.Lock()
	_ = startCoreLocked()
	coreMu.Unlock()
}

// downloadRoute names the route downloads will take right now, so
// status messages can show the user which path is in use.
func downloadRoute() string {
	if proxyActive() {
		return "走节点"
	}
	return "直连"
}

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
			if body.Enabled {
				logf("system", "info", "全局加速：开")
			} else {
				logf("system", "info", "全局加速：关")
			}
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
