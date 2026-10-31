package main

// ---------- OpenClaw component ("小龙虾", v0.25) ----------
//
// An optional, heavy AI-assistant component, downloaded on demand
// like the Firefox component (MJ-OpenClaw.zip + openclaw.json on the
// Release, size + SHA-256 pinned in the manifest) and supervised by
// the daemon. It lives entirely under Data/openclaw:
//
//	runtime/   portable Node (official zip, extracted as-is)
//	app/       OpenClaw itself (npm local-prefix install)
//	state/     OpenClaw state: config, sqlite, logs, workspace
//	workspace/ layout placeholder (the real workspace is state/workspace)
//	tmp/       TMPDIR / TEMP + Node compile cache
//	appdata/   per-process APPDATA/LOCALAPPDATA redirect target (Windows)
//	home/      per-process HOME redirect target (non-Windows)
//
// The zip ships ONLY runtime/ + app/; state etc. are provisioned by
// the daemon at first start, never baked into the package.
//
// Provisioning discipline (from the 2026-10-27 portable validations,
// Linux REPORT.md + Windows runs #1-#3 — do not improvise here):
//   * Config is written/verified BEFORE any openclaw process starts:
//     gateway.mode=local (a config without it makes the gateway
//     refuse to start) and logging.file pointing INSIDE the
//     component. logging.file is an absolute path, so it is
//     re-verified (and rewritten when stale) on every start — the
//     folder may have been copied to another machine.
//   * The gateway never persists an auth token by itself: without
//     one it mints an ephemeral per-start token nobody can read
//     back. So provisioning generates a random token once and stores
//     gateway.auth {mode: token, token: ...} in the config; the
//     page reads it back from the config file for display/copy.
//     The token is NEVER written to the diagnostics log.
//   * The gateway is run in foreground mode ("gateway run") under
//     the daemon's supervision — never "gateway install" (on Windows
//     that registers a scheduled task, breaking the portable rule).
//   * Known residue: the gateway's state-owner lock lives at a
//     homedir-hardcoded path (%LOCALAPPDATA%\OpenClaw on Windows)
//     that ignores env redirects. The daemon snapshots whether that
//     directory existed before the FIRST start (marker file inside
//     the component) and, on stop, deletes it only when it did not
//     exist before and all gateway processes are gone.
//
// Red line (same as everywhere): API keys and the gateway token
// never go into the hub log; gateway stdout/stderr go to a file
// inside the component, only lifecycle events go through logf with
// module "openclaw".

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mjlight/internal/diag"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const ocManifestName = "openclaw.json"
const ocZipName = "MJ-OpenClaw.zip"
const ocPort = 18789

