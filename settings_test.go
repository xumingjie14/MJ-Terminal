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

// v0.56 keys: stageDisplay / deskIconSize / deskSort defaults
// and sanitize fallbacks (a settings.json written by an older
// or newer build, or by hand, must land on sane values).
func TestV056SettingDefaults(t *testing.T) {
	d := defaultSettings()
	if d.StageDisplay != "preview" || d.DeskIconSize != 54 || d.DeskSort != "manual" {
		t.Errorf("v0.56 defaults = %q/%d/%q, want preview/54/manual", d.StageDisplay, d.DeskIconSize, d.DeskSort)
	}
	s := sanitizeSettings(mjSettings{StageDisplay: "identity", DeskIconSize: 96, DeskSort: "freq", SummonHotkey: "F8"})
	if s.StageDisplay != "identity" || s.DeskIconSize != 96 || s.DeskSort != "freq" {
		t.Errorf("valid v0.56 values should survive sanitize, got %q/%d/%q", s.StageDisplay, s.DeskIconSize, s.DeskSort)
	}
	s = sanitizeSettings(mjSettings{StageDisplay: "bogus", DeskIconSize: 400, DeskSort: "size", SummonHotkey: "F8"})
	if s.StageDisplay != "preview" || s.DeskIconSize != 54 || s.DeskSort != "manual" {
		t.Errorf("invalid v0.56 values should fall back, got %q/%d/%q", s.StageDisplay, s.DeskIconSize, s.DeskSort)
	}
	m := settingsMap()
	if m["stageDisplay"] != "preview" || m["deskSort"] != "manual" {
		t.Errorf("settingsMap should carry the v0.56 keys, got %v / %v", m["stageDisplay"], m["deskSort"])
	}
}
