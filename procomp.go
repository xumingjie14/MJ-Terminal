package main

// ---------- productivity components (v0.51) ----------
//
// Five open-source, portable, good-looking productivity apps ship
// as on-demand components, generalizing the Firefox/OpenClaw
// pattern (firefox.go / openclaw.go): each is one zip + one
// manifest JSON (version/file/size/sha256) on the same GitHub
// Release, assembled by CI in build.yml from the vendor's official
// package (versions pinned in the workflow). The daemon downloads
// a component into Data/components/<id>/, verifies size + SHA-256
// before extracting, and registers it as a desktop app. The main
// EXE and the main update package never grow because of them.
//
// Data containment (the point of the exercise -- user data must
// live in Data, not scatter over the host):
//   - ONLYOFFICE: no documented portable/data-dir switch exists,
//     so the daemon launches it with APPDATA / LOCALAPPDATA / TEMP
//     redirected into the component's profile/ folder. Whether a
//     given build resolves those folders from the environment is
//     verified on a real machine (acceptance list); the release
//     notes say exactly this.
//   - LibreOffice: vendor-supported -env:UserInstallation launch
//     argument points the whole user profile into the component.
//   - ImageGlass: the official portable zip keeps its settings
//     (igconfig.xml) in the program folder, which is already
//     inside Data -- nothing to redirect.
//   - Krita: vendor-supported --resource-location launch argument
//     points the resource folder (brushes, presets, workspaces,
//     resource database) into the component. Krita's small config
//     files (kritarc & friends) have NO vendor redirect and stay
//     in the host's %LOCALAPPDATA% -- disclosed, not hidden.
//   - VSCodium: official portable mode -- a "data" folder next to
//     the exe holds all user data and extensions; the daemon
//     pre-creates it inside the component dir (which is in Data).
//
// Launches go through the same claiming path as store apps
// (window snapshot -> noteSpawnedProcess -> trackAppLaunch), so
// Stage Manager claiming/landing, immersion and the exit-time
// close-apps sweep treat them like every other external app.

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
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

type prodComponent struct {
	ID       string
	Name     string // desktop / display name
	Manifest string // manifest JSON file name on the Release
	ExeRel   string // main exe, slash path inside the install dir
	Blurb    string // one-line description for Settings
}

// prodComponents is the fixed shelf. ExeRel values match the
// pinned CI assembly in build-v51.yml (LibreOffice is repacked
// flat from the MSI admin image; the other four ship their
// upstream zip layouts unchanged).
var prodComponents = []prodComponent{
	{ID: "onlyoffice", Name: "ONLYOFFICE 文档", Manifest: "onlyoffice.json", ExeRel: "DesktopEditors.exe", Blurb: "文档 / 表格 / 演示 · ONLYOFFICE Desktop Editors（开源）"},
	{ID: "libreoffice", Name: "LibreOffice", Manifest: "libreoffice.json", ExeRel: "program/soffice.exe", Blurb: "文档 / 表格 / 演示 · LibreOffice 完整套件（开源）"},
	{ID: "imageglass", Name: "ImageGlass 看图", Manifest: "imageglass.json", ExeRel: "ImageGlass_10.0.6.906_win-x64/ImageGlass.exe", Blurb: "图片查看 · ImageGlass（开源，80+ 格式）"},
	{ID: "krita", Name: "Krita 绘画", Manifest: "krita.json", ExeRel: "krita-x64-6.0.4.1/bin/krita.exe", Blurb: "绘画与图像编辑 · Krita（开源）"},
	{ID: "vscodium", Name: "VSCodium 代码", Manifest: "vscodium.json", ExeRel: "VSCodium.exe", Blurb: "代码编辑 · VSCodium（VS Code 的开源构建）"},
}

func prodByID(id string) (prodComponent, bool) {
	for _, c := range prodComponents {
		if c.ID == id {
			return c, true
		}
	}
	return prodComponent{}, false
}

// ---------- paths & install state ----------

func compRoot() string { return filepath.Join(dataDir(), "components") }

func compDir(id string) string { return filepath.Join(compRoot(), id) }

func compExePath(c prodComponent) string {
	return filepath.Join(compDir(c.ID), filepath.FromSlash(c.ExeRel))
}

func compInstalled(c prodComponent) bool {
	_, err := os.Stat(compExePath(c))
	return err == nil
}

