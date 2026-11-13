package main

// updaterestart_test.go -- v0.71 block 5: the restart hint's
// predicate, pinned as a truth table. The choreography
// around it (WM_CLOSE, lockfile wait, relaunch) is Windows
// work and is field-verified; the DECISION of when the hint
// may appear is pure and pinned here.

import "testing"

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
