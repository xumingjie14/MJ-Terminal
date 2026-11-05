package main

// v0.52: the boss-key combo rulebook (hotkey.go's pure parser).
// These tables ARE the contract the Settings recorder and the
// settings POST rely on: what parses, what the RegisterHotKey
// parameters are, and which named reason each rejection carries.

import (
	"strings"
	"testing"
	"time"
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

// ---------- v0.62 test2: the registration retry contract ----------

// The pure policy: only a conflict is transient, and only
// inside the budget. This table IS the fix for the 2026-11-05
// handover race -- attempt 1 conflict must retry, the busy
// warning belongs to the LAST attempt only.
func TestDecideHKRetry(t *testing.T) {
	cases := []struct {
		name    string
		attempt int
		outcome hkAttemptOutcome
		want    hkRetryAction
	}{
		{"第一次就成功→落定", 1, hkOutcomeOK, hkActSettled},
		{"重试后成功→落定", 7, hkOutcomeOK, hkActSettled},
		{"硬错误→立即落定（报错）", 1, hkOutcomeError, hkActSettled},
		{"冲突后硬错误→立即落定", 3, hkOutcomeError, hkActSettled},
		{"第一次被占用→重试（交接竞态的核心场景）", 1, hkOutcomeConflict, hkActRetry},
		{"预算内被占用→继续重试", hkRetryMaxAttempts - 1, hkOutcomeConflict, hkActRetry},
		{"最后一次仍被占用→预算耗尽，警告出场", hkRetryMaxAttempts, hkOutcomeConflict, hkActExhausted},
		{"预算外仍被占用→保持耗尽", hkRetryMaxAttempts + 5, hkOutcomeConflict, hkActExhausted},
	}
	for _, c := range cases {
		if got := decideHKRetry(c.attempt, c.outcome); got != c.want {
			t.Errorf("%s: decideHKRetry(%d, %v) = %v, want %v", c.name, c.attempt, c.outcome, got, c.want)
		}
	}
	if hkRetryMaxAttempts*int(hkRetryDelay/time.Millisecond) < 10000 {
		t.Errorf("retry budget %d x %v is under 10s: too short to outlast a handover exit", hkRetryMaxAttempts, hkRetryDelay)
	}
}

// hkTestGeneration mirrors hotkeyBeginApply's bookkeeping
// WITHOUT the platform call, so the driver test below stays
// deterministic on every OS (the Windows platform apply is an
// asynchronous thread + a real OS registration).
func hkTestGeneration(combo hotkeyCombo) {
	hkRetryState.Lock()
	hkRetryState.gen++
	hkRetryState.attempts = 0
	hkRetryState.noted = false
	hkRetryState.combo = combo
	if hkRetryState.timer != nil {
		hkRetryState.timer.Stop()
		hkRetryState.timer = nil
	}
	hkRetryState.Unlock()
}

func hkTestReset(t *testing.T) {
	t.Helper()
	hotkeyStop()
	hotkeyState.Lock()
	hotkeyState.started = false
	hotkeyState.key = ""
	hotkeyState.registered = false
	hotkeyState.note = ""
	hotkeyState.Unlock()
	hotkeyConflict.Store(false)
}

func hkTestState() (bool, string) {
	hotkeyState.Lock()
	defer hotkeyState.Unlock()
	return hotkeyState.registered, hotkeyState.note
}

// The driver, end to end at the seam level: conflicts schedule
// retries without surfacing the busy warning; success after
// retries lands the registration and resets the budget; a hard
// error settles at once; the budget's last conflict is the one
// that warns; a retry scheduled by a retired generation (a
// settings kick, a daemon stop) can never fire.
func TestHotkeyRetryDriver(t *testing.T) {
	hkTestReset(t)
	defer hkTestReset(t)

	var scheduled []func()
	origSchedule := hkScheduleRetry
	hkScheduleRetry = func(d time.Duration, f func()) *time.Timer {
		if d != hkRetryDelay {
			t.Errorf("retry scheduled after %v, want %v", d, hkRetryDelay)
		}
		scheduled = append(scheduled, f)
		return time.NewTimer(time.Hour) // captured, never fires on its own
	}
	defer func() { hkScheduleRetry = origSchedule }()

	combo, err := parseHotkeyCombo("Ctrl+F6")
	if err != nil {
		t.Fatal(err)
	}
	hotkeyState.Lock()
	hotkeyState.key = combo.String()
	hotkeyState.Unlock()

	// Phase 1: two conflicts, then success. Each conflict
	// schedules exactly one retry; the busy warning must NOT
	// be the recorded state yet; the conflict flag is up so
	// the status endpoint says 「被占用」 while retrying.
	hkTestGeneration(combo)
	hotkeyReportAttempt(hkOutcomeConflict, "")
	hotkeyReportAttempt(hkOutcomeConflict, "")
	if len(scheduled) != 2 {
		t.Fatalf("two conflicts scheduled %d retries, want 2", len(scheduled))
	}
	if registered, note := hkTestState(); registered || note == hotkeyBusyNote {
		t.Fatalf("during retries: registered=%v note=%q -- the busy warning belongs to budget exhaustion only", registered, note)
	}
	if !hotkeyConflict.Load() {
		t.Fatal("conflict flag down during retries: status must read busy")
	}
	hotkeyReportAttempt(hkOutcomeOK, "")
	if registered, note := hkTestState(); !registered || note != "" {
		t.Fatalf("after retry success: registered=%v note=%q, want true/\"\"", registered, note)
	}
	if hotkeyConflict.Load() {
		t.Fatal("conflict flag still up after a successful registration")
	}
	// The budget reset with the success: a fresh conflict is
	// attempt 1 again (one more scheduled retry, no warning).
	hotkeyReportAttempt(hkOutcomeConflict, "")
	if len(scheduled) != 3 {
		t.Fatalf("post-success conflict scheduled %d total retries, want 3 (a fresh budget)", len(scheduled))
	}

	// Phase 2: a hard error settles immediately -- recorded
	// with its note, nothing scheduled.
	scheduled = nil
	hkTestGeneration(combo)
	hotkeyReportAttempt(hkOutcomeError, "系统拒绝注册（错误 5）")
	if registered, note := hkTestState(); registered || note != "系统拒绝注册（错误 5）" {
		t.Fatalf("after hard error: registered=%v note=%q", registered, note)
	}
	if len(scheduled) != 0 {
		t.Fatalf("hard error scheduled %d retries, want 0", len(scheduled))
	}

	// Phase 3: the budget runs out -- exactly
	// hkRetryMaxAttempts conflicts, one fewer scheduled retry,
	// and the busy warning is the recorded state.
	hkTestGeneration(combo)
	for i := 0; i < hkRetryMaxAttempts; i++ {
		hotkeyReportAttempt(hkOutcomeConflict, "")
	}
	if registered, note := hkTestState(); registered || note != hotkeyBusyNote {
		t.Fatalf("after budget spent: registered=%v note=%q, want false/the busy warning", registered, note)
	}
	if len(scheduled) != hkRetryMaxAttempts-1 {
		t.Fatalf("budget run scheduled %d retries, want %d (none after the last attempt)", len(scheduled), hkRetryMaxAttempts-1)
	}

	// Phase 4: a retry scheduled by a retired generation is
	// inert. Retire it the way production does -- a fresh
	// apply (new generation) and, separately, hotkeyStop.
	scheduled = nil
	hkTestGeneration(combo)
	hotkeyReportAttempt(hkOutcomeConflict, "")
	if len(scheduled) != 1 {
		t.Fatalf("conflict scheduled %d retries, want 1", len(scheduled))
	}
	stale := scheduled[0]
	hkTestGeneration(combo) // the settings kick retires it
	hotkeyState.Lock()
	hotkeyState.note = "sentinel"
	hotkeyState.Unlock()
	stale()
	if _, note := hkTestState(); note != "sentinel" {
		t.Fatalf("stale retry fired into the new generation: note=%q, want the sentinel untouched", note)
	}
	scheduled = nil
	hkTestGeneration(combo)
	hotkeyReportAttempt(hkOutcomeConflict, "")
	hotkeyStop() // daemon exit retires it too
	hotkeyState.Lock()
	hotkeyState.note = "sentinel"
	hotkeyState.Unlock()
	scheduled[0]()
	if _, note := hkTestState(); note != "sentinel" {
		t.Fatalf("retry fired after hotkeyStop: note=%q, want the sentinel untouched", note)
	}
}
