package main

// updaterestart.go -- v0.71 block 5: the post-update
// "restart the browser once" hint.
//
// The wart (v0.69 field round): several browser-side pins
// (the download / save-as landing directories above all)
// only take effect when the isolated Chrome has FULLY
// exited and been relaunched, because a running browser
// holds its preferences in memory. After an in-app update
// the user kept browsing in the old session and concluded
// the fix had not landed. This block closes that loop:
//
//   - noteVersionAtStartup (startServicesOnce) compares the
//     settings store's lastVersion against AppVersion. A
//     change is recorded (lastVersion := AppVersion) and
//     remembered for this run; a first install records
//     silently -- there is no "previous session" to restart.
//   - GET /api/update/restart-hint answers the pure
//     predicate below: show only when the version changed
//     this run, the user has not already been told about
//     THIS version, and the isolated Chrome is actually
//     running (the only state in which the advice matters).
//   - POST /api/update/restart-hint/dismiss stamps
//     updateNagVersion = AppVersion: this version never
//     hints again, whichever button the user picked.
//   - POST /api/update/restart-hint/restart does the same
//     stamp, then restarts the isolated browser politely:
//     WM_CLOSE to its registered windows (its own X-button
//     request -- it may save and quit on its own terms), a
//     bounded wait for the profile lock to fall, and a fresh
//     launch through the normal path (which re-pins the
//     landing directories on the way in).

import (
	"net/http"
	"path/filepath"
	"sync/atomic"
	"time"
)

// versionChangedThisRun is set by noteVersionAtStartup when
// the previous run recorded a different AppVersion.
var versionChangedThisRun atomic.Bool

// restartHintShow is the hint's whole rulebook, kept pure:
// show exactly when the version changed on this run, this
// version has not been dismissed yet, and the isolated
// browser is running (nothing to restart otherwise).
func restartHintShow(changedThisRun bool, naggedVersion, currentVersion string, chromeRunning bool) bool {
	return changedThisRun && chromeRunning && naggedVersion != currentVersion
}

// settingsSetVersionMarks persists the two internal version
// marks (an empty argument leaves that mark unchanged).
func settingsSetVersionMarks(last, nag string) {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	if last != "" {
		setState.cur.LastVersion = last
	}
	if nag != "" {
		setState.cur.UpdateNagVersion = nag
	}
	if err := settingsSaveLocked(); err != nil {
		logf("update", "error", "版本标记保存失败：%v", err)
	}
}

// noteVersionAtStartup records which version this run is
// and whether it differs from the previous run's.
func noteVersionAtStartup() {
	prev := settingsGet().LastVersion
	if prev == AppVersion {
		return
	}
	settingsSetVersionMarks(AppVersion, "")
	if prev == "" {
		logf("update", "info", "已记录当前版本 v%s", AppVersion)
		return
	}
	versionChangedThisRun.Store(true)
	logf("update", "info", "版本已更新：v%s → v%s；隔离浏览器完全退出并重新打开后，文件保存位置等设置即刻生效（如浏览器正在运行，桌面会给出一次重启提醒）", prev, AppVersion)
}

// isolatedChromeRunning reports whether the isolated
// browser profile currently has a live browser behind it
// (the profile lockfile is present for the browser's whole
// lifetime -- the same signal containment.go trusts).
func isolatedChromeRunning() bool {
	return profileLockPresent(filepath.Join(dataDir(), "ChromeProfile"))
}

// restartHintHandler serves GET /api/update/restart-hint.
func restartHintHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	s := settingsGet()
	show := restartHintShow(versionChangedThisRun.Load(), s.UpdateNagVersion, AppVersion, isolatedChromeRunning())
	writeJSON(w, restartHintResponse{Show: show, Version: AppVersion})
}

// restartHintDismissHandler serves
// POST /api/update/restart-hint/dismiss: the user has seen
// this version's hint; never show it again for it.
func restartHintDismissHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	settingsSetVersionMarks("", AppVersion)
	logf("update", "info", "更新后的浏览器重启提醒：用户已确认（v%s 不再重复提醒）", AppVersion)
	writeJSON(w, okResponse{OK: true})
}

// restartHintRestartHandler serves
// POST /api/update/restart-hint/restart: dismiss + perform
// the graceful browser restart in the background (the view
// answers immediately; the choreography is logged).
func restartHintRestartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "只接受 POST")
		return
	}
	settingsSetVersionMarks("", AppVersion)
	logf("update", "info", "更新后的浏览器重启提醒：用户选择立即重启浏览器（v%s）", AppVersion)
	go restartIsolatedBrowser()
	writeJSON(w, okResponse{OK: true})
}

// restartIsolatedBrowser politely closes the isolated
// Chrome's registered windows and relaunches the browser
// through the normal launch path. Only windows whose owning
// process is the Chromium browser of the isolated profile
// are asked to close (Firefox and everything else the
// terminal manages are not part of this).
func restartIsolatedBrowser() {
	extMu.Lock()
	var wins []*extWindow
	for _, e := range extWins {
		if e.Shell {
			continue
		}
		switch platformProcessImageBase(e.pid) {
		case "chrome.exe", "msedge.exe":
			cp := *e
			wins = append(wins, &cp)
		}
	}
	extMu.Unlock()
	closed := 0
	for _, e := range wins {
		if platformCloseWindow(e.HWND) {
			closed++
		}
	}
	logf("update", "info", "浏览器重启：已向隔离浏览器的 %d 个窗口发出关闭请求，等它完全退出", closed)
	// Bounded wait for the profile lock to fall (the browser
	// is really out); proceed anyway after ~8 s -- a window
	// that refused to close keeps its session, and the fresh
	// launch simply hands off to it.
	deadline := time.Now().Add(8 * time.Second)
	for isolatedChromeRunning() && time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
	}
	if isolatedChromeRunning() {
		logf("update", "warn", "浏览器重启：等待超时，浏览器仍在运行——将直接重新打开（会交给现有会话）")
	}
	proxy := accelEnabled() && len(nodes) > 0
	if err := launchIsolatedBrowser(proxy, "", false); err != nil {
		logf("update", "error", "浏览器重启：重新打开失败：%v", err)
		return
	}
	logf("update", "info", "浏览器重启：已重新打开隔离浏览器，新版设置即刻生效")
}