type ocManifest struct {
	Version string `json:"version"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type ocState struct {
	mu       sync.Mutex
	Phase    string // install: idle|downloading|extracting|done|error
	Progress int64
	Size     int64
	Message  string // install state message

	Running  bool
	Starting bool
	Healthy  bool
	Since    time.Time
	ViaNode  bool   // actual route of the current run
	RunMsg   string // runtime status message （启动中… / 运行中 / 已退出…)
	proc     *ocProc
	stopReq  bool
	startSeq int64

	Restarts    int
	LastExit    string
	restartHist []time.Time // crash-restart attempts inside the budget window
}

var ocSt = &ocState{Phase: "idle", RunMsg: "未启动"}

func (s *ocState) setInstall(phase, msg string) {
	s.mu.Lock()
	s.Phase, s.Message = phase, msg
	s.mu.Unlock()
}

func ocSnapshot() (phase string, progress, size int64, message string,
	running, starting, healthy bool, since time.Time, viaNode bool, runMsg string) {
	ocSt.mu.Lock()
	defer ocSt.mu.Unlock()
	return ocSt.Phase, ocSt.Progress, ocSt.Size, ocSt.Message,
		ocSt.Running, ocSt.Starting, ocSt.Healthy, ocSt.Since, ocSt.ViaNode, ocSt.RunMsg
}

func ocRunning() bool {
	ocSt.mu.Lock()
	defer ocSt.mu.Unlock()
	return ocSt.Running || ocSt.Starting
}

func ocBusy() bool {
	p, _, _, _, _, _, _, _, _, _ := ocSnapshot()
	return p == "downloading" || p == "extracting"
}

// ---------- paths ----------

func ocBase() string {
	if v := strings.TrimSpace(os.Getenv("MJ_OPENCLAW_BASE")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "https://github.com/" + updateRepo + "/releases/latest/download"
}

func ocDir() string { return filepath.Join(dataDir(), "openclaw") }

func ocVersionPath() string { return filepath.Join(dataDir(), "openclaw-version.txt") }

func ocNodeBin(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "runtime", "node.exe")
	}
	return filepath.Join(root, "runtime", "bin", "node")
}

func ocEntryMJS(root string) string {
	return filepath.Join(root, "app", "node_modules", "openclaw", "openclaw.mjs")
}

func ocShim(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "app", "node_modules", ".bin", "openclaw.cmd")
	}
	return filepath.Join(root, "app", "node_modules", ".bin", "openclaw")
}

// ocInstalled: the component counts as installed only when both the
// runtime and the OpenClaw entry are really on disk.
func ocInstalled() bool {
	root := ocDir()
	for _, p := range []string{ocNodeBin(root), ocEntryMJS(root), ocShim(root)} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func ocInstalledVersion() string {
	b, status, err := dsLoad("oc-version")
	if err != nil || status == dsMissing {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func ocConfigPath() string { return filepath.Join(ocDir(), "state", "openclaw.json") }

// ---------- preferences (via-node toggle) ----------

func ocPrefsPath() string { return filepath.Join(ocDir(), "mj-prefs.json") }

// Via-node preference lives with the rest of the OpenClaw prefs
// in openclaw_plus.go (ocGetPrefs / ocSetViaNodePref).

// ---------- manifest ----------

func fetchOCManifest(ctx context.Context) (*ocManifest, error) {
	resp, err := dlGet(ctx, ocBase()+"/"+ocManifestName, "MJ-Terminal-OpenClaw/"+AppVersion)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回 %d", resp.StatusCode)
	}
	var m ocManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// The status endpoint is polled once a second while the page is
// open; cache a successful manifest fetch briefly so polling does
// not hammer GitHub (the Firefox page does not cache — this one is
// kinder because the component page also polls while running).
var (
	ocManMu      sync.Mutex
	ocManCache   *ocManifest
	ocManFetched time.Time
)

func fetchOCManifestCached() (*ocManifest, error) {
	ocManMu.Lock()
	defer ocManMu.Unlock()
	if ocManCache != nil && time.Since(ocManFetched) < time.Minute {
		return ocManCache, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	m, err := fetchOCManifest(ctx)
	if err == nil {
		ocManCache, ocManFetched = m, time.Now()
	}
	return m, err
}

// ---------- install ----------

func ocInstallHandler(w http.ResponseWriter, r *http.Request) {
	if ocBusy() {
		writeJSON(w, map[string]any{"ok": true, "message": "已经在下载了"})
		return
	}
	if ocRunning() {
		writeJSON(w, map[string]any{"error": "小龙虾正在运行，先停止再重装组件"})
		return
	}
	logf("openclaw", "info", "开始下载小龙虾组件")
	go runOCInstall()
	writeJSON(w, map[string]any{"ok": true, "message": "开始下载"})
}

func runOCInstall() {
	ocSt.setInstall("downloading", "正在读取组件清单…")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	m, err := fetchOCManifest(ctx)
	if err != nil {
		ocSt.setInstall("error", "读取组件清单失败："+err.Error())
		logf("openclaw", "error", "小龙虾组件清单读取失败：%v", err)
		return
	}
	ocSt.mu.Lock()
	ocSt.Size = m.Size
	ocSt.Progress = 0
	ocSt.mu.Unlock()
	ocSt.setInstall("downloading", "正在下载小龙虾组件…")
	logf("openclaw", "info", "小龙虾组件 %s 开始下载（%.1f MB）", m.Version, float64(m.Size)/1048576)

	resp, err := dlGet(ctx, ocBase()+"/"+m.File, "MJ-Terminal-OpenClaw/"+AppVersion)
	if err != nil {
		ocSt.setInstall("error", "下载失败："+err.Error())
		logf("openclaw", "error", "小龙虾组件下载失败：%v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ocSt.setInstall("error", fmt.Sprintf("下载失败：服务器返回 %d", resp.StatusCode))
		logf("openclaw", "error", "小龙虾组件下载失败：服务器返回 %d", resp.StatusCode)
		return
	}
	_ = os.MkdirAll(updateDir(), 0o755)
	part := filepath.Join(updateDir(), "openclaw.zip.part")
	f, err := os.Create(part)
	if err != nil {
		ocSt.setInstall("error", "下载失败："+err.Error())
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
				ocSt.setInstall("error", "下载失败（写入）："+werr.Error())
				logf("openclaw", "error", "小龙虾组件下载写入失败：%v", werr)
				return
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			ocSt.mu.Lock()
			ocSt.Progress = written
			ocSt.mu.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			_ = os.Remove(part)
			ocSt.setInstall("error", "下载中断："+rerr.Error())
			logf("openclaw", "error", "小龙虾组件下载中断：%v", rerr)
			return
		}
	}
	_ = f.Close()
	if m.Size > 0 && written != m.Size {
		_ = os.Remove(part)
		ocSt.setInstall("error", fmt.Sprintf("下载不完整：大小对不上（%d / %d 字节）", written, m.Size))
		logf("openclaw", "error", "小龙虾组件大小对不上（%d / %d 字节），已删包", written, m.Size)
		return
	}
	if m.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), m.SHA256) {
		_ = os.Remove(part)
		ocSt.setInstall("error", "校验失败：SHA-256 对不上，已放弃这次安装")
		logf("openclaw", "error", "小龙虾组件 SHA-256 校验失败，已删包拒绝安装")
		return
	}

	ocSt.setInstall("extracting", "正在释放小龙虾组件…")
	stage := filepath.Join(dataDir(), "openclaw.tmp")
	_ = os.RemoveAll(stage)
	if err := extractZipTo(part, stage); err != nil {
		_ = os.RemoveAll(stage)
		_ = os.Remove(part)
		ocSt.setInstall("error", "释放失败："+err.Error())
		logf("openclaw", "error", "小龙虾组件释放失败：%v", err)
		return
	}
	_ = os.Remove(part)
	for _, p := range []string{ocNodeBin(stage), ocEntryMJS(stage), ocShim(stage)} {
		if _, err := os.Stat(p); err != nil {
			_ = os.RemoveAll(stage)
			ocSt.setInstall("error", "组件包不完整：缺 "+filepath.Base(p))
			logf("openclaw", "error", "小龙虾组件包不完整：缺 %s", p)
			return
		}
	}
	ocFixExecBits(stage)
	dst := ocDir()
	_ = os.RemoveAll(dst)
	if err := os.Rename(stage, dst); err != nil {
		_ = os.RemoveAll(stage)
		ocSt.setInstall("error", "安装失败："+err.Error())
		logf("openclaw", "error", "小龙虾组件落位失败：%v", err)
		return
	}
	_ = dsSave("oc-version", []byte(m.Version))
	ocSt.setInstall("done", "安装完成：小龙虾 "+m.Version)
	logf("openclaw", "info", "小龙虾组件安装完成：OpenClaw %s（%.1f MB，校验通过）", m.Version, float64(written)/1048576)
	// v0.54: an app-category notification (通知中心 → 应用通知）.
	postAppNotify("openclaw", "小龙虾 OpenClaw", "installed",
		"小龙虾安装完成", "OpenClaw "+m.Version+" 已就绪", "openclaw")
}

// ocFixExecBits restores executable bits after zip extraction on
// non-Windows systems (zip extraction here does not preserve modes;
// node and the .bin shims must be executable). No-op on Windows.
func ocFixExecBits(root string) {
	if runtime.GOOS == "windows" {
		return
	}
	for _, dir := range []string{
		filepath.Join(root, "runtime", "bin"),
		filepath.Join(root, "app", "node_modules", ".bin"),
	} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				_ = os.Chmod(filepath.Join(dir, e.Name()), 0o755)
			}
		}
	}
}

// ---------- provisioning ----------

// ocProvision makes sure the component's state layout exists and the
// config is valid for THIS machine location, before any openclaw
// process is started. See the discipline notes at the top.
func ocProvision() error {
	root := ocDir()
	for _, d := range []string{
		"state", "state/logs", "state/workspace", "workspace",
		"tmp", "tmp/node-compile-cache",
		"appdata/Local", "appdata/Roaming", "home",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	cfgPath := ocConfigPath()
	wantLog := filepath.Join(root, "state", "logs", "openclaw.log")
	cfg := map[string]any{}
	changed := false
	fresh := false
	if b, err := os.ReadFile(cfgPath); err == nil {
		if json.Unmarshal(b, &cfg) != nil || cfg == nil {
			cfg = map[string]any{}
			changed = true
		}
	} else {
		fresh = true
		changed = true
	}
	gw, _ := cfg["gateway"].(map[string]any)
	if gw == nil {
		gw = map[string]any{}
		cfg["gateway"] = gw
		changed = true
	}
	if p, _ := gw["port"].(float64); int(p) != ocPort {
		gw["port"] = ocPort
		changed = true
	}
	if gw["mode"] != "local" {
		gw["mode"] = "local"
		changed = true
	}
	// Auth: persist a daemon-generated token so the page can show it
	// and the Control UI login is stable across restarts. Only fill
	// in when no usable token exists; never override a mode the user
	// chose inside OpenClaw (e.g. password) with our token.
	auth, _ := gw["auth"].(map[string]any)
	if auth == nil {
		tok, err := ocGenToken()
		if err != nil {
			return err
		}
		gw["auth"] = map[string]any{"mode": "token", "token": tok}
		changed = true
		logf("openclaw", "info", "已为小龙虾生成并保存网关令牌（令牌本身不记入日志）")
	} else if mode, _ := auth["mode"].(string); mode == "" || mode == "token" {
		if tok, _ := auth["token"].(string); strings.TrimSpace(tok) == "" {
			nt, err := ocGenToken()
			if err != nil {
				return err
			}
			auth["mode"] = "token"
			auth["token"] = nt
			changed = true
			logf("openclaw", "info", "已为小龙虾补上缺失的网关令牌（令牌本身不记入日志）")
		}
	}
	lg, _ := cfg["logging"].(map[string]any)
	if lg == nil {
		lg = map[string]any{}
		cfg["logging"] = lg
		changed = true
	}
	if cur, _ := lg["file"].(string); cur != wantLog {
		if cur != "" && !fresh {
			logf("openclaw", "info", "检测到组件位置变化，已把日志路径改写到当前位置")
		}
		lg["file"] = wantLog
		changed = true
	}
	if changed {
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		// 0600: the config carries the gateway token.
		if err := os.WriteFile(cfgPath, b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func ocGenToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ocReadToken reads gateway.auth.token back from the config for the
// page (masked there; copied in full). "" when none is configured.
func ocReadToken() string {
	b, err := os.ReadFile(ocConfigPath())
	if err != nil {
		return ""
	}
	var cfg struct {
		Gateway struct {
			Auth struct {
				Token string `json:"token"`
			} `json:"auth"`
		} `json:"gateway"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Gateway.Auth.Token)
}

