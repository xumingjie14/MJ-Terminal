package main

// ---------- remote assistance (v0.33) ----------
//
// Lets a paired operator -- the user's own AI assistants, never a
// hardcoded identity -- reach a RESTRICTED slice of the daemon's
// API over the user's private Tailscale network, so a bug report
// stops meaning "export a zip and carry it over by hand": the
// operator reads the console and the live state directly, and with
// the assist tier can also perform the reversible actions the UI
// itself offers (settings within the whitelist, node start/stop,
// component start/stop, update check/download/apply).
//
// Shape of the thing:
//   - Networking is an embedded tsnet node (remote_tsnet.go,
//     windows/amd64 + linux/amd64; a stub elsewhere). Pure
//     userspace: no install, no driver, no admin, and its state
//     lives in Data/tsnet so it travels with the folder.
//   - The remote listener (:18790 on the tailnet only) serves its
//     OWN mux. The local 127.0.0.1 mux is never re-exposed.
//   - Every request passes one auth chain: WhoIs the source ->
//     paired-operator lookup -> tier gate -> logf. Every request
//     is logged (module "remote", operator name included), allowed
//     or denied, no exceptions.
//   - Pairing, tier changes and revocation are LOCAL-UI-only
//     operations. An unpaired node that knocks is recorded as
//     "pending" with a 6-digit pairing code derived from its node
//     key; the user compares the code out of band and approves in
//     设置 -> 安全与隐私. New operators start at the read tier.
//   - Red lines, enforced by construction (the endpoints simply do
//     not exist on the remote mux, and every shared response goes
//     through a shaping function): file contents, SSH private keys
//     and passwords, node share links / subscription URLs, the
//     OpenClaw gateway token, the update token, shutdown, app
//     install/uninstall, the OpenClaw wizard, and the remote
//     settings themselves -- a remote settings POST that even
//     mentions "remoteEnabled" is rejected whole.
//
// The operator model is deliberately generic: Data/remote.json
// holds a LIST of operators (name + tailnet node identity + tier),
// any of which the user can revoke individually. Muse (device
// `muse`) is simply the first entry a user is expected to pair.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// remotePort is the port the remote mux listens on, on the tailnet
// interface only (via tsnet). It numerically matches the first
// local daemon port on purpose -- different interfaces, no clash,
// and one number for the user to remember.
const remotePort = 18790

// ---------- operator store ----------

type remoteOperator struct {
	ID       string `json:"id"`   // tailnet StableID of the operator's node
	Name     string `json:"name"` // display name, chosen by the user at approval
	Device   string `json:"device"`
	Tier     string `json:"tier"` // "read" | "assist"
	PairedAt string `json:"pairedAt"`
}

type remotePending struct {
	Peer      remotePeer
	Code      string
	FirstSeen time.Time
	LastSeen  time.Time
}

// remotePeer is what the platform backend resolves a connection's
// source address to. ID is the identity operators are stored
// under; KeyFP is key material used ONLY to derive the human
// pairing code (never persisted, never served).
type remotePeer struct {
	ID     string
	Name   string // short device name (MagicDNS base name / hostname)
	Device string // full device name as the tailnet knows it
	KeyFP  string
}

// remoteNetState is the backend's view of the world for the local
// state endpoint and the Settings page.
type remoteNetState struct {
	State   string   // off | starting | needslogin | online | error | unsupported
	AuthURL string   // set while State == needslogin
	IPs     []string // our tailnet addresses while online
	Device  string   // our own tailnet device name while online
	Err     string
}

// remoteBackend is the platform seam. The tsnet implementation
// lives in remote_tsnet.go (windows/amd64, linux/amd64); the stub
// in remote_stub.go reports unsupported everywhere else.
type remoteBackend interface {
	Start(handler http.Handler) error
	Stop()
	State() remoteNetState
	WhoIs(remoteAddr string) (remotePeer, error)
}

var remoteKickCh = make(chan struct{}, 1)

// remoteKick nudges the reconcile loop to re-evaluate the master
// switch immediately (settings POST path / daemon boot).
func remoteKick() {
	select {
	case remoteKickCh <- struct{}{}:
	default:
	}
}

var rmt struct {
	sync.Mutex
	be        remoteBackend
	running   bool
	starting  bool
	loaded    bool
	operators []remoteOperator
	pending   map[string]*remotePending
	lastSeen  map[string]time.Time // operator ID -> last persisted LastSeen is overkill; in-memory only
}

