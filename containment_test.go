package main

// containment_test.go -- v0.69 block 3 gates: the Chrome
// download lock merge, the Firefox user.js lines, and the PWA
// sweep's path predicate (positive AND negative: a shortcut to
// the host's own Chrome must never match).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChromeDownloadPrefs(t *testing.T) {
	dir := filepath.Join("D:", "MJ", "Data", "Downloads")
	prefs := map[string]any{}
	if !chromeDownloadPrefs(prefs, dir) {
		t.Fatal("empty prefs must report a change")
	}
	dl := prefs["download"].(map[string]any)
	if dl["default_directory"] != dir {
		t.Fatalf("default_directory = %v, want %v", dl["default_directory"], dir)
	}
	if chromeDownloadPrefs(prefs, dir) {
		t.Fatal("second application must be a no-op")
	}
	// Other profile content survives the merge.
	prefs["browser"] = map[string]any{"theme": 1}
	other := filepath.Join("E:", "Elsewhere", "Downloads")
	if !chromeDownloadPrefs(prefs, other) {
		t.Fatal("a different directory must report a change")
	}
	if prefs["browser"].(map[string]any)["theme"] != 1 {
		t.Fatal("unrelated prefs were disturbed")
	}
}

// TestLockChromeDownloadDir fixtures mirror the REAL Chrome
// layout: the profile argument is the user-data dir, the
// Preferences Chrome reads live in its Default subdir, and the
// lockfile sits at the user-data-dir root. (The test1/test2
// fixtures put Preferences at the root -- the same wrong layout
// the code wrote to, which is why CI stayed green while real
// machines saw no lock at all.)
func TestLockChromeDownloadDir(t *testing.T) {
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
	downloadDirOf := func(m map[string]any) string {
		dl, _ := m["download"].(map[string]any)
		s, _ := dl["default_directory"].(string)
		return s
	}

	// Case 1: fresh profile (no Default dir at all) -- the
	// lock creates Default/Preferences holding the lock, and
	// must NOT create a Preferences at the user-data-dir root.
	profile := t.TempDir()
	if got := lockChromeDownloadDir(profile, dlDir); got != "locked" {
		t.Fatalf("fresh profile lock = %q, want locked", got)
	}
	if _, err := os.Stat(dlDir); err != nil {
		t.Fatalf("downloads dir was not created: %v", err)
	}
	if m := readPrefs(t, profile); downloadDirOf(m) != dlDir {
		t.Fatalf("fresh profile default_directory = %q, want %q", downloadDirOf(m), dlDir)
	}
	if _, err := os.Stat(filepath.Join(profile, "Preferences")); !os.IsNotExist(err) {
		t.Fatalf("root Preferences must never be created (stat err = %v)", err)
	}

	// Case 2: existing Default/Preferences -- merge: the lock
	// overwrites default_directory and leaves every other key
	// (inside download and elsewhere) untouched; a repeat lock
	// is a no-op that still reports locked.
	profile2 := t.TempDir()
	existing := `{"browser":{"theme":1},"download":{"default_directory":"C:\\old","prompt_for_download":true}}`
	if err := os.MkdirAll(filepath.Join(profile2, "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefsPath(profile2), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := lockChromeDownloadDir(profile2, dlDir); got != "locked" {
		t.Fatalf("existing profile lock = %q, want locked", got)
	}
	m2 := readPrefs(t, profile2)
	if downloadDirOf(m2) != dlDir {
		t.Fatalf("merged default_directory = %q, want %q", downloadDirOf(m2), dlDir)
	}
	if m2["download"].(map[string]any)["prompt_for_download"] != true {
		t.Fatal("merge disturbed download.prompt_for_download")
	}
	if m2["browser"].(map[string]any)["theme"] != float64(1) {
		t.Fatal("merge disturbed unrelated prefs")
	}
	if got := lockChromeDownloadDir(profile2, dlDir); got != "locked" {
		t.Fatalf("second lock = %q, want locked", got)
	}

	// Case 3: lockfile at the user-data-dir root = browser
	// running -- nothing is rewritten.
	before, _ := os.ReadFile(prefsPath(profile2))
	if err := os.WriteFile(filepath.Join(profile2, "lockfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "Other")
	if got := lockChromeDownloadDir(profile2, other); got != "running" {
		t.Fatalf("live profile lock = %q, want running", got)
	}
	after, _ := os.ReadFile(prefsPath(profile2))
	if string(after) != string(before) {
		t.Fatal("live profile Default/Preferences were rewritten")
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

func TestFFDownloadPrefLines(t *testing.T) {
	dir := `D:\MJ\Data\Downloads`
	lines := ffDownloadPrefLines(dir)
	for _, want := range []string{
		`user_pref("browser.download.folderList", 2);`,
		`user_pref("browser.download.dir", "D:\\MJ\\Data\\Downloads");`,
		`user_pref("browser.download.useDownloadDir", true);`,
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
