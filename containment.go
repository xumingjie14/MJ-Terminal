package main

// containment.go -- v0.69 block 3: the landing-point rule.
//
// THE RULE: files the terminal produces land ONLY inside its
// own folder (Data). Two browser behaviours broke it, and this
// file closes both:
//
//  1. Downloads. An isolated browser with a factory profile
//     downloads into the HOST user's Downloads folder -- the
//     no-trace promise leaked there (raised by the user
//     2026-10-27, re-raised as his own rule 2026-11-11). At
//     every launch the browser's download directory is now
//     pinned to the CURRENT machine's absolute Data/Downloads:
//     Chrome through its profile Preferences (written before
//     launch, the seedChromeDarkTheme route), Firefox through
//     its seeded user.js download prefs (firefox.go). Every
//     launch logs one self-check line. In-shell save/export
//     flows the user invokes explicitly keep their semantics --
//     they are the user's choice, not a leak.
//
//  2. PWA shortcuts. Chrome's own "install app" writes .lnk
//     files into the HOST user's Start Menu / Desktop pointing
//     at this profile. The daemon sweeps those folders (at
//     browser launch, gated by the pwaSweep setting, default
//     on) and deletes ONLY shortcuts whose bytes reference
//     this terminal's Data\ChromeProfile path -- judged purely
//     by path features, erring toward keeping (宁可漏删不可误删）:
//     a shortcut to the host's own Chrome never contains this
//     profile's path, so it can never match. Every deletion is
//     logged with its full path.
//
// Round 2 (v0.69-test4, after the test3 field round): the
// download-directory pin alone did not close the browser's
// exits. (a) The Save-As family (image / link save-as,
// Ctrl+S, print-to-PDF) never consults
// download.default_directory -- Chromium keeps a SEPARATE
// savefile memory, and a profile that once saved into the
// host Downloads folder keeps opening its save dialogs
// there; download.prompt_for_download likewise pops a
// dialog rooted in that old memory. The pin now covers all
// three keys (chromiumLandingPrefs). (b) Preferences can
// only be written while the browser is fully exited, so a
// resident instance kept sailing on the old values while
// the launch-time pin silently no-opped; an exit watchdog
// on the patrol cadence (below) re-pins a profile the
// moment its lockfile disappears. (c) The desktop VIEW (an
// Edge shell over Data/ShellProfile) had never been pinned
// at all: console log exports, Files / SSH downloads and
// data exports triggered inside the view landed in the
// host Downloads folder; the shell spawn pins the same
// three keys (main.go spawnShellWindow). (d) Firefox
// remembered its last save directory PER SITE; the seeded
// user.js now disables the per-site memory and pins the
// one global lastDir too (ffDownloadPrefLines). The
// boundary is unchanged: a location the PROGRAM picks is
// always Data; a location the USER picks by hand inside a
// save dialog (or by dragging out) is an explicit export
// and is not intercepted -- but the dialog always STARTS
// in Data.
//
// Audit trail for the rest of the rule lives in the v0.69
// report: store apps install under Data/apps (apps.go
// appsRoot), components under Data/components (procomp.go
// compRoot), sandbox payloads under Data/sandbox (sandbox.go
// sandboxDir), web-app icons under Data/webicons (addapp.go) --
// all verified Data-rooted at this version.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// downloadsDir is the one place browser downloads may land.
func downloadsDir() string { return filepath.Join(dataDir(), "Downloads") }

// chromiumPrefsPath is where a Chromium browser actually
// keeps the default profile's Preferences:
// <user-data-dir>/Default/Preferences. Chrome and Edge share
// the layout, so the isolated browser and the desktop-view
// shell both resolve through this one helper. The
// user-data-dir ROOT carries no Preferences file a browser
// reads -- v0.54's theme seed and v0.69-test2's download
// lock both wrote profile/Preferences, a dead file the
// browser never opened, so on real machines the seed and
// the lock silently did nothing (the theme's visible effect
// was carried by the DWM caption layer instead) while the
// tests, built on the same wrong layout, stayed green.
func chromiumPrefsPath(profile string) string {
	return filepath.Join(profile, "Default", "Preferences")
}

// ---------- Chromium: Preferences landing pin ----------

