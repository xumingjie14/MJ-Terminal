package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

// v0.59: updateBeta rides the settings whitelist like every
// other key: a bool POST persists (and shows in settingsMap),
// a non-bool is rejected, and the default is OFF.
func TestUpdateBetaSettingWhitelist(t *testing.T) {
	dsTestRoot(t)
	setState.Lock()
	oldCur, oldLoaded, oldFresh := setState.cur, setState.loaded, setState.fresh
	setState.cur, setState.loaded = defaultSettings(), true
	setState.Unlock()
	defer func() {
		setState.Lock()
		setState.cur, setState.loaded, setState.fresh = oldCur, oldLoaded, oldFresh
		setState.Unlock()
	}()
	if settingsGet().UpdateBeta {
		t.Fatal("updateBeta default must be OFF")
	}
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		rec := httptest.NewRecorder()
		settingsHandler(rec, req)
		return rec
	}
	rec := post(`{"updateBeta": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST updateBeta=true: status %d, body %s", rec.Code, rec.Body.String())
	}
	if !settingsGet().UpdateBeta {
		t.Fatal("updateBeta=true did not persist in the settings store")
	}
	if got := settingsMap()["updateBeta"]; got != true {
		t.Fatalf("settingsMap updateBeta = %v, want true", got)
	}
	rec = post(`{"updateBeta": "yes"}`)
	if !strings.Contains(rec.Body.String(), `"code"`) || settingsGet().UpdateBeta != true {
		t.Fatalf("non-bool updateBeta must be rejected without changing the value: %s", rec.Body.String())
	}
	rec = post(`{"updateBeta": false}`)
	if rec.Code != http.StatusOK || settingsGet().UpdateBeta {
		t.Fatalf("updateBeta=false must persist: %d %s", rec.Code, rec.Body.String())
	}
}