var remoteLastActivity atomic.Int64 // unix seconds of the most recent allowed remote request

func remoteStorePath() string { return filepath.Join(dataDir(), "remote.json") }

// remoteLoadLocked reads Data/remote.json onto the in-memory
// operator list. Caller holds rmt.mu.
func remoteLoadLocked() {
	if rmt.loaded {
		return
	}
	rmt.loaded = true
	rmt.pending = map[string]*remotePending{}
	rmt.lastSeen = map[string]time.Time{}
	b, err := os.ReadFile(remoteStorePath())
	if err != nil {
		return
	}
	var f struct {
		Operators []remoteOperator `json:"operators"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		logf("remote", "warn", "remote.json 解析失败，操作者列表按空处理：%v", err)
		return
	}
	rmt.operators = f.Operators
}

// remoteSaveLocked persists the operator list atomically.
// Caller holds rmt.mu.
func remoteSaveLocked() {
	f := struct {
		Operators []remoteOperator `json:"operators"`
	}{Operators: rmt.operators}
	if f.Operators == nil {
		f.Operators = []remoteOperator{}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	tmp := remoteStorePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		logf("remote", "error", "操作者列表保存失败：%v", err)
		return
	}
	if err := os.Rename(tmp, remoteStorePath()); err != nil {
		_ = os.Remove(tmp)
		logf("remote", "error", "操作者列表保存失败：%v", err)
	}
}

func remoteBackendLocked() remoteBackend {
	if rmt.be == nil {
		rmt.be = newRemoteBackend()
	}
	return rmt.be
}

func remoteFindOperator(id string) *remoteOperator {
	rmt.Lock()
	defer rmt.Unlock()
	remoteLoadLocked()
	for i := range rmt.operators {
		if rmt.operators[i].ID == id {
			cp := rmt.operators[i]
			return &cp
		}
	}
	return nil
}

// remotePairCode derives the 6-digit code the user compares out of
// band before approving a pending node. It comes from the node's
// public key, so two different nodes practically never share a
// code, and the same node always shows the same code.
func remotePairCode(p remotePeer) string {
	h := sha256.Sum256([]byte(p.KeyFP + "|" + p.ID))
	n := binary.BigEndian.Uint32(h[:4])%900000 + 100000
	return strconv.Itoa(int(n))
}

// remoteNotePending records (or refreshes) an unpaired node that
// knocked on the remote mux, so the Settings page can offer it for
// approval. Pending entries nobody acts on age out after 15
// minutes without a knock.
func remoteNotePending(p remotePeer) {
	rmt.Lock()
	defer rmt.Unlock()
	remoteLoadLocked()
	if e, ok := rmt.pending[p.ID]; ok {
		e.LastSeen = time.Now()
		return
	}
	rmt.pending[p.ID] = &remotePending{Peer: p, Code: remotePairCode(p), FirstSeen: time.Now(), LastSeen: time.Now()}
	logf("remote", "info", "未配对节点来访，已列入待配对：%s（配对码 %s），去 设置 → 安全与隐私 核对批准", p.Name, remotePairCode(p))
}

// remotePrunePendingLocked drops stale pending entries. Caller
// holds rmt.mu.
func remotePrunePendingLocked() {
	cut := time.Now().Add(-15 * time.Minute)
	for id, e := range rmt.pending {
		if e.LastSeen.Before(cut) {
			delete(rmt.pending, id)
		}
	}
}

// ---------- lifecycle ----------

// remoteInit loads the operator store and starts the reconcile
// loop. Called once from runServer; cheap when the feature is off
// (the loop just ticks and sees the switch is off).
func remoteInit() {
	rmt.Lock()
	remoteLoadLocked()
	n := len(rmt.operators)
	rmt.Unlock()
	if n > 0 {
		logf("remote", "info", "远程协助：已配对操作者 %d 位", n)
	}
	go remoteReconcileLoop()
	if settingRemoteEnabled() {
		remoteKick()
	}
}

func remoteReconcileLoop() {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-remoteKickCh:
		case <-tick.C:
		}
		remoteReconcile()
	}
}

// remoteReconcile drives the backend towards the master switch:
// switch on + not running -> start; switch off + running -> stop.
func remoteReconcile() {
	if vaultLocked() {
		// v0.48: the tsnet state lives inside the (sealed) Data
		// tree -- the backend must stay down until unlock. The
		// quiesce already stopped it; never restart it here.
		return
	}
	want := settingRemoteEnabled()
	rmt.Lock()
	if want && !rmt.running && !rmt.starting {
		rmt.starting = true
		be := remoteBackendLocked()
		rmt.Unlock()
		go func() {
			err := be.Start(remoteMuxHandler())
			rmt.Lock()
			rmt.starting = false
			if err != nil {
				rmt.Unlock()
				if settingRemoteEnabled() {
					logf("remote", "error", "远程协助组网启动失败：%v", err)
				} else {
					logf("remote", "info", "远程协助已在组网启动期间关闭")
				}
				return
			}
			if !settingRemoteEnabled() {
				// The switch was flipped back off while Start was
				// blocked (e.g. waiting for a login): come up just
				// long enough to go straight back down.
				rmt.Unlock()
				be.Stop()
				logf("remote", "info", "远程协助在组网完成前已被关闭，入口已撤下")
				return
			}
			rmt.running = true
			rmt.Unlock()
			logf("remote", "info", "远程协助已开启：受限入口监听 tailnet :%d（仅已配对操作者可入）", remotePort)
		}()
		return
	}
	if !want && (rmt.running || rmt.starting) {
		be := rmt.be
		rmt.running = false
		rmt.Unlock()
		if be != nil {
			// Stop also unblocks a Start still waiting inside
			// Listen (e.g. nobody ever completed the login):
			// closing the server makes it fail fast, and the
			// start goroutine then just notes the shutdown.
			be.Stop()
		}
		logf("remote", "info", "远程协助已关闭，tailnet 入口已撤下")
		return
	}
	rmt.Unlock()
}

// remoteShutdown stops the networking for daemon shutdown paths
// (退出系统 / idle exit / update apply), next to ocStopForShutdown.
func remoteShutdown() {
	rmt.Lock()
	be := rmt.be
	was := rmt.running
	rmt.running = false
	rmt.Unlock()
	if was && be != nil {
		be.Stop()
	}
}

// remoteSupported reports whether this build/platform has the
// tsnet backend compiled in.
func remoteSupported() bool { return remotePlatformSupported }

// ---------- local state / pairing API (127.0.0.1 mux only) ----------

func remoteStateMap() map[string]any {
	ns := remoteNetState{State: "off"}
	if !remotePlatformSupported {
		ns.State = "unsupported"
	} else {
		rmt.Lock()
		be := rmt.be
		running, starting := rmt.running, rmt.starting
		rmt.Unlock()
		switch {
		case running && be != nil:
			ns = be.State()
		case starting:
			ns.State = "starting"
		}
	}
	rmt.Lock()
	remoteLoadLocked()
	remotePrunePendingLocked()
	nOps, nPend := len(rmt.operators), len(rmt.pending)
	rmt.Unlock()
	last := remoteLastActivity.Load()
	return map[string]any{
		"supported":      remotePlatformSupported,
		"enabled":        settingRemoteEnabled(),
		"netState":       ns.State,
		"authURL":        ns.AuthURL,
		"ips":            ns.IPs,
		"device":         ns.Device,
		"err":            ns.Err,
		"port":           remotePort,
		"operators":      nOps,
		"pending":        nPend,
		"recentActivity": last > 0 && time.Now().Unix()-last <= 60,
		// v0.47: read-only session flag for the menu bar privacy
		// indicator (orange dot while an operator session is live).
		"sessionActive": remoteSessionActive(),
	}
}

func remoteStateHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, remoteStateMap())
}

// remoteOpenLoginHandler serves POST /api/remote/openlogin on the
// LOCAL mux ONLY. It is deliberately absent from remoteRoutes():
// nobody on the tailnet may ask this daemon to open a browser.
// It opens the Tailscale login page in the terminal's OWN isolated
// browser (v0.34 fix: the Settings page used to link the AuthURL
// with target=_blank, which escaped into the HOST's default
// browser). The URL is never taken from the request and never
// written to the log: the handler reads the AuthURL this daemon is
// itself holding for the pending tsnet login, and that link
// carries login semantics, so it is treated like a secret.
func remoteOpenLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST only"})
		return
	}
	if runtime.GOOS != "windows" {
		writeJSON(w, map[string]any{"error": "当前不是 Windows 环境（这是测试环境），到 Windows 上会在终端的隔离浏览器里打开登录页"})
		return
	}
	u := remoteStateMap()["authURL"]
	authURL, _ := u.(string)
	if authURL == "" {
		writeJSON(w, map[string]any{"error": "现在没有待登录的链接（远程协助未开启，或已经登录完成）"})
		return
	}
	if !strings.HasPrefix(authURL, "https://") {
		logf("remote", "warn", "登录链接不是 https，已拒绝在浏览器中打开")
		writeJSON(w, map[string]any{"error": "登录链接异常，已拒绝打开"})
		return
	}
	// Through the node when in-app acceleration is on and nodes
	// exist (the same rule as opening a website app), with a direct
	// retry when the node cannot start.
	err := error(nil)
	viaNode := accelEnabled() && len(nodes) > 0
	if viaNode {
		if err = launchIsolatedBrowser(true, authURL, false); err != nil {
			logf("remote", "warn", "登录页走节点打开失败，降级隔离浏览器直连：%v", err)
		}
	}
	if !viaNode || err != nil {
		err = launchIsolatedBrowser(false, authURL, false)
	}
	if err != nil {
		logf("remote", "error", "在终端内打开登录页失败：%v", err)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	logf("remote", "info", "已在终端内打开登录页（隔离浏览器）")
	writeJSON(w, map[string]any{"ok": true})
}

type remotePendingView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Device string `json:"device"`
	Code   string `json:"code"`
}

func remotePairingView() map[string]any {
	rmt.Lock()
	defer rmt.Unlock()
	remoteLoadLocked()
	remotePrunePendingLocked()
	pend := []remotePendingView{}
	for _, e := range rmt.pending {
		pend = append(pend, remotePendingView{ID: e.Peer.ID, Name: e.Peer.Name, Device: e.Peer.Device, Code: e.Code})
	}
	ops := rmt.operators
	if ops == nil {
		ops = []remoteOperator{}
	}
	lastSeen := map[string]string{}
	for id, t := range rmt.lastSeen {
		lastSeen[id] = t.Format("2006-01-02 15:04:05")
	}
	return map[string]any{"pending": pend, "operators": ops, "lastSeen": lastSeen}
}

// remotePairingHandler serves the LOCAL pairing management API.
// POST actions: approve | reject | revoke | tier | clear.
// Pairing management is deliberately not reachable from the
// remote mux at any tier.
func remotePairingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, remotePairingView())
		return
	}
	var body struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Name   string `json:"name"`
		Tier   string `json:"tier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	rmt.Lock()
	remoteLoadLocked()
	fail := ""
	switch body.Action {
	case "approve":
		e, ok := rmt.pending[body.ID]
		if !ok {
			fail = "这条待配对请求已经不在了（可能已过期），让对方再访问一次即可"
			break
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = e.Peer.Name
		}
		rmt.operators = append(rmt.operators, remoteOperator{
			ID: e.Peer.ID, Name: name, Device: e.Peer.Device,
			Tier: "read", PairedAt: time.Now().Format("2006-01-02 15:04:05"),
		})
		delete(rmt.pending, body.ID)
		remoteSaveLocked()
		logf("remote", "info", "已批准配对：操作者「%s」（设备 %s，只读档），对方现在可以远程查看状态与日志", name, e.Peer.Device)
	case "reject":
		if e, ok := rmt.pending[body.ID]; ok {
			delete(rmt.pending, body.ID)
			logf("remote", "info", "已拒绝待配对节点：%s", e.Peer.Name)
		}
	case "revoke":
		for i := range rmt.operators {
			if rmt.operators[i].ID == body.ID {
				logf("remote", "info", "已撤销操作者「%s」的远程资格", rmt.operators[i].Name)
				rmt.operators = append(rmt.operators[:i], rmt.operators[i+1:]...)
				remoteSaveLocked()
				break
			}
		}
	case "tier":
		if body.Tier != "read" && body.Tier != "assist" {
			fail = "档位只能是 read 或 assist"
			break
		}
		for i := range rmt.operators {
			if rmt.operators[i].ID == body.ID {
				rmt.operators[i].Tier = body.Tier
				remoteSaveLocked()
				logf("remote", "info", "操作者「%s」的档位已改为 %s", rmt.operators[i].Name, body.Tier)
				break
			}
		}
	case "clear":
		n := len(rmt.operators)
		rmt.operators = nil
		rmt.pending = map[string]*remotePending{}
		remoteSaveLocked()
		rmt.Unlock()
		settingsSetRemoteEnabledInternal(false)
		logf("remote", "info", "已断开并清除全部配对（原有操作者 %d 位），远程协助关闭", n)
		writeJSON(w, remotePairingView())
		return
	default:
		fail = "未知操作"
	}
	rmt.Unlock()
	if fail != "" {
		writeJSON(w, map[string]any{"error": fail})
		return
	}
	writeJSON(w, remotePairingView())
}

// ---------- the remote mux ----------

type remoteRoute struct {
	Method string
	Path   string
	Tier   string // "read" | "assist"
	Note   string
	H      http.HandlerFunc
}

type remoteOpCtxKey struct{}

func remoteOperatorOf(r *http.Request) *remoteOperator {
	op, _ := r.Context().Value(remoteOpCtxKey{}).(*remoteOperator)
	return op
}

func remoteRoutes() []remoteRoute {
	return []remoteRoute{
		// --- read tier: state, logs, diagnostics ---
		{"GET", "/api/remote/capabilities", "read", "自描述：版本、端点清单与所需档位、你自己的身份", remoteCapabilitiesHandler},
		{"GET", "/api/remote/state", "read", "组网状态、tailnet 地址、已配对人数", remoteRemoteStateHandler},
		{"GET", "/api/remote/diagstatus", "read", "脱敏版诊断状态总览", remoteDiagStatusHandler},
		{"GET", "/api/remote/diagexport", "read", "诊断包（mj.log + 脱敏状态）下载", remoteDiagExportHandler},
		{"GET", "/api/diag/logs", "read", "控制台日志（hub 本就脱敏）", diagLogsHandler},
		{"GET", "/api/system", "read", "系统信息（版本/系统/目录）", sysInfo},
		{"GET", "/api/settings", "read", "全部设置（不含任何秘密）", remoteSettingsGetHandler},
		{"GET", "/api/storage/usage", "read", "存储占用盘点", storageUsageHandler},
		{"GET", "/api/node", "read", "节点列表（仅名称/类型/服务器/端口）", nodeControl},
		{"GET", "/api/accel", "read", "全局加速状态", accelHandler},
		{"GET", "/api/extwin", "read", "台前调度外部窗口列表", extWinListHandler},
		{"GET", "/api/firefox", "read", "Firefox 组件状态", ffStatusHandler},
		{"GET", "/api/openclaw/status", "read", "小龙虾状态（令牌已脱敏）", remoteOCStatusHandler},
		{"GET", "/api/servermon", "read", "服务器监控总览（不含密钥信息）", remoteServermonHandler},
		{"POST", "/api/update/check", "read", "检查更新（只查不下）", updateCheckHandler},
		// --- assist tier: reversible, UI-equivalent writes ---
		{"POST", "/api/settings", "assist", "改设置（白名单校验；remoteEnabled 被整体拒绝）", remoteSettingsPostHandler},
		{"POST", "/api/core", "assist", "节点核心 start/stop/status", coreControl},
		{"POST", "/api/accel", "assist", "全局加速开关", accelHandler},
		{"POST", "/api/openclaw/start", "assist", "启动小龙虾", ocStartHandler},
		{"POST", "/api/openclaw/stop", "assist", "停止小龙虾", ocStopHandler},
		{"POST", "/api/update/download", "assist", "下载已检查到的更新", updateDownloadHandler},
		{"POST", "/api/update/apply", "assist", "应用更新（守护进程交接重启）", updateApplyHandler},
	}
}

// remoteMuxHandler is the single entry of the tailnet listener.
// Auth chain, in order, with NO bypasses:
//  1. resolve the source via the backend's WhoIs; failure = 403.
//  2. paired-operator lookup by node identity; miss = record as
//     pending + 403.
//  3. route lookup; miss = 404.
//  4. tier gate; a read operator on an assist route = 403.
//  5. log the request (module remote) and serve.
func remoteMuxHandler() http.Handler {
	routes := remoteRoutes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rmt.Lock()
		be := rmt.be
		rmt.Unlock()
		if be == nil {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "远程协助未启动"})
			return
		}
		peer, err := be.WhoIs(r.RemoteAddr)
		if err != nil || peer.ID == "" {
			logf("remote", "warn", "拒绝远程请求：无法确认来源身份（%s %s，来源 %s）", r.Method, r.URL.Path, r.RemoteAddr)
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "无法确认你的 tailnet 身份"})
			return
		}
		op := remoteFindOperator(peer.ID)
		if op == nil {
			remoteNotePending(peer)
			logf("remote", "warn", "拒绝远程请求：节点 %s 尚未配对（%s %s）", peer.Name, r.Method, r.URL.Path)
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "这个节点还没有被终端主人配对批准，请先在终端的 设置 → 安全与隐私 里核对配对码并批准"})
			return
		}
		var route *remoteRoute
		for i := range routes {
			if routes[i].Method == r.Method && routes[i].Path == r.URL.Path {
				route = &routes[i]
				break
			}
		}
		if route == nil {
			logf("remote", "warn", "拒绝远程请求：路径不在远程白名单内（操作者 %s，%s %s）", op.Name, r.Method, r.URL.Path)
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "这个接口不对远程开放"})
			return
		}
		if route.Tier == "assist" && op.Tier != "assist" {
			logf("remote", "warn", "拒绝远程请求：操作者 %s 是只读档，不能执行写操作（%s %s）", op.Name, r.Method, r.URL.Path)
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "你是只读档，这个动作需要终端主人把你升为协助档"})
			return
		}
		if route.Tier == "assist" && vaultLocked() {
			// v0.48 second line of defence (the listener is
			// normally down while locked -- its state is sealed
			// inside the vault): if a request still arrives, all
			// write routes refuse cleanly while locked. Read
			// routes keep their scrubbed behaviour.
			logf("remote", "warn", "拒绝远程写请求：保险库锁定中（操作者 %s，%s %s）", op.Name, r.Method, r.URL.Path)
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "终端机已锁定（保险库），解锁后才能执行写操作"})
			return
		}
		remoteLastActivity.Store(time.Now().Unix())
		rmt.Lock()
		if rmt.lastSeen != nil {
			rmt.lastSeen[op.ID] = time.Now()
		}
		rmt.Unlock()
		logf("remote", "info", "远程请求放行：%s %s（操作者 %s · %s 档）", r.Method, r.URL.Path, op.Name, op.Tier)
		// v0.47 hook (notify): observe session start/end for the
		// notification events and the privacy dot. Observation
		// only -- nothing about the request changes.
		notifyRemoteActivity(op.Name)
		ctx := context.WithValue(r.Context(), remoteOpCtxKey{}, op)
		route.H(w, r.WithContext(ctx))
	})
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------- remote handlers & shaping ----------

func remoteCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	op := remoteOperatorOf(r)
	eps := []map[string]any{}
	for _, rt := range remoteRoutes() {
		eps = append(eps, map[string]any{"method": rt.Method, "path": rt.Path, "tier": rt.Tier, "note": rt.Note})
	}
	writeJSON(w, map[string]any{
		"app":     "MJ-Terminal",
		"version": AppVersion,
		"operator": map[string]any{
			"name": op.Name, "tier": op.Tier, "device": op.Device,
		},
		"tiers": map[string]any{
			"read":   "只读诊断：状态、日志、诊断包、各功能状态、检查更新",
			"assist": "协助操作：在只读之上加可逆写操作（设置白名单内、节点启停、组件启停、更新下载与应用）",
		},
		"endpoints": eps,
		"neverServed": []string{
			"文件内容与文件接口", "SSH 私钥与密码", "节点分享链接与订阅地址",
			"小龙虾网关令牌原文", "更新令牌", "退出系统", "应用安装/卸载",
			"小龙虾配置向导", "远程协助自身的开关与配对管理",
		},
		"notes": []string{
			"你发出的每一个请求都会记入终端的控制台（模块 remote），终端主人可随时在 设置 → 安全与隐私 撤销你或改你的档位。",
			"冷启动先读本端点；排障主线：/api/remote/diagstatus 看总览，/api/diag/logs 看日志（支持 after/level/module 参数），/api/remote/diagexport 取完整诊断包。",
		},
	})
}

func remoteRemoteStateHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, remoteStateMap())
}

// remoteDiagStatusMap is diagStatusMap() passed through an
// allowlist: the remote variant is built key by key so a future
// local field cannot leak out here by accident.
func remoteDiagStatusMap() map[string]any {
	src := diagStatusMap()
	out := map[string]any{}
	for _, k := range []string{
		"version", "os", "arch", "port", "uptimeSec", "uptimeText",
		"coreRunning", "nodeConnected", "nodeCount", "accelEnabled",
		"openclaw", "extWindows", "sshConnections", "sshLive", "logCount",
	} {
		if v, ok := src[k]; ok {
			out[k] = v
		}
	}
	return out
}

func remoteDiagStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, remoteDiagStatusMap())
}

// remoteDiagExportHandler is the remote twin of the local
// diagnostics export, minus everything the spec excludes: the
// bundle is exactly mj.log (already redacted by the hub's logging
// discipline) + the shaped status.json. No core log, no files.
func remoteDiagExportHandler(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		fw, err := zw.Create(name)
		if err == nil {
			_, _ = fw.Write(data)
		}
	}
	if b, err := json.MarshalIndent(remoteDiagStatusMap(), "", "  "); err == nil {
		add("status.json", b)
	}
	if b, err := os.ReadFile(diagLogPath()); err == nil {
		add("mj.log", b)
	} else {
		add("mj.log", []byte("(暂无日志文件)"))
	}
	_ = zw.Close()
	ts := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"mj-diag-remote-%s.zip\"", ts))
	_, _ = w.Write(buf.Bytes())
	logf("remote", "info", "远程诊断包已导出（%d 字节）", buf.Len())
}

