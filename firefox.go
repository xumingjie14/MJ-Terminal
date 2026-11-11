package main

// ---------- Firefox component (v0.16) ----------
//
// An optional second browser, distributed as a separate Release
// asset (MJ-Firefox.zip + firefox.json) and downloaded on demand so
// the main package stays small. It lives under core/firefox (next to
// sing-box) and always runs with its own profile in Data, so
// logins/cookies/history travel with the folder — Firefox profiles
// keep their keys inside the profile, unlike Chrome whose cookies
// are bound to the Windows account.
// The daemon seeds the profile before every launch: proxy prefs in
// user.js (through the node, or direct) and, for app mode, a
// userChrome.css that hides the tab bar and address bar.
//
// v0.64 (detail-fix ④, option A): the single profile became TWO
// entries with one profile each — 窗口版 (normal chrome,
// Data/FirefoxProfile, the pre-v0.64 profile with all its state)
// and 应用化版 (app chrome, Data/FirefoxProfileApp, created fresh
// on first launch). Before this, one profile served both modes
// and the mode was fixed at process birth, so a launch in the
// other mode could only conflict and offer a restart-and-switch.
// Now each entry owns its profile and its process, both may run
// at the same time, and there is no mode to switch: an entry's
// chrome is a property of the entry. The binary stays shared.

