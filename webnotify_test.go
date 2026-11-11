package main

// webnotify_test.go -- v0.68 (detail-fix ⑧): the web
// notification bridge's daemon side. The extension itself runs
// in a real browser (field-verified); what is pinned here is
// everything the daemon owns: site identity derivation, the
// settings gate, the recorded event's shape (app category of
// the sending site, click-back route), the embedded release,
// and the click queue.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebNotifyHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://web.telegram.org/k/", "web.telegram.org"},
		{"https://EXAMPLE.com:8443/x?y=1", "example.com:8443"},
		{"http://localhost:3000/a", "localhost:3000"},
		{"https://sub.a.b.co.uk/", "sub.a.b.co.uk"},
		{"chrome://extensions", ""},
		{"file:///tmp/x.html", ""},
		{"about:blank", ""},
		{"", ""},
		{"not a url", ""},
	} {
		if got := webNotifyHost(tc.in); got != tc.want {
			t.Errorf("webNotifyHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWebNotifyAppID(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"web.telegram.org", "web-web-telegram-org"},
		{"example.com:8443", "web-example-com-8443"},
		{"localhost:3000", "web-localhost-3000"},
	} {
		got := webNotifyAppID(tc.in)
		if got != tc.want {
			t.Errorf("webNotifyAppID(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if !validNotifyAppID(got) {
			t.Errorf("webNotifyAppID(%q) = %q must pass the settings id whitelist", tc.in, got)
		}
	}
}

func TestWebNotifySettingsGate(t *testing.T) {
	d := defaultSettings()
	if !d.NotifyAllowWeb || !d.NotifyBannerWeb {
		t.Fatal("web bridge switches must default ON like every other source")
	}
	for _, k := range []string{"notify_allow_web", "notify_banner_web"} {
		p := boolSettingPtr(&d, k)
		if p == nil || !*p {
			t.Fatalf("boolSettingPtr(%q) must resolve to a true default", k)
		}
	}
	if !notifyAllowed(d, notifySrcWeb) || !notifyBanner(d, notifySrcWeb) {
		t.Fatal("notifyAllowed/notifyBanner must see the web source as on by default")
	}
	d.NotifyAllowWeb = false
	if notifyAllowed(d, notifySrcWeb) {
		t.Fatal("notifyAllowed(web) must follow notify_allow_web")
	}
}

// swapNotifyGlobals points the handler's store and the live
// settings at isolated test state, restoring both afterwards.
func swapNotifyGlobals(t *testing.T) {
	t.Helper()
	oldStore := notifySt
	notifySt = notifyStoreForTests(filepath.Join(t.TempDir(), "notifications.json"))
	setState.Lock()
	oldCur, oldLoaded := setState.cur, setState.loaded
	setState.cur = defaultSettings()
	setState.loaded = true
	setState.Unlock()
	t.Cleanup(func() {
		notifySt = oldStore
		setState.Lock()
		setState.cur, setState.loaded = oldCur, oldLoaded
		setState.Unlock()
	})
}

func postWebNotify(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/webnotify", strings.NewReader(body))
	rec := httptest.NewRecorder()
	webNotifyHandler(rec, req)
	return rec
}

func TestWebNotifyHandlerRecordsAppEvent(t *testing.T) {
	swapNotifyGlobals(t)
	rec := postWebNotify(t, `{"title":"新消息","body":"你好","url":"https://web.telegram.org/k/"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.OK || resp.ID == "" {
		t.Fatalf("response = %s, want ok with an event id", rec.Body.String())
	}
	evs := notifySt.list()
	if len(evs) != 1 {
		t.Fatalf("recorded %d events, want 1", len(evs))
	}
	ev := evs[0]
	if ev.ID != resp.ID || ev.Category != "app" || ev.Source != notifySrcApp {
		t.Fatalf("event identity wrong: %+v", ev)
	}
	if ev.AppID != "web-web-telegram-org" || ev.AppName != "web.telegram.org" {
		t.Fatalf("event must carry the sending site: %+v", ev)
	}
	if ev.Route != "webnotify:web.telegram.org:"+ev.ID {
		t.Fatalf("route = %q, want webnotify:<host>:<id>", ev.Route)
	}
	if ev.Title != "新消息" || ev.Body != "你好" {
		t.Fatalf("event content wrong: %+v", ev)
	}
	// No dedupe: a second, identical notification is a second
	// event (a chat's next message is not the first one).
	postWebNotify(t, `{"title":"新消息","body":"你好","url":"https://web.telegram.org/k/"}`)
	if n := len(notifySt.list()); n != 2 {
		t.Fatalf("after identical repost: %d events, want 2 (no dedupe)", n)
	}
}

func TestWebNotifyHandlerGateOff(t *testing.T) {
	swapNotifyGlobals(t)
	setState.Lock()
	setState.cur.NotifyAllowWeb = false
	setState.Unlock()
	rec := postWebNotify(t, `{"title":"x","url":"https://example.com/"}`)
	var resp struct {
		OK       bool `json:"ok"`
		Disabled bool `json:"disabled"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK || !resp.Disabled {
		t.Fatalf("gate off must answer disabled, got %s", rec.Body.String())
	}
	if n := len(notifySt.list()); n != 0 {
		t.Fatalf("gate off recorded %d events, want 0", n)
	}
}

func TestWebNotifyHandlerRejectsBadSource(t *testing.T) {
	swapNotifyGlobals(t)
	for _, body := range []string{
		`{"title":"x","url":"chrome://extensions"}`,
		`{"title":"x","url":""}`,
		`{"title":"x"}`,
		`not json`,
	} {
		rec := postWebNotify(t, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
	if n := len(notifySt.list()); n != 0 {
		t.Fatalf("bad posts recorded %d events, want 0", n)
	}
}

func TestWebNotifyPingReportsSwitch(t *testing.T) {
	swapNotifyGlobals(t)
	ping := func() map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/api/webnotify/ping", strings.NewReader(`{"version":"1.0.0"}`))
		rec := httptest.NewRecorder()
		webNotifyPingHandler(rec, req)
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}
	if m := ping(); m["enabled"] != true || m["version"] != AppVersion {
		t.Fatalf("ping = %v, want enabled=true and this daemon's version", m)
	}
	setState.Lock()
	setState.cur.NotifyAllowWeb = false
	setState.Unlock()
	if m := ping(); m["enabled"] != false {
		t.Fatalf("ping after switch off = %v, want enabled=false", m)
	}
	webNotifyBridge.Lock()
	webNotifyBridge.lastPing = time.Time{}
	webNotifyBridge.Unlock()
}

func TestWebNotifyClickQueue(t *testing.T) {
	_ = webNotifyDrainClicks() // start clean
	req := httptest.NewRequest(http.MethodPost, "/api/webnotify/click", strings.NewReader(`{"id":"n42"}`))
	rec := httptest.NewRecorder()
	webNotifyClickHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("click status = %d, want 200", rec.Code)
	}
	got := webNotifyDrainClicks()
	if len(got) != 1 || got[0].ID != "n42" {
		t.Fatalf("drained %+v, want the one click", got)
	}
	if again := webNotifyDrainClicks(); len(again) != 0 {
		t.Fatalf("queue must stay drained, got %+v", again)
	}
}

func TestWebNotifyRelease(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "webnotify")
	if got := releaseWebNotifyExtTo(dir, "http://127.0.0.1:18799"); got != dir {
		t.Fatalf("release returned %q, want %q", got, dir)
	}
	for _, name := range append(append([]string{}, webNotifyExtFiles...), "config.json") {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("released file %s missing: %v", name, err)
		}
	}
	cfg, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !strings.Contains(string(cfg), "127.0.0.1:18799") {
		t.Fatalf("config.json = %s (%v), want the stamped daemon base", cfg, err)
	}
	// The released manifest must stay a valid MV3 bridge: the
	// daemon's base URL belongs in config.json only, never in
	// the shipped sources (the port is per-instance).
	man, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var manifest struct {
		Version int `json:"manifest_version"`
		BG      struct {
			SW string `json:"service_worker"`
		} `json:"background"`
	}
	if err := json.Unmarshal(man, &manifest); err != nil || manifest.Version != 3 || manifest.BG.SW != "sw.js" {
		t.Fatalf("released manifest.json lost its MV3/service-worker shape: %s", man)
	}
	if got := releaseWebNotifyExtTo("", "http://127.0.0.1:1"); got != "" {
		t.Fatalf("empty dir must fail the release, got %q", got)
	}
	if got := releaseWebNotifyExtTo(dir, ""); got != "" {
		t.Fatalf("empty base must fail the release, got %q", got)
	}
}
