// MJ Light System v0.2 - lightweight HTML desktop with REAL system functions.
//
// Key v0.2 feature for shared/public PCs: an isolated Google Chrome window
// whose traffic -- and only its traffic -- goes through a sing-box node.
//   - No system-wide proxy is ever changed: the public PC's own browser and
//     apps stay direct, this window uses --proxy-server=socks5://127.0.0.1:2080.
//   - A separate --user-data-dir (next to the EXE, in Data/ChromeProfile) is
//     essential: Chrome ignores --proxy-server when attaching to an already
//     running instance with the default profile. The separate profile also
//     keeps history/cookies/logins out of the PC owner's own Chrome, and the
//     whole Data folder travels with the EXE / can be deleted afterwards.
//   - No bundled Chromium, no VM: keeps old PCs fast.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed index.html
var indexHTML []byte

// sing-box (official 1.14.4, legacy Windows 7 amd64 build) is embedded
// COMPRESSED (~26MB zip for an 80MB exe) so the whole system ships as
// ONE single MJ-System.exe file. On first run the daemon extracts it
// to core/sing-box.exe next to the EXE (see extractEmbeddedCore).
//
//go:embed singbox.zip
var singboxZip []byte

const proxyPort = 2080

var (
	termCwd string
	mu      sync.Mutex
	coreMu  sync.Mutex
	coreCmd *exec.Cmd
	// Atomics: written by the Wait() goroutine, read from handlers that
	// may already hold coreMu (Go mutexes are not re-entrant).
	coreExited  atomic.Bool  // set when sing-box really dies
	coreExitErr atomic.Value // string: why it exited ("" = clean/stopped)
	coreStopReq atomic.Bool  // user pressed stop, so the exit is expected
	nodes       []nodeEntry
)

type nodeEntry struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Server   string         `json:"server"`
	Port     int            `json:"port"`
	Raw      string         `json:"-"`
	Outbound map[string]any `json:"-"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return string(filepath.Separator)
	}
	return h
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(p)
}

// dataDir keeps ALL personal data (browser profiles, saved nodes) next to
// the EXE so a public PC keeps almost nothing: take the folder away and
// the data goes with it. Falls back to LocalAppData if the EXE folder is
// not writable.
func dataDir() string {
	d := filepath.Join(exeDir(), "Data")
	if err := os.MkdirAll(d, 0o755); err == nil {
		probe := filepath.Join(d, ".write-test")
		if err := os.WriteFile(probe, []byte("ok"), 0o600); err == nil {
			_ = os.Remove(probe)
			return d
		}
	}
	d = filepath.Join(os.Getenv("LocalAppData"), "MJLight")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// ---------- custom wallpaper (stored in Data/, travels with the EXE) ----------

func wallpaperFile() string     { return filepath.Join(dataDir(), "wallpaper.bin") }
func wallpaperTypeFile() string { return filepath.Join(dataDir(), "wallpaper.ctype") }

func wallpaperHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(wallpaperFile())
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ct, _ := os.ReadFile(wallpaperTypeFile())
		if len(ct) == 0 {
			ct = []byte("image/jpeg")
		}
		w.Header().Set("Content-Type", string(ct))
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 25*1024*1024))
		if err != nil {
			writeJSON(w, map[string]any{"error": "图片太大或读取失败（上限 25MB）"})
			return
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "image/") {
			ct = "image/jpeg"
		}
		if err := os.WriteFile(wallpaperFile(), body, 0o644); err != nil {
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		_ = os.WriteFile(wallpaperTypeFile(), []byte(ct), 0o644)
		writeJSON(w, map[string]any{"ok": true, "size": len(body)})
	case http.MethodDelete:
		_ = os.Remove(wallpaperFile())
		_ = os.Remove(wallpaperTypeFile())
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func sysInfo(w http.ResponseWriter, r *http.Request) {
	host, _ := os.Hostname()
	writeJSON(w, map[string]any{
		"app": "MJ-Terminal", "version": AppVersion,
		"os": runtime.GOOS, "arch": runtime.GOARCH, "numCPU": runtime.NumCPU(),
		"hostname": host, "home": homeDir(), "exeDir": exeDir(), "dataDir": dataDir(),
		"time": time.Now().Format("2006-01-02 15:04:05"),
	})
}

type fileItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

func listFiles(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = homeDir()
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error(), "path": abs})
		return
	}
	items := []fileItem{}
	for _, e := range ents {
		var sz int64
		if info, err := e.Info(); err == nil {
			sz = info.Size()
		}
		items = append(items, fileItem{Name: e.Name(), Path: filepath.Join(abs, e.Name()), Dir: e.IsDir(), Size: sz})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Dir != items[j].Dir {
			return items[i].Dir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	writeJSON(w, map[string]any{"path": abs, "items": items})
}

func runCmd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cmd string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "bad request"})
		return
	}
	cmdStr := strings.TrimSpace(body.Cmd)
	mu.Lock()
	defer mu.Unlock()
	if termCwd == "" {
		termCwd = homeDir()
	}
	lower := strings.ToLower(cmdStr)
	if lower == "cd" || lower == "pwd" {
		writeJSON(w, map[string]any{"output": termCwd, "cwd": termCwd})
		return
	}
	if strings.HasPrefix(lower, "cd ") || strings.HasPrefix(lower, "chdir ") {
		target := strings.TrimSpace(cmdStr[strings.Index(cmdStr, " ")+1:])
		target = strings.Trim(target, `"`)
		if target == "~" {
			target = homeDir()
		}
		np := target
		if !filepath.IsAbs(np) {
			np = filepath.Join(termCwd, np)
		}
		if st, err := os.Stat(np); err == nil && st.IsDir() {
			termCwd = np
			writeJSON(w, map[string]any{"output": "", "cwd": termCwd})
		} else {
			writeJSON(w, map[string]any{"output": "目录不存在: " + np, "cwd": termCwd})
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd.exe", "/C", cmdStr)
	} else {
		c = exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
	}
	c.Dir = termCwd
	hideConsole(c)
	if proxyActive() {
		// v0.8 in-app global acceleration: tools that honor proxy env
		// vars (curl, git, pip, npm, ...) go through the node. Only
		// this child process gets the vars; the host PC is untouched.
		px := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
		sx := fmt.Sprintf("socks5://127.0.0.1:%d", proxyPort)
		c.Env = append(os.Environ(),
			"HTTP_PROXY="+px, "HTTPS_PROXY="+px, "ALL_PROXY="+sx,
			"http_proxy="+px, "https_proxy="+px, "all_proxy="+sx,
			"NO_PROXY=127.0.0.1,localhost", "no_proxy=127.0.0.1,localhost")
	}
	out, err := c.CombinedOutput()
	s := string(out)
	if len(s) > 64*1024 {
		s = s[:64*1024] + "\n…(输出过长，已截断)"
	}
	resp := map[string]any{"output": s, "cwd": termCwd}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, resp)
}