import (
	"archive/zip"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mjlight/internal/asec"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const ffManifestName = "firefox.json"
const ffZipName = "MJ-Firefox.zip"

type ffManifest struct {
	Version string `json:"version"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type ffState struct {
	mu       sync.Mutex
	Phase    string // idle|downloading|extracting|ready|error
	Progress int64
	Size     int64
	Message  string
}

var ffSt = ffState{Phase: "idle"}

func (s *ffState) set(phase, msg string) {
	// Mirror terminal install states into the diagnostics hub (v0.22).
	switch phase {
	case "error":
		logf("firefox", "error", "Firefox 组件：%s", msg)
	case "ready":
		logf("firefox", "info", "Firefox 组件：%s", msg)
	}
	s.mu.Lock()
	s.Phase, s.Message = phase, msg
	s.mu.Unlock()
}

func (s *ffState) get() (string, int64, int64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Phase, s.Progress, s.Size, s.Message
}

func ffBase() string {
	if v := strings.TrimSpace(os.Getenv("MJ_FF_BASE")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "https://github.com/" + updateRepo + "/releases/latest/download"
}

func ffInstallDir() string {
	beside := filepath.Join(exeDir(), "core", "firefox")
	if _, err := os.Stat(filepath.Join(beside, "firefox.exe")); err == nil {
		return beside
	}
	under := filepath.Join(dataDir(), "core", "firefox")
	if _, err := os.Stat(filepath.Join(under, "firefox.exe")); err == nil {
		return under
	}
	return beside
}

func ffInstalled() bool {
	_, err := os.Stat(filepath.Join(ffInstallDir(), "firefox.exe"))
	return err == nil
}

func ffVersionPath() string { return filepath.Join(dataDir(), "firefox-version.txt") }

func ffInstalledVersion() string {
	b, status, err := dsLoad("ff-version")
	if err != nil || status == dsMissing {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func ffProfileDir() string { return filepath.Join(dataDir(), "FirefoxProfile") }

// ffAppProfileDir is the 应用化 entry's own profile (v0.64). It is
// created fresh by the first app-entry launch; the pre-v0.64
// single profile stays with the window entry (see ffProfileDir),
// so no existing login/bookmark/history moves or is lost.
func ffAppProfileDir() string { return filepath.Join(dataDir(), "FirefoxProfileApp") }

// ffProfileDirs lists every profile the component owns, in entry
// order. Patrol ownership, storage accounting and the cleanup
// guards all run over this list so the second profile is never a
// blind spot.
func ffProfileDirs() []string { return []string{ffProfileDir(), ffAppProfileDir()} }

// ---------- v0.64: the two Firefox entries ----------
//
// An entry is one launchable Firefox: a fixed chrome mode and its
// own profile directory. The mode is no longer chosen per launch
// against a shared process — it is a property of the entry, which
// is what makes the old fresh/join/conflict question a purely
// per-entry one (decideFFLaunch survives unchanged, asked about
// THIS entry's profile and process only).

const (
	ffEntryWindow = "window" // 普通： normal chrome, pre-v0.64 profile
	ffEntryApp    = "app"    // 应用化： app chrome, fresh profile
)

type ffEntry struct {
	ID      string
	AppMode bool
}

var ffEntries = []ffEntry{
	{ID: ffEntryWindow, AppMode: false},
	{ID: ffEntryApp, AppMode: true},
}

func ffEntryByID(id string) (ffEntry, bool) {
	for _, e := range ffEntries {
		if e.ID == id {
			return e, true
		}
	}
	return ffEntry{}, false
}

func ffEntryForMode(appMode bool) ffEntry {
	for _, e := range ffEntries {
		if e.AppMode == appMode {
			return e
		}
	}
	return ffEntries[0]
}

// Name is the entry's display name, which is also its chrome
// mode's name （普通 / 应用化） — mode and entry are one axis now.
func (e ffEntry) Name() string { return ffModeName(e.AppMode) }

func (e ffEntry) ProfileDir() string {
	if e.AppMode {
		return ffAppProfileDir()
	}
	return ffProfileDir()
}

func fetchFFManifest(ctx context.Context) (*ffManifest, error) {
	resp, err := dlGet(ctx, ffBase()+"/"+ffManifestName, "MJ-Terminal-Firefox/"+AppVersion)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回 %d", resp.StatusCode)
	}
	var m ffManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func ffStatusHandler(w http.ResponseWriter, r *http.Request) {
	phase, prog, size, msg := ffSt.get()
	out := ffStatusResponse{
		Installed: ffInstalled(),
		Version:   ffInstalledVersion(),
		Phase:     phase,
		Progress:  prog,
		Size:      size,
		Message:   msg,
	}
	// v0.56: the live process truth for the Firefox page (the
	// conflict banner read these): whether a process holds the
	// profile, and the mode it was born in ("" when none,
	// "未知" when a process predates the birth-mode record).
	// v0.64: reconciliation is per entry — each entry's profile
	// lock and birth record are its own, so one entry dying
	// never touches the other's state. The aggregate fields
	// keep their pre-v0.64 wire meaning for older readers:
	// procAlive = any entry runs; procMode names what runs
	// (ffProcModeSummary). The per-entry truth rides in
	// instances, which the page now renders directly.
	var parts []string
	for _, e := range ffEntries {
		alive, birthApp, known, pid := ffReconcileEntry(e)
		inst := ffInstanceStatus{ID: e.ID, Name: e.Name(), AppMode: e.AppMode, Alive: alive}
		if alive {
			out.ProcAlive = true
			inst.PID = pid // 0 (omitted) when the birth is unrecorded
			if known {
				parts = append(parts, ffModeName(birthApp))
			} else {
				parts = append(parts, "未知")
			}
		}
		out.Instances = append(out.Instances, inst)
	}
	out.ProcMode = ffProcModeSummary(parts)
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if m, err := fetchFFManifest(ctx); err == nil {
		out.Available = m.Version
		out.AvailableSize = m.Size
		iv := ffInstalledVersion()
		ua := ffInstalled() && iv != "" && m.Version != iv
		out.UpdateAvailable = &ua
	}
	writeJSON(w, out)
}

func ffDownloadHandler(w http.ResponseWriter, r *http.Request) {
	phase, _, _, _ := ffSt.get()
	if phase == "downloading" || phase == "extracting" {
		writeJSON(w, map[string]any{"ok": true, "message": "已经在下载了"})
		return
	}
	logf("firefox", "info", "开始下载 Firefox 组件")
	go runFFInstall()
	writeJSON(w, map[string]any{"ok": true, "message": "开始下载"})
}

func runFFInstall() {
	ensureCoreForDownload()
	ffSt.set("downloading", "正在读取组件清单…")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	m, err := fetchFFManifest(ctx)
	if err != nil {
		ffSt.set("error", "读取组件清单失败："+err.Error())
		return
	}
	ffSt.mu.Lock()
	ffSt.Size = m.Size
	ffSt.Progress = 0
	ffSt.mu.Unlock()
	ffSt.set("downloading", "正在下载 Firefox 组件（"+downloadRoute()+"）…")

	resp, err := dlGet(ctx, ffBase()+"/"+m.File, "MJ-Terminal-Firefox/"+AppVersion)
	if err != nil {
		ffSt.set("error", "下载失败："+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ffSt.set("error", fmt.Sprintf("下载失败：服务器返回 %d", resp.StatusCode))
		return
	}
	_ = os.MkdirAll(updateDir(), 0o755)
	part := filepath.Join(updateDir(), "ff.zip.part")
	f, err := os.Create(part)
	if err != nil {
		ffSt.set("error", "下载失败："+err.Error())
		return
	}
	h := asec.NewSHA256()
	buf := make([]byte, 256*1024)
	var written int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = f.Close()
				ffSt.set("error", "下载失败（写入）："+werr.Error())
				return
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			ffSt.mu.Lock()
			ffSt.Progress = written
			ffSt.mu.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			ffSt.set("error", "下载中断："+rerr.Error())
			return
		}
	}
	_ = f.Close()
	if m.Size > 0 && written != m.Size {
		ffSt.set("error", fmt.Sprintf("下载不完整：大小对不上（%d / %d 字节）", written, m.Size))
		return
	}
	if m.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), m.SHA256) {
		ffSt.set("error", "校验失败：SHA-256 对不上，已放弃这次安装")
		return
	}
	zipPath := filepath.Join(updateDir(), "ff.zip")
	_ = os.Remove(zipPath)
	if err := os.Rename(part, zipPath); err != nil {
		ffSt.set("error", "下载失败："+err.Error())
		return
	}

	ffSt.set("extracting", "正在释放 Firefox…")
	root := filepath.Join(exeDir(), "core")
	if err := os.MkdirAll(root, 0o755); err != nil {
		root = filepath.Join(dataDir(), "core")
		_ = os.MkdirAll(root, 0o755)
	}
	tmpDir := filepath.Join(root, "firefox.tmp")
	_ = os.RemoveAll(tmpDir)
	if err := extractZipTo(zipPath, tmpDir); err != nil {
		ffSt.set("error", "释放失败："+err.Error())
		return
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "firefox.exe")); err != nil {
		ffSt.set("error", "组件包里没找到 firefox.exe")
		return
	}
	dst := filepath.Join(root, "firefox")
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmpDir, dst); err != nil {
		ffSt.set("error", "安装失败："+err.Error())
		return
	}
	_ = dsSave("ff-version", []byte(m.Version))
	// Pre-seed the WINDOW entry's profile so the first launch is
	// ready out of the box (v0.64: the app entry's profile is
	// seeded by its own first launch instead — same seeding,
	// applied per profile).
	_ = seedFFProfile(ffProfileDir(), true, false)
	ffSt.set("ready", "安装完成：Firefox "+m.Version)
	// v0.54: an app-category notification (通知中心 → 应用通知）.
	postAppNotify("firefox", "Firefox", "installed",
		"Firefox 安装完成", "Firefox "+m.Version+" 已就绪，配置目录在 Data 里，跟着文件夹走", "firefox")
}

// ffIconHandler serves the Firefox component's real icon for the
// desktop / Launchpad / Dock tiles (v0.54 parity with the
// productivity components' /api/components/icon). The extracted
// .ico is cached in Data (ff-icon.ico) -- a runtime-derived file
// in the data home, never a stored absolute path, so it travels
// and re-extracts on any machine where the cache is missing.
// Since test3 the cache is keyed by the exe's path+mtime+size
// (exeIconServed in icons.go): a Firefox component update swaps
// the exe and the stale icon can no longer serve forever. The
// response itself is no-cache like every icon endpoint.
func ffIconHandler(w http.ResponseWriter, r *http.Request) {
	if !ffInstalled() {
		http.NotFound(w, r)
		return
	}
	exe := filepath.Join(ffInstallDir(), "firefox.exe")
	cache := filepath.Join(dataDir(), "ff-icon.ico")
	ico, origin, err := exeIconServed(cache, exe)
	if err != nil || len(ico) == 0 {
		http.NotFound(w, r)
		return
	}
	if origin == "extract" {
		logf("firefox", "info", "Firefox 图标已从 %s 提取并缓存（缓存缺失或 exe 已变化）", exe)
	}
	serveIconBytes(w, ico, "image/x-icon")
}

// extractZipTo unzips into dir, guarding against zip-slip paths.
func extractZipTo(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		name := filepath.Clean(zf.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		dst := filepath.Join(dir, name)
		if zf.FileInfo().IsDir() {
			_ = os.MkdirAll(dst, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// seedFFProfile (re)writes one profile's launch-time prefs: proxy
// choice and, for app mode, the chrome-hiding stylesheet. user.js
// is reapplied by Firefox at every start, so the last launch's
// choices win. The dir is the caller's entry profile (v0.64):
// both entries are seeded by this one function, so the fresh
// app-entry profile gets exactly the seeding the window profile
// always got — same proxy routing, same prefs, only the chrome
// differs (by the entry's mode).
//
// v0.56: every seed write is logged -- the console used to have a
// blind spot here (neither the seed nor the process's birth/death
// was recorded), which made the app-mode field failure a guessing
// game. Callers must only seed when a FRESH process will read the
// files (see decideFFLaunch): seeding for a launch that merely
// joins the running process rewrites the disk copy while the live
// process keeps its birth chrome, and the two diverge silently.
func seedFFProfile(dir string, proxy bool, appMode bool) error {
	if err := os.MkdirAll(filepath.Join(dir, "chrome"), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("// Managed by MJ Terminal at launch time.\n")
	b.WriteString("user_pref(\"browser.shell.checkDefaultBrowser\", false);\n")
	b.WriteString("user_pref(\"app.update.auto\", false);\n")
	b.WriteString("user_pref(\"app.update.enabled\", false);\n")
	b.WriteString("user_pref(\"datareporting.healthreport.uploadEnabled\", false);\n")
	b.WriteString("user_pref(\"toolkit.telemetry.enabled\", false);\n")
	b.WriteString("user_pref(\"browser.cache.disk.capacity\", 262144);\n")
	b.WriteString("user_pref(\"toolkit.legacyUserProfileCustomizations.stylesheets\", true);\n")
	b.WriteString("user_pref(\"network.proxy.no_proxies_on\", \"localhost, 127.0.0.1\");\n")
	// v0.69 block 3 (containment): downloads land in THIS
	// machine's Data/Downloads, never the host user's folder.
	// Seeded with the profile at every fresh launch; the prefs
	// persist in the profile afterwards, so joined launches
	// inherit them.
	_ = os.MkdirAll(downloadsDir(), 0o755)
	b.WriteString(ffDownloadPrefLines(downloadsDir()))
	if proxy {
		b.WriteString("user_pref(\"network.proxy.type\", 1);\n")
		b.WriteString("user_pref(\"network.proxy.socks\", \"127.0.0.1\");\n")
		fmt.Fprintf(&b, "user_pref(\"network.proxy.socks_port\", %d);\n", proxyPort)
		b.WriteString("user_pref(\"network.proxy.socks_version\", 5);\n")
		b.WriteString("user_pref(\"network.proxy.socks_remote_dns\", true);\n")
	} else {
		b.WriteString("user_pref(\"network.proxy.type\", 0);\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "user.js"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	modeName := "普通"
	if appMode {
		modeName = "应用化"
	}
	proxyName := "直连"
	if proxy {
		proxyName = "走节点"
	}
	logf("firefox", "info", "已播种 Firefox 档案（模式：%s，网络：%s，下载目录已锁定 Data/Downloads）", modeName, proxyName)
	css := "/* Managed by MJ Terminal: normal mode (chrome visible). */\n"
	if appMode {
		// v0.54 root cause (field report "app-mode window has no
		// close/minimize buttons"): the pre-v0.54 sheet collapsed
		// #titlebar -- but in Firefox's own-drawn frame the
		// window buttons (.titlebar-buttonbox-container: close /
		// minimize / maximize) LIVE inside #titlebar, so the
		// whole button box went with it. The claim/presentation
		// machinery was innocent (Chrome passes through the same
		// ops and keeps its buttons). Only the toolbars are
		// collapsed now; #titlebar stays, and the button box is
		// pinned visible so no future toolbar rule can take it
		// again. The sheet is rewritten at every launch, so the
		// fix applies itself on the next app-mode start.
		css = `/* Managed by MJ Terminal: app mode (no tabs, no address bar). */
#TabsToolbar { visibility: collapse !important; }
#nav-bar { visibility: collapse !important; }
#PersonalToolbar { visibility: collapse !important; }
#toolbar-menubar { visibility: collapse !important; }
.titlebar-buttonbox-container { visibility: visible !important; }
`
	}
	return os.WriteFile(filepath.Join(dir, "chrome", "userChrome.css"), []byte(css), 0o644)
}

// ---------- Firefox process birth-mode tracking (v0.56, dual-entry v0.64) ----------
//
// Field-proven root cause (2026-10-30): Firefox is a single
// process per profile, and userChrome.css is read ONCE, when
// that process is born. A launch while the process is alive
// joins it and inherits its birth mode -- in BOTH directions:
// open 普通 first and every later 应用化 window comes up with
// tabs and an address bar; open 应用化 first and a later 普通
// window comes up bare. Worse, the daemon used to re-seed the
// profile on every launch, so the disk copy and the live
// process's chrome diverged silently.
//
// v0.56's fix was to know and respect the birth mode of THE
// profile: a fresh launch recorded {appmode, pid, startedAt};
// a same-mode launch joined without seeding; a conflicting
// launch was refused and the page offered a restart-and-switch.
//
// v0.64 removes the reason conflicts existed: each entry owns a
// profile, so an entry's launches can only ever meet a process
// born in that entry's own mode. The birth record survives —
// one per entry now — because the truth it carries is still
// needed exactly once: during the upgrade, a pre-v0.64 process
// born in 应用化 mode may still hold the WINDOW entry's profile
// (the old single profile). That entry's launches conflict
// honestly until the process exits (the page explains; no
// restart machinery remains), while the app entry launches
// freely the whole time. Process birth and death stay logged
// per entry — the console blind spot that made the original
// field failure hard to see does not come back.

type ffProcRecord struct {
	AppMode   bool  `json:"appmode"`
	PID       int   `json:"pid"`
	StartedAt int64 `json:"startedAt"`
}

func ffStatePath() string { return filepath.Join(dataDir(), "ffstate.json") }

// ffState guards the records map's read-modify-write cycles:
// the status poll and an open call can reconcile concurrently.
var ffStateMu sync.Mutex

// ffParseState decodes a persisted ffstate blob (pure). The
// current format is {"records": {"window": {...}, "app": {...}}};
// a pre-v0.64 blob is a single bare ffProcRecord and is reported
// as legacy so the caller can migrate it (ffMigrateLegacyRecords).
// Anything unparseable is neither — callers treat it as empty.
func ffParseState(b []byte) (records map[string]ffProcRecord, legacy *ffProcRecord) {
	var probe map[string]json.RawMessage
	if json.Unmarshal(b, &probe) != nil {
		return nil, nil
	}
	if raw, ok := probe["records"]; ok {
		records = map[string]ffProcRecord{}
		_ = json.Unmarshal(raw, &records)
		return records, nil
	}
	if _, ok := probe["pid"]; ok {
		var rec ffProcRecord
		if json.Unmarshal(b, &rec) == nil {
			r := rec
			return nil, &r
		}
	}
	return nil, nil
}

// ffMigrateLegacyRecords attributes a pre-v0.64 single record to
// the WINDOW entry (pure): the process it describes holds
// Data/FirefoxProfile, which is the window entry's profile now.
// The birth mode is kept verbatim — a legacy app-born process
// really does hold that profile in app mode until it exits, and
// the window entry's launch rule must see that conflict rather
// than paper over it. Idempotent by construction: the migrated
// blob is in the new format, so a second load parses it as new
// and migrates nothing.
func ffMigrateLegacyRecords(legacy ffProcRecord) map[string]ffProcRecord {
	return map[string]ffProcRecord{ffEntryWindow: legacy}
}

// ffLoadRecords reads the per-entry birth records, migrating a
// legacy single record on first sight (persisted immediately,
// logged once). Called by every state accessor, so the
// migration lands at the first status poll or launch after the
// upgrade — no separate startup hook needed.
func ffLoadRecords() map[string]ffProcRecord {
	b, status, err := dsLoad("ffstate")
	if err != nil || status == dsMissing {
		return map[string]ffProcRecord{}
	}
	records, legacy := ffParseState(b)
	if legacy != nil {
		records = ffMigrateLegacyRecords(*legacy)
		ffSaveRecords(records)
		logf("firefox", "info", "Firefox 双档案迁移：旧版单档案的进程记录已归到窗口版入口（出生模式：%s，PID %d）；既有档案 Data/FirefoxProfile 归窗口版，应用化版将使用全新档案 Data/FirefoxProfileApp",
			ffModeName(legacy.AppMode), legacy.PID)
		return records
	}
	if records == nil {
		return map[string]ffProcRecord{}
	}
	return records
}

func ffSaveRecords(records map[string]ffProcRecord) {
	if b, err := json.Marshal(struct {
		Records map[string]ffProcRecord `json:"records"`
	}{Records: records}); err == nil {
		_ = dsSave("ffstate", b)
	}
}

func ffSaveRecord(entry ffEntry, pid int) {
	ffStateMu.Lock()
	defer ffStateMu.Unlock()
	records := ffLoadRecords()
	records[entry.ID] = ffProcRecord{AppMode: entry.AppMode, PID: pid, StartedAt: time.Now().Unix()}
	ffSaveRecords(records)
}

func ffClearEntryRecord(entry ffEntry) {
	records := ffLoadRecords()
	if _, ok := records[entry.ID]; !ok {
		return
	}
	delete(records, entry.ID)
	ffSaveRecords(records)
}

// ffClearRecord removes ALL entries' records AND the
// keep-last-good backup: a cleared record must not be able to
// resurrect from the .bak. Used by uninstall.
func ffClearRecord() {
	if p, err := dsPath("ffstate"); err == nil {
		dsRemoveFile(p)
	}
}

func ffModeName(appMode bool) string {
	if appMode {
		return "应用化"
	}
	return "普通"
}

// ffDirLocked reports whether a Firefox process currently holds
// the given profile (its parent.lock file exists) -- the same
// probe storage.go's cleanup guard uses.
func ffDirLocked(dir string) bool {
	locked := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (d.Name() == "parent.lock" || d.Name() == ".parentlock") {
			locked = true
			return filepath.SkipAll
		}
		return nil
	})
	return locked
}

// ffAnyProfileLocked reports whether ANY entry's profile is held
// (the uninstall guard: the component cannot go away under a
// live process of either entry).
func ffAnyProfileLocked() bool {
	for _, dir := range ffProfileDirs() {
		if ffDirLocked(dir) {
			return true
		}
	}
	return false
}

// ffReconcileEntry folds one entry's record and its profile's
// live lock into the current truth: alive = a process holds THIS
// entry's profile now; known/birthAppMode = the mode that
// process was born in (from the record); pid = the recorded PID
// (0 when unknown). A record whose process has died is logged
// and cleared here, so the death lands in the console even if
// nobody opens the Firefox page afterwards (the status handler
// and every open call reconcile both entries).
func ffReconcileEntry(entry ffEntry) (alive bool, birthAppMode bool, known bool, pid int) {
	ffStateMu.Lock()
	defer ffStateMu.Unlock()
	rec, has := ffLoadRecords()[entry.ID]
	if !ffDirLocked(entry.ProfileDir()) {
		if has {
			logf("firefox", "info", "Firefox 进程已退出（入口：%s，出生模式：%s，PID %d）", entry.Name(), ffModeName(rec.AppMode), rec.PID)
			ffClearEntryRecord(entry)
		}
		return false, false, false, 0
	}
	if !has {
		return true, false, false, 0
	}
	return true, rec.AppMode, true, rec.PID
}

// ffProcModeSummary renders the aggregate procMode wire field
// from the per-entry truths (pure): "" when nothing runs; the
// single running entry's birth-mode name (or 未知）; both names
// joined with + when both entries run.
func ffProcModeSummary(parts []string) string {
	return strings.Join(parts, "+")
}

type ffLaunchDecision int

const (
	ffFresh ffLaunchDecision = iota
	ffJoin
	ffConflict
)

// decideFFLaunch is the pure launch rule (ffstate_test.go pins
// it), asked PER ENTRY since v0.64: no process on this entry's
// profile -> fresh; a process born in this entry's mode -> join;
// anything else -> conflict. Post-upgrade the conflict case is
// reachable only on the window entry, against a pre-v0.64
// process born in the other mode that still holds the old
// profile (or a process whose birth was never recorded):
// joining it would silently hand the user a window in the
// wrong chrome, so the open is refused with an explanation
// until that process exits — the other entry is unaffected.
func decideFFLaunch(alive bool, known bool, birthAppMode bool, wantAppMode bool) ffLaunchDecision {
	if !alive {
		return ffFresh
	}
	if known && birthAppMode == wantAppMode {
		return ffJoin
	}
	return ffConflict
}

// startFFProcess performs one firefox.exe start for an entry.
// fresh=true means the caller has established that no process
// holds the entry's profile: the profile is seeded for this
// launch (the app entry's very first launch creates its profile
// here, with the same seeding the window profile gets) and the
// birth is recorded under the entry. fresh=false is a join: NO
// seeding -- the running process keeps its birth chrome and the
// disk copy must not diverge from it.
func startFFProcess(entry ffEntry, proxy bool, target string, fresh bool) error {
	exe := filepath.Join(ffInstallDir(), "firefox.exe")
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("还没安装 Firefox 组件，先在 Firefox 页点「下载组件」")
	}
	if proxy {
		coreMu.Lock()
		err := startCoreLocked()
		coreMu.Unlock()
		if err != nil {
			return fmt.Errorf("节点启动失败：%w", err)
		}
	}
	if fresh {
		if err := seedFFProfile(entry.ProfileDir(), proxy, entry.AppMode); err != nil {
			return err
		}
	}
	if target == "" {
		target = "about:home"
	}
	args := []string{"-profile", entry.ProfileDir(), "-no-remote", "-new-window", target}
	cmd := exec.Command(exe, args...)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Firefox 打开失败: %w", err)
	}
	// v0.36: Firefox windows used to be invisible to the Stage
	// Manager -- launchFirefox never registered the window, so its
	// PID could never enter the old patrol's watched set. The
	// inventory patrol now claims it from the spawn ledger and the
	// -profile command-line fingerprint (v0.64: per entry — each
	// entry's process carries its own profile path).
	noteSpawnedProcess(uint32(cmd.Process.Pid), "Firefox")
	if fresh {
		ffSaveRecord(entry, cmd.Process.Pid)
		logf("firefox", "info", "Firefox 进程启动（入口：%s，PID %d）", entry.Name(), cmd.Process.Pid)
	}
	return nil
}

// ffOpenHandler serves POST /api/firefox/open. The body names
// the entry two ways for wire compatibility: "instance"
// ("window"|"app", preferred) and the pre-v0.64 "appmode" bool,
// which selects the same entry — mode and entry are one axis
// now, so both spellings resolve through ffEntryForMode /
// ffEntryByID to the same thing. The launch rule runs against
// the CHOSEN entry only: the other entry's state can never
// refuse, delay or alter this launch.
func ffOpenHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxy    bool   `json:"proxy"`
		AppMode  bool   `json:"appmode"`
		Instance string `json:"instance"`
		URL      string `json:"url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if runtime.GOOS != "windows" {
		apiErrBadRequest(w, errCodeInvalid, "当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开 Firefox")
		return
	}
	entry := ffEntryForMode(body.AppMode)
	if body.Instance != "" {
		if e, ok := ffEntryByID(body.Instance); ok {
			entry = e
		}
	}
	target := strings.TrimSpace(body.URL)
	alive, birthApp, known, _ := ffReconcileEntry(entry)
	switch decideFFLaunch(alive, known, birthApp, entry.AppMode) {
	case ffConflict:
		// Reachable only on the window entry, against a
		// pre-v0.64 process born in app mode that still holds
		// the old profile (or a process whose birth was never
		// recorded). There is no restart-and-switch anymore:
		// the answer is to let that process exit; the other
		// entry opens normally in the meantime.
		running := "未知"
		if known {
			running = ffModeName(birthApp)
		}
		logf("firefox", "warn", "Firefox 打开被拒（遗留进程）：%s入口的档案正被以%s模式启动的进程占用（出生模式%s），本次请求未执行",
			entry.Name(), running, map[bool]string{true: "有记录", false: "无记录"}[known])
		writeJSON(w, map[string]any{
			"conflict":    true,
			"runningMode": running,
			"error":       "「" + entry.Name() + "」入口的档案正被一个以" + running + "模式启动的 Firefox 进程占着——那是升级到双档案之前启动的进程，模式在它出生时就定死了，它退出之前这个入口打不开。把那个 Firefox 的窗口全部关掉让它退出，再点打开就行；另一个入口不受影响，可以照常打开。",
		})
		return
	case ffJoin:
		if err := startFFProcess(entry, body.Proxy, target, false); err != nil {
			logf("firefox", "error", "打开 Firefox 失败：%v", err)
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, err.Error())
			return
		}
	default: // ffFresh
		if err := startFFProcess(entry, body.Proxy, target, true); err != nil {
			logf("firefox", "error", "打开 Firefox 失败：%v", err)
			writeAPIError(w, http.StatusInternalServerError, errCodeInternal, err.Error())
			return
		}
	}
	kind := "直连"
	if body.Proxy {
		kind = "走节点"
	}
	kind += " · " + entry.Name()
	logf("firefox", "info", "已打开 Firefox（%s）", kind)
	writeJSON(w, map[string]any{"ok": true, "mode": kind})
}

