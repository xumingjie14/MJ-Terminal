package main

// OpenClaw page upgrade (v0.29): a real preferences store, the
// first-run configuration wizard (model provider + API key + default
// model + connectivity test), component data export/import and an
// explicit update check.
//
// The wizard drives OpenClaw's OWN CLI (`config set` / `models set`)
// so config writes are always schema-valid for the installed version
// and hot-apply without a gateway restart; the CLI even keeps a .bak
// of the config. Provider keys are credentials: they travel only
// between the page, this daemon and the component's config file on
// the user's disk -- never into the diagnostics log, and any error
// text that could echo a key is scrubbed before it is returned or
// logged.

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---------- preferences ----------

type ocPrefs struct {
	ViaNode      bool `json:"viaNode"`      // default true
	AutoStart    bool `json:"autoStart"`    // start with the terminal (default false)
	CrashRestart bool `json:"crashRestart"` // relaunch after a crash (default true)
}

func ocGetPrefs() ocPrefs {
	p := ocPrefs{ViaNode: true, CrashRestart: true}
	b, status, err := dsLoad("oc-prefs")
	if err != nil || status == dsMissing {
		return p
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return p
	}
	var v bool
	if r, ok := raw["viaNode"]; ok && json.Unmarshal(r, &v) == nil {
		p.ViaNode = v
	}
	if r, ok := raw["autoStart"]; ok && json.Unmarshal(r, &v) == nil {
		p.AutoStart = v
	}
	if r, ok := raw["crashRestart"]; ok && json.Unmarshal(r, &v) == nil {
		p.CrashRestart = v
	}
	return p
}

func ocSavePrefs(p ocPrefs) {
	b, _ := json.Marshal(p)
	_ = dsSave("oc-prefs", b)
}

func ocViaNodePref() bool { return ocGetPrefs().ViaNode }

func ocSetViaNodePref(on bool) {
	p := ocGetPrefs()
	p.ViaNode = on
	ocSavePrefs(p)
}

func ocPrefsHandler(w http.ResponseWriter, r *http.Request) {
	var body map[string]*bool
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求不完整"})
		return
	}
	p := ocGetPrefs()
	if v, ok := body["viaNode"]; ok && v != nil {
		p.ViaNode = *v
	}
	if v, ok := body["autoStart"]; ok && v != nil {
		p.AutoStart = *v
	}
	if v, ok := body["crashRestart"]; ok && v != nil {
		p.CrashRestart = *v
	}
	ocSavePrefs(p)
	logf("openclaw", "info", "小龙虾设置已更新（走节点 %v，随终端自启 %v，崩溃自启 %v）", p.ViaNode, p.AutoStart, p.CrashRestart)
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- CLI runner ----------

// ocRunCLI runs the component's own CLI under the component env.
// Callers must never pass secrets to the logger; error text is
// scrubbed by the caller for the arguments it used.
func ocRunCLI(timeout time.Duration, args ...string) (string, error) {
	root := ocDir()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ocNodeBin(root), append([]string{ocEntryMJS(root)}, args...)...)
	cmd.Dir = root
	cmd.Env = ocBuildEnv(root, false)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func ocScrub(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}

// ---------- wizard ----------

var ocBundledProviders = map[string]string{ // id -> default API base
	"openai":    "https://api.openai.com/v1",
	"anthropic": "https://api.anthropic.com/v1",
	"deepseek":  "https://api.deepseek.com/v1",
}

var ocProviderIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type ocWizardProvider struct {
	ID      string `json:"id"`
	HasKey  bool   `json:"hasKey"`
	KeyTail string `json:"keyTail"`
	BaseURL string `json:"baseUrl"`
	Custom  bool   `json:"custom"`
}

func ocReadConfig() map[string]any {
	cfg := map[string]any{}
	if b, err := os.ReadFile(ocConfigPath()); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}

