package main

import "testing"

// v0.52: summonHotkey migration. The v0.50 values (F8 / F9 / F7 /
// off) survive sanitizing unchanged; any valid combo survives in
// canonical form ("ctrl+f8" -> "Ctrl+F8"); anything the combo
// parser rejects falls back to the F8 default. (v0.50 pinned
// F5 / F10 to the fallback because the whitelist had only three
// keys -- the v0.52 rulebook allows any single F1-F12, so those
// pins are deliberately gone.)
func TestSummonHotkeyWhitelist(t *testing.T) {
	for _, ok := range []string{"F8", "F9", "F7", "off", "F5", "F12", "Ctrl+F8", "Alt+Q", "Ctrl+Shift+F9"} {
		s := sanitizeSettings(mjSettings{SummonHotkey: ok})
		if s.SummonHotkey != ok {
			t.Errorf("summonHotkey %q should survive sanitize, got %q", ok, s.SummonHotkey)
		}
	}
	for in, want := range map[string]string{
		"ctrl+f8":         "Ctrl+F8",
		"SHIFT+CTRL+f9":   "Ctrl+Shift+F9",
		" alt + q ":       "Alt+Q",
		"OFF":             "off",
		"control+shift+5": "Ctrl+Shift+5",
	} {
		s := sanitizeSettings(mjSettings{SummonHotkey: in})
		if s.SummonHotkey != want {
			t.Errorf("summonHotkey %q should canonicalize to %q, got %q", in, want, s.SummonHotkey)
		}
	}
	for _, bad := range []string{"", "A", "5", "Win+F8", "Ctrl+", "F13", "Ctrl+Alt", "Ctrl+Space", "F8+F9"} {
		s := sanitizeSettings(mjSettings{SummonHotkey: bad})
		if s.SummonHotkey != "F8" {
			t.Errorf("summonHotkey %q should fall back to F8, got %q", bad, s.SummonHotkey)
		}
	}
	if got := defaultSettings().SummonHotkey; got != "F8" {
		t.Errorf("default summonHotkey = %q, want F8", got)
	}
}
