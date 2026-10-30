package main

// ---------- SSH app (v0.18) ----------
//
// A built-in SSH client: the desktop window runs xterm.js and talks
// to the daemon over a WebSocket; the daemon bridges it to a real
// SSH session via golang.org/x/crypto/ssh. Connections, private
// keys and the known_hosts file live in Data/ssh, so they travel
// with the folder. Passwords (and key passphrases) are typed at
// connect time and never stored. SFTP rides the live terminal
// connection, so the file panel needs no credentials of its own.
// Host keys are pinned on first connect (TOFU) and verified on
// every later connect.

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

//go:embed assets/xterm.js
var xtermJS []byte

//go:embed assets/xterm.css
var xtermCSS []byte

//go:embed assets/xterm-fit.js
var xtermFitJS []byte

func assetHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch kind {
		case "js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(xtermJS)
		case "fit":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(xtermFitJS)
		case "css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = w.Write(xtermCSS)
		}
	}
}

// ---------- storage ----------

type sshConn struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Auth    string `json:"auth"` // key | password
	KeyFile string `json:"keyfile"`
	Proxy   bool   `json:"proxy"`
}

var sshStoreMu sync.Mutex

func sshDir() string {
	d := filepath.Join(dataDir(), "ssh")
	_ = os.MkdirAll(filepath.Join(d, "keys"), 0o700)
	return d
}

func sshKeysDir() string { return filepath.Join(sshDir(), "keys") }

func sshLoadConns() []sshConn {
	sshStoreMu.Lock()
	defer sshStoreMu.Unlock()
	// v0.57: through the data layer (atomic saves + keep-last-good
	// recovery for the connection list; the KEYS stay on their own
	// path -- key material is not data-layer state).
	b, status, err := dsLoad("ssh-connections")
	if err != nil || status == dsMissing {
		return []sshConn{}
	}
	var out []sshConn
	if json.Unmarshal(b, &out) != nil || out == nil {
		return []sshConn{}
	}
	return out
}

func sshSaveConns(list []sshConn) error {
	sshStoreMu.Lock()
	defer sshStoreMu.Unlock()
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return dsSave("ssh-connections", b)
}

func sshKeyNames() []string {
	ents, err := os.ReadDir(sshKeysDir())
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, e := range ents {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func sshListHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"connections": sshLoadConns(), "keys": sshKeyNames()})
}

func sshSaveHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Conn    sshConn `json:"conn"`
		Delete  bool    `json:"delete"`
		KeyName string  `json:"keyname"`
		KeyData string  `json:"keydata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求格式不对"})
		return
	}
	// Store a pasted private key under Data/ssh/keys.
	if strings.TrimSpace(body.KeyData) != "" {
		name := filepath.Base(strings.TrimSpace(body.KeyName))
		if name == "" || name == "." || name == ".." {
			writeJSON(w, map[string]any{"error": "密钥文件名不对"})
			return
		}
		if err := os.WriteFile(filepath.Join(sshKeysDir(), name), []byte(strings.TrimSpace(body.KeyData)), 0o600); err != nil {
			logf("ssh", "error", "私钥文件保存失败：%s", name)
			writeJSON(w, map[string]any{"error": "密钥保存失败：" + err.Error()})
			return
		}
		logf("ssh", "info", "已保存私钥文件：%s（内容不记入日志）", name)
		body.Conn.KeyFile = name
	}
	list := sshLoadConns()
	if body.Delete {
		for _, c := range list {
			if c.ID == body.Conn.ID {
				logf("ssh", "info", "已删除 SSH 连接：%s（%s@%s:%d）", c.Name, c.User, c.Host, c.Port)
			}
		}
		out := list[:0]
		for _, c := range list {
			if c.ID != body.Conn.ID {
				out = append(out, c)
			}
		}
		list = out
	} else {
		c := body.Conn
		if c.Port <= 0 {
			c.Port = 22
		}
		if c.Auth != "key" {
			c.Auth = "password"
		}
		if c.ID == "" {
			c.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
			list = append(list, c)
		} else {
			found := false
			for i := range list {
				if list[i].ID == c.ID {
					list[i] = c
					found = true
				}
			}
			if !found {
				list = append(list, c)
			}
		}
	}
	if err := sshSaveConns(list); err != nil {
		logf("ssh", "error", "SSH 连接保存失败：%v", err)
		writeJSON(w, map[string]any{"error": "保存失败：" + err.Error()})
		return
	}
	if !body.Delete {
		c := body.Conn
		if c.Port <= 0 {
			c.Port = 22
		}
		logf("ssh", "info", "已保存 SSH 连接：%s（%s@%s:%d）", c.Name, c.User, c.Host, c.Port)
	}
	writeJSON(w, map[string]any{"ok": true, "connections": sshLoadConns(), "keys": sshKeyNames()})
}

func sshKeyDelHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := filepath.Base(strings.TrimSpace(body.Name))
	if name == "" || name == "." || name == ".." {
		writeJSON(w, map[string]any{"error": "文件名不对"})
		return
	}
	_ = os.Remove(filepath.Join(sshKeysDir(), name))
	logf("ssh", "info", "已删除私钥文件：%s", name)
	writeJSON(w, map[string]any{"ok": true, "keys": sshKeyNames()})
}

// ---------- host keys (TOFU pinning) ----------

func hostKeyCallback() ssh.HostKeyCallback {
	return func(hostport string, _ net.Addr, key ssh.PublicKey) error {
		khPath := filepath.Join(sshDir(), "known_hosts")
		want := key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
		if b, err := os.ReadFile(khPath); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				f := strings.Fields(line)
				if len(f) == 3 && f[0] == hostport {
					if f[1]+" "+f[2] == want {
						return nil
					}
					logf("ssh", "error", "服务器指纹不一致，已拦截连接：%s（记录 %s，实际 %s）", hostport, f[1], key.Type())
					return fmt.Errorf("服务器指纹和记录的不一致（记录 %s，实际 %s）——如果不是你自己重装/换的服务器，不要继续", f[1], key.Type())
				}
			}
		}
		f, err := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err = fmt.Fprintf(f, "%s %s\n", hostport, want); err == nil {
			logf("ssh", "info", "首次连接，已记录服务器指纹：%s（%s）", hostport, key.Type())
		}
		return err
	}
}

// ---------- dialing ----------

// socks5Dial is a minimal no-auth SOCKS5 CONNECT through the local
// sing-box mixed port, enough to tunnel one SSH connection.
func socks5Dial(proxyAddr, target string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	c, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = c.Close()
		return nil, err
	}
	buf := make([]byte, 512)
	if _, err := io.ReadFull(c, buf[:2]); err != nil || buf[0] != 0x05 || buf[1] != 0x00 {
		_ = c.Close()
		return nil, fmt.Errorf("本地代理握手失败")
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, host...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		_ = c.Close()
		return nil, err
	}
	if _, err := io.ReadFull(c, buf[:4]); err != nil || buf[1] != 0x00 {
		_ = c.Close()
		return nil, fmt.Errorf("本地代理拒绝连接（节点没通？）")
	}
	var skip int
	switch buf[3] {
	case 0x01:
		skip = 4
	case 0x03:
		if _, err := io.ReadFull(c, buf[:1]); err != nil {
			_ = c.Close()
			return nil, err
		}
		skip = int(buf[0])
	case 0x04:
		skip = 16
	}
	if _, err := io.ReadFull(c, buf[:skip+2]); err != nil {
		_ = c.Close()
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

func dialSSH(c sshConn, password, passphrase string) (*ssh.Client, error) {
	var methods []ssh.AuthMethod
	if c.Auth == "key" {
		kb, err := os.ReadFile(filepath.Join(sshKeysDir(), filepath.Base(c.KeyFile)))
		if err != nil {
			return nil, fmt.Errorf("读不到私钥文件：%w", err)
		}
		var signer ssh.Signer
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(kb, []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(kb)
		}
		if err != nil {
			return nil, fmt.Errorf("私钥解析失败（有口令的话要填对口令）：%w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	} else {
		methods = append(methods, ssh.Password(password))
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback(),
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	if c.Proxy {
		coreMu.Lock()
		err := startCoreLocked()
		coreMu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("节点启动失败：%w", err)
		}
		raw, err := socks5Dial(fmt.Sprintf("127.0.0.1:%d", proxyPort), addr)
		if err != nil {
			return nil, err
		}
		sc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
		if err != nil {
			return nil, err
		}
		return ssh.NewClient(sc, chans, reqs), nil
	}
	return ssh.Dial("tcp", addr, cfg)
}

// ---------- live sessions (terminal + SFTP share one client) ----------

var sshLiveMu sync.Mutex
var sshLive = map[string]*ssh.Client{}

func sshLiveSet(id string, cl *ssh.Client) {
	sshLiveMu.Lock()
	defer sshLiveMu.Unlock()
	if old, ok := sshLive[id]; ok && old != cl {
		_ = old.Close()
	}
	sshLive[id] = cl
}

func sshLiveGet(id string) (*ssh.Client, bool) {
	sshLiveMu.Lock()
	defer sshLiveMu.Unlock()
	cl, ok := sshLive[id]
	return cl, ok
}

func sshLiveDrop(id string, cl *ssh.Client) {
	sshLiveMu.Lock()
	defer sshLiveMu.Unlock()
	if cur, ok := sshLive[id]; ok && cur == cl {
		delete(sshLive, id)
		_ = cl.Close()
	}
}

// ---------- WebSocket terminal ----------

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // loopback daemon only
}

func sshWSHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("conn")
	var target *sshConn
	for _, c := range sshLoadConns() {
		if c.ID == id {
			cc := c
			target = &cc
		}
	}
	if target == nil {
		http.Error(w, "连接不存在", http.StatusNotFound)
		return
	}
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	via := "直连"
	if target.Proxy {
		via = "走节点"
	}
	client, err := dialSSH(*target, q.Get("password"), q.Get("passphrase"))
	if err != nil {
		// Only host + result go into the log -- never the password or
		// passphrase from the query string.
		logf("ssh", "error", "SSH 连接失败：%s@%s:%d（%s）：%v", target.User, target.Host, target.Port, via, err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n连接失败："+err.Error()+"\r\n"))
		return
	}
	logf("ssh", "info", "SSH 已连接：%s@%s:%d（%s，%s）", target.User, target.Host, target.Port, target.Name, via)
	sshLiveSet(id, client)
	defer sshLiveDrop(id, client)

	sess, err := client.NewSession()
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n开会话失败："+err.Error()+"\r\n"))
		return
	}
	defer sess.Close()
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	if cols <= 0 {
		cols = 100
	}
	if rows <= 0 {
		rows = 30
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return
	}
	// With a PTY requested, stderr arrives on the stdout pipe too.
	if err := sess.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{}); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n申请终端失败："+err.Error()+"\r\n"))
		return
	}
	if err := sess.Shell(); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n启动 shell 失败："+err.Error()+"\r\n"))
		return
	}
	// stdout -> ws
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	}()
	// ws -> stdin / control
	for {
		mt, msg, rerr := ws.ReadMessage()
		if rerr != nil {
			break
		}
		if mt == websocket.TextMessage {
			var ctl struct {
				T    string `json:"t"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(msg, &ctl) == nil && ctl.T == "resize" && ctl.Cols > 0 && ctl.Rows > 0 {
				_ = sess.WindowChange(ctl.Rows, ctl.Cols)
				continue
			}
		}
		if _, werr := stdin.Write(msg); werr != nil {
			break
		}
	}
	_ = sess.Close()
	<-done
	logf("ssh", "info", "SSH 会话结束：%s@%s:%d", target.User, target.Host, target.Port)
}

