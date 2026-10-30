package main

// ---------- app store: portable native apps (v0.24) ----------
//
// The desktop's 「应用商店」 installs *portable* Windows apps into
// Data/apps/<id>/ and launches them from there, so the apps and
// their data travel with the folder like everything else in MJ.
// The store only lists apps we ship ourselves in apps.json: every
// entry is downloaded from the vendor's official site, its size and
// SHA-256 are pinned in the catalog, and it was verified by hand
// before listing (see README「应用商店」). The catalog is fetched
// from the repo (main/apps.json) at runtime so new apps can be
// added without an app release; the embedded copy is the fallback.
//
// Only truly portable builds qualify (official portable / official
// green zip). Nothing is ever "installed" on the host: download ->
// hash check -> unzip into Data/apps -> run from there.

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed apps.json
var embeddedAppsJSON []byte

type storeApp struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Desc      string   `json:"desc"`
	Category  string   `json:"category"`
	Version   string   `json:"version"`
	Size      int64    `json:"size"`
	URL       string   `json:"url"`
	SHA256    string   `json:"sha256"`
	Exe       string   `json:"exe"`
	Icon      string   `json:"icon"`
	Source    string   `json:"source"`
	Homepage  string   `json:"homepage"`
	PostMkdir []string `json:"postMkdir,omitempty"`
}

type storeCatalog struct {
	Apps []storeApp `json:"apps"`
}

func appsRoot() string { return filepath.Join(dataDir(), "apps") }

func appsTmpDir() string {
	d := filepath.Join(appsRoot(), ".tmp")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func appDir(id string) string { return filepath.Join(appsRoot(), id) }

func appsRegistryPath() string { return filepath.Join(appsRoot(), "apps.json") }

type installedApp struct {
	Version string `json:"version"`
	At      string `json:"at"`
}

func loadAppsRegistry() map[string]installedApp {
	out := map[string]installedApp{}
	b, err := os.ReadFile(appsRegistryPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func saveAppsRegistry(reg map[string]installedApp) {
	_ = os.MkdirAll(appsRoot(), 0o755)
	b, err := json.MarshalIndent(reg, "", " ")
	if err != nil {
		return
	}
	tmp := appsRegistryPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, appsRegistryPath())
	}
}

// appInstalled trusts the registry only when the folder and the
// launch exe are really there (folder may have been carried over
// without the registry, or deleted by hand).
func appInstalled(entry storeApp, reg map[string]installedApp) bool {
	if _, ok := reg[entry.ID]; !ok {
		return false
	}
	_, err := os.Stat(filepath.Join(appDir(entry.ID), filepath.FromSlash(entry.Exe)))
	return err == nil
}

// ---------- catalog (remote first, embedded fallback) ----------

var (
	catalogMu      sync.Mutex
	catalogCache   []storeApp
	catalogSrc     string // "remote" | "embedded"
	catalogFetched time.Time
)

func catalogURL() string {
	if v := strings.TrimSpace(os.Getenv("MJ_APPS_CATALOG_URL")); v != "" {
		return v
	}
	return "https://raw.githubusercontent.com/" + updateRepo + "/main/apps.json"
}

func getCatalog() ([]storeApp, string) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if catalogCache != nil && time.Since(catalogFetched) < 10*time.Minute {
		return catalogCache, catalogSrc
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	apps, err := fetchCatalog(ctx, catalogURL())
	src := "remote"
	if err != nil {
		logf("apps", "warn", "应用目录拉取失败（%s），用内置目录：%v", urlHost(catalogURL()), err)
		apps, err = parseCatalog(embeddedAppsJSON)
		src = "embedded"
		if err != nil {
			logf("apps", "error", "内置应用目录解析失败：%v", err)
			apps = nil
		}
	}
	if apps != nil {
		catalogCache, catalogSrc, catalogFetched = apps, src, time.Now()
		return apps, src
	}
	return catalogCache, catalogSrc
}

func fetchCatalog(ctx context.Context, rawurl string) ([]storeApp, error) {
	resp, err := dlGet(ctx, rawurl, "MJ-Terminal-Apps/"+AppVersion)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回 %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	return parseCatalog(b)
}

func parseCatalog(b []byte) ([]storeApp, error) {
	var cat storeCatalog
	if err := json.Unmarshal(b, &cat); err != nil {
		return nil, err
	}
	out := cat.Apps[:0]
	seen := map[string]bool{}
	for _, a := range cat.Apps {
		if a.ID == "" || a.URL == "" || a.Exe == "" || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out, nil
}

func findApp(id string) (storeApp, bool) {
	apps, _ := getCatalog()
	for _, a := range apps {
		if a.ID == id {
			return a, true
		}
	}
	return storeApp{}, false
}

// storeAppNameForExe maps an executable path back to its store
// catalog name, for labelling patrol-claimed windows (v0.36). It
// reads ONLY the embedded catalog: this runs inside the 2-second
// patrol, where the remote catalog fetch in getCatalog would be
// far too heavy. Parsed once.
var embeddedCatalogOnce struct {
	once sync.Once
	apps []storeApp
}

func storeAppNameForExe(exePath string) string {
	if exePath == "" {
		return ""
	}
	embeddedCatalogOnce.once.Do(func() {
		embeddedCatalogOnce.apps, _ = parseCatalog(embeddedAppsJSON)
	})
	reg := loadAppsRegistry()
	for _, a := range embeddedCatalogOnce.apps {
		if !appInstalled(a, reg) {
			continue
		}
		if strings.EqualFold(filepath.Join(appDir(a.ID), filepath.FromSlash(a.Exe)), exePath) {
			return a.Name
		}
	}
	return ""
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "目录源"
	}
	return u.Host
}

func urlFileName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "包"
	}
	return filepath.Base(u.Path)
}