func ocWizardState() map[string]any {
	cfg := ocReadConfig()
	out := map[string]any{"providers": []ocWizardProvider{}, "primary": ""}
	models, _ := cfg["models"].(map[string]any)
	provs, _ := models["providers"].(map[string]any)
	list := []ocWizardProvider{}
	for id, raw := range provs {
		pm, _ := raw.(map[string]any)
		wp := ocWizardProvider{ID: id}
		if k, _ := pm["apiKey"].(string); k != "" {
			wp.HasKey = true
			if len(k) > 4 {
				wp.KeyTail = k[len(k)-4:]
			}
		}
		wp.BaseURL, _ = pm["baseUrl"].(string)
		_, bundled := ocBundledProviders[id]
		wp.Custom = !bundled
		list = append(list, wp)
	}
	out["providers"] = list
	if ag, _ := cfg["agents"].(map[string]any); ag != nil {
		if df, _ := ag["defaults"].(map[string]any); df != nil {
			if md, _ := df["model"].(map[string]any); md != nil {
				out["primary"], _ = md["primary"].(string)
			}
		}
	}
	out["configured"] = out["primary"] != ""
	return out
}

func ocWizardGetHandler(w http.ResponseWriter, r *http.Request) {
	if !ocInstalled() {
		writeJSON(w, map[string]any{"error": "先下载组件"})
		return
	}
	writeJSON(w, ocWizardState())
}

type ocWizardApplyBody struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseUrl"`
}

func ocWizardApply(body ocWizardApplyBody) (string, error) {
	prov := strings.ToLower(strings.TrimSpace(body.Provider))
	if !ocProviderIDRe.MatchString(prov) {
		return "", fmt.Errorf("厂商标识不合法（小写字母开头，可含数字、-、_）")
	}
	_, bundled := ocBundledProviders[prov]
	model := strings.TrimSpace(body.Model)
	key := strings.TrimSpace(body.APIKey)
	base := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")

	if !bundled {
		// Custom providers must be declared whole, models included.
		if base == "" || model == "" {
			return "", fmt.Errorf("自定义厂商必须填接口地址和模型名")
		}
		if key == "" {
			key = ocStoredKey(prov) // keep the stored key when only changing model/base
		}
		if key == "" {
			return "", fmt.Errorf("自定义厂商必须填 API Key")
		}
		obj := map[string]any{
			"baseUrl": base,
			"apiKey":  key,
			"api":     "openai-completions",
			"models":  []map[string]any{{"id": model, "name": model}},
		}
		jb, _ := json.Marshal(obj)
		out, err := ocRunCLI(90*time.Second, "config", "set", "models.providers."+prov, string(jb), "--strict-json")
		if err != nil {
			return "", fmt.Errorf("写入配置失败：%s", ocScrub(strings.TrimSpace(out), key))
		}
	} else {
		if key != "" {
			out, err := ocRunCLI(90*time.Second, "config", "set", "models.providers."+prov+".apiKey", key)
			if err != nil {
				return "", fmt.Errorf("写入 Key 失败：%s", ocScrub(strings.TrimSpace(out), key))
			}
		}
		if base != "" && base != ocBundledProviders[prov] {
			out, err := ocRunCLI(90*time.Second, "config", "set", "models.providers."+prov+".baseUrl", base)
			if err != nil {
				return "", fmt.Errorf("写入接口地址失败：%s", ocScrub(strings.TrimSpace(out), key))
			}
		}
	}
	if model != "" {
		out, err := ocRunCLI(90*time.Second, "models", "set", prov+"/"+model)
		if err != nil {
			return "", fmt.Errorf("设置默认模型失败：%s", ocScrub(strings.TrimSpace(out), key))
		}
	}
	return prov, nil
}

func ocStoredKey(prov string) string {
	cfg := ocReadConfig()
	models, _ := cfg["models"].(map[string]any)
	provs, _ := models["providers"].(map[string]any)
	pm, _ := provs[prov].(map[string]any)
	k, _ := pm["apiKey"].(string)
	return k
}