// ---------- SFTP (rides the live terminal connection) ----------

func sftpClientFor(id string) (*sftp.Client, error) {
	cl, ok := sshLiveGet(id)
	if !ok {
		return nil, fmt.Errorf("先在上面建立终端连接，文件面板共用那条连接")
	}
	return sftp.NewClient(cl)
}

type sftpBody struct {
	Conn string `json:"conn"`
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
	Dir  bool   `json:"dir"`
}

func sftpListHandler(w http.ResponseWriter, r *http.Request) {
	var b sftpBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	sc, err := sftpClientFor(b.Conn)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer sc.Close()
	p := b.Path
	if p == "" {
		p, _ = sc.Getwd()
	}
	ents, err := sc.ReadDir(p)
	if err != nil {
		logf("ssh", "warn", "SFTP 读目录失败：%s：%v", p, err)
		writeJSON(w, map[string]any{"error": "读目录失败：" + err.Error()})
		return
	}
	type ent struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Dir   bool   `json:"dir"`
		Mtime int64  `json:"mtime"`
	}
	out := []ent{}
	for _, e := range ents {
		out = append(out, ent{e.Name(), e.Size(), e.IsDir(), e.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, map[string]any{"ok": true, "path": p, "entries": out})
}

func sftpMkdirHandler(w http.ResponseWriter, r *http.Request) {
	var b sftpBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	sc, err := sftpClientFor(b.Conn)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer sc.Close()
	if err := sc.Mkdir(b.Path); err != nil {
		logf("ssh", "warn", "SFTP 建目录失败：%s：%v", b.Path, err)
		writeJSON(w, map[string]any{"error": "建目录失败：" + err.Error()})
		return
	}
	logf("ssh", "info", "SFTP 已建目录：%s", b.Path)
	writeJSON(w, map[string]any{"ok": true})
}

func sftpDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var b sftpBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	sc, err := sftpClientFor(b.Conn)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer sc.Close()
	var derr error
	if b.Dir {
		derr = sc.RemoveDirectory(b.Path)
	} else {
		derr = sc.Remove(b.Path)
	}
	if derr != nil {
		logf("ssh", "warn", "SFTP 删除失败：%s：%v", b.Path, derr)
		writeJSON(w, map[string]any{"error": "删除失败：" + derr.Error()})
		return
	}
	logf("ssh", "info", "SFTP 已删除：%s", b.Path)
	writeJSON(w, map[string]any{"ok": true})
}

func sftpUploadHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, map[string]any{"error": "上传格式不对"})
		return
	}
	connID := r.FormValue("conn")
	dir := r.FormValue("path")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, map[string]any{"error": "没收到文件"})
		return
	}
	defer file.Close()
	sc, err := sftpClientFor(connID)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	defer sc.Close()
	remote := path.Join(dir, filepath.Base(hdr.Filename))
	rf, err := sc.Create(remote)
	if err != nil {
		logf("ssh", "warn", "SFTP 上传建文件失败：%s：%v", remote, err)
		writeJSON(w, map[string]any{"error": "远程建文件失败：" + err.Error()})
		return
	}
	n, err := io.Copy(rf, file)
	if err != nil {
		_ = rf.Close()
		logf("ssh", "warn", "SFTP 上传写入失败：%s：%v", remote, err)
		writeJSON(w, map[string]any{"error": "写入失败：" + err.Error()})
		return
	}
	_ = rf.Close()
	logf("ssh", "info", "SFTP 已上传：%s（%d 字节）", remote, n)
	writeJSON(w, map[string]any{"ok": true, "path": remote})
}

func sftpDownloadHandler(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("conn")
	remote := r.URL.Query().Get("path")
	sc, err := sftpClientFor(connID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer sc.Close()
	rf, err := sc.Open(remote)
	if err != nil {
		http.Error(w, "打开远程文件失败："+err.Error(), http.StatusBadRequest)
		return
	}
	defer rf.Close()
	name := path.Base(remote)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+strings.ReplaceAll(name, " ", "%20"))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, rf)
}
