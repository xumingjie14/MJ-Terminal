package main

// v0.47 tests: the notification store (round-trip, rolling cap,
// dedupe, once-keys), the status-type assembly judgement, the
// settings whitelist for the new key families, and the remote
// session activity window. All pure / temp-dir based.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotifyStoreRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notifications.json")
	st := notifyStoreForTests(p)
	now := time.Now()
	if _, ok := st.record("sandbox", "k1", "标题一", "正文", "appstore", now); !ok {
		t.Fatal("first record should be stored")
	}
	if _, ok := st.record("node", "k2", "标题二", "", "core", now); !ok {
		t.Fatal("second record should be stored")
	}
	// A fresh store on the same file sees both, newest first.
	st2 := notifyStoreForTests(p)
	evs := st2.list()
	if len(evs) != 2 || evs[0].Title != "标题二" || evs[1].Title != "标题一" {
		t.Fatalf("round trip mismatch: %+v", evs)
	}
	if evs[0].ID == "" || evs[0].ID == evs[1].ID {
		t.Fatalf("ids must be non-empty and unique: %+v", evs)
	}
	// Ids keep increasing across the reload (no reuse).
	ev, ok := st2.record("files", "k3", "标题三", "", "", now)
	if !ok || ev.ID == evs[0].ID || ev.ID == evs[1].ID {
		t.Fatalf("id reused after reload: %q", ev.ID)
	}
}

func TestNotifyStoreCap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notifications.json")
	st := notifyStoreForTests(p)
	now := time.Now()
	for i := 0; i < notifyMaxEvents+10; i++ {
		key := string(rune('a'+i%26)) + time.Duration(i).String()
		if _, ok := st.record("node", "k"+key, "t", "", "", now.Add(time.Duration(i)*time.Minute)); !ok {
			t.Fatalf("record %d unexpectedly deduped", i)
		}
	}
	evs := st.list()
	if len(evs) != notifyMaxEvents {
		t.Fatalf("cap: got %d events, want %d", len(evs), notifyMaxEvents)
	}
}

func TestNotifyStoreDedupe(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notifications.json")
	st := notifyStoreForTests(p)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if _, ok := st.record("node", "connect-fail", "失败", "", "core", now); !ok {
		t.Fatal("first should record")
	}
	if _, ok := st.record("node", "connect-fail", "失败", "", "core", now.Add(5*time.Minute)); ok {
		t.Fatal("same source+key within 10 min must dedupe")
	}
	if _, ok := st.record("node", "connect-fail", "失败", "", "core", now.Add(11*time.Minute)); !ok {
		t.Fatal("after the window it should record again")
	}
	if _, ok := st.record("node", "core-exit", "断开", "", "core", now.Add(time.Minute)); !ok {
		t.Fatal("a different key must not dedupe")
	}
	if _, ok := st.record("remote", "connect-fail", "x", "", "", now.Add(time.Minute)); !ok {
		t.Fatal("a different source must not dedupe")
	}
	// Empty key never dedupes.
	if _, ok := st.record("files", "", "a", "", "", now); !ok {
		t.Fatal("empty key first should record")
	}
	if _, ok := st.record("files", "", "a", "", "", now); !ok {
		t.Fatal("empty key must never dedupe")
	}
}

func TestNotifyStoreDismissAndClear(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notifications.json")
	st := notifyStoreForTests(p)
	now := time.Now()
	e1, _ := st.record("node", "a", "n1", "", "", now)
	_, _ = st.record("node", "b", "n2", "", "", now)
	_, _ = st.record("files", "c", "f1", "", "", now)
	if !st.dismiss(e1.ID) {
		t.Fatal("dismiss existing should succeed")
	}
	if st.dismiss(e1.ID) {
		t.Fatal("dismiss twice should fail")
	}
	if n := st.clearSource("node"); n != 1 {
		t.Fatalf("clearSource node removed %d, want 1", n)
	}
	evs := st.list()
	if len(evs) != 1 || evs[0].Source != "files" {
		t.Fatalf("only the files event should remain: %+v", evs)
	}
	// Persisted: a fresh store agrees.
	if got := len(notifyStoreForTests(p).list()); got != 1 {
		t.Fatalf("persisted list has %d, want 1", got)
	}
}

func TestNotifyStoreOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notifications.json")
	st := notifyStoreForTests(p)
	if !st.tryOnce("update-available:0.47") {
		t.Fatal("first tryOnce should fire")
	}
	if st.tryOnce("update-available:0.47") {
		t.Fatal("second tryOnce must not fire")
	}
	if !st.tryOnce("update-available:0.48") {
		t.Fatal("a new version key should fire")
	}
	// Persisted across store instances.
	if notifyStoreForTests(p).tryOnce("update-available:0.47") {
		t.Fatal("once must persist across restarts")
	}
}

func TestNotifyStatusItems(t *testing.T) {
	on := defaultSettings()
	none := notifyStatusFacts{}
	if got := notifyStatusItems(on, none); len(got) != 0 {
		t.Fatalf("no facts -> no items, got %+v", got)
	}
	full := notifyStatusFacts{
		SandboxPending: true, SandboxDetail: "Telegram 安装包",
		UpdatePhase: "available", UpdateDetail: "发现新版本 v0.48",
		ServerAlert: true, ServerDetail: "AWS：磁盘已用 95%",
	}
	got := notifyStatusItems(on, full)
	if len(got) != 3 || got[0].ID != "sandbox-pending" || got[1].ID != "update-ready" || got[2].ID != "server-alert" {
		t.Fatalf("full facts -> 3 items in fixed order, got %+v", got)
	}
	// Downloading and ready phases also surface; idle/uptodate don't.
	for _, ph := range []string{"downloading", "ready"} {
		if n := len(notifyStatusItems(on, notifyStatusFacts{UpdatePhase: ph})); n != 1 {
			t.Fatalf("phase %s should surface, got %d items", ph, n)
		}
	}
	for _, ph := range []string{"", "idle", "checking", "uptodate", "error"} {
		if n := len(notifyStatusItems(on, notifyStatusFacts{UpdatePhase: ph})); n != 0 {
			t.Fatalf("phase %q should not surface, got %d items", ph, n)
		}
	}
	// Allow-off sources drop out (the feature's own pages are
	// unaffected -- only the Notification Center assembly reads
	// these switches).
	off := on
	off.NotifyAllowSandbox = false
	off.NotifyAllowServer = false
	got = notifyStatusItems(off, full)
	if len(got) != 1 || got[0].ID != "update-ready" {
		t.Fatalf("allow-off sources must drop out, got %+v", got)
	}
}

func TestSettingsV047Keys(t *testing.T) {
	d := defaultSettings()
	if d.DockIconSize != 52 {
		t.Fatalf("dockIconSize default = %d, want 52", d.DockIconSize)
	}
	for _, k := range []string{
		"cc_show_immersive", "cc_show_stage", "cc_show_fullscreen",
		"cc_show_dockhide", "cc_show_remote", "cc_show_wallpaper",
		"notify_allow_sandbox", "notify_allow_update", "notify_allow_servermon",
		"notify_allow_node", "notify_allow_remote", "notify_allow_files",
		"notify_banner_sandbox", "notify_banner_update", "notify_banner_servermon",
		"notify_banner_node", "notify_banner_remote", "notify_banner_files",
	} {
		p := boolSettingPtr(&d, k)
		if p == nil {
			t.Fatalf("boolSettingPtr(%q) = nil, want a field", k)
		}
		if !*p {
			t.Fatalf("default for %q should be true", k)
		}
	}
	if boolSettingPtr(&d, "cc_show_node") != nil {
		t.Fatal("cc_show_node must NOT exist (node tile cannot be hidden)")
	}
	if boolSettingPtr(&d, "notify_allow_bogus") != nil {
		t.Fatal("unknown notify source must not resolve")
	}
	if !notifyAllowed(d, "node") || notifyAllowed(func() mjSettings { s := d; s.NotifyAllowNode = false; return s }(), "node") {
		t.Fatal("notifyAllowed must follow the per-source switch")
	}
	if !notifyAllowed(d, "unknownsource") {
		t.Fatal("unknown sources default to allowed")
	}
	// Sanitize: icon size clamps back into range, widget layout
	// resets when malformed and survives when well-formed.
	s := sanitizeSettings(mjSettings{DockIconSize: 200, NCWidgets: "{bad"})
	if s.DockIconSize != 52 || s.NCWidgets != defaultNCWidgets {
		t.Fatalf("sanitize failed: %+v", s)
	}
	good := `[{"id":"node","show":false},{"id":"calendar","show":true}]`
	if got := sanitizeSettings(mjSettings{NCWidgets: good}).NCWidgets; got != good {
		t.Fatalf("well-formed layout must survive sanitize, got %q", got)
	}
	if ncWidgetsValid(`[{"id":"node","show":true},{"id":"node","show":true}]`) {
		t.Fatal("duplicate widget ids must be invalid")
	}
	if ncWidgetsValid(`[{"id":"weather","show":true}]`) {
		t.Fatal("unknown widget ids must be invalid (no weather widget)")
	}
	// settingsMap carries every new key (the frontend reads these
	// exact names).
	m := settingsMap()
	for _, k := range []string{"dockIconSize", "cc_show_remote", "notify_allow_node", "notify_banner_files", "nc_widgets"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("settingsMap missing %q", k)
		}
	}
}