// ---------- per-app install state ----------

type appInstState struct {
	mu       sync.Mutex
	Phase    string // idle|downloading|extracting|done|error
	Progress int64
	Size     int64
	Message  string
}

var appStates = struct {
	sync.Mutex
	m map[string]*appInstState
}{m: map[string]*appInstState{}}

func appState(id string) *appInstState {
	appStates.Lock()
	defer appStates.Unlock()
	s, ok := appStates.m[id]
	if !ok {
		s = &appInstState{Phase: "idle"}
		appStates.m[id] = s
	}
	return s
}

func (s *appInstState) set(phase, msg string) {
	s.mu.Lock()
	s.Phase, s.Message = phase, msg
	s.mu.Unlock()
}

func (s *appInstState) snapshot() (string, int64, int64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Phase, s.Progress, s.Size, s.Message
}

func (s *appInstState) busy() bool {
	p, _, _, _ := s.snapshot()
	return p == "downloading" || p == "extracting"
}

// ---------- HTTP handlers ----------

type storeAppView struct {
	storeApp
	Installed  bool   `json:"installed"`
	Phase      string `json:"phase"`
	Progress   int64  `json:"progress"`
	StateSize  int64  `json:"stateSize"`
	StateMsg   string `json:"stateMsg"`
	Installing bool   `json:"installing"`
}

func appsCatalogHandler(w http.ResponseWriter, r *http.Request) {
	apps, src := getCatalog()
	reg := loadAppsRegistry()
	views := make([]storeAppView, 0, len(apps))
	for _, a := range apps {
		phase, prog, size, msg := appState(a.ID).snapshot()
		views = append(views, storeAppView{
			storeApp:   a,
			Installed:  appInstalled(a, reg),
			Phase:      phase,
			Progress:   prog,
			StateSize:  size,
			StateMsg:   msg,
			Installing: phase == "downloading" || phase == "extracting",
		})
	}
	writeJSON(w, map[string]any{"apps": views, "source": src})
}

func appsInstallHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	entry, ok := findApp(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "目录里没有这个应用"})
		return
	}
	st := appState(entry.ID)
	if st.busy() {
		writeJSON(w, map[string]any{"ok": true, "message": "已经在安装了"})
		return
	}
	if appInstalled(entry, loadAppsRegistry()) {
		writeJSON(w, map[string]any{"error": "已经装好了，不用重复安装"})
		return
	}
	if _, err := os.Stat(appDir(entry.ID)); err == nil {
		logf("apps", "error", "安装 %s 失败：目录已存在但未登记，先卸载或手动删掉 %s", entry.Name, appDir(entry.ID))
		writeJSON(w, map[string]any{"error": "发现残留目录（装了一半或手动拷入），请先在商店里卸载一次再装"})
		return
	}
	logf("apps", "info", "开始安装 %s %s（%s，%.1f MB）", entry.Name, entry.Version, urlHost(entry.URL), float64(entry.Size)/1048576)
	go runAppInstall(entry)
	writeJSON(w, map[string]any{"ok": true, "message": "开始安装"})
}

func runAppInstall(entry storeApp) {
	st := appState(entry.ID)
	st.set("downloading", "正在下载…")
	st.mu.Lock()
	st.Size = entry.Size
	st.Progress = 0
	st.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	resp, err := dlGet(ctx, entry.URL, "MJ-Terminal-Apps/"+AppVersion)
	if err != nil {
		st.set("error", "下载失败："+err.Error())
		logf("apps", "error", "安装 %s 失败：下载出错（%s）：%v", entry.Name, urlHost(entry.URL), err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.set("error", fmt.Sprintf("下载失败：服务器返回 %d", resp.StatusCode))
		logf("apps", "error", "安装 %s 失败：%s 返回 %d", entry.Name, urlHost(entry.URL), resp.StatusCode)
		return
	}
	tmp := appsTmpDir()
	part := filepath.Join(tmp, entry.ID+".zip.part")
	f, err := os.Create(part)
	if err != nil {
		st.set("error", "下载失败："+err.Error())
		logf("apps", "error", "安装 %s 失败：临时文件建不了：%v", entry.Name, err)
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
				_ = os.Remove(part)
				st.set("error", "下载失败（写入）："+werr.Error())
				logf("apps", "error", "安装 %s 失败：写入出错：%v", entry.Name, werr)
				return
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			st.mu.Lock()
			st.Progress = written
			st.mu.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			_ = os.Remove(part)
			st.set("error", "下载中断："+rerr.Error())
			logf("apps", "error", "安装 %s 失败：下载中断：%v", entry.Name, rerr)
			return
		}
	}
	_ = f.Close()
	if entry.Size > 0 && written != entry.Size {
		_ = os.Remove(part)
		st.set("error", fmt.Sprintf("下载不完整：大小对不上（%d / %d 字节）", written, entry.Size))
		logf("apps", "error", "安装 %s 失败：大小对不上（%d / %d 字节）", entry.Name, written, entry.Size)
		return
	}
	if entry.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), entry.SHA256) {
		_ = os.Remove(part)
		st.set("error", "校验失败：SHA-256 对不上，包已删掉")
		logf("apps", "error", "安装 %s 失败：SHA-256 校验不过，疑似包被篡改或源已更新，已删包", entry.Name)
		return
	}

	st.set("extracting", "正在释放…")
	stage := filepath.Join(tmp, entry.ID)
	_ = os.RemoveAll(stage)
	if err := extractZipTo(part, stage); err != nil {
		_ = os.RemoveAll(stage)
		_ = os.Remove(part)
		st.set("error", "释放失败："+err.Error())
		logf("apps", "error", "安装 %s 失败：解压出错：%v", entry.Name, err)
		return
	}
	_ = os.Remove(part)
	if _, err := os.Stat(filepath.Join(stage, filepath.FromSlash(entry.Exe))); err != nil {
		_ = os.RemoveAll(stage)
		st.set("error", "包里没找到启动程序 "+entry.Exe)
		logf("apps", "error", "安装 %s 失败：解压后缺少 %s", entry.Name, entry.Exe)
		return
	}
	// Post-steps declared in the catalog (e.g. VS Code's portable
	// mode is switched on by an empty data/ folder next to Code.exe).
	for _, d := range entry.PostMkdir {
		_ = os.MkdirAll(filepath.Join(stage, filepath.FromSlash(d)), 0o755)
	}
	_ = os.MkdirAll(appsRoot(), 0o755)
	if err := os.Rename(stage, appDir(entry.ID)); err != nil {
		_ = os.RemoveAll(stage)
		st.set("error", "安装失败："+err.Error())
		logf("apps", "error", "安装 %s 失败：落位出错：%v", entry.Name, err)
		return
	}
	reg := loadAppsRegistry()
	reg[entry.ID] = installedApp{Version: entry.Version, At: time.Now().Format("2006-01-02 15:04:05")}
	saveAppsRegistry(reg)
	st.set("done", "安装完成")
	logf("apps", "info", "已安装 %s %s（%.1f MB，校验通过）", entry.Name, entry.Version, float64(written)/1048576)
	// v0.54: an app-category notification (通知中心 → 应用通知）.
	postAppNotify("store-"+entry.ID, entry.Name, "installed",
		entry.Name+" 安装完成", "已通过校验并装好，在桌面或启动台点图标即可打开", "appstore")
}

func appsLaunchHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	entry, ok := findApp(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "目录里没有这个应用"})
		return
	}
	if !appInstalled(entry, loadAppsRegistry()) {
		writeJSON(w, map[string]any{"error": "还没安装，先在应用商店里下载"})
		return
	}
	if runtime.GOOS != "windows" {
		logf("apps", "error", "启动 %s 失败：当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开", entry.Name)
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开"})
		return
	}
	exePath := filepath.Join(appDir(entry.ID), filepath.FromSlash(entry.Exe))
	// Window snapshot BEFORE the launch (v0.31): the window manager
	// claims the app's main window by diffing against it, the same
	// way browser launches are claimed -- a portable app may hand
	// off to a child process, so the started PID is not reliable.
	before := platformSnapshotWindows(exePath)
	cmd := exec.Command(exePath)
	cmd.Dir = appDir(entry.ID)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		logf("apps", "error", "启动 %s 失败：%v", entry.Name, err)
		writeJSON(w, map[string]any{"error": "启动失败：" + err.Error()})
		return
	}
	noteSpawnedProcess(uint32(cmd.Process.Pid), entry.Name)
	logf("apps", "info", "已启动 %s %s", entry.Name, entry.Version)
	// Register the app's window with the unified window manager:
	// unified AppID, one-shot landing, Stage Manager strip (v0.31).
	trackAppLaunchAs(entry.Name, exePath, before, "store:"+entry.ID)
	writeJSON(w, map[string]any{"ok": true})
}

func appsUninstallHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	entry, ok := findApp(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "目录里没有这个应用"})
		return
	}
	if appState(entry.ID).busy() {
		writeJSON(w, map[string]any{"error": "正在安装中，等它装完再卸载"})
		return
	}
	if err := os.RemoveAll(appDir(entry.ID)); err != nil {
		logf("apps", "error", "卸载 %s 失败：%v", entry.Name, err)
		writeJSON(w, map[string]any{"error": "删除失败：" + err.Error()})
		return
	}
	reg := loadAppsRegistry()
	delete(reg, entry.ID)
	saveAppsRegistry(reg)
	winGeomDrop("store:" + entry.ID) // v0.56: a reinstall starts from the default landing
	appState(entry.ID).set("idle", "")
	logf("apps", "info", "已卸载 %s（程序与数据一并删除）", entry.Name)
	writeJSON(w, map[string]any{"ok": true, "message": "已卸载：" + entry.Name + " 的程序和数据已全部删除"})
}