func openPath(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		URL  string `json:"url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	target := body.Path
	if body.URL != "" {
		target = body.URL
	}
	if target == "" {
		writeJSON(w, map[string]any{"error": "empty target"})
		return
	}
	// v0.8 in-app global acceleration: a link opened from inside the
	// system goes through the node (isolated Chrome), not to the host
	// PC's default browser. Falls back to the default browser when
	// the isolated one cannot start.
	if body.URL != "" && runtime.GOOS == "windows" && accelEnabled() {
		if err := launchIsolatedBrowser(true, target); err == nil {
			writeJSON(w, map[string]any{"ok": true, "via": "node"})
			return
		}
	}
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if body.URL != "" {
			c = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
		} else {
			c = exec.Command("explorer.exe", target)
		}
	case "darwin":
		c = exec.Command("open", target)
	default:
		c = exec.Command("xdg-open", target)
	}
	if err := c.Start(); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- node (proxy) parsing: share links -> sing-box outbounds ----------

func b64decode(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("base64 decode failed")
}

func splitHostPort(hp string) (string, int, error) {
	h, ps, err := net.SplitHostPort(hp)
	if err != nil {
		// maybe no port brackets handling needed; try last colon
		i := strings.LastIndex(hp, ":")
		if i < 0 {
			return "", 0, fmt.Errorf("no port in %q", hp)
		}
		h, ps = hp[:i], hp[i+1:]
		h = strings.Trim(h, "[]")
	}
	p, err := strconv.Atoi(ps)
	if err != nil {
		return "", 0, err
	}
	return h, p, nil
}

func addTransport(out map[string]any, network, path, host, serviceName string) {
	switch network {
	case "ws":
		t := map[string]any{"type": "ws", "path": path}
		if host != "" {
			t["headers"] = map[string]any{"Host": host}
		}
		out["transport"] = t
	case "grpc":
		out["transport"] = map[string]any{"type": "grpc", "service_name": serviceName}
	}
}

func addTLS(out map[string]any, serverName string, insecure bool, reality map[string]any, utlsFP string) {
	tls := map[string]any{"enabled": true}
	if serverName != "" {
		tls["server_name"] = serverName
	}
	if insecure {
		tls["insecure"] = true
	}
	if utlsFP != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": utlsFP}
	}
	if reality != nil {
		tls["reality"] = reality
	}
	out["tls"] = tls
}

func parseSS(raw string) (*nodeEntry, error) {
	s := strings.TrimPrefix(raw, "ss://")
	name := ""
	if i := strings.Index(s, "#"); i >= 0 {
		n, _ := url.QueryUnescape(s[i+1:])
		name = n
		s = s[:i]
	}
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i] // plugin options ignored in this version
	}
	var method, password, hostport string
	if strings.Contains(s, "@") {
		at := strings.LastIndex(s, "@")
		userinfo, hp := s[:at], s[at+1:]
		if dec, err := b64decode(userinfo); err == nil && strings.Contains(dec, ":") {
			userinfo = dec
		} else if u, err := url.QueryUnescape(userinfo); err == nil {
			userinfo = u
		}
		c := strings.Index(userinfo, ":")
		if c < 0 {
			return nil, fmt.Errorf("ss: bad userinfo")
		}
		method, password, hostport = userinfo[:c], userinfo[c+1:], hp
	} else {
		dec, err := b64decode(s)
		if err != nil {
			return nil, err
		}
		at := strings.LastIndex(dec, "@")
		if at < 0 {
			return nil, fmt.Errorf("ss: bad format")
		}
		c := strings.Index(dec[:at], ":")
		method, password, hostport = dec[:c], dec[c+1:at], dec[at+1:]
	}
	host, port, err := splitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = host
	}
	return &nodeEntry{Name: name, Type: "shadowsocks", Server: host, Port: port, Raw: raw,
		Outbound: map[string]any{"type": "shadowsocks", "server": host, "server_port": port, "method": method, "password": password}}, nil
}

func parseVMess(raw string) (*nodeEntry, error) {
	dec, err := b64decode(strings.TrimPrefix(raw, "vmess://"))
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(dec), &v); err != nil {
		return nil, err
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return x
		case float64:
			return strconv.Itoa(int(x))
		}
		return ""
	}
	host := str("add")
	port, _ := strconv.Atoi(str("port"))
	aid, _ := strconv.Atoi(str("aid"))
	out := map[string]any{"type": "vmess", "server": host, "server_port": port, "uuid": str("id"), "alter_id": aid, "security": "auto"}
	network := str("net")
	if network == "ws" || network == "grpc" {
		addTransport(out, network, str("path"), str("host"), str("path"))
	}
	if str("tls") == "tls" {
		sni := str("sni")
		if sni == "" {
			sni = str("host")
		}
		addTLS(out, sni, false, nil, "")
	}
	name := str("ps")
	if name == "" {
		name = host
	}
	return &nodeEntry{Name: name, Type: "vmess", Server: host, Port: port, Raw: raw, Outbound: out}, nil
}

func parseTrojanLike(raw, proto string) (*nodeEntry, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	q := u.Query()
	name, _ := url.QueryUnescape(u.Fragment)
	if name == "" {
		name = host
	}
	secret := ""
	if u.User != nil {
		secret = u.User.String()
		if p, ok := u.User.Password(); ok {
			secret = u.User.Username() + ":" + p
		}
	}
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	insecure := q.Get("allowInsecure") == "1" || q.Get("insecure") == "1"
	var out map[string]any
	switch proto {
	case "trojan":
		out = map[string]any{"type": "trojan", "server": host, "server_port": port, "password": secret}
		addTLS(out, sni, insecure, nil, "")
	case "hysteria2":
		out = map[string]any{"type": "hysteria2", "server": host, "server_port": port, "password": secret}
		addTLS(out, sni, insecure, nil, "")
	case "vless":
		out = map[string]any{"type": "vless", "server": host, "server_port": port, "uuid": secret}
		if f := q.Get("flow"); f != "" {
			out["flow"] = f
		}
		sec := q.Get("security")
		if sec == "tls" || sec == "reality" {
			var reality map[string]any
			if sec == "reality" {
				reality = map[string]any{"enabled": true, "public_key": q.Get("pbk"), "short_id": q.Get("sid")}
			}
			addTLS(out, sni, insecure, reality, q.Get("fp"))
		}
	}
	network := q.Get("type")
	if network == "ws" || network == "grpc" {
		addTransport(out, network, q.Get("path"), q.Get("host"), q.Get("serviceName"))
	}
	typ := proto
	return &nodeEntry{Name: name, Type: typ, Server: host, Port: port, Raw: raw, Outbound: out}, nil
}

func parseShareLink(raw string) (*nodeEntry, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "ss://"):
		return parseSS(raw)
	case strings.HasPrefix(raw, "vmess://"):
		return parseVMess(raw)
	case strings.HasPrefix(raw, "vless://"):
		return parseTrojanLike(raw, "vless")
	case strings.HasPrefix(raw, "trojan://"):
		return parseTrojanLike(raw, "trojan")
	case strings.HasPrefix(raw, "hysteria2://"), strings.HasPrefix(raw, "hy2://"):
		return parseTrojanLike(raw, "hysteria2")
	}
	return nil, fmt.Errorf("unsupported link")
}

func persistLinks() {
	var lines []string
	for _, n := range nodes {
		lines = append(lines, n.Raw)
	}
	_ = os.WriteFile(filepath.Join(dataDir(), "links.txt"), []byte(strings.Join(lines, "\n")), 0o600)
}

func loadPersistedLinks() {
	b, err := os.ReadFile(filepath.Join(dataDir(), "links.txt"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		if n, err := parseShareLink(line); err == nil {
			nodes = append(nodes, *n)
		}
	}
	if len(nodes) > 0 {
		_ = genConfig()
	}
}

func genConfig() error {
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes")
	}
	coreMu.Lock()
	defer coreMu.Unlock()
	return genConfigLocked()
}

func genConfigLocked() error {
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes")
	}
	outs := []map[string]any{}
	tags := []string{}
	for i, n := range nodes {
		tag := fmt.Sprintf("node-%d", i+1)
		o := map[string]any{}
		for k, v := range n.Outbound {
			o[k] = v
		}
		o["tag"] = tag
		outs = append(outs, o)
		tags = append(tags, tag)
	}
	selector := map[string]any{"type": "selector", "tag": "proxy", "outbounds": tags, "default": tags[0]}
	cfg := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []map[string]any{
			{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": proxyPort},
		},
		"outbounds": append([]map[string]any{selector}, append(outs, map[string]any{"type": "direct", "tag": "direct"})...),
		"route":     map[string]any{"final": "proxy"},
	}
	dir := coreDir()
	_ = os.MkdirAll(dir, 0o755)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.generated.json"), b, 0o600)
}

func nodeListJSON() map[string]any {
	list := []map[string]any{}
	for _, n := range nodes {
		list = append(list, map[string]any{"name": n.Name, "type": n.Type, "server": n.Server, "port": n.Port})
	}
	return map[string]any{"count": len(nodes), "nodes": list, "proxy": fmt.Sprintf("socks5://127.0.0.1:%d", proxyPort)}
}

func nodeControl(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, nodeListJSON())
		return
	}
	var body struct {
		Link   string `json:"link"`
		SubURL string `json:"suburl"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "bad request"})
		return
	}
	if body.Action == "clear" {
		nodes = nil
		persistLinks()
		writeJSON(w, nodeListJSON())
		return
	}
	if strings.TrimSpace(body.Link) != "" {
		n, err := parseShareLink(body.Link)
		if err != nil {
			writeJSON(w, map[string]any{"error": "节点链接解析失败: " + err.Error()})
			return
		}
		for _, old := range nodes {
			if old.Raw == n.Raw {
				resp := nodeListJSON()
				resp["message"] = "这个节点已经加过了，没重复添加"
				writeJSON(w, resp)
				return
			}
		}
		nodes = append(nodes, *n)
		persistLinks()
		if err := genConfig(); err != nil {
			writeJSON(w, map[string]any{"error": "生成配置失败: " + err.Error()})
			return
		}
		resp := nodeListJSON()
		resp["message"] = "已添加节点：" + n.Name
		writeJSON(w, resp)
		return
	}
	if strings.TrimSpace(body.SubURL) != "" {
		links, err := fetchSubscription(body.SubURL)
		if err != nil {
			writeJSON(w, map[string]any{"error": "订阅拉取失败: " + err.Error()})
			return
		}
		var parsed []nodeEntry
		seen := map[string]bool{}
		for _, l := range links {
			if n, err := parseShareLink(l); err == nil && !seen[n.Raw] {
				seen[n.Raw] = true
				parsed = append(parsed, *n)
			}
		}
		if len(parsed) == 0 {
			writeJSON(w, map[string]any{"error": fmt.Sprintf("订阅里没解析出可用节点（共 %d 条链接）", len(links))})
			return
		}
		nodes = parsed
		persistLinks()
		if err := genConfig(); err != nil {
			writeJSON(w, map[string]any{"error": "生成配置失败: " + err.Error()})
			return
		}
		resp := nodeListJSON()
		resp["message"] = fmt.Sprintf("订阅导入成功：%d 个节点", len(parsed))
		writeJSON(w, resp)
		return
	}
	writeJSON(w, map[string]any{"error": "empty"})
}