func compVersionPath(c prodComponent) string {
	return filepath.Join(compDir(c.ID), "mj-version.txt")
}

func compInstalledVersion(c prodComponent) string {
	b, err := os.ReadFile(compVersionPath(c))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func compDiskBytes(c prodComponent) int64 {
	var total int64
	_ = filepath.WalkDir(compDir(c.ID), func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, ierr := d.Info(); ierr == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// ---------- per-component install state ----------

type compInstState struct {
	mu       sync.Mutex
	Phase    string // idle|downloading|extracting|ready|error
	Progress int64
	Size     int64
	Message  string
}

var compStates = struct {
	sync.Mutex
	m map[string]*compInstState
}{m: map[string]*compInstState{}}

func compState(id string) *compInstState {
	compStates.Lock()
	defer compStates.Unlock()
	s, ok := compStates.m[id]
	if !ok {
		s = &compInstState{Phase: "idle"}
		compStates.m[id] = s
	}
	return s
}

func (s *compInstState) set(c prodComponent, phase, msg string) {
	switch phase {
	case "error":
		logf(c.ID, "error", "%s 组件：%s", c.Name, msg)
	case "ready":
		logf(c.ID, "info", "%s 组件：%s", c.Name, msg)
	}
	s.mu.Lock()
	s.Phase, s.Message = phase, msg
	s.mu.Unlock()
}

func (s *compInstState) snapshot() (string, int64, int64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Phase, s.Progress, s.Size, s.Message
}

func (s *compInstState) busy() bool {
	p, _, _, _ := s.snapshot()
	return p == "downloading" || p == "extracting"
}

// ---------- manifest & download ----------

// compManifest mirrors ffManifest / the OpenClaw manifest shape:
// the Release carries one JSON per component next to its zip.
type compManifest struct {
	Version string `json:"version"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

func compBase() string {
	if v := strings.TrimSpace(os.Getenv("MJ_COMP_BASE")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "https://github.com/" + updateRepo + "/releases/latest/download"
}

func fetchCompManifest(ctx context.Context, c prodComponent) (*compManifest, error) {
	resp, err := dlGet(ctx, compBase()+"/"+c.Manifest, "MJ-Terminal-Components/"+AppVersion)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回 %d", resp.StatusCode)
	}
	var m compManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&m); err != nil {
		return nil, err
	}
	if m.File == "" || m.Version == "" {
		return nil, fmt.Errorf("清单内容不完整")
	}
	return &m, nil
}

func compStatusList(w http.ResponseWriter, r *http.Request) {
	type avail struct {
		m   *compManifest
		err error
	}
	avails := make([]avail, len(prodComponents))
	var wg sync.WaitGroup
	for i, c := range prodComponents {
		wg.Add(1)
		go func(i int, c prodComponent) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			m, err := fetchCompManifest(ctx, c)
			avails[i] = avail{m, err}
		}(i, c)
	}
	wg.Wait()
	out := make([]map[string]any, 0, len(prodComponents))
	for i, c := range prodComponents {
		phase, prog, size, msg := compState(c.ID).snapshot()
		inst := compInstalled(c)
		row := map[string]any{
			"id": c.ID, "name": c.Name, "blurb": c.Blurb,
			"installed": inst, "version": compInstalledVersion(c),
			"phase": phase, "progress": prog, "size": size, "message": msg,
		}
		if inst {
			row["diskBytes"] = compDiskBytes(c)
			// The icon endpoint also extracts on demand, so an
			// installed component always has an icon to show.
			row["hasIcon"] = true
		}
		if avails[i].err == nil {
			row["available"] = avails[i].m.Version
			row["availableSize"] = avails[i].m.Size
			iv := compInstalledVersion(c)
			row["updateAvailable"] = inst && iv != "" && avails[i].m.Version != iv
		}
		out = append(out, row)
	}
	writeJSON(w, map[string]any{"components": out})
}

func compInstallHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	c, ok := prodByID(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "没有这个组件"})
		return
	}
	if compState(c.ID).busy() {
		writeJSON(w, map[string]any{"ok": true, "message": "已经在下载了"})
		return
	}
	logf(c.ID, "info", "开始下载 %s 组件", c.Name)
	go runCompInstall(c)
	writeJSON(w, map[string]any{"ok": true, "message": "开始下载"})
}

func runCompInstall(c prodComponent) {
	st := compState(c.ID)
	ensureCoreForDownload()
	st.set(c, "downloading", "正在读取组件清单…")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	m, err := fetchCompManifest(ctx, c)
	if err != nil {
		st.set(c, "error", "读取组件清单失败："+err.Error())
		return
	}
	st.mu.Lock()
	st.Size = m.Size
	st.Progress = 0
	st.mu.Unlock()
	st.set(c, "downloading", "正在下载 "+c.Name+"（"+downloadRoute()+"）…")

	resp, err := dlGet(ctx, compBase()+"/"+m.File, "MJ-Terminal-Components/"+AppVersion)
	if err != nil {
		st.set(c, "error", "下载失败："+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		st.set(c, "error", fmt.Sprintf("下载失败：服务器返回 %d", resp.StatusCode))
		return
	}
	_ = os.MkdirAll(updateDir(), 0o755)
	part := filepath.Join(updateDir(), "comp-"+c.ID+".zip.part")
	f, err := os.Create(part)
	if err != nil {
		st.set(c, "error", "下载失败："+err.Error())
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
				st.set(c, "error", "下载失败（写入）："+werr.Error())
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
			st.set(c, "error", "下载中断："+rerr.Error())
			return
		}
	}
	_ = f.Close()
	if m.Size > 0 && written != m.Size {
		st.set(c, "error", fmt.Sprintf("下载不完整：大小对不上（%d / %d 字节）", written, m.Size))
		return
	}
	if m.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), m.SHA256) {
		st.set(c, "error", "校验失败：SHA-256 对不上，已放弃这次安装")
		return
	}
	zipPath := filepath.Join(updateDir(), "comp-"+c.ID+".zip")
	_ = os.Remove(zipPath)
	if err := os.Rename(part, zipPath); err != nil {
		st.set(c, "error", "下载失败："+err.Error())
		return
	}

	st.set(c, "extracting", "正在释放 "+c.Name+"…")
	_ = os.MkdirAll(compRoot(), 0o755)
	tmpDir := compDir(c.ID) + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := extractZipTo(zipPath, tmpDir); err != nil {
		st.set(c, "error", "释放失败："+err.Error())
		return
	}
	exeInTmp := filepath.Join(tmpDir, filepath.FromSlash(c.ExeRel))
	if _, err := os.Stat(exeInTmp); err != nil {
		_ = os.RemoveAll(tmpDir)
		st.set(c, "error", "组件包里没找到主程序 "+c.ExeRel)
		return
	}
	dst := compDir(c.ID)
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmpDir, dst); err != nil {
		st.set(c, "error", "安装失败："+err.Error())
		return
	}
	_ = os.WriteFile(compVersionPath(c), []byte(m.Version), 0o644)
	compPrepare(c) // per-component first-run layout (portable data dirs)
	if ico, err := exeIconICO(compExePath(c)); err == nil && len(ico) > 0 {
		_ = os.WriteFile(filepath.Join(dst, "mj-icon.ico"), ico, 0o644)
	}
	_ = os.Remove(zipPath)
	st.set(c, "ready", "安装完成："+c.Name+" "+m.Version)
	// v0.54: an app-category notification (通知中心 → 应用通知）.
	postAppNotify("prod-"+c.ID, c.Name, "installed",
		c.Name+" 安装完成", c.Name+" "+m.Version+" 已就绪，在桌面或启动台点图标即可打开", "setpage:components")
}

func compUninstallHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	c, ok := prodByID(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "没有这个组件"})
		return
	}
	if compState(c.ID).busy() {
		writeJSON(w, map[string]any{"error": "正在下载/安装中，等它完成再卸载"})
		return
	}
	if err := os.RemoveAll(compDir(c.ID)); err != nil {
		logf(c.ID, "error", "卸载 %s 失败：%v", c.Name, err)
		writeJSON(w, map[string]any{"error": "卸载失败（程序可能还在运行，先关掉它）：" + err.Error()})
		return
	}
	st := compState(c.ID)
	st.mu.Lock()
	st.Phase, st.Message = "idle", ""
	st.mu.Unlock()
	logf(c.ID, "info", "已卸载 %s 组件（程序与它的数据一并删除）", c.Name)
	writeJSON(w, map[string]any{"ok": true})
}

// compPrepare creates the per-component portable data layout the
// launch relies on. Safe to call repeatedly.
func compPrepare(c prodComponent) {
	switch c.ID {
	case "vscodium":
		// Official portable mode: a "data" folder next to the exe
		// holds user data + extensions.
		_ = os.MkdirAll(filepath.Join(compDir(c.ID), "data"), 0o755)
	case "libreoffice":
		_ = os.MkdirAll(filepath.Join(compDir(c.ID), "profile"), 0o755)
	case "krita":
		_ = os.MkdirAll(filepath.Join(compDir(c.ID), "profile", "resource"), 0o755)
	case "onlyoffice":
		base := filepath.Join(compDir(c.ID), "profile")
		_ = os.MkdirAll(filepath.Join(base, "Roaming"), 0o755)
		_ = os.MkdirAll(filepath.Join(base, "Local"), 0o755)
		_ = os.MkdirAll(filepath.Join(base, "Temp"), 0o755)
	}
}

// ---------- launch (with data containment) ----------

func setEnvVar(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(prefix)) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// fileURLPath turns a Windows absolute path into a file:/// URL
// (LibreOffice's -env:UserInstallation wants the URL form).
func fileURLPath(abs string) string {
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}
	return u.String()
}

// compLaunchSpec computes the launch arguments and environment
// for one component. Pure apart from directory creation in
// compPrepare, so the containment wiring is unit-testable.
func compLaunchSpec(c prodComponent, fileAbs string) (args []string, env []string) {
	switch c.ID {
	case "libreoffice":
		args = append(args, "-env:UserInstallation="+fileURLPath(filepath.Join(compDir(c.ID), "profile")))
	case "krita":
		args = append(args, "--resource-location", filepath.Join(compDir(c.ID), "profile", "resource"))
	case "onlyoffice":
		base := filepath.Join(compDir(c.ID), "profile")
		env = os.Environ()
		env = setEnvVar(env, "APPDATA", filepath.Join(base, "Roaming"))
		env = setEnvVar(env, "LOCALAPPDATA", filepath.Join(base, "Local"))
		env = setEnvVar(env, "TEMP", filepath.Join(base, "Temp"))
		env = setEnvVar(env, "TMP", filepath.Join(base, "Temp"))
	}
	if fileAbs != "" {
		args = append(args, fileAbs)
	}
	return args, env
}

func launchComponent(c prodComponent, fileAbs string) error {
	exePath := compExePath(c)
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("还没下载 %s 组件", c.Name)
	}
	if runtime.GOOS != "windows" {
		return fmt.Errorf("当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开")
	}
	compPrepare(c)
	args, env := compLaunchSpec(c, fileAbs)
	before := platformSnapshotWindows(exePath)
	cmd := exec.Command(exePath, args...)
	cmd.Dir = compDir(c.ID)
	if env != nil {
		cmd.Env = env
	}
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败：%w", err)
	}
	noteSpawnedProcess(uint32(cmd.Process.Pid), c.Name)
	trackAppLaunch(c.Name, exePath, before)
	if fileAbs != "" {
		logf(c.ID, "info", "已用 %s 打开 %s", c.Name, filepath.Base(fileAbs))
	} else {
		logf(c.ID, "info", "已启动 %s", c.Name)
	}
	return nil
}

func compLaunchHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	c, ok := prodByID(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "没有这个组件"})
		return
	}
	if err := launchComponent(c, ""); err != nil {
		logf(c.ID, "error", "启动 %s 失败：%v", c.Name, err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- file-open routing ----------

var compOfficeExts = map[string]bool{
	".docx": true, ".xlsx": true, ".pptx": true,
	".odt": true, ".ods": true, ".odp": true,
	".csv": true, ".tsv": true,
}

// routeFileOpen decides which installed component (if any) should
// open a Data/files entry. kind/ext come from the file classifier.
// Returns (componentToUse, componentToInstall): at most one is
// non-empty; both empty means "use the built-in viewer/panel".
// Office formats prefer the configured default suite and fall
// back to the other suite; with neither installed they ask for
// the default suite's install. Images and text/code fall back to
// the built-in viewers quietly -- those viewers really work, so
// there is nothing to guide about.
func routeFileOpen(kind, ext string, installed map[string]bool, defaultOffice string) (string, string) {
	if compOfficeExts[ext] {
		pref := defaultOffice
		if !officeSuites[pref] {
			pref = "onlyoffice"
		}
		other := "libreoffice"
		if pref == "libreoffice" {
			other = "onlyoffice"
		}
		if installed[pref] {
			return pref, ""
		}
		if installed[other] {
			return other, ""
		}
		return "", pref
	}
	switch kind {
	case "image":
		if installed["imageglass"] {
			return "imageglass", ""
		}
	case "text":
		if installed["vscodium"] {
			return "vscodium", ""
		}
	}
	return "", ""
}

func compOpenFileHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	abs, err := filesResolve(body.Path)
	if err != nil {
		writeJSON(w, map[string]any{"action": "builtin"})
		return
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		writeJSON(w, map[string]any{"action": "builtin"})
		return
	}
	kind := fileKindOf(fi.Name(), false)
	ext := strings.ToLower(filepath.Ext(fi.Name()))
	installed := make(map[string]bool, len(prodComponents))
	for _, c := range prodComponents {
		installed[c.ID] = compInstalled(c)
	}
	comp, need := routeFileOpen(kind, ext, installed, settingDefaultOfficeSuite())
	switch {
	case comp != "":
		c, _ := prodByID(comp)
		if err := launchComponent(c, abs); err != nil {
			logf(c.ID, "error", "用 %s 打开 %s 失败：%v", c.Name, fi.Name(), err)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"action": "launched", "component": comp, "name": c.Name})
	case need != "":
		c, _ := prodByID(need)
		writeJSON(w, map[string]any{"action": "install", "component": need, "name": c.Name})
	default:
		writeJSON(w, map[string]any{"action": "builtin"})
	}
}

// ---------- real icon, extracted from the component's own exe ----------

func compIconHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	c, ok := prodByID(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	icoPath := filepath.Join(compDir(c.ID), "mj-icon.ico")
	if b, err := os.ReadFile(icoPath); err == nil && len(b) > 0 {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(b)
		return
	}
	if compInstalled(c) {
		if ico, err := exeIconICO(compExePath(c)); err == nil && len(ico) > 0 {
			_ = os.WriteFile(icoPath, ico, 0o644)
			w.Header().Set("Content-Type", "image/x-icon")
			w.Header().Set("Cache-Control", "max-age=3600")
			_, _ = w.Write(ico)
			return
		}
	}
	http.NotFound(w, r)
}

// exeIconICO extracts the best icon from a Windows PE's resources
// and returns a standalone .ico file (browsers render .ico in
// <img> directly, and the payload may be PNG or BMP -- both are
// valid inside an ICO container). Any structural surprise is an
// error; callers fall back to the letter tile.
func exeIconICO(exePath string) ([]byte, error) {
	f, err := pe.Open(exePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var sec *pe.Section
	for _, s := range f.Sections {
		if s.Name == ".rsrc" {
			sec = s
			break
		}
	}
	if sec == nil {
		return nil, fmt.Errorf("没有资源节")
	}
	data, err := sec.Data()
	if err != nil {
		return nil, err
	}
	base := sec.VirtualAddress
	iconFor := func(id uint32) []byte {
		b, err := rsrcLookup(data, base, 3, id) // RT_ICON
		if err != nil {
			return nil
		}
		return b
	}
	group, err := rsrcLookup(data, base, 14, 0) // RT_GROUP_ICON, any id
	if err != nil {
		return nil, err
	}
	return buildICO(group, iconFor)
}

// rsrcLookup walks the three-level resource tree:
// type -> name/id -> language, returning the first leaf's data.
// wantID 0 means "any id" (used for the icon group).
func rsrcLookup(data []byte, baseRVA, wantType, wantID uint32) ([]byte, error) {
	entries, err := rsrcEntries(data, 0)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.id != wantType || !e.isDir {
			continue
		}
		level2, err := rsrcEntries(data, int(e.offset))
		if err != nil {
			return nil, err
		}
		for _, e2 := range level2 {
			if wantID != 0 && e2.id != wantID {
				continue
			}
			if !e2.isDir {
				continue
			}
			level3, err := rsrcEntries(data, int(e2.offset))
			if err != nil {
				return nil, err
			}
			for _, e3 := range level3 {
				if e3.isDir {
					continue
				}
				return rsrcData(data, baseRVA, int(e3.offset))
			}
		}
	}
	return nil, fmt.Errorf("资源里找不到目标条目")
}

type rsrcEntry struct {
	id     uint32
	offset uint32 // subdirectory offset (relative to section start)
	isDir  bool
}

func rsrcEntries(data []byte, off int) ([]rsrcEntry, error) {
	if off < 0 || off+16 > len(data) {
		return nil, fmt.Errorf("资源目录越界")
	}
	named := int(binary.LittleEndian.Uint16(data[off+12:]))
	ids := int(binary.LittleEndian.Uint16(data[off+14:]))
	total := named + ids
	if total < 0 || total > 512 || off+16+total*8 > len(data) {
		return nil, fmt.Errorf("资源目录条目数异常")
	}
	out := make([]rsrcEntry, 0, total)
	for i := 0; i < total; i++ {
		p := off + 16 + i*8
		name := binary.LittleEndian.Uint32(data[p:])
		target := binary.LittleEndian.Uint32(data[p+4:])
		out = append(out, rsrcEntry{
			id:     name & 0x7fffffff,
			offset: target & 0x7fffffff,
			isDir:  target&0x80000000 != 0,
		})
	}
	return out, nil
}

func rsrcData(data []byte, baseRVA uint32, off int) ([]byte, error) {
	if off < 0 || off+16 > len(data) {
		return nil, fmt.Errorf("资源数据条目越界")
	}
	rva := binary.LittleEndian.Uint32(data[off:])
	size := binary.LittleEndian.Uint32(data[off+4:])
	start := int64(rva) - int64(baseRVA)
	if start < 0 || start+int64(size) > int64(len(data)) || size == 0 {
		return nil, fmt.Errorf("资源数据越界")
	}
	return data[start : start+int64(size)], nil
}

// buildICO assembles a one-image .ico from a RT_GROUP_ICON blob
// plus a resolver for the RT_ICON payloads. It picks the entry
// with the largest payload (normally the 256x256 PNG).
func buildICO(group []byte, iconFor func(id uint32) []byte) ([]byte, error) {
	if len(group) < 6 {
		return nil, fmt.Errorf("图标组太短")
	}
	if binary.LittleEndian.Uint16(group[0:]) != 0 || binary.LittleEndian.Uint16(group[2:]) != 1 {
		return nil, fmt.Errorf("图标组格式不对")
	}
	count := int(binary.LittleEndian.Uint16(group[4:]))
	if count < 1 || count > 64 || 6+count*14 > len(group) {
		return nil, fmt.Errorf("图标组条目数异常")
	}
	type cand struct {
		hdr  []byte // the 14-byte GRPICONDIR entry
		id   uint32
		size uint32
	}
	cands := make([]cand, 0, count)
	for i := 0; i < count; i++ {
		p := 6 + i*14
		e := group[p : p+14]
		cands = append(cands, cand{
			hdr:  e,
			id:   uint32(binary.LittleEndian.Uint16(e[12:])),
			size: binary.LittleEndian.Uint32(e[8:]),
		})
	}
	// Largest payload first; first one that resolves wins.
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].size > cands[i].size {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	for _, cd := range cands {
		icon := iconFor(cd.id)
		if len(icon) == 0 {
			continue
		}
		out := make([]byte, 0, 22+len(icon))
		hdr := make([]byte, 6)
		binary.LittleEndian.PutUint16(hdr[2:], 1)
		binary.LittleEndian.PutUint16(hdr[4:], 1)
		out = append(out, hdr...)
		entry := make([]byte, 16)
		copy(entry[0:4], cd.hdr[0:4])                                                    // width, height, colors, reserved
		binary.LittleEndian.PutUint16(entry[4:], binary.LittleEndian.Uint16(cd.hdr[4:])) // planes
		binary.LittleEndian.PutUint16(entry[6:], binary.LittleEndian.Uint16(cd.hdr[6:])) // bit count
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(icon)))
		binary.LittleEndian.PutUint32(entry[12:], 22)
		out = append(out, entry...)
		out = append(out, icon...)
		return out, nil
	}
	return nil, fmt.Errorf("图标组里没有可用的图标数据")
}
