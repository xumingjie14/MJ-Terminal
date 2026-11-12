package main

// ---------- v0.68 细节清单 ⑧：网页通知进通知中心 (module: notify) ----------
//
// The isolated Chrome's web pages raise their notifications
// through Chrome itself, which lands them in the WINDOWS
// notification center -- the terminal never saw them. This file
// is the bridge that changes that, for the daemon-managed
// isolated Chrome only (Data/ChromeProfile; normal windows and
// --app windows share that one profile and launch path):
//
//   - The extension ships embedded in the EXE (assets/webnotify)
//     and is RELEASED into a managed dir under Data at every
//     isolated-Chrome launch (releaseWebNotifyExt), with a
//     config.json stamped with this daemon's own base URL (the
//     port is per-instance, so it cannot be baked into the
//     extension). The launch adds --load-extension=<dir>; the
//     user never installs anything.
//   - patch.js (main world, document_start) takes over the
//     page's Notification constructor: granted notifications are
//     forwarded instead of shown natively (no double popups),
//     permission/requestPermission stay Chrome's own, and the
//     instance the page holds keeps its fields and events.
//   - bridge.js (isolated world) relays page <-> service worker;
//     sw.js POSTs to /api/webnotify here. The record lands in
//     Notification Center as an APP-category event of the
//     sending site (AppID "web-<host slug>", AppName = host), so
//     the center stacks per site and the source line IS the
//     domain. The master gate is the notify source "web"
//     （设置 → 通知 → 网页通知， notify_allow_web /
//     notify_banner_web): the handler re-checks it on every
//     post, and the ping tells the extension to stop forwarding
//     while it is off.
//   - Clicks: Notification Center hands the event's route
//     ("webnotify:<host>:<event id>") to the view, which raises
//     the matching Chrome window via the existing extwin
//     machinery AND posts /api/webnotify/click; the extension's
//     long-poll (/api/webnotify/poll) picks the click up,
//     focuses the tab, and fires the page instance's click
//     event -- so a page's own onclick works too.
//
// Known boundaries (stated in the release notes, not papered
// over):
//   - Service Worker BACKGROUND push is not intercepted -- the
//     bridge only sees notifications a live page constructs
//     (including page-context registration.showNotification,
//     which patch.js also takes over). Background push is a
//     separate, later stage.
//   - Firefox is not covered (different extension model).
//   - --load-extension takes effect when the browser process
//     STARTS with the profile: a Chrome that is already running
//     keeps its old session until fully closed and relaunched.
//   - Branded Google Chrome may ignore --load-extension on
//     recent versions (Google has been retiring the switch in
//     favour of Chrome for Testing); Edge and Chromium still
//     honour it. The ping below is the diagnostic: if a launch
//     is not followed by a bridge ping, noteWebNotifyLaunchCheck
//     says so in the console instead of failing silently.

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed assets/webnotify
var webNotifyExtFS embed.FS

// webNotifyExtFiles are the extension's own files, released
// verbatim from the embed on every isolated-Chrome launch
// (write-if-changed, so the on-disk copy always matches the
// running terminal version without churning the disk).
var webNotifyExtFiles = []string{"manifest.json", "patch.js", "bridge.js", "sw.js"}

// webNotifyExtDir is the managed release dir under Data.
func webNotifyExtDir() string {
	return filepath.Join(dataDir(), "extensions", "webnotify")
}

