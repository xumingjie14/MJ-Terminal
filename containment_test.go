package main

// containment_test.go -- v0.69 block 3 gates: the Chrome
// download lock merge, the Firefox user.js lines, and the PWA
// sweep's path predicate (positive AND negative: a shortcut to
// the host's own Chrome must never match).

import (
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

func TestLockChromeDownloadDir(t *testing.T) {
	profile := t.TempDir()
	dl := filepath.Join(t.TempDir(), "Data", "Downloads")
	if got := lockChromeDownloadDir(profile, dl); got != "locked" {
		t.Fatalf("first lock = %q, want locked", got)
	}
	if _, err := os.Stat(dl); err != nil {
		t.Fatalf("downloads dir was not created: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(profile, "Preferences"))
	if err != nil || !strings.Contains(string(b), "Downloads") {
		t.Fatalf("Preferences missing the lock: %v %s", err, b)
	}
	// A running browser (lockfile present) must not be rewritten.
	if err := os.WriteFile(filepath.Join(profile, "lockfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := string(b)
	other := filepath.Join(t.TempDir(), "Other")
	if got := lockChromeDownloadDir(profile, other); got != "running" {
		t.Fatalf("live profile lock = %q, want running", got)
	}
	b2, _ := os.ReadFile(filepath.Join(profile, "Preferences"))
	if string(b2) != before {
		t.Fatal("live profile Preferences were rewritten")
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
