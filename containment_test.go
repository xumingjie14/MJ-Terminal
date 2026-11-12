package main

// containment_test.go -- v0.69 block 3 gates: the Chromium
// landing pin merge (round 2: download + savefile + prompt),
// the exit re-pin watchdog's transitions, the Firefox user.js
// lines, and the PWA sweep's path predicate (positive AND
// negative: a shortcut to the host's own Chrome must never
// match).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChromiumLandingPrefs pins the round-2 merge: all
// THREE landing keys move together (download dir, savefile
// dir, prompt off), an already-true prompt flag is flipped
// and counts as a change, a fully pinned object is a no-op,
// and witness keys inside the touched sections -- plus
// unrelated top-level content -- survive every application.
func TestChromiumLandingPrefs(t *testing.T) {
	dir := filepath.Join("D:", "MJ", "Data", "Downloads")
	assertPinned := func(t *testing.T, prefs map[string]any, dir string) {
		t.Helper()
		dl := prefs["download"].(map[string]any)
		if dl["default_directory"] != dir {
			t.Fatalf("download.default_directory = %v, want %v", dl["default_directory"], dir)
		}
		if dl["prompt_for_download"] != false {
			t.Fatalf("download.prompt_for_download = %v, want false", dl["prompt_for_download"])
		}
		sf := prefs["savefile"].(map[string]any)
		if sf["default_directory"] != dir {
			t.Fatalf("savefile.default_directory = %v, want %v", sf["default_directory"], dir)
		}
	}

	// Empty prefs: everything is pinned in one application.
	prefs := map[string]any{}
	if !chromiumLandingPrefs(prefs, dir) {
		t.Fatal("empty prefs must report a change")
	}
	assertPinned(t, prefs, dir)
	if chromiumLandingPrefs(prefs, dir) {
		t.Fatal("second application must be a no-op")
	}

	// A profile whose prompt flag is ON (the field shape
	// that kept popping save dialogs): the pin flips it and
	// reports a change; witnesses inside both touched
	// sections and elsewhere survive.
	prefs2 := map[string]any{
		"download": map[string]any{"default_directory": dir, "prompt_for_download": true, "witness_dl": "keep"},
		"savefile": map[string]any{"default_directory": dir, "witness_sf": "keep"},
		"browser":  map[string]any{"theme": 1},
	}
	if !chromiumLandingPrefs(prefs2, dir) {
		t.Fatal("flipping prompt_for_download must report a change")
	}
	assertPinned(t, prefs2, dir)
	if prefs2["download"].(map[string]any)["witness_dl"] != "keep" {
		t.Fatal("merge disturbed a witness key inside download")
	}
	if prefs2["savefile"].(map[string]any)["witness_sf"] != "keep" {
		t.Fatal("merge disturbed a witness key inside savefile")
	}
	if prefs2["browser"].(map[string]any)["theme"] != 1 {
		t.Fatal("merge disturbed unrelated prefs")
	}
	if chromiumLandingPrefs(prefs2, dir) {
		t.Fatal("application after the flip must be a no-op")
	}

	// A different directory moves all three keys together.
	other := filepath.Join("E:", "Elsewhere", "Downloads")
	if !chromiumLandingPrefs(prefs2, other) {
		t.Fatal("a different directory must report a change")
	}
	assertPinned(t, prefs2, other)
	if prefs2["savefile"].(map[string]any)["witness_sf"] != "keep" {
		t.Fatal("re-pin disturbed a witness key inside savefile")
	}
}