// chromiumLandingPrefs is THE landing pin: one merge that
// closes every directory a Chromium browser picks on its
// own, inside a decoded Preferences object:
//   - download.default_directory = dir: automatic downloads.
//   - savefile.default_directory = dir: the Save-As family
//     (image / link save-as, Ctrl+S, print-to-PDF) -- a
//     SEPARATE memory from the download directory, which is
//     why test3's download-only pin still let save dialogs
//     open on the host Downloads folder.
//   - download.prompt_for_download = false: the containment
//     posture -- a download lands in Data directly instead
//     of popping a save dialog rooted in the old memory.
//
// Pure: returns whether anything changed, so tests pin the
// merge and callers can log honestly. Unrelated keys, inside
// the touched sections and elsewhere, are never disturbed.
func chromiumLandingPrefs(prefs map[string]any, dir string) bool {
	changed := false
	dl, _ := prefs["download"].(map[string]any)
	if dl == nil {
		dl = map[string]any{}
		prefs["download"] = dl
		changed = true
	}
	if cur, _ := dl["default_directory"].(string); cur != dir {
		dl["default_directory"] = dir
		changed = true
	}
	if cur, ok := dl["prompt_for_download"].(bool); !ok || cur {
		dl["prompt_for_download"] = false
		changed = true
	}
	sf, _ := prefs["savefile"].(map[string]any)
	if sf == nil {
		sf = map[string]any{}
		prefs["savefile"] = sf
		changed = true
	}
	if cur, _ := sf["default_directory"].(string); cur != dir {
		sf["default_directory"] = dir
		changed = true
	}
	return changed
}

// pinChromiumLanding writes the landing pin into the
// profile's real Preferences (Default/Preferences -- see
// chromiumPrefsPath). The discipline matches
// seedChromeDarkTheme: a running browser owns its
// Preferences and rewrites them at exit, so a live profile
// -- lockfile present at the user-data-dir root -- is left
// untouched and the pin already in it from the launch (or
// the exit re-pin) that wrote it stands. A fresh profile
// has no Default directory yet: it is created with a
// Preferences holding just the merged keys, which the
// browser adopts on first start. Returns a short status
// word for the caller's self-check log line: "locked"
// (written or already correct), "running" (profile live,
// not rewritten), or "error".
func pinChromiumLanding(profile, dir string) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "error"
	}
	if _, err := os.Stat(filepath.Join(profile, "lockfile")); err == nil {
		return "running"
	}
	prefsPath := chromiumPrefsPath(profile)
	prefs := map[string]any{}
	if b, err := os.ReadFile(prefsPath); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &prefs); err != nil {
			return "error"
		}
	}
	if !chromiumLandingPrefs(prefs, dir) {
		return "locked"
	}
	b, err := json.Marshal(prefs)
	if err != nil {
		return "error"
	}
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0o755); err != nil {
		return "error"
	}
	tmp := prefsPath + ".mjtmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return "error"
	}
	if err := os.Rename(tmp, prefsPath); err != nil {
		_ = os.Remove(tmp)
		return "error"
	}
	return "locked"
}

// ---------- Firefox: user.js download prefs ----------

// ffDownloadPrefLines renders the Firefox download lock as
// user.js lines (seeded at every fresh launch by
// seedFFProfile): folderList=2 means "the directory named by
// browser.download.dir", useDownloadDir keeps Firefox from
// asking per file. Round 2 (test4) adds the Save-As memory:
// Firefox remembers the last save directory PER SITE by
// default, and an old per-site record pointing at the host
// Downloads folder would keep save dialogs opening there --
// lastDir.savePerSite=false retires the per-site records
// (they are simply no longer consulted; no database surgery)
// and the one global lastDir is pinned to the same
// directory, so a save dialog starts in Data too. The path
// is JS-string-escaped.
func ffDownloadPrefLines(dir string) string {
	esc := strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(dir)
	return "user_pref(\"browser.download.folderList\", 2);\n" +
		"user_pref(\"browser.download.dir\", \"" + esc + "\");\n" +
		"user_pref(\"browser.download.useDownloadDir\", true);\n" +
		"user_pref(\"browser.download.lastDir.savePerSite\", false);\n" +
		"user_pref(\"browser.download.lastDir\", \"" + esc + "\");\n"
}

// ---------- exit re-pin watchdog ----------

// profileLockPresent reports whether a Chromium profile's
// lockfile exists at the user-data-dir root -- the signal
// both browsers raise for their whole lifetime.
func profileLockPresent(profile string) bool {
	_, err := os.Stat(filepath.Join(profile, "lockfile"))
	return err == nil
}

