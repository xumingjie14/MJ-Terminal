package main

// resident_test.go -- v0.71 block 2: the 锁住后台 rulebook.
// residentDecide / residentBackoff are pure on purpose: the
// patrol around them cannot run headless, so every rule the
// engine follows is pinned here instead.

import (
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
	if w := findResidentWindow(wins, "https://a.com"); w == nil || w.HWND != 2 {
		t.Fatalf("match = %v, want hwnd 2", w)
	}
	if w := findResidentWindow(wins, "https://shell.example"); w != nil {
		t.Fatal("shell windows are never resident targets")
	}
	if w := findResidentWindow(wins, "https://nope.com"); w != nil {
		t.Fatal("unknown URL must not match")
	}
}
