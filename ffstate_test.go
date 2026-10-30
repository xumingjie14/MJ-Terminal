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
