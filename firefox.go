package main

// ---------- Firefox component (v0.16) ----------
//
// An optional second browser, distributed as a separate Release
// asset (MJ-Firefox.zip + firefox.json) and downloaded on demand so
// the main package stays small. It lives under core/firefox (next to
// sing-box) and always runs with its own profile in
// Data/FirefoxProfile, so logins/cookies/history travel with the
// folder — Firefox profiles keep their keys inside the profile,
// unlike Chrome whose cookies are bound to the Windows account.
// The daemon seeds the profile before every launch: proxy prefs in
// user.js (through the node, or direct) and, for app mode, a
// userChrome.css that hides the tab bar and address bar.

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	b, err := os.ReadFile(ffVersionPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func ffProfileDir() string { return filepath.Join(dataDir(), "FirefoxProfile") }

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
	out := map[string]any{
		"installed": ffInstalled(),
		"version":   ffInstalledVersion(),
		"phase":     phase,
		"progress":  prog,
		"size":      size,
		"message":   msg,
	}
	// v0.56: the live process truth for the Firefox page (the
	// conflict banner reads these): whether a process holds the
	// profile, and the mode it was born in ("" when none,
	// "未知" when a process predates the birth-mode record).
	alive, birthApp, known := ffReconcileProc()
	out["procAlive"] = alive
	out["procMode"] = ""
	if alive {
		out["procMode"] = "未知"
		if known {
			out["procMode"] = ffModeName(birthApp)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if m, err := fetchFFManifest(ctx); err == nil {
		out["available"] = m.Version
		out["availableSize"] = m.Size
		iv := ffInstalledVersion()
		out["updateAvailable"] = ffInstalled() && iv != "" && m.Version != iv
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
	h := sha256.New()
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
	_ = os.WriteFile(ffVersionPath(), []byte(m.Version), 0o644)
	_ = seedFFProfile(true, false)
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
func ffIconHandler(w http.ResponseWriter, r *http.Request) {
	if !ffInstalled() {
		http.NotFound(w, r)
		return
	}
	cache := filepath.Join(dataDir(), "ff-icon.ico")
	if b, err := os.ReadFile(cache); err == nil && len(b) > 0 {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(b)
		return
	}
	ico, err := exeIconICO(filepath.Join(ffInstallDir(), "firefox.exe"))
	if err != nil || len(ico) == 0 {
		http.NotFound(w, r)
		return
	}
	_ = os.WriteFile(cache, ico, 0o644)
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(ico)
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

// seedFFProfile (re)writes the launch-time prefs: proxy choice and,
// for app mode, the chrome-hiding stylesheet. user.js is reapplied
// by Firefox at every start, so the last launch's choices win.
//
// v0.56: every seed write is logged -- the console used to have a
// blind spot here (neither the seed nor the process's birth/death
// was recorded), which made the app-mode field failure a guessing
// game. Callers must only seed when a FRESH process will read the
// files (see decideFFLaunch): seeding for a launch that merely
// joins the running process rewrites the disk copy while the live
// process keeps its birth chrome, and the two diverge silently.
func seedFFProfile(proxy bool, appMode bool) error {
	dir := ffProfileDir()
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
	logf("firefox", "info", "已播种 Firefox 档案（模式：%s，网络：%s）", modeName, proxyName)
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

// ---------- v0.56: Firefox process birth-mode tracking ----------
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
// The fix is to know and respect the birth mode:
//   - every FRESH launch (no process alive before, one alive
//     after) records {appmode, pid, startedAt} in Data/ffstate.json;
//   - a launch request whose mode matches the birth mode joins
//     the process WITHOUT seeding (the disk seed stays whatever
//     the live process was born with);
//   - a conflicting (or unknown-mode) request neither seeds nor
//     opens: the Firefox page is told the running mode and
//     offers a one-click 重启并切换 (ffRestartHandler);
//   - process birth and death are logged (the console blind
//     spot that made the field failure hard to see).

type ffProcRecord struct {
	AppMode   bool  `json:"appmode"`
	PID       int   `json:"pid"`
	StartedAt int64 `json:"startedAt"`
}

func ffStatePath() string { return filepath.Join(dataDir(), "ffstate.json") }

func ffLoadRecord() (ffProcRecord, bool) {
	b, err := os.ReadFile(ffStatePath())
	if err != nil {
		return ffProcRecord{}, false
	}
	var rec ffProcRecord
	if json.Unmarshal(b, &rec) != nil {
		return ffProcRecord{}, false
	}
	return rec, true
}

func ffSaveRecord(appMode bool, pid int) {
	rec := ffProcRecord{AppMode: appMode, PID: pid, StartedAt: time.Now().Unix()}
	if b, err := json.Marshal(rec); err == nil {
		_ = os.MkdirAll(dataDir(), 0o755)
		_ = os.WriteFile(ffStatePath(), b, 0o644)
	}
}

func ffClearRecord() { _ = os.Remove(ffStatePath()) }

func ffModeName(appMode bool) string {
	if appMode {
		return "应用化"
	}
	return "普通"
}

// ffProfileLocked reports whether a Firefox process currently
// holds the profile (its parent.lock file exists) -- the same
// probe storage.go's cleanup guard uses.
func ffProfileLocked() bool {
	locked := false
	_ = filepath.WalkDir(ffProfileDir(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (d.Name() == "parent.lock" || d.Name() == ".parentlock") {
			locked = true
			return filepath.SkipAll
		}
		return nil
	})
	return locked
}

// ffReconcileProc folds the record and the live lock into the
// current truth: alive = a process holds the profile now;
// known/birthAppMode = the mode that process was born in (from
// the record). A record whose process has died is logged and
// cleared here, so the death lands in the console even if
// nobody opens the Firefox page afterwards (the status handler
// and every open call reconcile).
func ffReconcileProc() (alive bool, birthAppMode bool, known bool) {
	rec, has := ffLoadRecord()
	if !ffProfileLocked() {
		if has {
			logf("firefox", "info", "Firefox 进程已退出（出生模式：%s，PID %d）", ffModeName(rec.AppMode), rec.PID)
			ffClearRecord()
		}
		return false, false, false
	}
	if !has {
		return true, false, false
	}
	return true, rec.AppMode, true
}

type ffLaunchDecision int

const (
	ffFresh ffLaunchDecision = iota
	ffJoin
	ffConflict
)

// decideFFLaunch is the pure launch rule (ffstate_test.go pins
// it): no process -> fresh; a process born in the requested
// mode -> join; anything else (other mode, or a process whose
// birth mode we never recorded) -> conflict, because joining
// would silently hand the user a window in the wrong mode.
func decideFFLaunch(alive bool, known bool, birthAppMode bool, wantAppMode bool) ffLaunchDecision {
	if !alive {
		return ffFresh
	}
	if known && birthAppMode == wantAppMode {
		return ffJoin
	}
	return ffConflict
}

// startFFProcess performs one firefox.exe start. fresh=true
// means the caller has established that no process holds the
// profile: the profile is seeded for this launch and the birth
// is recorded. fresh=false is a join: NO seeding -- the running
// process keeps its birth chrome and the disk copy must not
// diverge from it.
func startFFProcess(proxy bool, appMode bool, target string, fresh bool) error {
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
		if err := seedFFProfile(proxy, appMode); err != nil {
			return err
		}
	}
	if target == "" {
		target = "about:home"
	}
	args := []string{"-profile", ffProfileDir(), "-no-remote", "-new-window", target}
	cmd := exec.Command(exe, args...)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Firefox 打开失败: %w", err)
	}
	// v0.36: Firefox windows used to be invisible to the Stage
	// Manager -- launchFirefox never registered the window, so its
	// PID could never enter the old patrol's watched set. The
	// inventory patrol now claims it from the spawn ledger and the
	// -profile command-line fingerprint.
	noteSpawnedProcess(uint32(cmd.Process.Pid), "Firefox")
	if fresh {
		ffSaveRecord(appMode, cmd.Process.Pid)
		logf("firefox", "info", "Firefox 进程启动（模式：%s，PID %d）", ffModeName(appMode), cmd.Process.Pid)
	}
	return nil
}

func ffOpenHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxy   bool   `json:"proxy"`
		AppMode bool   `json:"appmode"`
		URL     string `json:"url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开 Firefox"})
		return
	}
	target := strings.TrimSpace(body.URL)
	alive, birthApp, known := ffReconcileProc()
	switch decideFFLaunch(alive, known, birthApp, body.AppMode) {
	case ffConflict:
		running := "未知"
		if known {
			running = ffModeName(birthApp)
		}
		logf("firefox", "warn", "Firefox 模式冲突：进程正以%s模式运行（出生模式%s），本次%s请求未执行，等待用户选择重启并切换",
			running, map[bool]string{true: "有记录", false: "无记录"}[known], ffModeName(body.AppMode))
		writeJSON(w, map[string]any{
			"conflict":    true,
			"runningMode": running,
			"error":       "Firefox 正以" + running + "模式运行，模式在进程启动时就定死了，本次打开没有执行。点「重启并切换」会关掉 Firefox 再以" + ffModeName(body.AppMode) + "模式重开（标签页会按 Firefox 的会话恢复回来）。",
		})
		return
	case ffJoin:
		if err := startFFProcess(body.Proxy, body.AppMode, target, false); err != nil {
			logf("firefox", "error", "打开 Firefox 失败：%v", err)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
	default: // ffFresh
		if err := startFFProcess(body.Proxy, body.AppMode, target, true); err != nil {
			logf("firefox", "error", "打开 Firefox 失败：%v", err)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
	}
	kind := "直连"
	if body.Proxy {
		kind = "走节点"
	}
	if body.AppMode {
		kind += " · 应用化"
	}
	logf("firefox", "info", "已打开 Firefox（%s）", kind)
	writeJSON(w, map[string]any{"ok": true, "mode": kind})
}

// ---------- v0.56 test2: restart close/kill sets ----------
//
// Field failure on test1: 「重启并切换」 timed out twice with
// "等了 15 秒 Firefox 还没完全退出（档案仍被占用）". The old code
// WM_CLOSEd only the windows the Stage Manager happened to have
// CLAIMED -- a Firefox window the patrol never claimed (or one
// claimed under a different registration) kept the single
// process alive, the profile lock never released, and the
// restart aborted. The close set is now computed from a full
// top-level window enumeration matched against the component
// exe's full path, unioned with the claimed set. And when even
// that is not enough there is an explicit second step: the page
// offers 「强制结束并切换」 (force=true), which terminates the
// component process TREE -- by PID set computed from full image
// paths, never a name-wide kill, so a host Firefox installed
// elsewhere is untouchable.

// ffCloseSet decides which top-level windows a restart closes:
// every window owned by a process whose image path IS the
// component firefox.exe. Pure (ffstate_test.go pins it).
func ffCloseSet(wins []windowFact, procs []procFact, exePath string) []uint64 {
	want := normalizeWinPath(exePath)
	if want == "" {
		return nil
	}
	byPID := map[uint32]string{}
	for _, p := range procs {
		byPID[p.PID] = p.ImagePath
	}
	seen := map[uint64]bool{}
	var out []uint64
	for _, wf := range wins {
		if wf.HWND == 0 || seen[wf.HWND] {
			continue
		}
		img, ok := byPID[wf.PID]
		if !ok || img == "" || normalizeWinPath(img) != want {
			continue
		}
		seen[wf.HWND] = true
		out = append(out, wf.HWND)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ffKillRoots picks the root PIDs for the force step: processes
// whose full image path IS the component firefox.exe. The kill
// set proper is computeKillSet over these roots (the house
// tree-expansion, with its self/kernel exclusions). A process
// whose path could not be queried is never a root -- safety
// over recall, here as everywhere in this codebase.
func ffKillRoots(procs []procFact, exePath string) map[uint32]bool {
	want := normalizeWinPath(exePath)
	roots := map[uint32]bool{}
	if want == "" {
		return roots
	}
	for _, p := range procs {
		if p.ImagePath != "" && normalizeWinPath(p.ImagePath) == want {
			roots[p.PID] = true
		}
	}
	return roots
}

func ffWaitUnlocked(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for ffProfileLocked() && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
	}
	return !ffProfileLocked()
}

// ffRestartHandler serves POST /api/firefox/restart: the
// one-click answer to a mode conflict. Graceful phase: close
// EVERY top-level window of the profile's processes (see
// ffCloseSet) and wait for the profile lock (~15 s). If the
// lock survives, the response carries stillRunning=true and
// the page offers the explicit 「强制结束并切换」 action,
// which re-enters here with force=true and terminates the
// process tree by PID set. Force never happens implicitly.
func ffRestartHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxy   bool   `json:"proxy"`
		AppMode bool   `json:"appmode"`
		URL     string `json:"url"`
		Force   bool   `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上才能重启 Firefox"})
		return
	}
	_, birthApp, known := ffReconcileProc()
	from := "未知"
	if known {
		from = ffModeName(birthApp)
	}
	exe := filepath.Join(ffInstallDir(), "firefox.exe")
	logf("firefox", "info", "重启并切换：开始关闭 Firefox（%s → %s，强制=%v）", from, ffModeName(body.AppMode), body.Force)
	// Graceful close: the full enumeration set, unioned with
	// the Stage Manager's claimed Firefox windows.
	closeSet := ffCloseSet(platformAllTopWindows(), platformProcessSnapshot(), exe)
	inSet := map[uint64]bool{}
	for _, h := range closeSet {
		inSet[h] = true
	}
	extMu.Lock()
	claimed := 0
	for h, e := range extWins {
		if e.Shell || platformProcessImageBase(e.pid) != "firefox.exe" {
			continue
		}
		claimed++
		if !inSet[h] {
			inSet[h] = true
			closeSet = append(closeSet, h)
		}
	}
	extMu.Unlock()
	logf("firefox", "info", "重启并切换：向 %d 个 Firefox 窗口发出关闭请求（台前调度在册 %d 个）", len(closeSet), claimed)
	for _, h := range closeSet {
		platformCloseWindow(h)
	}
	if !ffWaitUnlocked(15 * time.Second) {
		if !body.Force {
			logf("firefox", "error", "重启并切换失败：等了 15 秒 Firefox 还没完全退出（档案仍被占用），等待用户决定是否强制结束")
			writeJSON(w, map[string]any{
				"error":        "Firefox 还没完全退出，等了 15 秒档案仍被占用。可以点「强制结束并切换」直接结束它的进程再重开（没保存的东西会丢，标签页按会话恢复），或者手动把 Firefox 窗口全关掉再点打开。",
				"stillRunning": true,
			})
			return
		}
		// Force phase (explicit user click only): terminate the
		// component's process tree by PID set, with the same
		// pre-kill image re-verification closeAppsForExit uses
		// (a recycled PID shows a different image and is spared).
		procs := platformProcessSnapshot()
		roots := ffKillRoots(procs, exe)
		kill := computeKillSet(procs, roots, uint32(os.Getpid()))
		imageByPID := map[uint32]string{}
		for _, p := range procs {
			imageByPID[p.PID] = p.ImagePath
		}
		logf("firefox", "warn", "强制结束并切换：档案仍被占用，结束 Firefox 进程树（根进程 %d 个，连子进程共 %d 个）", len(roots), len(kill))
		procs2 := platformProcessSnapshot()
		image2 := map[uint32]string{}
		for _, p := range procs2 {
			image2[p.PID] = p.ImagePath
		}
		killed := 0
		for pid := range kill {
			if img, ok := image2[pid]; !ok || img != imageByPID[pid] {
				continue
			}
			proc, err := os.FindProcess(int(pid))
			if err != nil {
				continue
			}
			if err := proc.Kill(); err == nil {
				killed++
			}
		}
		logf("firefox", "info", "强制结束并切换：已结束 %d 个进程，等档案释放", killed)
		if !ffWaitUnlocked(10 * time.Second) {
			logf("firefox", "error", "强制结束并切换失败：进程已结束但档案仍被占用")
			writeJSON(w, map[string]any{"error": "已经强制结束 Firefox 进程，等了 10 秒档案还是被占用。可能有别的程序占着档案，稍等一下再点打开试试。"})
			return
		}
	}
	ffClearRecord()
	target := strings.TrimSpace(body.URL)
	if err := startFFProcess(body.Proxy, body.AppMode, target, true); err != nil {
		logf("firefox", "error", "重启并切换失败：%v", err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	logf("firefox", "info", "重启并切换完成：Firefox 已以%s模式重开", ffModeName(body.AppMode))
	writeJSON(w, map[string]any{"ok": true, "mode": ffModeName(body.AppMode)})
}

// ffUninstallHandler serves POST /api/firefox/uninstall (the
// desktop right-click menu's 卸载组件）: removes the component
// program. The profile (Data/FirefoxProfile -- the user's
// logins) is deliberately kept, and the confirm text in the UI
// says so; a running Firefox blocks the uninstall.
func ffUninstallHandler(w http.ResponseWriter, r *http.Request) {
	if ffProfileLocked() {
		writeJSON(w, map[string]any{"error": "Firefox 还在运行，先把它的窗口全关掉再卸载"})
		return
	}
	dir := ffInstallDir()
	if err := os.RemoveAll(dir); err != nil {
		logf("firefox", "error", "卸载 Firefox 组件失败：%v", err)
		writeJSON(w, map[string]any{"error": "卸载失败（程序可能还在运行，先关掉它）：" + err.Error()})
		return
	}
	_ = os.Remove(ffVersionPath())
	_ = os.Remove(filepath.Join(dataDir(), "ff-icon.ico"))
	ffClearRecord()
	ffSt.set("idle", "")
	logf("firefox", "info", "已卸载 Firefox 组件（程序已删除；档案保留在 Data/FirefoxProfile）")
	writeJSON(w, map[string]any{"ok": true})
}