func TestRemoteSessionActiveWindow(t *testing.T) {
	notifyRemote.Lock()
	notifyRemote.active = false
	notifyRemote.lastActivity = 0
	notifyRemote.Unlock()
	if remoteSessionActive() {
		t.Fatal("no activity -> no session")
	}
	notifyRemote.Lock()
	notifyRemote.active = true
	notifyRemote.lastActivity = time.Now().Unix()
	notifyRemote.Unlock()
	if !remoteSessionActive() {
		t.Fatal("fresh activity -> session active")
	}
	notifyRemote.Lock()
	notifyRemote.lastActivity = time.Now().Unix() - notifyRemoteIdleSecs - 10
	notifyRemote.Unlock()
	if remoteSessionActive() {
		t.Fatal("stale activity -> session over")
	}
	notifyRemote.Lock()
	notifyRemote.active = false
	notifyRemote.lastActivity = 0
	notifyRemote.Unlock()
}

// v0.54: app-category events -- category/app fields round-trip,
// per-app dedupe isolation, per-app clear, and the migration
// read (events stored before the split have no category and
// must read back as system).
func TestNotifyAppEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notifications.json")
	st := notifyStoreForTests(p)
	now := time.Date(2026, 10, 30, 19, 0, 0, 0, time.UTC)
	ev, ok := st.recordApp("store-telegram", "Telegram", "installed", "Telegram 安装完成", "", "appstore", now)
	if !ok {
		t.Fatal("first app event should record")
	}
	if ev.Category != "app" || ev.AppID != "store-telegram" || ev.AppName != "Telegram" || ev.Source != notifySrcApp {
		t.Fatalf("app event fields wrong: %+v", ev)
	}
	// Same app + same key inside the window dedupes...
	if _, ok := st.recordApp("store-telegram", "Telegram", "installed", "x", "", "", now.Add(time.Minute)); ok {
		t.Fatal("same app+key within 10 min must dedupe")
	}
	// ...but a DIFFERENT app with the same key must not.
	if _, ok := st.recordApp("prod-krita", "Krita", "installed", "Krita 安装完成", "", "", now.Add(time.Minute)); !ok {
		t.Fatal("a different app must not be deduped away")
	}
	// A system event alongside keeps its own category on read.
	if _, ok := st.record("node", "k", "节点事件", "", "", now); !ok {
		t.Fatal("system event should record")
	}
	evs := st.list()
	if len(evs) != 3 {
		t.Fatalf("list has %d events, want 3", len(evs))
	}
	for _, e := range evs {
		if e.Source == "node" && e.Category != "system" {
			t.Fatalf("system event must read back category=system: %+v", e)
		}
		if e.Source == notifySrcApp && e.Category != "app" {
			t.Fatalf("app event must read back category=app: %+v", e)
		}
	}
	// Per-app clear removes only that app's stack.
	if n := st.clearApp("store-telegram"); n != 1 {
		t.Fatalf("clearApp removed %d, want 1", n)
	}
	if n := st.clearApp("store-telegram"); n != 0 {
		t.Fatalf("clearApp again removed %d, want 0", n)
	}
	evs = st.list()
	if len(evs) != 2 {
		t.Fatalf("after clearApp, list has %d events, want 2", len(evs))
	}
	// Migration: a hand-written pre-v0.54 file (no category
	// anywhere) reads every event as system.
	p2 := filepath.Join(t.TempDir(), "notifications.json")
	legacy := `{"version":1,"events":[{"id":"n1","time":1,"source":"node","title":"旧事件"}],"dedupe":{},"once":[]}`
	if err := os.WriteFile(p2, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	evs = notifyStoreForTests(p2).list()
	if len(evs) != 1 || evs[0].Category != "system" {
		t.Fatalf("legacy event must migrate to category=system: %+v", evs)
	}
}