// lockFellEdge is the watchdog's transition predicate, kept
// pure for tests: a re-pin is owed exactly when the lockfile
// WAS present at the previous observation and is gone now.
// Rising edges (a launch) and steady states owe nothing --
// launches run their own pins, and a steady "gone" must not
// re-pin (and re-log) on every patrol tick.
func lockFellEdge(wasPresent, present bool) bool {
	return wasPresent && !present
}

// profileExitWatch carries one profile's last observed
// lockfile state across patrol ticks. Only the patrol
// goroutine touches the instances below, so no lock.
type profileExitWatch struct {
	lockSeen bool
}

// observe folds one lockfile observation into the watch and
// reports whether this observation is the falling edge --
// the browser has just fully exited.
func (w *profileExitWatch) observe(present bool) bool {
	fell := lockFellEdge(w.lockSeen, present)
	w.lockSeen = present
	return fell
}

var (
	chromeExitWatch profileExitWatch
	shellExitWatch  profileExitWatch
)

// watchBrowserProfileExits is the exit re-pin watchdog,
// called once per patrol tick (winmgr.go patrolTick, 2s
// cadence -- no loop of its own). Preferences can only be
// written while a browser is fully exited; a launch that
// found the profile live could only stand on whatever the
// profile already held, which is exactly how a resident
// instance kept the old host-Downloads values alive across
// test3. So the moment a profile's lockfile disappears,
// the profile owes a re-pin: the next launch then starts
// from pinned values no matter how the previous session
// ended. Both Chromium profiles are watched -- the
// isolated browser (ChromeProfile) and the desktop view
// (ShellProfile).
func watchBrowserProfileExits() {
	chromeProfile := filepath.Join(dataDir(), "ChromeProfile")
	if chromeExitWatch.observe(profileLockPresent(chromeProfile)) {
		repinAfterExit(chromeProfile, true)
	}
	shellProfile := filepath.Join(dataDir(), "ShellProfile")
	if shellExitWatch.observe(profileLockPresent(shellProfile)) {
		repinAfterExit(shellProfile, false)
	}
}

// repinAfterExit re-applies a profile's pins right after its
// browser fully exited. The isolated browser also gets its
// dark-theme seed re-applied (same disease, same cure: the
// seed is a Preferences write and obeys the same residency
// rule). Race discipline, stated honestly: the watchdog
// observes the lockfile gone, this function re-checks it is
// STILL gone immediately before writing, and each writer
// stats it once more on entry; if the browser relaunches
// inside that sliver the writers see the lockfile and stand
// down -- or, in the worst case, one launch starts unpinned
// and the NEXT exit re-pins. The window is a fraction of a
// patrol tick and the failure mode is one unpinned session,
// never a corrupted Preferences.
func repinAfterExit(profile string, browser bool) {
	if profileLockPresent(profile) {
		return // relaunched between the tick and now; its own launch pin stands
	}
	label := "桌面视图"
	if browser {
		label = "浏览器"
		seedChromeDarkTheme(profile)
	}
	switch pinChromiumLanding(profile, downloadsDir()) {
	case "locked":
		logf("browser", "info", "%s已退出，已回钉下载与保存目录（%s）", label, downloadsDir())
	case "running":
		// Lost the race against a relaunch; the next exit
		// re-pins. Not worth a console line.
	default:
		logf("browser", "warn", "%s退出回钉失败：写不进档案 Preferences，下载可能落到宿主的下载文件夹", label)
	}
}

// ---------- PWA shortcut sweep ----------

// lnkPointsToProfile reports whether a .lnk file's bytes
// reference the given profile directory. Chrome writes the
// shortcut's target and arguments into the file in both ANSI
// and UTF-16LE forms; both are searched, case-insensitively.
// A match must be BOUNDED: the character after the path may
// not extend the final path segment (so a sibling directory
// like ChromeProfile2 never matches ChromeProfile), and the
// path must be followed by a separator, quote, space, or the
// end of data -- the shapes a real command line takes.
func lnkPointsToProfile(data []byte, profileDir string) bool {
	if profileDir == "" || len(data) == 0 {
		return false
	}
	// UTF-16LE: the case Chrome wrote is the case on disk, but
	// try the folded form too -- cheap insurance, still bounded.
	if hitPathRef(data, utf16LEBytes(profileDir, false), 2) {
		return true
	}
	if hitPathRef(data, utf16LEBytes(profileDir, true), 2) {
		return true
	}
	// ANSI: fold the haystack once and search the folded needle.
	if hitPathRef(data, []byte(strings.ToLower(profileDir)), 1) {
		return true
	}
	return false
}

