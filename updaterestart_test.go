package main

// updaterestart_test.go -- v0.71 block 5: the restart hint's
// predicate, pinned as a truth table. The choreography
// around it (WM_CLOSE, lockfile wait, relaunch) is Windows
// work and is field-verified; the DECISION of when the hint
// may appear is pure and pinned here.

import (
	"strings"
	"testing"
)

func TestRestartHintShow(t *testing.T) {
	cases := []struct {
		name    string
		changed bool
		nagged  string
		current string
		running bool
		want    bool
	}{
		{"changed, running, not dismissed", true, "0.70", "0.71", true, true},
		{"changed, running, fresh marks", true, "", "0.71", true, true},
		{"not changed this run", false, "", "0.71", true, false},
		{"already dismissed for this version", true, "0.71", "0.71", true, false},
		{"browser not running", true, "", "0.71", false, false},
		{"nothing at all", false, "0.71", "0.71", false, false},
	}
	for _, c := range cases {
		if got := restartHintShow(c.changed, c.nagged, c.current, c.running); got != c.want {
			t.Errorf("%s: restartHintShow = %v, want %v", c.name, got, c.want)
		}
	}
}

// v0.71 review fix （发现 4): the restart's closing log line
// must tell the two endings apart. The handoff ending (the
// wait timed out, the browser never fully exited) must NOT
// claim the new settings are in effect -- they land only
// after a full exit and relaunch.
func TestRestartOutcome(t *testing.T) {
	level, msg := restartOutcome(false)
	if level != "info" || !strings.Contains(msg, "即刻生效") {
		t.Errorf("clean restart outcome = (%q, %q), want info + 即刻生效", level, msg)
	}
	level, msg = restartOutcome(true)
	if level != "warn" {
		t.Errorf("handoff outcome level = %q, want warn", level)
	}
	if !strings.Contains(msg, "完全退出") || !strings.Contains(msg, "后生效") {
		t.Errorf("handoff outcome must defer the settings to a full exit, got %q", msg)
	}
	if strings.Contains(msg, "即刻生效") {
		t.Errorf("handoff outcome must not claim 即刻生效, got %q", msg)
	}
}

// v0.71 review suggestion （建议 5): the close log names each
// window (app + title) so a wrongly-closed window can be
// identified from the console.
func TestRestartWinLabel(t *testing.T) {
	cases := []struct {
		win  extWindow
		want string
	}{
		{extWindow{HWND: 5, App: "Telegram", Title: "Telegram Web"}, "Telegram · Telegram Web"},
		{extWindow{HWND: 5, App: "Telegram", Title: "Telegram"}, "Telegram"},
		{extWindow{HWND: 5, App: "", Title: "某页面"}, "某页面"},
		{extWindow{HWND: 5, App: "浏览器", Title: ""}, "浏览器"},
		{extWindow{HWND: 42, App: "", Title: ""}, "窗口 42"},
	}
	for _, c := range cases {
		if got := restartWinLabel(&c.win); got != c.want {
			t.Errorf("restartWinLabel(%+v) = %q, want %q", c.win, got, c.want)
		}
	}
}
