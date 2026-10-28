package main

// ---------- storage usage + one-click cache cleanup (v0.30) ----------
//
// The portable folder grows quietly: the isolated Chrome's caches
// are the usual whale, then component state and store apps. The
// Settings app's 「存储空间」 page answers "what is eating the
// folder" with real numbers (GET /api/storage/usage, one walk over
// the program folder, classified by subtree) and offers a one-click
// cleanup (POST /api/storage/cleanup) that removes ONLY pure cache
// directories from a hard-coded whitelist:
//
//   - Chrome profile: Cache / Code Cache / GPUCache / GrShaderCache /
//     ShaderCache / DawnCache (+ close cousins) at any depth
//   - Firefox profile: cache2
//   - Data/update: staged update leftovers, but only when the
//     updater is idle -- a package in phase checking/downloading/
//     ready is live state, never touched
//   - core/: rotated sing-box logs (sing-box.log.*), never the
//     live sing-box.log itself
//
// It NEVER touches cookies, local storage, IndexedDB, history,
// bookmarks, preferences, or any node / SSH / OpenClaw state.
// Safety valve: while the isolated Chrome or Firefox looks running
// (a registered external window is alive, or the profile still
// holds its lock file) cleanup refuses with a readable reason --
// deleting cache out from under a live browser corrupts profiles.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dirSize sums the regular files under root. Missing roots count 0.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, ierr := d.Info(); ierr == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

type usageBuckets struct {
	total, exe, core, data          int64
	chrome, ffProfile, openclaw     int64
	apps, logs, wallpaper, otherDat int64
}

// classifyWalk walks root once, adding every regular file to acc
// under the bucket its path belongs to. exeRoot is only used to
// know when a path leaves the program folder.
func classifyWalk(root string, acc *usageBuckets, self, core, data, chrome, ffProf, oc, appsDir, logs string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		sz := fi.Size()
		acc.total += sz
		switch {
		case p == self:
			acc.exe += sz
		case strings.HasPrefix(p, chrome+string(os.PathSeparator)):
			acc.chrome += sz
			acc.data += sz
		case strings.HasPrefix(p, ffProf+string(os.PathSeparator)):
			acc.ffProfile += sz
			acc.data += sz
		case strings.HasPrefix(p, oc+string(os.PathSeparator)):
			acc.openclaw += sz
			acc.data += sz
		case strings.HasPrefix(p, appsDir+string(os.PathSeparator)):
			acc.apps += sz
			acc.data += sz
		case strings.HasPrefix(p, logs+string(os.PathSeparator)):
			acc.logs += sz
			acc.data += sz
		case p == wallpaperFile() || p == wallpaperTypeFile():
			acc.wallpaper += sz
			acc.data += sz
		case strings.HasPrefix(p, core+string(os.PathSeparator)):
			acc.core += sz
			if strings.HasPrefix(p, data+string(os.PathSeparator)) {
				acc.data += sz
			}
		case strings.HasPrefix(p, data+string(os.PathSeparator)):
			acc.otherDat += sz
			acc.data += sz
		}
		return nil
	})
}

func storageUsage() (map[string]any, int64) {
	start := time.Now()
	self, _ := os.Executable()
	core := coreDir()
	data := dataDir()
	chrome := filepath.Join(data, "ChromeProfile")
	ffProf := ffProfileDir()
	oc := ocDir()
	appsDir := filepath.Join(data, "apps")
	logs := diagLogsDir()

	acc := &usageBuckets{}
	exeRoot := exeDir()
	classifyWalk(exeRoot, acc, self, core, data, chrome, ffProf, oc, appsDir, logs)
	// Data can live outside the program folder (LocalAppData
	// fallback when the folder is not writable): account for it too.
	if !strings.HasPrefix(data, exeRoot+string(os.PathSeparator)) {
		classifyWalk(data, acc, self, core, data, chrome, ffProf, oc, appsDir, logs)
	}

	program := acc.exe + acc.core
	other := acc.total - program - acc.chrome - acc.ffProfile - acc.openclaw - acc.apps - acc.logs - acc.wallpaper
	if other < 0 {
		other = 0
	}
	took := time.Since(start).Milliseconds()
	return map[string]any{
		"totalBytes": acc.total,
		"dataBytes":  acc.data,
		"tookMs":     took,
		"groups": []map[string]any{
			{"key": "program", "name": "程序本体（程序 + 核心）", "bytes": program},
			{"key": "chrome", "name": "Chrome 档案", "bytes": acc.chrome},
			{"key": "firefoxProfile", "name": "Firefox 档案", "bytes": acc.ffProfile},
			{"key": "openclaw", "name": "小龙虾组件与数据", "bytes": acc.openclaw},
			{"key": "apps", "name": "商店应用", "bytes": acc.apps},
			{"key": "logs", "name": "日志", "bytes": acc.logs},
			{"key": "wallpaper", "name": "自定义壁纸", "bytes": acc.wallpaper},
			{"key": "other", "name": "其他", "bytes": other},
		},
	}, took
}

func storageUsageHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := storageUsage()
	writeJSON(w, u)
}

// ---------- cleanup ----------

var chromeCacheDirNames = map[string]bool{
	"Cache": true, "Code Cache": true, "GPUCache": true,
	"GrShaderCache": true, "ShaderCache": true,
	"DawnCache": true, "DawnGraphiteCache": true, "GraphiteDawnCache": true,
	"component_crx_cache": true, "extensions_crx_cache": true,
}