// v0.54: the per-app notification settings -- id whitelist,
// default-on semantics, sanitize pruning, settingsMap emission,
// and the POST handler round-trip for the dynamic keys.
func TestSettingsAppNotifyKeys(t *testing.T) {
	for _, id := range []string{"firefox", "openclaw", "store-telegram", "prod-onlyoffice", "web-github-com", "a", "a-b-c-1"} {
		if !validNotifyAppID(id) {
			t.Errorf("validNotifyAppID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "-lead", "Upper", "has space", "under_score", "dot.name", "中文", "a/b", strings.Repeat("a", 65)} {
		if validNotifyAppID(id) {
			t.Errorf("validNotifyAppID(%q) = true, want false", id)
		}
	}
	// Defaults: absent entry = allowed (both gates).
	d := defaultSettings()
	if !notifyAppAllowed(d, "store-telegram") || !notifyAppBanner(d, "store-telegram") {
		t.Fatal("absent app entries must default to allowed")
	}
	d.NotifyAllowApp = map[string]bool{"store-telegram": false}
	d.NotifyBannerApp = map[string]bool{"store-telegram": false, "firefox": true}
	if notifyAppAllowed(d, "store-telegram") || !notifyAppAllowed(d, "prod-krita") {
		t.Fatal("notifyAppAllowed must follow stored entries and default the rest")
	}
	if notifyAppBanner(d, "store-telegram") || !notifyAppBanner(d, "firefox") {
		t.Fatal("notifyAppBanner must follow stored entries")
	}
	// Sanitize prunes invalid ids and keeps valid ones.
	s := sanitizeSettings(mjSettings{NotifyAllowApp: map[string]bool{"store-ok": false, "BAD ID": false, "": true}})
	if _, ok := s.NotifyAllowApp["BAD ID"]; ok {
		t.Fatal("sanitize must prune invalid app ids")
	}
	if v, ok := s.NotifyAllowApp["store-ok"]; !ok || v {
		t.Fatal("sanitize must keep valid app entries verbatim")
	}
	// settingsMap emits stored entries flattened.
	setState.Lock()
	oldCur, oldLoaded := setState.cur, setState.loaded
	setState.cur = defaultSettings()
	setState.cur.NotifyAllowApp = map[string]bool{"prod-krita": false}
	setState.loaded = true
	setState.Unlock()
	m := settingsMap()
	setState.Lock()
	setState.cur, setState.loaded = oldCur, oldLoaded
	setState.Unlock()
	if v, ok := m["notify_allow_app_prod-krita"]; !ok || v != false {
		t.Fatalf("settingsMap must emit notify_allow_app_prod-krita=false, got %v", m["notify_allow_app_prod-krita"])
	}
	if _, ok := m["notify_allow_app_firefox"]; ok {
		t.Fatal("settingsMap must not invent entries the user never set")
	}
	// POST handler: a dynamic key round-trips into the maps; an
	// invalid id is rejected and changes nothing.
	setState.Lock()
	setState.cur = defaultSettings()
	setState.loaded = true
	setState.Unlock()
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		rec := httptest.NewRecorder()
		settingsHandler(rec, req)
		return rec
	}
	rec := post(`{"notify_allow_app_store-vlc": false}`)
	if !strings.Contains(rec.Body.String(), `"notify_allow_app_store-vlc":false`) {
		t.Fatalf("POST response must carry the stored app key: %s", rec.Body.String())
	}
	if notifyAppAllowed(settingsGet(), "store-vlc") {
		t.Fatal("the stored switch must read back off")
	}
	rec = post(`{"notify_allow_app_BAD ID": false}`)
	if !strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("an invalid app id must be rejected: %s", rec.Body.String())
	}
	setState.Lock()
	setState.cur, setState.loaded = oldCur, oldLoaded
	setState.Unlock()
}