func ocWizardApplyHandler(w http.ResponseWriter, r *http.Request) {
	if !ocInstalled() {
		writeJSON(w, map[string]any{"error": "先下载组件"})
		return
	}
	var body ocWizardApplyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求不完整"})
		return
	}
	prov, err := ocWizardApply(body)
	if err != nil {
		logf("openclaw", "error", "小龙虾配置向导保存失败（厂商 %s）：%v", body.Provider, err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	logf("openclaw", "info", "小龙虾配置已保存：厂商 %s，默认模型 %s（Key 只写进组件配置，未进日志）", prov, strings.TrimSpace(body.Model))
	writeJSON(w, map[string]any{"ok": true})
}

func ocWizardTestHandler(w http.ResponseWriter, r *http.Request) {
	var body ocWizardApplyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求不完整"})
		return
	}
	prov := strings.ToLower(strings.TrimSpace(body.Provider))
	key := strings.TrimSpace(body.APIKey)
	if key == "" {
		key = ocStoredKey(prov)
	}
	if key == "" {
		writeJSON(w, map[string]any{"error": "还没有 Key：先填一个，或先保存已有的配置"})
		return
	}
	base := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	if base == "" {
		base = ocBundledProviders[prov]
	}
	if base == "" {
		cfg := ocReadConfig()
		models, _ := cfg["models"].(map[string]any)
		provs, _ := models["providers"].(map[string]any)
		pm, _ := provs[prov].(map[string]any)
		base, _ = pm["baseUrl"].(string)
		base = strings.TrimRight(base, "/")
	}
	if base == "" {
		writeJSON(w, map[string]any{"error": "不知道接口地址：自定义厂商要填接口地址"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		writeJSON(w, map[string]any{"error": "接口地址不合法"})
		return
	}
	if prov == "anthropic" {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := dlClient().Do(req)
	if err != nil {
		logf("openclaw", "warn", "小龙虾 Key 测试失败（厂商 %s）：%v", prov, err)
		writeJSON(w, map[string]any{"ok": false, "message": "连不上接口：" + err.Error()})
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusOK:
		logf("openclaw", "info", "小龙虾 Key 测试通过（厂商 %s）", prov)
		writeJSON(w, map[string]any{"ok": true, "message": "测通了：接口能连上、Key 有效"})
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		writeJSON(w, map[string]any{"ok": false, "message": fmt.Sprintf("接口拒绝了这个 Key（HTTP %d）——多半是 Key 不对或没权限", resp.StatusCode)})
	default:
		writeJSON(w, map[string]any{"ok": false, "message": fmt.Sprintf("接口返回 HTTP %d（能连上，但状态不正常）", resp.StatusCode)})
	}
}

// ---------- log tail ----------

func ocLogTailHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"lines": ocLogTail(120)})
}

// ---------- update check ----------

func ocCheckUpdateHandler(w http.ResponseWriter, r *http.Request) {
	ocManMu.Lock()
	ocManCache, ocManFetched = nil, time.Time{}
	ocManMu.Unlock()
	m, err := fetchOCManifestCached()
	if err != nil {
		writeJSON(w, map[string]any{"error": "检查更新失败：" + err.Error()})
		return
	}
	iv := ocInstalledVersion()
	writeJSON(w, map[string]any{
		"ok": true, "current": iv, "available": m.Version,
		"updateAvailable": iv != "" && m.Version != iv,
	})
}

// ---------- data export / import ----------