// writeFileIfChanged writes b to path only when the current
// content differs. Returns whether it wrote.
func writeFileIfChanged(path string, b []byte) (bool, error) {
	if cur, err := os.ReadFile(path); err == nil && string(cur) == string(b) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".mjtmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// releaseWebNotifyExtTo is the testable core of the release:
// the embedded extension files into dir, plus config.json
// stamped with the daemon base URL the service worker should
// talk to. Any failure returns "" (the caller logs it); a
// browser launch must never fail because the bridge could not
// be provisioned.
func releaseWebNotifyExtTo(dir, base string) string {
	if dir == "" || base == "" {
		return ""
	}
	for _, name := range webNotifyExtFiles {
		b, err := webNotifyExtFS.ReadFile("assets/webnotify/" + name)
		if err != nil {
			logf("notify", "warn", "网页通知桥扩展释放失败（读内嵌 %s）：%v", name, err)
			return ""
		}
		if _, err := writeFileIfChanged(filepath.Join(dir, name), b); err != nil {
			logf("notify", "warn", "网页通知桥扩展释放失败（写 %s）：%v", name, err)
			return ""
		}
	}
	cfg, _ := json.Marshal(map[string]string{"base": base})
	if _, err := writeFileIfChanged(filepath.Join(dir, "config.json"), cfg); err != nil {
		logf("notify", "warn", "网页通知桥扩展释放失败（写 config.json）：%v", err)
		return ""
	}
	return dir
}

// releaseWebNotifyExt releases the bridge extension for a real
// launch (managed dir under Data, this daemon's base URL).
func releaseWebNotifyExt() string {
	return releaseWebNotifyExtTo(webNotifyExtDir(), daemonSelfURL)
}

// ---------- bridge liveness (the console diagnostic) ----------

var webNotifyBridge = struct {
	sync.Mutex
	lastPing time.Time
}{}

// webNotifyLaunchCheck runs after an isolated-Chrome launch:
// if the bridge has not pinged since just before the launch,
// the extension almost certainly did not load (most often a
// branded-Chrome build ignoring --load-extension, or a browser
// session that was already running and never restarted). One
// warn line in the console instead of a silent no-op; the line
// names the browser executable that was launched (v0.69-test3)
// so the next field round can tell at a glance WHICH browser
// ignored the flag.
func webNotifyLaunchCheck(since time.Time, browserName string) {
	if !notifyAllowed(settingsGet(), notifySrcWeb) {
		return // bridge intentionally off: silence is correct
	}
	go func() {
		time.Sleep(30 * time.Second)
		webNotifyBridge.Lock()
		last := webNotifyBridge.lastPing
		webNotifyBridge.Unlock()
		if last.Before(since) {
			logf("notify", "warn", "网页通知桥未见连接：隔离浏览器可能没有加载桥扩展（本次浏览器：%s；已在运行的浏览器要完全退出再打开才加载；新版 Google Chrome 可能忽略 --load-extension，Edge / Chromium 不受影响）——网页通知暂时进不了通知中心", browserName)
		}
	}()
}

// ---------- site identity (pure, unit-tested) ----------

// webNotifyHost derives the display host (with port, lowercased
// host part) from a page URL. Only http/https pages can send
// notifications; anything else yields "" and is rejected.
func webNotifyHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return strings.ToLower(u.Hostname()) + portSuffix(u)
}

func portSuffix(u *url.URL) string {
	if p := u.Port(); p != "" {
		return ":" + p
	}
	return ""
}

// webNotifyAppID maps a host to the per-app notify id family
// ("web-<slug>") the settings model already reserves for web
// apps: lowercase letters, digits and dashes only.
func webNotifyAppID(host string) string {
	var b strings.Builder
	lastDash := false
	for _, c := range strings.ToLower(host) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 56 {
		slug = strings.Trim(slug[:56], "-")
	}
	if slug == "" {
		slug = "site"
	}
	return "web-" + slug
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// ---------- recording ----------

// recordWeb stores one web page notification. Unlike record /
// recordApp there is NO dedupe: each notification a page raises
// is a distinct event (a chat app's second message must not be
// swallowed by the first), and the rolling store cap keeps the
// center bounded. The route carries host + the event's own id
// ("webnotify:<host>:<id>") -- the id only exists once assigned,
// so this method builds the event itself instead of taking a
// route parameter like record().
func (st *notifyStore) recordWeb(appID, appName, title, body string, now time.Time) notifyEvent {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loadLocked()
	st.seq++
	id := fmt.Sprintf("n%d", st.seq)
	ev := notifyEvent{
		ID: id, Time: now.UnixMilli(), Source: notifySrcApp,
		Title: title, Body: body,
		Route:    "webnotify:" + appName + ":" + id,
		Category: "app", AppID: appID, AppName: appName,
	}
	st.events = append([]notifyEvent{ev}, st.events...)
	if len(st.events) > notifyMaxEvents {
		st.events = st.events[:notifyMaxEvents]
	}
	st.saveLocked()
	return ev
}

// ---------- click queue (Notification Center -> extension) ----------

type webNotifyClick struct {
	ID string `json:"id"`
}

var webNotifyClickQ = struct {
	sync.Mutex
	q   []webNotifyClick
	sig chan struct{}
}{sig: make(chan struct{}, 1)}

func webNotifyPushClick(id string) {
	webNotifyClickQ.Lock()
	if len(webNotifyClickQ.q) < 20 { // clicks for a dead browser are worthless; cap the queue
		webNotifyClickQ.q = append(webNotifyClickQ.q, webNotifyClick{ID: id})
	}
	webNotifyClickQ.Unlock()
	select {
	case webNotifyClickQ.sig <- struct{}{}:
	default:
	}
}

func webNotifyDrainClicks() []webNotifyClick {
	webNotifyClickQ.Lock()
	defer webNotifyClickQ.Unlock()
	if len(webNotifyClickQ.q) == 0 {
		return nil
	}
	out := webNotifyClickQ.q
	webNotifyClickQ.q = nil
	return out
}

// ---------- HTTP ----------

type webNotifyPost struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Icon  string `json:"icon"` // carried for the future; the center has no per-event icon slot yet
	Tag   string `json:"tag"`
	URL   string `json:"url"`
}