// ---------- v0.64: per-entry claim attribution (winmgr) ----------
//
// The two entries run the same firefox.exe, so the window
// patrol tells their processes apart by the -profile path on
// the command line — the same fingerprint classifyOwnership
// claims them by. ffEntryForProfile is the pure attribution
// (ffstate_test.go pins it, including the sibling trap:
// FirefoxProfile must not match a FirefoxProfileApp command
// line, which cmdlineHasProfile's boundary rule prevents).

// ffEntryForProfile returns the entry ID whose profile dir the
// command line carries, or "" when neither matches (a host
// Firefox, an empty command line). The app dir is checked
// first; the boundary rule makes the order belt-and-braces,
// not load-bearing.
func ffEntryForProfile(cmdline, windowDir, appDir string) string {
	if cmdlineHasProfile(cmdline, appDir) {
		return ffEntryApp
	}
	if cmdlineHasProfile(cmdline, windowDir) {
		return ffEntryWindow
	}
	return ""
}

// ffClaimTag names the entry a Firefox process belongs to, for
// the Stage Manager label: "普通" / "应用化", or "" when the
// process is not one of ours (host Firefox, unqueryable command
// line) — those windows keep their plain label.
func ffClaimTag(p procFact) string {
	if !strings.EqualFold(filepath.Base(p.ImagePath), "firefox.exe") {
		return ""
	}
	id := ffEntryForProfile(p.CmdLine, ffProfileDir(), ffAppProfileDir())
	if id == "" {
		return ""
	}
	e, _ := ffEntryByID(id)
	return e.Name()
}