func fetchSubscription(subURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sing-box/1.14 MJLight/0.2")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(string(b))
	if !strings.Contains(content, "://") {
		if dec, err := b64decode(content); err == nil {
			content = dec
		}
	}
	var links []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "://") {
			links = append(links, line)
		}
	}
	return links, nil
}

// ---------- sing-box core ----------

// coreDir is where sing-box lives: normally core/ next to the EXE. If
// that folder is not usable, the extracted core goes under Data/core.
func coreDir() string {
	beside := filepath.Join(exeDir(), "core")
	if _, err := os.Stat(filepath.Join(beside, "sing-box.exe")); err == nil {
		return beside
	}
	under := filepath.Join(dataDir(), "core")
	if _, err := os.Stat(filepath.Join(under, "sing-box.exe")); err == nil {
		return under
	}
	return beside
}

func corePath() string { return filepath.Join(coreDir(), "sing-box.exe") }

// extractEmbeddedCore unpacks the embedded sing-box into core/ on first
// run, so a single copied MJ-System.exe provisions itself. No-op when a
// sing-box.exe is already present in either core location.
func extractEmbeddedCore() {
	if _, err := os.Stat(corePath()); err == nil {
		return
	}
	under := filepath.Join(dataDir(), "core", "sing-box.exe")
	if _, err := os.Stat(under); err == nil {
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(singboxZip), int64(len(singboxZip)))
	if err != nil {
		return
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "sing-box.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return
		}
		// Prefer core/ next to the EXE; fall back to Data/core.
		for _, dir := range []string{filepath.Join(exeDir(), "core"), filepath.Join(dataDir(), "core")} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				continue
			}
			dst := filepath.Join(dir, "sing-box.exe")
			tmp := dst + ".tmp"
			if err := os.WriteFile(tmp, data, 0o755); err != nil {
				continue
			}
			if err := os.Rename(tmp, dst); err == nil {
				return
			}
			_ = os.Remove(tmp)
		}
		return
	}
}