// utf16LEBytes encodes s as little-endian UTF-16 bytes,
// optionally lowercased first.
func utf16LEBytes(s string, fold bool) []byte {
	if fold {
		s = strings.ToLower(s)
	}
	u := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(u)*2)
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// hitPathRef searches data (lowercased first when unit==1) for
// a bounded occurrence of needle. unit is the code-unit width.
func hitPathRef(data, needle []byte, unit int) bool {
	hay := data
	if unit == 1 {
		hay = []byte(strings.ToLower(string(data)))
	}
	for off := 0; ; {
		i := indexBytes(hay[off:], needle)
		if i < 0 {
			return false
		}
		pos := off + i
		end := pos + len(needle)
		if pathRefBounded(hay, end, unit) {
			return true
		}
		off = pos + unit
	}
}

func pathRefBounded(hay []byte, end, unit int) bool {
	if end >= len(hay) {
		return true
	}
	if unit == 2 {
		if end+1 >= len(hay) {
			return true
		}
		c := uint16(hay[end]) | uint16(hay[end+1])<<8
		return c == 0 || c == '\\' || c == '/' || c == '"' || c == ' ' || c == '\''
	}
	c := hay[end]
	return c == 0 || c == '\\' || c == '/' || c == '"' || c == ' ' || c == '\''
}

func indexBytes(b, sub []byte) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		ok := true
		for j := range sub {
			if b[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// pwaSweepRoots returns the host locations Chrome's own
// "install app" can drop shortcuts into for the CURRENT user:
// the Start Menu programs tree and the Desktop (plus the
// OneDrive-redirected Desktop when OneDrive is in play).
// Empty/missing roots are skipped by the walker.
func pwaSweepRoots() []string {
	var roots []string
	if appData := os.Getenv("APPDATA"); appData != "" {
		roots = append(roots, filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, "Desktop"))
	}
	if od := os.Getenv("OneDrive"); od != "" {
		roots = append(roots, filepath.Join(od, "Desktop"))
	}
	return roots
}

// sweepPWAShortcuts deletes .lnk files under the sweep roots
// that reference this terminal's Chrome profile, logging each
// deletion under [browser] with its full path, then removes a
// "Chrome Apps" folder that the deletions left empty. It
// returns how many shortcuts were removed. Conservative by
// construction: unreadable files are kept, non-.lnk files are
// never touched, and the ONLY deletion criterion is the path
// predicate above.
func sweepPWAShortcuts() int {
	profile := filepath.Join(dataDir(), "ChromeProfile")
	deleted := 0
	var chromeAppsDirs []string
	for _, root := range pwaSweepRoots() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.EqualFold(d.Name(), "Chrome Apps") {
					chromeAppsDirs = append(chromeAppsDirs, path)
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(d.Name()), ".lnk") {
				return nil
			}
			b, rerr := readFileCapped(path, 1<<20)
			if rerr != nil {
				return nil
			}
			if !lnkPointsToProfile(b, profile) {
				return nil
			}
			if derr := os.Remove(path); derr == nil {
				deleted++
				logf("browser", "info", "已清扫宿主里的 PWA 快捷方式（指向本终端档案）：%s", path)
			} else {
				logf("browser", "warn", "PWA 快捷方式清扫失败：%s：%v", path, derr)
			}
			return nil
		})
	}
	for _, dir := range chromeAppsDirs {
		// os.Remove on a directory only succeeds when it is
		// empty -- exactly the cleanup the rule allows.
		if err := os.Remove(dir); err == nil {
			logf("browser", "info", "已移除清空的 Chrome Apps 文件夹：%s", dir)
		}
	}
	return deleted
}

// maybeSweepPWAShortcuts runs one sweep pass in the background
// when the setting allows it (Windows only -- the shortcut
// mechanism itself is a Windows artefact).
func maybeSweepPWAShortcuts() {
	if runtime.GOOS != "windows" || !settingPWASweep() {
		return
	}
	go func() {
		if n := sweepPWAShortcuts(); n > 0 {
			logf("browser", "info", "PWA 快捷方式清扫完成：共移除 %d 个", n)
		}
	}()
}

// readFileCapped reads at most capBytes of a file.
func readFileCapped(path string, capBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, capBytes))
}
