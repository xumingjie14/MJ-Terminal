package main

// v0.52: the boss-key combo rulebook (hotkey.go's pure parser).
// These tables ARE the contract the Settings recorder and the
// settings POST rely on: what parses, what the RegisterHotKey
// parameters are, and which named reason each rejection carries.

import (
	"strings"
	"testing"
)

func TestParseHotkeyComboValid(t *testing.T) {
	cases := []struct {
		in        string
		wantMods  uint32
		wantVK    uint32
		canonical string
	}{
		// The v0.50 legacy values: modifier-less F-keys and off.
		{"F8", 0, 0x77, "F8"},
		{"F9", 0, 0x78, "F9"},
		{"F7", 0, 0x76, "F7"},
		{"off", 0, 0, "off"},
		{"OFF", 0, 0, "off"},
		// The full F1-F12 range is allowed bare.
		{"F1", 0, 0x70, "F1"},
		{"F12", 0, 0x7B, "F12"},
		{"f6", 0, 0x75, "F6"},
		// Modifier combos: letters, digits, F-keys.
		{"Ctrl+F8", hotkeyModControl, 0x77, "Ctrl+F8"},
		{"Alt+Q", hotkeyModAlt, 0x51, "Alt+Q"},
		{"Ctrl+Shift+F9", hotkeyModControl | hotkeyModShift, 0x78, "Ctrl+Shift+F9"},
		{"Ctrl+Alt+Shift+A", hotkeyModControl | hotkeyModAlt | hotkeyModShift, 0x41, "Ctrl+Alt+Shift+A"},
		{"Shift+3", hotkeyModShift, 0x33, "Shift+3"},
		// Messy input canonicalizes: case, order, spaces, the
		// long "control" spelling, duplicate modifiers.
		{"ctrl+f8", hotkeyModControl, 0x77, "Ctrl+F8"},
		{"shift+ctrl+f9", hotkeyModControl | hotkeyModShift, 0x78, "Ctrl+Shift+F9"},
		{" Alt + q ", hotkeyModAlt, 0x51, "Alt+Q"},
		{"Control+F10", hotkeyModControl, 0x79, "Ctrl+F10"},
		{"Ctrl+Ctrl+F8", hotkeyModControl, 0x77, "Ctrl+F8"},
	}
	for _, c := range cases {
		got, err := parseHotkeyCombo(c.in)
		if err != nil {
			t.Errorf("parseHotkeyCombo(%q) errored: %v", c.in, err)
			continue
		}
		if c.canonical == "off" {
			if !got.Off {
				t.Errorf("parseHotkeyCombo(%q) = %+v, want Off", c.in, got)
			}
		} else if got.Off || got.Mods != c.wantMods || got.VK != c.wantVK {
			t.Errorf("parseHotkeyCombo(%q) = %+v, want mods=%#x vk=%#x", c.in, got, c.wantMods, c.wantVK)
		}
		if got.String() != c.canonical {
			t.Errorf("parseHotkeyCombo(%q).String() = %q, want %q", c.in, got.String(), c.canonical)
		}
		// Canonical form is a fixed point: parsing it again
		// yields the identical combo.
		again, err := parseHotkeyCombo(got.String())
		if err != nil || again != got {
			t.Errorf("canonical %q does not round-trip: %+v, %v", got.String(), again, err)
		}
	}
}

func TestParseHotkeyComboRejected(t *testing.T) {
	cases := []struct {
		in         string
		wantReason string // substring of the named reason
	}{
		{"", "为空"},
		{"   ", "为空"},
		// Bare letters / digits would eat normal typing.
		{"A", "裸字母/数字"},
		{"q", "裸字母/数字"},
		{"5", "裸字母/数字"},
		// Win-key combos: OS-reserved, unrecordable.
		{"Win+F8", "Win"},
		{"Meta+Q", "Win"},
		{"Ctrl+Win+A", "Win"},
		// Modifiers alone are not a key.
		{"Ctrl", "不够"},
		{"Ctrl+Alt", "不够"},
		{"Shift", "不够"},
		// Structural problems.
		{"Ctrl+", "空的键位"},
		{"F8+F9", "多个主键"},
		{"Ctrl+A+B", "多个主键"},
		// Out-of-range / unknown keys.
		{"F13", "F1–F12"},
		{"F0", "F1–F12"},
		{"Ctrl+Space", "不支持的按键"},
		{"Ctrl+Enter", "不支持的按键"},
	}
	for _, c := range cases {
		got, err := parseHotkeyCombo(c.in)
		if err == nil {
			t.Errorf("parseHotkeyCombo(%q) = %+v, want rejection mentioning %q", c.in, got, c.wantReason)
			continue
		}
		if !strings.Contains(err.Error(), c.wantReason) {
			t.Errorf("parseHotkeyCombo(%q) error = %q, want it to mention %q", c.in, err.Error(), c.wantReason)
		}
		if _, err2 := canonicalHotkey(c.in); err2 == nil {
			t.Errorf("canonicalHotkey(%q) should fail like the parser", c.in)
		}
	}
}

// The registration parameters are exactly the parsed pair:
// RegisterHotKey(hwnd, id, MOD_NOREPEAT | combo.Mods, combo.VK)
// -- pinned here so a parser change can never silently alter
// what the OS is asked for.
func TestHotkeyComboRegistrationParams(t *testing.T) {
	c, err := parseHotkeyCombo("Ctrl+Shift+F9")
	if err != nil {
		t.Fatal(err)
	}
	if c.Mods != 0x0002|0x0004 { // MOD_CONTROL | MOD_SHIFT
		t.Errorf("mods = %#x, want MOD_CONTROL|MOD_SHIFT", c.Mods)
	}
	if c.VK != 0x78 { // VK_F9
		t.Errorf("vk = %#x, want VK_F9 (0x78)", c.VK)
	}
	alt, err := parseHotkeyCombo("Alt+Q")
	if err != nil {
		t.Fatal(err)
	}
	if alt.Mods != 0x0001 || alt.VK != 0x51 { // MOD_ALT, VK_Q
		t.Errorf("Alt+Q = mods %#x vk %#x, want MOD_ALT / VK_Q", alt.Mods, alt.VK)
	}
}