// coreRunningLocked reports the REAL process state. v0.2 bug being fixed
// here: it read ProcessState without ever calling Wait(), so a sing-box
// that died instantly (bad config, killed by AV, ...) still showed as
// "running" and Chrome then failed with ERR_PROXY_CONNECTION_FAILED.
// Caller must hold coreMu.
func coreRunningLocked() bool {
	return coreCmd != nil && coreCmd.Process != nil && !coreExited.Load()
}

func coreLogPath() string { return filepath.Join(coreDir(), "sing-box.log") }

func coreLogTail() string {
	b, err := os.ReadFile(coreLogPath())
	if err != nil {
		return "(暂无日志)"
	}
	s := string(b)
	if len(s) > 3000 {
		s = s[len(s)-3000:]
	}
	return strings.TrimSpace(s)
}

func coreConfigPath() string {
	gen := filepath.Join(coreDir(), "config.generated.json")
	if _, err := os.Stat(gen); err == nil {
		return gen
	}
	manual := filepath.Join(coreDir(), "config.json")
	if _, err := os.Stat(manual); err == nil {
		return manual
	}
	return ""
}

// Caller must hold coreMu.
func startCoreLocked() error {
	if coreRunningLocked() {
		return nil
	}
	if _, err := os.Stat(corePath()); err != nil {
		return fmt.Errorf("未找到 core/sing-box.exe，请先把它放到程序同目录的 core 文件夹")
	}
	cfg := coreConfigPath()
	if cfg == "" {
		return fmt.Errorf("还没有节点配置：先在「节点」里粘贴节点链接或导入订阅")
	}
	// 1) Validate the config with sing-box itself before starting, so a
	//    bad node fails here with a readable reason instead of Chrome
	//    showing a bare proxy error later.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	checkCmd := exec.CommandContext(ctx, corePath(), "check", "-c", cfg)
	checkCmd.Dir = coreDir()
	hideConsole(checkCmd)
	checkOut, checkErr := checkCmd.CombinedOutput()
	cancel()
	if checkErr != nil {
		return fmt.Errorf("sing-box 配置校验没过：%v；输出：%s", checkErr, strings.TrimSpace(string(checkOut)))
	}
	// 2) Start with stdout/stderr captured to core/sing-box.log.
	logFile, err := os.OpenFile(coreLogPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("日志文件打不开：%v", err)
	}
	coreCmd = exec.Command(corePath(), "run", "-c", cfg)
	coreCmd.Dir = coreDir()
	hideConsole(coreCmd)
	coreCmd.Stdout = logFile
	coreCmd.Stderr = logFile
	if err := coreCmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("sing-box 启动失败：%v", err)
	}
	coreExited.Store(false)
	coreExitErr.Store("")
	coreStopReq.Store(false)
	cmd := coreCmd
	go func() {
		waitErr := cmd.Wait()
		_ = logFile.Close()
		coreExited.Store(true)
		if waitErr != nil && !coreStopReq.Load() {
			coreExitErr.Store(waitErr.Error())
		}
	}()
	// 3) Wait briefly until the proxy port really accepts connections,
	//    or the process dies -- then report which one happened.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 300*time.Millisecond); err == nil {
			_ = conn.Close()
			return nil
		}
		exited := coreExited.Load()
		exitErr, _ := coreExitErr.Load().(string)
		if exited {
			return fmt.Errorf("sing-box 启动后立刻退出了：%s；日志：%s", exitErr, coreLogTail())
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("sing-box 已启动，但 %d 端口 4 秒内没开始监听；日志：%s", proxyPort, coreLogTail())
}