// TestPinChromiumLanding fixtures mirror the REAL Chromium
// layout: the profile argument is the user-data dir, the
// Preferences the browser reads live in its Default subdir,
// and the lockfile sits at the user-data-dir root. (The
// test1/test2 fixtures put Preferences at the root -- the
// same wrong layout the code wrote to, which is why CI
// stayed green while real machines saw no lock at all.)
func TestPinChromiumLanding(t *testing.T) {
	dlDir := filepath.Join(t.TempDir(), "Data", "Downloads")
	prefsPath := func(profile string) string {
		return filepath.Join(profile, "Default", "Preferences")
	}
	readPrefs := func(t *testing.T, profile string) map[string]any {
		t.Helper()
		b, err := os.ReadFile(prefsPath(profile))
		if err != nil {
			t.Fatalf("read Default/Preferences: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("Default/Preferences is not valid JSON: %v", err)
		}
		return m
	}
	assertLanded := func(t *testing.T, m map[string]any) {
		t.Helper()
		dl, _ := m["download"].(map[string]any)
		if s, _ := dl["default_directory"].(string); s != dlDir {
			t.Fatalf("download.default_directory = %q, want %q", s, dlDir)
		}
		if raw, ok := dl["prompt_for_download"]; !ok || raw != false {
			t.Fatalf("download.prompt_for_download = %v (present %v), want explicit false", raw, ok)
		}
		sf, _ := m["savefile"].(map[string]any)
		if s, _ := sf["default_directory"].(string); s != dlDir {
			t.Fatalf("savefile.default_directory = %q, want %q", s, dlDir)
		}
	}

	// Case 1: fresh profile (no Default dir at all) -- the
	// pin creates Default/Preferences holding all three
	// keys, and must NOT create a Preferences at the
	// user-data-dir root.
	profile := t.TempDir()
	if got := pinChromiumLanding(profile, dlDir); got != "locked" {
		t.Fatalf("fresh profile pin = %q, want locked", got)
	}
	if _, err := os.Stat(dlDir); err != nil {
		t.Fatalf("downloads dir was not created: %v", err)
	}
	assertLanded(t, readPrefs(t, profile))
	if _, err := os.Stat(filepath.Join(profile, "Preferences")); !os.IsNotExist(err) {
		t.Fatalf("root Preferences must never be created (stat err = %v)", err)
	}

	// Case 2: existing Default/Preferences -- merge: the pin
	// overwrites the landing keys (including flipping a
	// prompt_for_download the user had on, the round-2
	// point) and leaves witness keys inside the touched
	// sections and elsewhere untouched; a repeat pin is a
	// no-op that still reports locked.
	profile2 := t.TempDir()
	existing := `{"browser":{"theme":1},"download":{"default_directory":"C:\\old","prompt_for_download":true,"witness_dl":"keep"},"savefile":{"default_directory":"C:\\old2","witness_sf":"keep"}}`
	if err := os.MkdirAll(filepath.Join(profile2, "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefsPath(profile2), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pinChromiumLanding(profile2, dlDir); got != "locked" {
		t.Fatalf("existing profile pin = %q, want locked", got)
	}
	m2 := readPrefs(t, profile2)
	assertLanded(t, m2)
	if m2["download"].(map[string]any)["witness_dl"] != "keep" {
		t.Fatal("merge disturbed a witness key inside download")
	}
	if m2["savefile"].(map[string]any)["witness_sf"] != "keep" {
		t.Fatal("merge disturbed a witness key inside savefile")
	}
	if m2["browser"].(map[string]any)["theme"] != float64(1) {
		t.Fatal("merge disturbed unrelated prefs")
	}
	if got := pinChromiumLanding(profile2, dlDir); got != "locked" {
		t.Fatalf("second pin = %q, want locked", got)
	}

	// Case 3: lockfile at the user-data-dir root = browser
	// running -- nothing is rewritten.
	before, _ := os.ReadFile(prefsPath(profile2))
	if err := os.WriteFile(filepath.Join(profile2, "lockfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "Other")
	if got := pinChromiumLanding(profile2, other); got != "running" {
		t.Fatalf("live profile pin = %q, want running", got)
	}
	after, _ := os.ReadFile(prefsPath(profile2))
	if string(after) != string(before) {
		t.Fatal("live profile Default/Preferences were rewritten")
	}
}

// TestPinChromiumLandingShellProfile is the shell-view
// scenario of the same pin: the desktop view's Edge shell
// runs over Data/ShellProfile, a user-data dir with the
// same Chromium layout. spawnShellWindow pins it with the
// same function before every spawn (main.go); this test
// proves a ShellProfile-shaped directory takes the full
// three-key pin and still never grows a root Preferences.
func TestPinChromiumLandingShellProfile(t *testing.T) {
	dlDir := filepath.Join(t.TempDir(), "Data", "Downloads")
	profile := filepath.Join(t.TempDir(), "Data", "ShellProfile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := pinChromiumLanding(profile, dlDir); got != "locked" {
		t.Fatalf("shell profile pin = %q, want locked", got)
	}
	b, err := os.ReadFile(filepath.Join(profile, "Default", "Preferences"))
	if err != nil {
		t.Fatalf("read shell Default/Preferences: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("shell Default/Preferences is not valid JSON: %v", err)
	}
	dl, _ := m["download"].(map[string]any)
	if s, _ := dl["default_directory"].(string); s != dlDir {
		t.Fatalf("shell download.default_directory = %q, want %q", s, dlDir)
	}
	if raw, ok := dl["prompt_for_download"]; !ok || raw != false {
		t.Fatalf("shell download.prompt_for_download = %v (present %v), want explicit false", raw, ok)
	}
	sf, _ := m["savefile"].(map[string]any)
	if s, _ := sf["default_directory"].(string); s != dlDir {
		t.Fatalf("shell savefile.default_directory = %q, want %q", s, dlDir)
	}
	if _, err := os.Stat(filepath.Join(profile, "Preferences")); !os.IsNotExist(err) {
		t.Fatal("shell profile root Preferences must never be created")
	}
}

// TestSeedChromeDarkThemePath pins the v0.69-test3 path fix
// for the OTHER writer that shared the wrong layout: the seed
// lands in Default/Preferences (created on a fresh profile),
// never at the root, and a live profile is skipped.
func TestSeedChromeDarkThemePath(t *testing.T) {
	profile := t.TempDir()
	seedChromeDarkTheme(profile)
	b, err := os.ReadFile(filepath.Join(profile, "Default", "Preferences"))
	if err != nil {
		t.Fatalf("seed did not create Default/Preferences: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("seeded Preferences is not valid JSON: %v", err)
	}
	auto, _ := m["autogenerated"].(map[string]any)
	theme, _ := auto["theme"].(map[string]any)
	if theme["color"] == nil {
		t.Fatalf("seeded Preferences missing autogenerated.theme.color: %s", b)
	}
	if _, err := os.Stat(filepath.Join(profile, "Preferences")); !os.IsNotExist(err) {
		t.Fatal("seed must never write the root Preferences")
	}
	// Running browser (lockfile at the root): seed skips.
	profile2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile2, "lockfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedChromeDarkTheme(profile2)
	if _, err := os.Stat(filepath.Join(profile2, "Default", "Preferences")); !os.IsNotExist(err) {
		t.Fatal("seed wrote into a live profile")
	}
}

// TestLockFellEdge is the watchdog's pure transition
// table: a re-pin is owed ONLY on the falling edge.
func TestLockFellEdge(t *testing.T) {
	cases := []struct {
		name     string
		was, now bool
		wantFell bool
	}{
		{"never ran", false, false, false},
		{"launch (rising edge)", false, true, false},
		{"still running", true, true, false},
		{"just exited (falling edge)", true, false, true},
	}
	for _, tc := range cases {
		if got := lockFellEdge(tc.was, tc.now); got != tc.wantFell {
			t.Errorf("%s: lockFellEdge(%v, %v) = %v, want %v", tc.name, tc.was, tc.now, got, tc.wantFell)
		}
	}
}

// TestProfileExitWatch walks a watch through two full
// run/exit cycles: launches never fire, each exit fires
// exactly once, and a steady absent lockfile does not keep
// firing on later ticks.
func TestProfileExitWatch(t *testing.T) {
	var w profileExitWatch
	steps := []struct {
		present bool
		want    bool
		why     string
	}{
		{false, false, "steady absent at daemon start"},
		{false, false, "steady absent, next tick"},
		{true, false, "browser launched (rising edge owes nothing)"},
		{true, false, "browser still running"},
		{false, true, "browser exited: re-pin owed"},
		{false, false, "absent again next tick: no repeat"},
		{true, false, "second launch"},
		{false, true, "second exit fires again"},
		{false, false, "and settles"},
	}
	for i, s := range steps {
		if got := w.observe(s.present); got != s.want {
			t.Fatalf("step %d (%s): observe(%v) = %v, want %v", i, s.why, s.present, got, s.want)
		}
	}
}

func TestFFDownloadPrefLines(t *testing.T) {
	dir := `D:\MJ\Data\Downloads`
	lines := ffDownloadPrefLines(dir)
	for _, want := range []string{
		`user_pref("browser.download.folderList", 2);`,
		`user_pref("browser.download.dir", "D:\\MJ\\Data\\Downloads");`,
		`user_pref("browser.download.useDownloadDir", true);`,
		`user_pref("browser.download.lastDir.savePerSite", false);`,
		`user_pref("browser.download.lastDir", "D:\\MJ\\Data\\Downloads");`,
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("missing line %q in:\n%s", want, lines)
		}
	}
}

func lnkWithUTF16(parts ...string) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, utf16LEBytes(p, false)...)
		out = append(out, 0, 0)
	}
	return out
}

func TestLnkPointsToProfile(t *testing.T) {
	profile := filepath.Join("D:", "MJ", "Data", "ChromeProfile")
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"utf16 user-data-dir arg", lnkWithUTF16(`C:\Program Files\Google\Chrome\Application\chrome.exe`, `--app-id=abc --user-data-dir=`+profile), true},
		{"utf16 subpath inside profile", lnkWithUTF16(profile + `\Default\Web Applications`), true},
		{"ansi arg", []byte(`--app-id=abc --user-data-dir=` + profile + ` --flag`), true},
		{"host chrome default profile", lnkWithUTF16(`--user-data-dir=C:\Users\me\AppData\Local\Google\Chrome\User Data`), false},
		{"sibling directory must not match", lnkWithUTF16(`--user-data-dir=` + profile + `2`), false},
		{"parent data dir alone", lnkWithUTF16(filepath.Join("D:", "MJ", "Data")), false},
		{"empty data", nil, false},
		{"unrelated shortcut", lnkWithUTF16(`C:\Program Files\Telegram\Telegram.exe`), false},
	}
	for _, tc := range cases {
		if got := lnkPointsToProfile(tc.data, profile); got != tc.want {
			t.Errorf("%s: lnkPointsToProfile = %v, want %v", tc.name, got, tc.want)
		}
	}
}
