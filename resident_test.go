package main

// resident_test.go -- v0.71 block 2: the 锁住后台 rulebook.
// residentDecide / residentBackoff are pure on purpose: the
// patrol around them cannot run headless, so every rule the
// engine follows is pinned here instead.

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResidentBackoff(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{0, 15 * time.Second},
		{1, 15 * time.Second},
		{2, 30 * time.Second},
		{3, 60 * time.Second},
		{4, 2 * time.Minute},
		{5, 4 * time.Minute},
		{6, 5 * time.Minute},  // capped
		{20, 5 * time.Minute}, // stays capped
	}
	for _, c := range cases {
		if got := residentBackoff(c.attempts); got != c.want {
			t.Errorf("residentBackoff(%d) = %v, want %v", c.attempts, got, c.want)
		}
	}
}

func TestResidentDecide(t *testing.T) {
	boot := time.Unix(1_700_000_000, 0)
	afterBoot := boot.Add(10 * time.Minute)
	cases := []struct {
		name       string
		st         residentAppState
		present    bool
		missingFor time.Duration
		now        time.Time
		want       residentAction
	}{
		{"present plain", residentAppState{}, true, 0, afterBoot, residentNone},
		{"present hide owed", residentAppState{HidePending: true}, true, 0, afterBoot, residentHide},
		{"missing within boot grace", residentAppState{}, false, time.Minute, boot.Add(10 * time.Second), residentNone},
		{"missing unconfirmed", residentAppState{}, false, residentConfirm - time.Second, afterBoot, residentNone},
		{"missing confirmed", residentAppState{}, false, residentConfirm, afterBoot, residentRelaunch},
		{"missing long", residentAppState{}, false, time.Hour, afterBoot, residentRelaunch},
		{"missing but backing off", residentAppState{NextTry: afterBoot.Add(time.Minute)}, false, time.Hour, afterBoot, residentNone},
		{"missing backoff elapsed", residentAppState{NextTry: afterBoot.Add(-time.Second)}, false, time.Hour, afterBoot, residentRelaunch},
		{"present beats backoff", residentAppState{NextTry: afterBoot.Add(time.Hour)}, true, 0, afterBoot, residentNone},
	}
	for _, c := range cases {
		if got := residentDecide(c.st, c.present, c.missingFor, c.now, boot); got != c.want {
			t.Errorf("%s: residentDecide = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSameWebAppURL(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://x.com", "https://x.com", true},
		{"https://x.com/", "https://x.com", true},
		{"https://x.com", "https://x.com/", true},
		{"https://x.com/app", "https://x.com/app/", true},
		{"https://x.com/app", "https://x.com/other", false},
		{"https://x.com", "https://y.com", false},
		{"", "https://x.com", false},
	}
	for _, c := range cases {
		if got := sameWebAppURL(c.a, c.b); got != c.want {
			t.Errorf("sameWebAppURL(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestWinURLLocked(t *testing.T) {
	set := map[string]bool{"https://a.com/": true, "https://b.com": true}
	if !winURLLocked(set, "https://a.com") {
		t.Error("trailing-slash key should match bare URL")
	}
	if !winURLLocked(set, "https://b.com/") {
		t.Error("bare key should match slashed URL")
	}
	if winURLLocked(set, "https://c.com") {
		t.Error("unknown URL must not match")
	}
	if winURLLocked(set, "") {
		t.Error("empty URL must not match")
	}
	if winURLLocked(nil, "https://a.com") {
		t.Error("empty set must not match")
	}
}

func TestPruneWebAppLocks(t *testing.T) {
	in := map[string]bool{
		"https://ok.com":  true,
		"http://ok2.com":  true,
		"not a url":       true,
		"ftp://no.com":    true,
		"https://off.com": false,
	}
	out := pruneWebAppLocks(in)
	if len(out) != 2 || !out["https://ok.com"] || !out["http://ok2.com"] {
		t.Fatalf("prune = %v, want only the two valid true entries", out)
	}
	if pruneWebAppLocks(nil) != nil {
		t.Error("nil in, nil out")
	}
	if pruneWebAppLocks(map[string]bool{"ftp://x.com": true}) != nil {
		t.Error("all-invalid prunes to nil")
	}
}

func TestFindResidentWindow(t *testing.T) {
	wins := []*extWindow{
		{HWND: 1, URL: "https://shell.example", Shell: true},
		{HWND: 2, URL: "https://a.com/"},
		{HWND: 3, URL: ""},
	}
	locks := map[string]bool{"https://a.com": true}
	names := map[string]string{"a.com": "https://a.com"}
	if w := findResidentWindow(wins, locks, names, "https://a.com"); w == nil || w.HWND != 2 {
		t.Fatalf("match = %v, want hwnd 2", w)
	}
	if w := findResidentWindow(wins, locks, names, "https://shell.example"); w != nil {
		t.Fatal("shell windows are never resident targets")
	}
	if w := findResidentWindow(wins, locks, names, "https://nope.com"); w != nil {
		t.Fatal("unknown URL must not match")
	}
}

// v0.71 review fix （发现 1): a window the patrol claimed
// after a daemon restart carries no URL -- the launch rung
// is gone. It must still stand for its locked app through
// the name rung, or the engine relaunches a hidden duplicate
// beside it on every update.
func TestFindResidentWindowClaimedByName(t *testing.T) {
	boot := time.Unix(1_700_000_000, 0)
	afterBoot := boot.Add(10 * time.Minute)
	locks := map[string]bool{"https://a.com": true}
	names := map[string]string{"a.com": "https://a.com"}
	claimed := &extWindow{HWND: 7, App: "a.com", URL: "", browser: true, origin: "profile"}

	w := findResidentWindow([]*extWindow{claimed}, locks, names, "https://a.com")
	if w == nil || w.HWND != 7 {
		t.Fatalf("claimed window must stand for its app, got %v", w)
	}
	// The composed verdict the review pinned: present, so the
	// state machine must not judge the app missing / relaunch.
	if got := residentDecide(residentAppState{}, w != nil, 0, afterBoot, boot); got == residentRelaunch {
		t.Fatal("claimed window present: decide must not relaunch")
	}

	// A launch-registered window carrying the URL wins over a
	// name-aligned claimed window.
	launched := &extWindow{HWND: 9, App: "a.com", URL: "https://a.com", browser: true, origin: "launch"}
	if w := findResidentWindow([]*extWindow{claimed, launched}, locks, names, "https://a.com"); w == nil || w.HWND != 9 {
		t.Fatalf("URL rung must win, got %v", w)
	}

	// A different title aligns nothing.
	other := &extWindow{HWND: 8, App: "other.example", URL: "", browser: true, origin: "profile"}
	if w := findResidentWindow([]*extWindow{other}, locks, names, "https://a.com"); w != nil {
		t.Fatal("unrelated claimed window must not stand for the app")
	}

	// A non-browser claimed window (a store app) never aligns,
	// even with a matching label.
	store := &extWindow{HWND: 10, App: "a.com", URL: "", browser: false, origin: "profile"}
	if w := findResidentWindow([]*extWindow{store}, locks, names, "https://a.com"); w != nil {
		t.Fatal("non-browser window must not align by name")
	}

	// The generic fallback label never aligns.
	generic := &extWindow{HWND: 11, App: "浏览器", URL: "", browser: true, origin: "profile"}
	locks2 := map[string]bool{"https://b.com": true}
	names2 := map[string]string{"浏览器": "https://b.com"}
	if w := findResidentWindow([]*extWindow{generic}, locks2, names2, "https://b.com"); w != nil {
		t.Fatal("generic 浏览器 label must not align")
	}
}

// v0.71 review fix （发现 1): two locked apps sharing one
// display name cannot be told apart by the name rung -- it
// aligns nothing for them rather than guessing.
func TestResidentNameIndexAmbiguous(t *testing.T) {
	locks := map[string]bool{"https://a.com": true, "https://a.com/app": true}
	names := residentNameIndex(locks)
	if names["a.com"] != "" {
		t.Fatalf("shared name must map to no key, got %q", names["a.com"])
	}
	claimed := &extWindow{HWND: 7, App: "a.com", URL: "", browser: true, origin: "profile"}
	if w := findResidentWindow([]*extWindow{claimed}, locks, names, "https://a.com"); w != nil {
		t.Fatal("ambiguous name must not align")
	}
	if w := findResidentWindow([]*extWindow{claimed}, locks, names, "https://a.com/app"); w != nil {
		t.Fatal("ambiguous name must not align")
	}
}

func TestResidentWindowLockURL(t *testing.T) {
	locks := map[string]bool{"https://a.com": true}
	names := map[string]string{"a.com": "https://a.com"}
	if got := residentWindowLockURL(&extWindow{URL: "https://a.com/"}, locks, names); got != "https://a.com" {
		t.Errorf("URL rung = %q, want the lock key", got)
	}
	if got := residentWindowLockURL(&extWindow{URL: "https://nope.com"}, locks, names); got != "" {
		t.Errorf("unlocked URL = %q, want \"\"", got)
	}
	if got := residentWindowLockURL(&extWindow{Shell: true, URL: "https://a.com"}, locks, names); got != "" {
		t.Errorf("shell = %q, want \"\"", got)
	}
	if got := residentWindowLockURL(nil, locks, names); got != "" {
		t.Errorf("nil = %q, want \"\"", got)
	}
}

// v0.71 review fix （发现 3): a failed relaunch must disarm
// HidePending -- otherwise the hide stays armed and swallows
// a window the user later opens by hand.
func TestResidentLaunchFailureDisarmsHide(t *testing.T) {
	boot := time.Unix(1_700_000_000, 0)
	afterBoot := boot.Add(10 * time.Minute)
	st := residentAppState{Attempts: 1, HidePending: true}
	residentNoteLaunchResult(&st, false)
	if st.HidePending {
		t.Fatal("failed relaunch must disarm HidePending")
	}
	if got := residentDecide(st, true, 0, afterBoot, boot); got != residentNone {
		t.Fatalf("hand-opened window after a failed relaunch: decide = %v, want residentNone (never hidden)", got)
	}
	// A successful relaunch keeps the hide armed: its window
	// is on its way and is hidden when it registers.
	st2 := residentAppState{Attempts: 1, HidePending: true}
	residentNoteLaunchResult(&st2, true)
	if !st2.HidePending {
		t.Fatal("successful relaunch keeps HidePending armed")
	}
	if got := residentDecide(st2, true, 0, afterBoot, boot); got != residentHide {
		t.Fatalf("relaunched window registering: decide = %v, want residentHide", got)
	}
}

// ---------- v0.71 test3, layer 1 ----------

func TestDecideResidentCardState(t *testing.T) {
	cases := []struct {
		name  string
		facts residentCardFacts
		want  residentCardState
	}{
		{"有窗且可见→台前", residentCardFacts{HasWindow: true, WindowVisible: true}, residentCardFront},
		{"有窗不可见→后台", residentCardFacts{HasWindow: true, WindowVisible: false}, residentCardBack},
		{"有窗不可见但多次失败→仍后台（窗在就是窗在）", residentCardFacts{HasWindow: true, WindowVisible: false, Attempts: 9}, residentCardBack},
		{"无窗→恢复中", residentCardFacts{}, residentCardRestoring},
		{"无窗失败 3 次→仍恢复中", residentCardFacts{Attempts: 3}, residentCardRestoring},
		{"无窗失败 4 次→重拉未稳（不许假装恢复中）", residentCardFacts{Attempts: 4}, residentCardUnstable},
		{"无窗失败 9 次→重拉未稳", residentCardFacts{Attempts: 9}, residentCardUnstable},
	}
	for _, c := range cases {
		if got := decideResidentCardState(c.facts); got != c.want {
			t.Errorf("%s: decideResidentCardState(%+v) = %q, want %q", c.name, c.facts, got, c.want)
		}
	}
}

// L1-I2: the hidden-window book survives only while the
// aligned window is the window it names.
func TestResidentRemapHiddenBook(t *testing.T) {
	if got := residentRemapHiddenBook(55, 55); got != 55 {
		t.Errorf("same window: got %d, want 55 (book stands)", got)
	}
	if got := residentRemapHiddenBook(55, 77); got != 0 {
		t.Errorf("alignment moved to a sibling: got %d, want 0 (old hide debt is void)", got)
	}
	if got := residentRemapHiddenBook(0, 77); got != 0 {
		t.Errorf("no book: got %d, want 0", got)
	}
}

// HS-4: the restore route exists on the local mux only -- the
// remote-assist surface must never gain "raise a browser
// window on this machine" (mirrors TestThumbRouteRegisteredLocalOnly).
func TestRestoreRouteRegisteredLocalOnly(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `mux.HandleFunc("/api/webapps/restore", webAppRestoreHandler)`) {
		t.Fatal("/api/webapps/restore is not registered on the local mux")
	}
	rb, err := os.ReadFile("remote.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rb), "webapps/restore") {
		t.Fatal("/api/webapps/restore leaked into the remote-assist mux")
	}
}

// The restore handler refuses an app that is not locked: a
// windowless card can only exist for a lock, so anything else
// is a caller bug and must not launch browsers.
func TestRestoreHandlerRejectsUnlocked(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webapps/restore", strings.NewReader(`{"url":"https://example.com/"}`))
	webAppRestoreHandler(rec, req)
	if rec.Code != 400 {
		t.Fatalf("unlocked url: got status %d, want 400", rec.Code)
	}
}