// remoteOCStatusMap shapes the OpenClaw status for remote eyes:
// the raw gateway token is replaced by hasToken + a mask (same
// mask style the desktop UI shows its owner).
func remoteOCStatusMap() map[string]any {
	m := ocStatusMap()
	tok, _ := m["token"].(string)
	delete(m, "token")
	m["hasToken"] = tok != ""
	m["tokenMasked"] = remoteMaskToken(tok)
	return m
}

func remoteMaskToken(t string) string {
	if len(t) > 8 {
		return t[:4] + "••••••••" + t[len(t)-4:]
	}
	if t != "" {
		return "••••••••"
	}
	return ""
}

func remoteOCStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, remoteOCStatusMap())
}

// remoteServermonOverview mirrors the local server list minus the
// fields a remote operator has no use for (key file name, auth
// type) -- identity, config, usage and the latest sample only.
func remoteServermonHandler(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID     string     `json:"id"`
		Name   string     `json:"name"`
		Host   string     `json:"host"`
		Port   int        `json:"port"`
		User   string     `json:"user"`
		Proxy  bool       `json:"proxy"`
		Cfg    monCfg     `json:"cfg"`
		Usage  *monUsage  `json:"usage"`
		Snap   *monSample `json:"snap,omitempty"`
		Engine bool       `json:"engine"`
	}
	out := []item{}
	for _, c := range sshLoadConns() {
		st := monState(c.ID)
		st.mu.Lock()
		snap := st.snap
		eng := st.engine
		st.mu.Unlock()
		u := monGetUsage(c.ID)
		monUsageMu.Lock()
		ucopy := *u
		monUsageMu.Unlock()
		if snap != nil {
			cp := *snap
			am := monAgentGet(c.ID)
			cp.Agent = am.Installed
			cp.AgentLast = am.LastTs
			cp.AgentSync = am.LastSync
			snap = &cp
		}
		out = append(out, item{
			ID: c.ID, Name: c.Name, Host: c.Host, Port: c.Port, User: c.User, Proxy: c.Proxy,
			Cfg: monGetCfg(c.ID), Usage: &ucopy, Snap: snap, Engine: eng,
		})
	}
	writeJSON(w, map[string]any{"servers": out})
}

func remoteSettingsGetHandler(w http.ResponseWriter, r *http.Request) {
	m := settingsMap()
	delete(m, "fresh")
	writeJSON(w, m)
}

// remoteSettingsPostHandler enforces the one exclusion the shared
// settings whitelist cannot express: remote-assistance keys are
// local-only. A body that even mentions remoteEnabled is rejected
// whole (no partial apply), then the untouched request is handed
// to the normal settings handler with all its validation+logging.
func remoteSettingsPostHandler(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		writeJSON(w, map[string]any{"error": "请求内容不对"})
		return
	}
	if _, bad := probe["remoteEnabled"]; bad {
		op := remoteOperatorOf(r)
		name := "?"
		if op != nil {
			name = op.Name
		}
		logf("remote", "warn", "拒绝远程改设置：请求包含 remoteEnabled（操作者 %s），远程开关只许本机操作", name)
		writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "远程协助自身的开关不接受远程修改"})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	settingsHandler(w, r)
}