// webNotifyHandler serves POST /api/webnotify: one page
// notification from the bridge extension. The master switch is
// re-checked here on every post (the daemon is authoritative;
// the extension also stops forwarding when the ping tells it
// the switch is off).
func webNotifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	var body webNotifyPost
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadRequest, "参数不对")
		return
	}
	host := webNotifyHost(body.URL)
	if host == "" {
		apiErrBadRequest(w, errCodeBadRequest, "来源不对")
		return
	}
	title := truncateRunes(strings.TrimSpace(body.Title), 200)
	if title == "" {
		title = host
	}
	text := truncateRunes(strings.TrimSpace(body.Body), 500)
	s := settingsGet()
	if !notifyAllowed(s, notifySrcWeb) {
		logf("notify", "info", "网页通知被总开关拦下（%s：%s）", host, title)
		writeJSON(w, map[string]any{"ok": false, "disabled": true})
		return
	}
	ev := notifySt.recordWeb(webNotifyAppID(host), host, title, text, time.Now())
	logf("notify", "info", "网页通知 [%s] %s", host, title)
	if notifyBanner(s, notifySrcWeb) {
		notifyPushBanner(ev)
	}
	writeJSON(w, map[string]any{"ok": true, "id": ev.ID})
}

// webNotifyPingHandler serves POST /api/webnotify/ping: the
// extension's service worker checking in. Answers the master
// switch state so the extension can stop forwarding while off,
// and stamps the liveness clock the launch check reads. A fresh
// ping after a quiet period logs one line -- the console proof
// that the bridge is really loaded in the running browser.
func webNotifyPingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024)).Decode(&body)
	now := time.Now()
	webNotifyBridge.Lock()
	stale := webNotifyBridge.lastPing.IsZero() || now.Sub(webNotifyBridge.lastPing) > 5*time.Minute
	webNotifyBridge.lastPing = now
	webNotifyBridge.Unlock()
	if stale {
		logf("notify", "info", "网页通知桥已连接（扩展 v%s，隔离 Chrome）", body.Version)
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"enabled": notifyAllowed(settingsGet(), notifySrcWeb),
		"version": AppVersion,
	})
}

// webNotifyPollHandler serves GET /api/webnotify/poll: the
// extension's long-poll for Notification Center clicks. Holds
// the request up to ~25 s; an empty answer at the deadline is
// normal and the extension simply re-polls.
func webNotifyPollHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		if clicks := webNotifyDrainClicks(); len(clicks) > 0 {
			writeJSON(w, map[string]any{"clicks": clicks})
			return
		}
		if time.Now().After(deadline) {
			writeJSON(w, map[string]any{"clicks": []webNotifyClick{}})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-webNotifyClickQ.sig:
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// webNotifyClickHandler serves POST /api/webnotify/click {id}:
// the view reports a Notification Center click on a web event
// (its route carries the id). The click joins the queue the
// extension long-polls; delivery to the actual tab/page is the
// extension's side of the bridge.
func webNotifyClickHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		apiErrBadRequest(w, errCodeBadRequest, "参数不对")
		return
	}
	webNotifyPushClick(body.ID)
	logf("notify", "info", "网页通知被点击（%s），已回传页面", body.ID)
	writeJSON(w, okResponse{OK: true})
}