// ffEntryLabel decorates a window label with its entry tag
// (pure): "标题 · 应用化". An empty entry leaves the label
// untouched.
func ffEntryLabel(base, entryName string) string {
	if entryName == "" {
		return base
	}
	return base + " · " + entryName
}

// ffUninstallHandler serves POST /api/firefox/uninstall (the
// desktop right-click menu's 卸载组件）: removes the component
// program. BOTH profiles (Data/FirefoxProfile and
// Data/FirefoxProfileApp — the user's logins) are deliberately
// kept, and the confirm text in the UI says so; a Firefox of
// either entry still running blocks the uninstall.
func ffUninstallHandler(w http.ResponseWriter, r *http.Request) {
	if ffAnyProfileLocked() {
		apiErrBadRequest(w, errCodeInvalid, "Firefox 还在运行，先把它的窗口全关掉再卸载")
		return
	}
	dir := ffInstallDir()
	if err := os.RemoveAll(dir); err != nil {
		logf("firefox", "error", "卸载 Firefox 组件失败：%v", err)
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "卸载失败（程序可能还在运行，先关掉它）："+err.Error())
		return
	}
	dsRemoveFile(ffVersionPath()) // the version stamp and its data-layer backup
	_ = os.Remove(filepath.Join(dataDir(), "ff-icon.ico"))
	ffClearRecord()
	ffSt.set("idle", "")
	logf("firefox", "info", "已卸载 Firefox 组件（程序已删除；两个档案保留在 Data/FirefoxProfile 与 Data/FirefoxProfileApp）")
	writeJSON(w, map[string]any{"ok": true})
}