// Caller must hold coreMu.
func stopCoreLocked() {
	if coreCmd != nil && coreCmd.Process != nil && !coreExited.Load() {
		coreStopReq.Store(true)
		_ = coreCmd.Process.Kill()
	}
	coreCmd = nil
}

func coreControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	coreMu.Lock()
	defer coreMu.Unlock()
	switch body.Action {
	case "status":
		if _, err := os.Stat(corePath()); err != nil {
			writeJSON(w, map[string]any{"running": false, "message": "未找到 core/sing-box.exe（其他功能不受影响）"})
			return
		}
		if coreRunningLocked() {
			writeJSON(w, map[string]any{"running": true, "message": fmt.Sprintf("运行中（已确认进程活着），本地代理 127.0.0.1:%d", proxyPort)})
			return
		}
		if exitErr, _ := coreExitErr.Load().(string); coreCmd != nil && coreExited.Load() && exitErr != "" {
			writeJSON(w, map[string]any{"running": false, "message": "sing-box 已异常退出：" + exitErr + "；日志末尾：" + coreLogTail()})
			return
		}
		writeJSON(w, map[string]any{"running": false, "message": "已停止，sing-box 已就位"})
	case "start":
		if err := startCoreLocked(); err != nil {
			writeJSON(w, map[string]any{"running": false, "message": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"running": true, "message": fmt.Sprintf("已启动并确认 %d 端口在监听", proxyPort)})
	case "stop":
		stopCoreLocked()
		writeJSON(w, map[string]any{"running": false, "message": "已停止"})
	default:
		writeJSON(w, map[string]any{"error": "unknown action"})
	}
}

func coreLogHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"log": coreLogTail()})
}

// ---------- proxy self-test ----------

// socks5Connect performs a minimal SOCKS5 no-auth CONNECT through the
// local mixed inbound to host:port, reporting the proxy's reply code.
func socks5Connect(host string, port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 3*time.Second)
	if err != nil {
		return fmt.Errorf("连不上本地代理端口：%v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("socks5 握手失败：%v", err)
	}
	hb := []byte(host)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(hb))}
	req = append(req, hb...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("socks5 无回复：%v", err)
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5 连接被拒绝，回复码 %d", head[1])
	}
	// consume bound address + port
	var n int
	switch head[3] {
	case 0x01:
		n = 4
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			return err
		}
		n = int(lb[0])
	case 0x04:
		n = 16
	}
	if _, err := io.ReadFull(conn, make([]byte, n+2)); err != nil {
		return err
	}
	return nil
}

func proxyTest(w http.ResponseWriter, r *http.Request) {
	coreMu.Lock()
	running := coreRunningLocked()
	coreMu.Unlock()
	exitErr, _ := coreExitErr.Load().(string)
	if !running {
		msg := "节点没在运行（进程不在）"
		if exitErr != "" {
			msg += "，退出原因：" + exitErr + "；日志末尾：" + coreLogTail()
		}
		writeJSON(w, map[string]any{"ok": false, "stage": "core", "message": msg})
		return
	}
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 2*time.Second); err != nil {
		writeJSON(w, map[string]any{"ok": false, "stage": "port", "message": fmt.Sprintf("进程在，但 %d 端口连不上：%v", proxyPort, err)})
		return
	} else {
		_ = conn.Close()
	}
	// Through the node to Google, the same path Chrome will take.
	if err := socks5Connect("www.google.com", 443); err != nil {
		writeJSON(w, map[string]any{"ok": false, "stage": "node", "message": "本地代理通，但经节点连 Google:443 失败：" + err.Error() + "；多半是这个节点连不上/握手失败，换个节点试。sing-box 日志末尾：" + coreLogTail()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "stage": "done", "message": "自检通过：本地代理正常，经节点能连到 Google:443，Chrome 走节点打开就能上"})
}

// ---------- browsers ----------

