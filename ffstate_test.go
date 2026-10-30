package main

import "testing"

// v0.56: the Firefox launch rule. A process's birth mode is
// destiny: matching requests join, everything else conflicts
// (including a running process whose birth was never recorded,
// e.g. one started by a pre-v0.56 build).
func TestDecideFFLaunch(t *testing.T) {
	cases := []struct {
		name                         string
		alive, known, birth, wantApp bool
		want                         ffLaunchDecision
	}{
		{"no process, normal", false, false, false, false, ffFresh},
		{"no process, app", false, false, false, true, ffFresh},
		{"alive normal + want normal", true, true, false, false, ffJoin},
		{"alive app + want app", true, true, true, true, ffJoin},
		{"alive normal + want app", true, true, false, true, ffConflict},
		{"alive app + want normal", true, true, true, false, ffConflict},
		{"alive unknown + want normal", true, false, false, false, ffConflict},
		{"alive unknown + want app", true, false, false, true, ffConflict},
	}
	for _, c := range cases {
		if got := decideFFLaunch(c.alive, c.known, c.birth, c.wantApp); got != c.want {
			t.Errorf("%s: decideFFLaunch = %v, want %v", c.name, got, c.want)
		}
	}
}

// v0.56 test2: the restart close set is enumerated from ALL
// top-level windows matched by the component exe's full path --
// test1 closed only the Stage Manager's claimed windows and a
// live unclaimed window kept the profile locked forever. The
// host's own Firefox (a different install path) must never be
// in either set: path equality is the whole safety story.
func TestFFCloseSetAndKillRoots(t *testing.T) {
	exe := `C:\MJ\Data\components\firefox\firefox.exe`
	procs := []procFact{
		{PID: 100, ImagePath: exe},
		{PID: 101, ImagePath: exe},
		{PID: 200, ImagePath: `C:\Program Files\Mozilla Firefox\firefox.exe`}, // host Firefox
		{PID: 300, ImagePath: `C:\Windows\explorer.exe`},
		{PID: 400, ImagePath: ""}, // unqueryable: never matched
	}
	wins := []windowFact{
		{HWND: 11, PID: 100},
		{HWND: 12, PID: 101},
		{HWND: 13, PID: 200}, // host Firefox window: hands off
		{HWND: 14, PID: 300},
		{HWND: 15, PID: 400},
		{HWND: 11, PID: 100}, // duplicate enumeration entry
		{HWND: 16, PID: 999}, // pid not in the snapshot
	}
	got := ffCloseSet(wins, procs, exe)
	if len(got) != 2 || got[0] != 11 || got[1] != 12 {
		t.Fatalf("ffCloseSet = %v, want [11 12]", got)
	}
	// Case and separator differences in the exe path must not
	// matter (the snapshot's spelling vs the install dir's).
	got2 := ffCloseSet(wins, procs, `c:/mj/data/components/firefox/firefox.exe`)
	if len(got2) != 2 {
		t.Fatalf("ffCloseSet with differently-spelled exe = %v, want 2 windows", got2)
	}
	roots := ffKillRoots(procs, exe)
	if len(roots) != 2 || !roots[100] || !roots[101] {
		t.Fatalf("ffKillRoots = %v, want {100,101}", roots)
	}
	if roots[200] {
		t.Fatal("host Firefox (different path) became a kill root")
	}
	if got := ffCloseSet(wins, procs, ""); got != nil {
		t.Fatalf("empty exe path: ffCloseSet = %v, want nil", got)
	}
	if got := ffKillRoots(procs, ""); len(got) != 0 {
		t.Fatalf("empty exe path: ffKillRoots = %v, want empty", got)
	}
}