func ocExportHandler(w http.ResponseWriter, r *http.Request) {
	if !ocInstalled() {
		http.Error(w, "not installed", http.StatusBadRequest)
		return
	}
	stateDir := filepath.Join(ocDir(), "state")
	ts := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"mj-openclaw-data-%s.zip\"", ts))
	zw := zip.NewWriter(w)
	defer zw.Close()
	_ = filepath.WalkDir(stateDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(ocDir(), p)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(rel, filepath.Join("state", "logs")+string(os.PathSeparator)) {
			return nil // logs do not travel
		}
		if strings.HasPrefix(rel, filepath.Join("state", "npm")+string(os.PathSeparator)) {
			return nil // plugin download cache: re-created on demand, huge
		}
		if strings.HasPrefix(rel, filepath.Join("state", "tmp")+string(os.PathSeparator)) ||
			strings.HasPrefix(rel, filepath.Join("state", "cache")+string(os.PathSeparator)) {
			return nil // scratch/cache dirs: re-created on demand
		}
		if strings.HasSuffix(rel, ".lock") {
			return nil // stale lock files do not travel
		}
		fw, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		_, _ = io.Copy(fw, f)
		return nil
	})
	logf("openclaw", "info", "小龙虾数据已导出（配置+工作区，不含日志与插件缓存）")
}

func ocImportHandler(w http.ResponseWriter, r *http.Request) {
	if ocRunning() {
		writeJSON(w, map[string]any{"error": "小龙虾正在运行，先停止再导入"})
		return
	}
	if !ocInstalled() {
		writeJSON(w, map[string]any{"error": "先下载组件再导入数据"})
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, map[string]any{"error": "没有收到完整文件（上传中断或临时空间不足）：" + err.Error()})
		return
	}
	defer f.Close()
	tmpZip := filepath.Join(ocDir(), "import.tmp.zip")
	out, err := os.Create(tmpZip)
	if err != nil {
		writeJSON(w, map[string]any{"error": "临时文件写不了"})
		return
	}
	if _, err := io.Copy(out, io.LimitReader(f, 512*1024*1024)); err != nil {
		out.Close()
		_ = os.Remove(tmpZip)
		writeJSON(w, map[string]any{"error": "读取上传失败"})
		return
	}
	out.Close()
	defer os.Remove(tmpZip)

	zr, err := zip.OpenReader(tmpZip)
	if err != nil {
		writeJSON(w, map[string]any{"error": "这不是有效的 zip 文件"})
		return
	}
	defer zr.Close()
	stateDir := filepath.Join(ocDir(), "state")
	bakDir := filepath.Join(ocDir(), "state.bak-"+time.Now().Format("20060102-150405"))
	moved := false
	for _, zf := range zr.File {
		name := filepath.Clean(filepath.FromSlash(zf.Name))
		if !strings.HasPrefix(name, "state"+string(os.PathSeparator)) && name != "state" {
			continue
		}
		if strings.Contains(name, "..") {
			writeJSON(w, map[string]any{"error": "压缩包里有不安全的路径，已拒绝"})
			return
		}
		if !moved {
			if _, err := os.Stat(stateDir); err == nil {
				if err := os.Rename(stateDir, bakDir); err != nil {
					writeJSON(w, map[string]any{"error": "备份现有数据失败：" + err.Error()})
					return
				}
			}
			moved = true
		}
		if zf.FileInfo().IsDir() {
			_ = os.MkdirAll(filepath.Join(ocDir(), name), 0o755)
			continue
		}
		dst := filepath.Join(ocDir(), name)
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		wf, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = io.Copy(wf, io.LimitReader(rc, 512*1024*1024))
			_ = wf.Close()
		}
		_ = rc.Close()
	}
	if !moved {
		writeJSON(w, map[string]any{"error": "压缩包里没有 state 数据（要用本页导出的文件）"})
		return
	}
	if err := ocProvision(); err != nil {
		logf("openclaw", "warn", "导入后整理配置失败：%v", err)
	}
	logf("openclaw", "info", "小龙虾数据已导入（旧数据备份在 %s）", filepath.Base(bakDir))
	writeJSON(w, map[string]any{"ok": true, "message": "导入完成。旧数据没有删，备份在组件目录的 " + filepath.Base(bakDir) + "，确认没问题后可自行删除"})
}