// ---------- state-owner lock dir baseline + cleanup (Windows) ----------

func ocBaselinePath() string { return filepath.Join(ocDir(), ".mj-lock-baseline.json") }

// ocLockDir is where the gateway's state-owner lock lives on
// Windows: hardcoded from os.homedir() in OpenClaw's source, i.e.
// %USERPROFILE%\AppData\Local\OpenClaw (== %LOCALAPPDATA%\OpenClaw
// in the default layout). "" on other platforms (their lock paths
// follow TMPDIR, which already points inside the component).
func ocLockDir() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	return filepath.Join(homeDir(), "AppData", "Local", "OpenClaw")
}

// ocRecordBaseline snapshots, once, whether the lock dir existed
// before MJ ever started the gateway here. Called before spawning.
func ocRecordBaseline() {
	dir := ocLockDir()
	if dir == "" {
		return
	}
	if _, err := os.Stat(ocBaselinePath()); err == nil {
		return // already recorded
	}
	_, statErr := os.Stat(dir)
	existed := statErr == nil
	b, _ := json.Marshal(map[string]any{
		"existed": existed,
		"at":      time.Now().Format("2006-01-02 15:04:05"),
	})
	_ = os.WriteFile(ocBaselinePath(), b, 0o644)
	if existed {
		logf("openclaw", "info", "基线记录：小龙虾锁目录在本机原本就存在，停止时保留不动")
	} else {
		logf("openclaw", "info", "基线记录：小龙虾锁目录在本机原本不存在，停止后将清理")
	}
}