// browserBusy reports whether the isolated Chrome or Firefox looks
// alive right now, with a readable reason when it does.
func browserBusy() (bool, string) {
	// Any registered external window that is still alive belongs to
	// a browser on the isolated profile (Chrome web apps and plain
	// launches alike register here).
	extMu.Lock()
	alive := false
	for _, e := range extWins {
		if e.Shell {
			continue
		}
		if _, ok := platformWindowTitle(e.HWND); ok {
			alive = true
			break
		}
	}
	extMu.Unlock()
	if alive {
		return true, "有从终端机打开的浏览器窗口还开着，请先把它们关掉再清理"
	}
	chromeProf := filepath.Join(dataDir(), "ChromeProfile")
	for _, n := range []string{"lockfile", "SingletonLock"} {
		if _, err := os.Stat(filepath.Join(chromeProf, n)); err == nil {
			return true, "隔离 Chrome 似乎还在运行（档案里有它的锁文件），请确认 Chrome 已完全关闭再试；若刚才是异常退出，重新打开并正常关闭一次 Chrome 即可"
		}
	}
	// Firefox keeps its lock inside each profile subdirectory
	// (xxx.default/parent.lock), not at the profiles root.
	ffProf := ffProfileDir()
	ffLocked := false
	_ = filepath.WalkDir(ffProf, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (d.Name() == "parent.lock" || d.Name() == ".parentlock") {
			ffLocked = true
			return filepath.SkipAll
		}
		return nil
	})
	if ffLocked {
		return true, "Firefox 似乎还在运行（档案里有它的锁文件），请先关掉 Firefox 再清理"
	}
	return false, ""
}

// updateLeftovers lists the staged-update files that are safe to
// delete right now. While the updater is checking, downloading or
// holding a package ready to apply, its staging directory is live
// state and nothing is returned.
func updateLeftovers() []string {
	upd.mu.Lock()
	phase := upd.Phase
	upd.mu.Unlock()
	if phase == "checking" || phase == "downloading" || phase == "ready" {
		return nil
	}
	var out []string
	for _, n := range []string{"pkg.zip", "pkg.zip.part", "MJ-System.exe", "MJ-System.exe.tmp"} {
		p := filepath.Join(updateDir(), n)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// cleanupTargets measures everything the cleanup is allowed to
// remove, so the freed-bytes figure is measured, not estimated.
func cleanupTargets() (chromeProf, ffProf string, total int64) {
	chromeProf = filepath.Join(dataDir(), "ChromeProfile")
	ffProf = ffProfileDir()
	_ = filepath.WalkDir(chromeProf, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != chromeProf && chromeCacheDirNames[d.Name()] {
			total += dirSize(p)
			return filepath.SkipDir
		}
		return nil
	})
	for _, p := range firefoxCacheDirs(ffProf) {
		total += dirSize(p)
	}
	for _, p := range updateLeftovers() {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	entries, _ := os.ReadDir(coreDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sing-box.log.") {
			if fi, err := e.Info(); err == nil {
				total += fi.Size()
			}
		}
	}
	return chromeProf, ffProf, total
}

func storageCleanupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, map[string]any{"error": "method not allowed"})
		return
	}
	if busy, why := browserBusy(); busy {
		logf("storage", "warn", "一键清理被拒绝：%s", why)
		writeJSON(w, map[string]any{"error": why})
		return
	}
	chromeProf, ffProf, before := cleanupTargets()
	var removed []string

	// Chrome pure-cache directories, at any depth of the profile.
	var cacheDirs []string
	_ = filepath.WalkDir(chromeProf, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != chromeProf && chromeCacheDirNames[d.Name()] {
			cacheDirs = append(cacheDirs, p)
			return filepath.SkipDir
		}
		return nil
	})
	for _, p := range cacheDirs {
		if err := os.RemoveAll(p); err == nil {
			rel, _ := filepath.Rel(chromeProf, p)
			removed = append(removed, "Chrome 缓存 "+rel)
			logf("storage", "info", "清理 Chrome 缓存目录：%s", rel)
		}
	}
	// Firefox cache2 (one per profile subdirectory, at any depth).
	for _, p := range firefoxCacheDirs(ffProf) {
		if err := os.RemoveAll(p); err == nil {
			rel, _ := filepath.Rel(ffProf, p)
			removed = append(removed, "Firefox 缓存 "+rel)
			logf("storage", "info", "清理 Firefox 缓存目录：%s", rel)
		}
	}
	// Staged-update leftovers (idle phases only, see updateLeftovers).
	for _, p := range updateLeftovers() {
		if err := os.Remove(p); err == nil {
			removed = append(removed, "更新暂存 "+filepath.Base(p))
			logf("storage", "info", "清理更新暂存残留：%s", filepath.Base(p))
		}
	}
	// Rotated sing-box logs; the live sing-box.log is never touched.
	entries, _ := os.ReadDir(coreDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sing-box.log.") {
			if err := os.Remove(filepath.Join(coreDir(), e.Name())); err == nil {
				removed = append(removed, "核心旧日志 "+e.Name())
				logf("storage", "info", "清理核心旧日志：%s", e.Name())
			}
		}
	}

	_, _, after := cleanupTargets()
	freed := before - after
	if freed < 0 {
		freed = 0
	}
	logf("storage", "info", "一键清理完成：共 %d 项，释放 %d 字节", len(removed), freed)
	writeJSON(w, map[string]any{
		"ok":         true,
		"freedBytes": freed,
		"removed":    removed,
	})
}

// firefoxCacheDirs lists every cache2 directory under the Firefox
// profiles root. Firefox keeps one profile per subdirectory
// (e.g. FirefoxProfile/ab12cd34.default/cache2), so -- unlike a
// single fixed path -- this walks the tree, mirroring how the
// Chrome cache directories are found.
func firefoxCacheDirs(ffProf string) []string {
	var out []string
	_ = filepath.WalkDir(ffProf, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != ffProf && d.Name() == "cache2" {
			out = append(out, p)
			return filepath.SkipDir
		}
		return nil
	})
	return out
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