func findBinary(candidates []string, names ...string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// Google Chrome first (that is what the user asked for), Edge as fallback.
func findChrome() string {
	pf, pfx, la := os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")
	return findBinary([]string{
		filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(pfx, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(la, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(pfx, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"),
	}, "chrome.exe", "msedge.exe")
}

func findShellBrowser() string {
	pf, pfx, la := os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")
	return findBinary([]string{
		filepath.Join(pfx, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(la, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(pfx, "Google", "Chrome", "Application", "chrome.exe"),
	}, "msedge.exe", "chrome.exe")
}

// openBrowser opens the ISOLATED browser: separate profile + optional proxy.
// Only this window goes through the node; nothing system-wide changes.
// launchIsolatedBrowser starts the isolated Chrome/Edge window:
// separate profile + optional proxy. Only this window goes through
// the node; nothing system-wide changes.
func launchIsolatedBrowser(proxy bool, target string) error {
	browser := findChrome()
	if browser == "" {
		return fmt.Errorf("这台电脑没找到 Chrome 或 Edge，请先装一个 Chrome")
	}
	if proxy {
		coreMu.Lock()
		err := startCoreLocked()
		coreMu.Unlock()
		if err != nil {
			return fmt.Errorf("节点启动失败：%w", err)
		}
	}
	if target == "" {
		target = "https://www.google.com"
	}
	profile := filepath.Join(dataDir(), "ChromeProfile")
	_ = os.MkdirAll(profile, 0o755)
	args := []string{"--user-data-dir=" + profile, "--no-first-run", "--start-maximized"}
	if proxy {
		args = append(args, "--proxy-server=socks5://127.0.0.1:"+strconv.Itoa(proxyPort), "--proxy-bypass-list=127.0.0.1;localhost")
	}
	args = append(args, target)
	if err := exec.Command(browser, args...).Start(); err != nil {
		return fmt.Errorf("浏览器打开失败: %w", err)
	}
	return nil
}

func openBrowser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Proxy bool   `json:"proxy"`
		URL   string `json:"url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会正常打开 Chrome"})
		return
	}
	if err := launchIsolatedBrowser(body.Proxy, body.URL); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	kind := "直连"
	if body.Proxy {
		kind = fmt.Sprintf("走节点（127.0.0.1:%d）", proxyPort)
	}
	writeJSON(w, map[string]any{"ok": true, "browser": filepath.Base(findChrome()), "mode": kind, "profile": filepath.Join(dataDir(), "ChromeProfile")})
}

// ---------- background daemon (v0.4) ----------
//
// The desktop window is now just a view. The real work (local API +
// sing-box core) lives in a hidden daemon process:
//   * User double-clicks MJ-System.exe -> launcher makes sure the daemon
//     is up (starting it detached if needed), opens the desktop window,
//     and exits itself.
//   * Closing the desktop window only closes the view; the daemon and
//     the node keep running in the background, so the isolated Chrome
//     keeps its proxy.
//   * Double-clicking the EXE again re-opens the desktop from the same
//     daemon (nodes, state, everything still there).
//   * The ONLY way to actually stop it is inside the system:
//     设置 -> 退出系统 (stops the node AND the daemon), or stopping
//     the node in 节点. That is what keeps a shared PC safe from an
//     accidental window-close killing the connection mid-use.

const daemonBasePort = 18790
const daemonPortSpan = 10

var lastActivity atomic.Int64 // unix seconds, updated on any API/UI request

func touchActivity() { lastActivity.Store(time.Now().Unix()) }

func daemonURLForPort(p int) string { return fmt.Sprintf("http://127.0.0.1:%d/", p) }

// findDaemon scans the daemon port range for an already-running MJ
// daemon and returns its base URL ("" when none is up).
func findDaemon() string {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	for p := daemonBasePort; p < daemonBasePort+daemonPortSpan; p++ {
		resp, err := client.Get(daemonURLForPort(p) + "api/system")
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return daemonURLForPort(p)
		}
	}
	return ""
}

func openShellWindow(url string, proxy bool) error {
	browser := findShellBrowser()
	profile := filepath.Join(dataDir(), "ShellProfile")
	_ = os.MkdirAll(profile, 0o755)
	if browser == "" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	// Deliberately NOT waited on: this window may be closed freely, it
	// no longer owns the backend's lifetime.
	args := []string{"--app=" + url, "--start-maximized", "--user-data-dir=" + profile, "--no-first-run"}
	if proxy {
		// The desktop itself goes through the node (v0.8 in-app global
		// acceleration); loopback stays direct so the local UI itself
		// never depends on the node being alive.
		args = append(args, "--proxy-server=socks5://127.0.0.1:"+strconv.Itoa(proxyPort), "--proxy-bypass-list=127.0.0.1;localhost")
	}
	return exec.Command(browser, args...).Start()
}

// shellOpenHandler runs in the DAEMON: it knows the real core state,
// so it decides whether the desktop window launches through the node.
func shellOpenHandler(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（测试环境），桌面窗口在 Windows 上会打开"})
		return
	}
	proxy := false
	if accelEnabled() && len(nodes) > 0 {
		// Give the auto-starting core a moment to come up.
		for i := 0; i < 20 && !coreRunningQuick(); i++ {
			time.Sleep(300 * time.Millisecond)
		}
		proxy = coreRunningQuick()
	}
	url := "http://" + r.Host + "/"
	if err := openShellWindow(url, proxy); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "proxy": proxy})
}

// askDaemonOpenShell lets the daemon open the desktop window (it
// decides about the proxy). False means the daemon is too old to know
// the endpoint or failed -- the launcher falls back to a direct open.
func askDaemonOpenShell(base string) bool {
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Post(base+"api/openshell", "application/json", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct {
		OK bool `json:"ok"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return v.OK
}

// launcherMain runs on a normal double-click on Windows: ensure the
// daemon is up, open the desktop, exit.
func launcherMain() {
	if url := findDaemon(); url != "" {
		if !askDaemonOpenShell(url) {
			_ = openShellWindow(url, false)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	daemon := exec.Command(exe, "-daemon")
	daemon.Dir = exeDir()
	if err := daemon.Start(); err != nil {
		return
	}
	for i := 0; i < 30; i++ {
		if url := findDaemon(); url != "" {
			if !askDaemonOpenShell(url) {
				_ = openShellWindow(url, false)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func activityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		touchActivity()
		next.ServeHTTP(w, r)
	})
}

func runServer(portFlag int, isDaemon bool) {
	termCwd = homeDir()
	_ = os.MkdirAll(filepath.Join(exeDir(), "core"), 0o755)
	extractEmbeddedCore() // single-file EXE provisions its own sing-box
	loadPersistedLinks()
	touchActivity()

	var ln net.Listener
	var err error
	if portFlag > 0 {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", portFlag))
		if err != nil {
			fmt.Fprintln(os.Stderr, "listen:", err)
			os.Exit(1)
		}
	} else {
		for p := daemonBasePort; p < daemonBasePort+daemonPortSpan; p++ {
			ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
			if err == nil {
				break
			}
		}
		if ln == nil {
			fmt.Fprintln(os.Stderr, "listen: no free daemon port")
			os.Exit(1)
		}
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	url := daemonURLForPort(actualPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/api/system", sysInfo)
	mux.HandleFunc("/api/wallpaper", wallpaperHandler)
	mux.HandleFunc("/api/list", listFiles)
	mux.HandleFunc("/api/run", runCmd)
	mux.HandleFunc("/api/open", openPath)
	mux.HandleFunc("/api/openurl", openPath)
	mux.HandleFunc("/api/core", coreControl)
	mux.HandleFunc("/api/core/log", coreLogHandler)
	mux.HandleFunc("/api/proxytest", proxyTest)
	mux.HandleFunc("/api/node", nodeControl)
	mux.HandleFunc("/api/browser", openBrowser)
	mux.HandleFunc("/api/update", updateStatusHandler)
	mux.HandleFunc("/api/update/token", updateTokenHandler)
	mux.HandleFunc("/api/update/check", updateCheckHandler)
	mux.HandleFunc("/api/update/download", updateDownloadHandler)
	mux.HandleFunc("/api/update/apply", updateApplyHandler)
	mux.HandleFunc("/api/accel", accelHandler)
	mux.HandleFunc("/api/openshell", shellOpenHandler)
	srv := &http.Server{Handler: activityMiddleware(mux)}
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "message": "后台和节点一起退出"})
		go func() {
			time.Sleep(300 * time.Millisecond)
			coreMu.Lock()
			stopCoreLocked()
			coreMu.Unlock()
			_ = srv.Shutdown(context.Background())
			os.Exit(0)
		}()
	})

	fmt.Println("MJ Light System daemon:", url)
	go func() { _ = srv.Serve(ln) }()

	// Coming up in the background: if nodes are already saved, bring the
	// core up right away so the proxy is ready without opening anything.
	if len(nodes) > 0 {
		go func() {
			coreMu.Lock()
			err := startCoreLocked()
			coreMu.Unlock()
			if err != nil {
				fmt.Println("auto-start core:", err)
			}
		}()
	}

	// OTA leftovers: an MJ-System.exe.old next to us means a previous
	// update already succeeded (we are the new binary and we are up),
	// so it can go.
	go func() {
		time.Sleep(8 * time.Second)
		if self, err := os.Executable(); err == nil {
			_ = os.Remove(self + ".old")
		}
	}()

	// OTA: silently check on daemon start and pre-download a newer
	// release; applying still waits for the user to click in
	// 设置 -> 软件更新. The repo is public, so no token is needed.
	if isDaemon {
		go func() {
			time.Sleep(20 * time.Second)
			checkUpdate()
			upd.mu.Lock()
			avail := upd.Phase == "available"
			upd.mu.Unlock()
			if avail {
				downloadUpdate()
			}
		}()
	}

	if isDaemon {
		// Safety net for shared PCs: if the node is NOT running and
		// nobody has touched the system for 30 minutes, the daemon
		// cleans itself up instead of lingering forever. While the
		// node runs (someone may be browsing through it), never exit.
		go func() {
			for range time.Tick(time.Minute) {
				coreMu.Lock()
				running := coreRunningLocked()
				coreMu.Unlock()
				if !running && time.Now().Unix()-lastActivity.Load() > 30*60 {
					coreMu.Lock()
					stopCoreLocked()
					coreMu.Unlock()
					_ = srv.Shutdown(context.Background())
					os.Exit(0)
				}
			}
		}()
	}

	select {}
}

func main() {
	port := flag.Int("port", 0, "port to listen on (0 = daemon port range)")
	noOpen := flag.Bool("no-open", false, "run the server in this process (dev/test)")
	daemon := flag.Bool("daemon", false, "run as the hidden background daemon")
	applyUpdate := flag.Bool("apply-update", false, "internal: OTA takeover from the old binary")
	updateTarget := flag.String("update-target", "", "internal: exe path to replace with -apply-update")
	flag.Parse()

	if *applyUpdate {
		runApplyUpdate(*updateTarget)
		return
	}

	if *daemon || *noOpen || runtime.GOOS != "windows" {
		runServer(*port, *daemon)
		return
	}
	// Normal double-click on Windows.
	launcherMain()
}