// ocCleanupLockDir runs after the gateway processes are all gone:
// delete the lock dir only when the baseline says it was not there
// before MJ's first start (per the Windows validation rule).
func ocCleanupLockDir() {
	dir := ocLockDir()
	if dir == "" {
		return
	}
	b, err := os.ReadFile(ocBaselinePath())
	if err != nil {
		logf("openclaw", "warn", "锁目录清理跳过：没有基线记录")
		return
	}
	var base struct {
		Existed bool `json:"existed"`
	}
	if json.Unmarshal(b, &base) != nil {
		logf("openclaw", "warn", "锁目录清理跳过：基线记录读不了")
		return
	}
	if base.Existed {
		logf("openclaw", "info", "锁目录清理：基线显示原本就存在，保留不动")
		return
	}
	if _, err := os.Stat(dir); err != nil {
		logf("openclaw", "info", "锁目录清理：目录不存在，无需清理")
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		logf("openclaw", "warn", "锁目录清理失败：%v", err)
		return
	}
	logf("openclaw", "info", "锁目录清理：已删除本次运行新建的宿主锁目录")
}

// ---------- gateway lifecycle ----------

func ocBuildEnv(root string, viaNode bool) []string {
	env := os.Environ()
	set := func(k, v string) {
		prefix := k + "="
		for i, e := range env {
			if strings.HasPrefix(e, prefix) {
				env[i] = prefix + v
				return
			}
		}
		env = append(env, prefix+v)
	}
	set("OPENCLAW_STATE_DIR", filepath.Join(root, "state"))
	set("OPENCLAW_CONFIG_PATH", filepath.Join(root, "state", "openclaw.json"))
	set("OPENCLAW_WORKSPACE_DIR", filepath.Join(root, "state", "workspace"))
	set("NODE_COMPILE_CACHE", filepath.Join(root, "tmp", "node-compile-cache"))
	tmp := filepath.Join(root, "tmp")
	if runtime.GOOS == "windows" {
		set("TEMP", tmp)
		set("TMP", tmp)
		set("APPDATA", filepath.Join(root, "appdata", "Roaming"))
		set("LOCALAPPDATA", filepath.Join(root, "appdata", "Local"))
	} else {
		set("TMPDIR", tmp)
		set("HOME", filepath.Join(root, "home"))
	}
	binDir := filepath.Join(root, "runtime")
	if runtime.GOOS != "windows" {
		binDir = filepath.Join(root, "runtime", "bin")
	}
	set("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if viaNode {
		px := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
		set("HTTP_PROXY", px)
		set("HTTPS_PROXY", px)
		set("http_proxy", px)
		set("https_proxy", px)
		set("NO_PROXY", "127.0.0.1,localhost")
		set("no_proxy", "127.0.0.1,localhost")
	}
	return env
}

func ocStartHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ViaNode *bool `json:"viaNode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.ViaNode != nil {
		ocSetViaNodePref(*body.ViaNode)
	}
	if !ocInstalled() {
		writeJSON(w, map[string]any{"error": "还没安装小龙虾组件，先点「下载组件」"})
		return
	}
	if ocBusy() {
		writeJSON(w, map[string]any{"error": "组件正在下载中，装完再启动"})
		return
	}
	ocSt.mu.Lock()
	if ocSt.Running || ocSt.Starting {
		ocSt.mu.Unlock()
		writeJSON(w, map[string]any{"ok": true, "message": "已经在运行了"})
		return
	}
	ocSt.Starting = true
	ocSt.Healthy = false
	ocSt.stopReq = false
	ocSt.restartHist = nil
	ocSt.startSeq++
	seq := ocSt.startSeq
	ocSt.RunMsg = "启动中…"
	ocSt.mu.Unlock()
	go ocStart(seq)
	writeJSON(w, map[string]any{"ok": true, "message": "开始启动"})
}

func ocStart(seq int64) {
	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		ocSt.mu.Lock()
		if ocSt.startSeq == seq {
			ocSt.Starting = false
			ocSt.RunMsg = "启动失败：" + msg
		}
		ocSt.mu.Unlock()
		logf("openclaw", "error", "小龙虾启动失败：%s", msg)
	}
	// (a) Provision config + state layout before any process starts.
	if err := ocProvision(); err != nil {
		fail("准备配置失败：%v", err)
		return
	}
	// (b) Baseline for the lock-dir cleanup rule (first start only).
	ocRecordBaseline()
	// (c) Route decision: via the local node when the toggle is on.
	// If the core is down, try to bring it up (saved nodes only); if
	// that fails, start direct and SAY SO — never silently.
	wantNode := ocViaNodePref()
	viaNode := false
	directNote := ""
	if wantNode {
		if coreRunningQuick() {
			viaNode = true
		} else if len(nodes) > 0 {
			coreMu.Lock()
			err := startCoreLocked()
			coreMu.Unlock()
			if err == nil {
				viaNode = true
			} else {
				directNote = "节点没能启动起来"
				logf("openclaw", "warn", "小龙虾本次直连启动：节点启动失败（%v）", err)
			}
		} else {
			directNote = "还没有保存的节点"
		}
		if !viaNode {
			logf("openclaw", "warn", "小龙虾本次直连启动（开关是走节点，但%s）", directNote)
		}
	}
	// (d) Spawn the gateway in foreground mode, daemon-supervised.
	root := ocDir()
	logPath := filepath.Join(root, "state", "logs", "gateway-process.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fail("组件日志文件打不开：%v", err)
		return
	}
	cmd := exec.Command(ocNodeBin(root), ocEntryMJS(root),
		"gateway", "run", "--port", fmt.Sprint(ocPort), "--bind", "loopback", "--verbose")
	cmd.Dir = root
	cmd.Env = ocBuildEnv(root, viaNode)
	hideConsole(cmd)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	proc, err := ocProcStart(cmd)
	if err != nil {
		_ = logFile.Close()
		fail("网关进程启动失败：%v", err)
		return
	}
	startedAt := time.Now()
	ocSt.mu.Lock()
	if ocSt.startSeq != seq {
		ocSt.mu.Unlock()
		proc.killTree()
		<-proc.done
		_ = logFile.Close()
		return
	}
	ocSt.proc = proc
	ocSt.Running = true
	ocSt.Since = startedAt
	ocSt.ViaNode = viaNode
	ocSt.RunMsg = "启动中…（网关起来要十几秒）"
	ocSt.mu.Unlock()
	route := "直连"
	if viaNode {
		route = "走本地节点"
	}
	logf("openclaw", "info", "小龙虾网关启动中（模型流量%s）", route)
	go ocWatchExit(proc, logFile, seq)
	go ocHealthPoll(seq, startedAt)
}

// ocWatchExit records an unexpected gateway exit (no auto-restart in
// v1) and closes the process log file once Wait() has returned.
func ocWatchExit(proc *ocProc, logFile *os.File, seq int64) {
	<-proc.done
	_ = logFile.Close()
	ocSt.mu.Lock()
	defer ocSt.mu.Unlock()
	if ocSt.startSeq != seq || ocSt.proc != proc {
		return
	}
	ocSt.Running = false
	ocSt.Starting = false
	ocSt.Healthy = false
	ocSt.proc = nil
	if ocSt.stopReq {
		ocSt.RunMsg = "已停止"
		return
	}
	if proc.waitErr != nil {
		ocSt.RunMsg = "已退出（" + proc.waitErr.Error() + "）"
		ocSt.LastExit = proc.waitErr.Error()
		logf("openclaw", "error", "小龙虾网关意外退出：%v（细节看组件内 state/logs 的日志）", proc.waitErr)
	} else {
		ocSt.RunMsg = "已退出（退出码 0，非手动停止）"
		ocSt.LastExit = "退出码 0"
		logf("openclaw", "warn", "小龙虾网关自行退出（非手动停止）")
	}
	if ocGetPrefs().CrashRestart {
		ocScheduleRestartLocked(seq)
	}
}

// ocScheduleRestartLocked arranges one crash restart with backoff
// (5s / 20s / 60s), at most 3 attempts in a 10-minute window.
// Caller must hold ocSt.mu.
func ocScheduleRestartLocked(seq int64) {
	now := time.Now()
	kept := ocSt.restartHist[:0]
	for _, t := range ocSt.restartHist {
		if now.Sub(t) < 10*time.Minute {
			kept = append(kept, t)
		}
	}
	ocSt.restartHist = kept
	if len(kept) >= 3 {
		ocSt.RunMsg += "（自动重启已达上限，先不自动拉了）"
		logf("openclaw", "error", "小龙虾 10 分钟内已自动重启 3 次，停手不再自动重启（多半是配置或 Key 有问题，先看日志）")
		return
	}
	delay := []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}[len(kept)]
	ocSt.restartHist = append(ocSt.restartHist, now)
	ocSt.Restarts++
	attempt := len(kept) + 1
	ocSt.RunMsg += fmt.Sprintf("（%d 秒后自动重启，第 %d 次）", int(delay.Seconds()), attempt)
	logf("openclaw", "warn", "小龙虾将在 %d 秒后自动重启（第 %d 次）", int(delay.Seconds()), attempt)
	time.AfterFunc(delay, func() {
		ocSt.mu.Lock()
		if ocSt.Running || ocSt.Starting || ocSt.stopReq || ocSt.startSeq != seq {
			ocSt.mu.Unlock()
			return
		}
		ocSt.Starting = true
		ocSt.Healthy = false
		ocSt.startSeq++
		nseq := ocSt.startSeq
		ocSt.RunMsg = "自动重启中…"
		ocSt.mu.Unlock()
		if !ocInstalled() {
			return
		}
		go ocStart(nseq)
	})
}

func ocHealthPoll(seq int64, startedAt time.Time) {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		ocSt.mu.Lock()
		alive := ocSt.Running && ocSt.startSeq == seq
		ocSt.mu.Unlock()
		if !alive {
			return
		}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", ocPort))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ocSt.mu.Lock()
				if ocSt.startSeq == seq && ocSt.Running {
					ocSt.Healthy = true
					ocSt.Starting = false
					ocSt.RunMsg = "运行中"
				}
				ocSt.mu.Unlock()
				logf("openclaw", "info", "小龙虾网关已就绪（启动约 %d 秒，控制台 http://127.0.0.1:%d/）",
					int(time.Since(startedAt).Seconds()), ocPort)
				return
			}
		}
		time.Sleep(time.Second)
	}
	ocSt.mu.Lock()
	if ocSt.startSeq == seq && ocSt.Running {
		ocSt.Starting = false
		ocSt.RunMsg = "已启动，但 120 秒内健康检查没通过（进程还在，细节看组件日志）"
	}
	ocSt.mu.Unlock()
	logf("openclaw", "warn", "小龙虾网关 120 秒健康检查未通过（进程仍在运行）")
}

// ocStopGateway stops the gateway tree, waits for the port to free
// up, then applies the lock-dir cleanup rule. Blocking; safe to call
// when nothing is running (returns a "not running" message).
func ocStopGateway() string {
	ocSt.mu.Lock()
	proc := ocSt.proc
	active := ocSt.Running || ocSt.Starting
	ocSt.stopReq = true
	ocSt.mu.Unlock()
	if proc == nil || !active {
		return "小龙虾没在运行"
	}
	proc.killTree()
	select {
	case <-proc.done:
	case <-time.After(10 * time.Second):
		logf("openclaw", "warn", "小龙虾网关 10 秒内未完全退出，继续收尾")
	}
	// Wait for the port to be released before cleanup, so no
	// gateway process can still be holding the state-owner lock.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ocPort), 300*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(300 * time.Millisecond)
	}
	ocCleanupLockDir()
	ocSt.mu.Lock()
	ocSt.Running = false
	ocSt.Starting = false
	ocSt.Healthy = false
	ocSt.proc = nil
	ocSt.RunMsg = "已停止"
	ocSt.mu.Unlock()
	logf("openclaw", "info", "小龙虾已停止")
	return "已停止"
}

// ocStopForShutdown is the hook for the daemon's own exit paths
// （设置→退出系统， idle auto-exit, OTA handover): the gateway must
// not outlive the daemon that supervises it.
func ocStopForShutdown() {
	if ocRunning() {
		logf("openclaw", "info", "守护进程退出，先停小龙虾网关")
		_ = ocStopGateway()
	}
}

func ocStopHandler(w http.ResponseWriter, r *http.Request) {
	msg := ocStopGateway()
	writeJSON(w, map[string]any{"ok": true, "message": msg})
}

func ocUninstallHandler(w http.ResponseWriter, r *http.Request) {
	if ocBusy() {
		writeJSON(w, map[string]any{"error": "组件正在下载中，等它装完再卸载"})
		return
	}
	if ocRunning() {
		writeJSON(w, map[string]any{"error": "小龙虾正在运行，先停止再卸载"})
		return
	}
	if err := os.RemoveAll(ocDir()); err != nil {
		logf("openclaw", "error", "小龙虾卸载失败：%v", err)
		writeJSON(w, map[string]any{"error": "删除失败：" + err.Error()})
		return
	}
	dsRemoveFile(ocVersionPath()) // the version stamp and its data-layer backup
	_ = os.RemoveAll(filepath.Join(dataDir(), "openclaw.tmp"))
	ocSt.setInstall("idle", "")
	ocSt.mu.Lock()
	ocSt.RunMsg = "未启动"
	ocSt.mu.Unlock()
	logf("openclaw", "info", "小龙虾组件已卸载（程序、配置与数据一并删除）")
	writeJSON(w, map[string]any{"ok": true, "message": "已卸载：小龙虾的程序、配置和数据已全部删除"})
}

func ocConsoleHandler(w http.ResponseWriter, r *http.Request) {
	_, _, _, _, running, _, healthy, _, _, _ := ocSnapshot()
	if !running {
		writeJSON(w, map[string]any{"error": "小龙虾还没启动"})
		return
	}
	if !healthy {
		writeJSON(w, map[string]any{"error": "网关还在启动中，等状态变成运行中再打开"})
		return
	}
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会在隔离浏览器里打开控制台"})
		return
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", ocPort)
	if err := launchIsolatedBrowser(false, url, false); err != nil {
		logf("openclaw", "error", "打开小龙虾控制台失败：%v", err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	logf("openclaw", "info", "已在隔离浏览器打开小龙虾控制台（本机 %d 端口，直连）", ocPort)
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- runtime stats ----------

var (
	ocDiskMu     sync.Mutex
	ocDiskBytes  int64
	ocDiskCalcAt time.Time
)

// ocDiskUsage totals the component folder, cached for a minute --
// the page polls status every second and node_modules is huge.
func ocDiskUsage() int64 {
	ocDiskMu.Lock()
	defer ocDiskMu.Unlock()
	if time.Since(ocDiskCalcAt) < time.Minute {
		return ocDiskBytes
	}
	var total int64
	root := ocDir()
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	ocDiskBytes, ocDiskCalcAt = total, time.Now()
	return total
}

// ocLogTail returns the last n lines of the gateway process log.
func ocLogTail(n int) []string {
	path := filepath.Join(ocDir(), "state", "logs", "gateway-process.log")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 256*1024 {
		_, _ = f.Seek(fi.Size()-256*1024, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ocAutoStartInit starts the gateway at daemon start when the user
// opted in （小龙虾设置： 打开终端机时自动启动）. Called once from main.
func ocAutoStartInit() {
	if !ocInstalled() || !ocGetPrefs().AutoStart {
		return
	}
	ocSt.mu.Lock()
	if ocSt.Running || ocSt.Starting {
		ocSt.mu.Unlock()
		return
	}
	ocSt.Starting = true
	ocSt.Healthy = false
	ocSt.stopReq = false
	ocSt.startSeq++
	seq := ocSt.startSeq
	ocSt.RunMsg = "启动中…"
	ocSt.mu.Unlock()
	logf("openclaw", "info", "小龙虾随终端机自动启动")
	go ocStart(seq)
}

// ocStatusMap builds the full local status object. NOTE: it
// contains the gateway token in the clear ("token") -- the desktop
// UI needs it for its copy button. Anything serving this map to a
// non-local caller must shape it first (see remoteOCStatusMap in
// remote.go, which strips the token down to hasToken + a mask).
func ocStatusMap() map[string]any {
	phase, prog, size, msg, running, starting, healthy, since, viaNode, runMsg := ocSnapshot()
	installed := ocInstalled()
	out := map[string]any{
		"installed":    installed,
		"version":      ocInstalledVersion(),
		"phase":        phase,
		"progress":     prog,
		"size":         size,
		"stateMsg":     msg,
		"running":      running,
		"starting":     starting,
		"healthy":      healthy,
		"viaNode":      viaNode,
		"viaNodePref":  ocViaNodePref(),
		"runMsg":       runMsg,
		"port":         ocPort,
		"hasToken":     false,
		"token":        "",
		"componentDir": ocDir(),
	}
	if running {
		out["since"] = since.Unix()
		out["uptimeText"] = diag.FmtUptime(time.Since(since))
	}
	ocSt.mu.Lock()
	if ocSt.proc != nil && ocSt.proc.cmd != nil && ocSt.proc.cmd.Process != nil && running {
		out["rssBytes"] = ocTreeRSS(ocSt.proc.cmd.Process.Pid)
	}
	out["restarts"] = ocSt.Restarts
	out["lastExit"] = ocSt.LastExit
	ocSt.mu.Unlock()
	if installed {
		out["diskBytes"] = ocDiskUsage()
	}
	prefs := ocGetPrefs()
	out["autoStart"] = prefs.AutoStart
	out["crashRestart"] = prefs.CrashRestart
	if installed {
		if tok := ocReadToken(); tok != "" {
			out["hasToken"] = true
			out["token"] = tok
		}
	}
	if m, err := fetchOCManifestCached(); err == nil {
		out["available"] = m.Version
		out["availableSize"] = m.Size
		iv := ocInstalledVersion()
		out["updateAvailable"] = installed && iv != "" && m.Version != iv
	}
	return out
}

func ocStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, ocStatusMap())
}
