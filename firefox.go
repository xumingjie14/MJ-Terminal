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
	ffSt.set("downloading", "正在下载 Firefox 组件…")

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
	css := "/* Managed by MJ Terminal: normal mode (chrome visible). */\n"
	if appMode {
		css = `/* Managed by MJ Terminal: app mode (no tabs, no address bar). */
#TabsToolbar { visibility: collapse !important; }
#nav-bar { visibility: collapse !important; }
#PersonalToolbar { visibility: collapse !important; }
#titlebar { visibility: collapse !important; }
`
	}
	return os.WriteFile(filepath.Join(dir, "chrome", "userChrome.css"), []byte(css), 0o644)
}

func launchFirefox(proxy bool, appMode bool, target string) error {
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
	if err := seedFFProfile(proxy, appMode); err != nil {
		return err
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
	if err := launchFirefox(body.Proxy, body.AppMode, strings.TrimSpace(body.URL)); err != nil {
		logf("firefox", "error", "打开 Firefox 失败：%v", err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
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
