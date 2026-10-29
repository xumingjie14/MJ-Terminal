package main

import "testing"

// v0.50: the summonHotkey whitelist — only F8 / F9 / F7 / off may
// survive sanitizing; anything else falls back to the F8 default,
// and the settings map must expose the key for the frontend.
func TestSummonHotkeyWhitelist(t *testing.T) {
	for _, ok := range []string{"F8", "F9", "F7", "off"} {
		s := sanitizeSettings(mjSettings{SummonHotkey: ok})
		if s.SummonHotkey != ok {
			t.Errorf("summonHotkey %q should survive sanitize, got %q", ok, s.SummonHotkey)
		}
	}
	for _, bad := range []string{"", "F5", "f8", "Ctrl+Space", "F10", "F8 "} {
		s := sanitizeSettings(mjSettings{SummonHotkey: bad})
		if s.SummonHotkey != "F8" {
			t.Errorf("summonHotkey %q should fall back to F8, got %q", bad, s.SummonHotkey)
		}
	}
	if got := defaultSettings().SummonHotkey; got != "F8" {
		t.Errorf("default summonHotkey = %q, want F8", got)
	}
	if _, ok := hotkeyVK["F8"]; !ok {
		t.Error("hotkeyVK must map F8")
	}
	if _, ok := hotkeyVK["F5"]; ok {
		t.Error("hotkeyVK must NOT map F5 (F5 is the in-view Spotlight key, not global)")
	}
}
