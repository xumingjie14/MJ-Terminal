package main

// ---------- Server monitor panel (v0.26) ----------
//
// Agentless server monitoring, Nezha-grade in scope, built on the
// SSH connections the user already saved: one batched exec per
// sample reads /proc, df and service states straight from the
// server. Nothing is installed on the server and no new
// credentials exist -- key connections (no passphrase) and
// terminals the user already has open can be sampled
// automatically; password / passphrase connections sample on
// demand with credentials typed for that one sample (never
// stored). History and traffic totals live in Data/servermon.
// Everything reports into the diagnostics console (module
// "servermon") per the standing console rule.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ---------- types ----------

type monService struct {
	Name string `json:"name"`
	On   bool   `json:"on"`
}

type monProc struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
}

type monSample struct {
	Ts         int64        `json:"ts"`
	OK         bool         `json:"ok"`
	Err        string       `json:"err,omitempty"`
	UptimeSec  int64        `json:"uptimeSec"`
	CPUPct     float64      `json:"cpuPct"`
	MemPct     float64      `json:"memPct"`
	MemUsed    uint64       `json:"memUsed"`
	MemTotal   uint64       `json:"memTotal"`
	RxRate     float64      `json:"rxRate"` // bytes/s
	TxRate     float64      `json:"txRate"`
	RxTotal    uint64       `json:"rxTotal"` // NIC counters (since boot)
	TxTotal    uint64       `json:"txTotal"`
	DiskPct    float64      `json:"diskPct"`
	DiskUsed   uint64       `json:"diskUsed"`
	DiskTotal  uint64       `json:"diskTotal"`
	Load1      float64      `json:"load1"`
	Services   []monService `json:"services"`
	Ports      []string     `json:"ports"`
	Top        []monProc    `json:"top"`
	LiveClient bool         `json:"liveClient"` // sampled via the user's open terminal
}

type monCfg struct {
	BG           bool    `json:"bg"`           // background sampling every 60s
	QuotaGB      float64 `json:"quotaGB"`      // monthly traffic quota, 0 = unset
	AlertDisk    int     `json:"alertDisk"`    // percent, 0 = off
	AlertTraffic int     `json:"alertTraffic"` // percent of quota, 0 = off
	AlertSvc     bool    `json:"alertSvc"`     // a service that was up goes down
}

type monUsage struct {
	Day     string `json:"day"`
	DayRx   uint64 `json:"dayRx"`
	DayTx   uint64 `json:"dayTx"`
	Month   string `json:"month"`
	MonthRx uint64 `json:"monthRx"`
	MonthTx uint64 `json:"monthTx"`
	TotalRx uint64 `json:"totalRx"`
	TotalTx uint64 `json:"totalTx"`
}

type monServerState struct {
	mu           sync.Mutex
	client       *ssh.Client // sampler-owned client (key connections)
	lastCPU      []uint64    // total, idle from previous sample
	lastNet      [2]uint64   // rx, tx counters
	prevCounters [2]uint64   // counters at previous usage accumulation
	lastTs       time.Time
	snap         *monSample
	engine       bool // goroutine running
	liveUntil    time.Time
	online       bool
	alerted      map[string]string // kind -> date already alerted
	svcWasOn     map[string]bool
}

var monStatesMu sync.Mutex
var monStates = map[string]*monServerState{}

var monCfgMu sync.Mutex
var monCfgs = map[string]monCfg{}

var monUsageMu sync.Mutex
var monUsages = map[string]*monUsage{}

func monDir() string {
	d := filepath.Join(dataDir(), "servermon")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func monState(id string) *monServerState {
	monStatesMu.Lock()
	defer monStatesMu.Unlock()
	st, ok := monStates[id]
	if !ok {
		st = &monServerState{alerted: map[string]string{}, svcWasOn: map[string]bool{}}
		monStates[id] = st
	}
	return st
}

// ---------- config & usage persistence ----------

func monLoadPersisted() {
	monCfgMu.Lock()
	if b, err := os.ReadFile(filepath.Join(monDir(), "config.json")); err == nil {
		_ = json.Unmarshal(b, &monCfgs)
	}
	monCfgMu.Unlock()
	monUsageMu.Lock()
	var u map[string]*monUsage
	if b, err := os.ReadFile(filepath.Join(monDir(), "usage.json")); err == nil {
		if json.Unmarshal(b, &u) == nil && u != nil {
			monUsages = u
		}
	}
	monUsageMu.Unlock()
}

func monSaveCfgs() {
	monCfgMu.Lock()
	defer monCfgMu.Unlock()
	b, _ := json.MarshalIndent(monCfgs, "", " ")
	_ = os.WriteFile(filepath.Join(monDir(), "config.json"), b, 0o600)
}

func monSaveUsages() {
	monUsageMu.Lock()
	defer monUsageMu.Unlock()
	b, _ := json.MarshalIndent(monUsages, "", " ")
	_ = os.WriteFile(filepath.Join(monDir(), "usage.json"), b, 0o600)
}

func monGetCfg(id string) monCfg {
	monCfgMu.Lock()
	defer monCfgMu.Unlock()
	if c, ok := monCfgs[id]; ok {
		return c
	}
	return monCfg{AlertDisk: 90, AlertTraffic: 90, AlertSvc: true}
}

func monGetUsage(id string) *monUsage {
	monUsageMu.Lock()
	defer monUsageMu.Unlock()
	u, ok := monUsages[id]
	if !ok {
		u = &monUsage{}
		monUsages[id] = u
	}
	return u
}

// ---------- sampling ----------

const monProbeCmd = `echo ==UP==; cat /proc/uptime; echo ==STAT==; head -n 1 /proc/stat; echo ==MEM==; grep -E '^(MemTotal|MemAvailable):' /proc/meminfo; echo ==NET==; cat /proc/net/dev; echo ==DISK==; df -P / | tail -n 1; echo ==LOAD==; cat /proc/loadavg; echo ==SVC==; for s in sing-box xray hysteria-server hysteria nginx docker; do if systemctl is-active --quiet $s 2>/dev/null || pgrep -x $s >/dev/null 2>&1; then echo "$s on"; else echo "$s off"; fi; done; echo ==PORTS==; (ss -tlnH 2>/dev/null || netstat -tln 2>/dev/null) | head -n 40; echo ==PS==; ps -eo comm,%cpu,%mem --sort=-%mem 2>/dev/null | head -n 7; echo ==END==`

func monExec(cl *ssh.Client, cmd string, timeout time.Duration) (string, error) {
	type res struct {
		out string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		sess, err := cl.NewSession()
		if err != nil {
			ch <- res{"", err}
			return
		}
		defer sess.Close()
		b, err := sess.CombinedOutput(cmd)
		ch <- res{string(b), err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(timeout):
		return "", fmt.Errorf("采样超时")
	}
}

// monClientFor returns a usable SSH client for a connection:
// the user's open terminal first, then a sampler-owned dial for
// key connections without a passphrase.
func monClientFor(st *monServerState, c sshConn) (*ssh.Client, bool, error) {
	if cl, ok := sshLiveGet(c.ID); ok {
		return cl, true, nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.client != nil {
		if _, _, err := st.client.SendRequest("keepalive@mj", true, nil); err == nil {
			return st.client, false, nil
		}
		_ = st.client.Close()
		st.client = nil
	}
	if c.Auth != "key" {
		return nil, false, fmt.Errorf("密码连接需要先在 SSH 应用里连上，或在面板里输一次密码手动采样")
	}
	cl, err := dialSSH(c, "", "")
	if err != nil {
		if strings.Contains(err.Error(), "私钥解析失败") {
			return nil, false, fmt.Errorf("密钥带口令，自动采样需要先在 SSH 应用里连上，或在面板里输口令手动采样")
		}
		return nil, false, err
	}
	st.client = cl
	logf("servermon", "info", "监控采样已连接：%s@%s:%d", c.User, c.Host, c.Port)
	return cl, false, nil
}

func monParseSections(out string) map[string]string {
	secs := map[string]string{}
	cur := ""
	var sb strings.Builder
	flush := func() {
		if cur != "" {
			secs[cur] = sb.String()
		}
		sb.Reset()
	}
	for _, ln := range strings.Split(out, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "==") && strings.HasSuffix(t, "==") && len(t) > 4 {
			flush()
			cur = strings.Trim(t, "=")
			continue
		}
		sb.WriteString(ln + "\n")
	}
	flush()
	return secs
}

func monTakeSample(st *monServerState, c sshConn) *monSample {
	now := time.Now()
	s := &monSample{Ts: now.Unix()}
	cl, live, err := monClientFor(st, c)
	if err != nil {
		s.Err = err.Error()
		return s
	}
	s.LiveClient = live
	out, err := monExec(cl, monProbeCmd, 15*time.Second)
	if err != nil && out == "" {
		s.Err = "采样失败：" + err.Error()
		if !live { // drop a possibly dead sampler client so next sample redials
			st.mu.Lock()
			if st.client == cl {
				_ = st.client.Close()
				st.client = nil
			}
			st.mu.Unlock()
		}
		return s
	}
	secs := monParseSections(out)

	// uptime
	if f := strings.Fields(secs["UP"]); len(f) >= 1 {
		if v, e := strconv.ParseFloat(f[0], 64); e == nil {
			s.UptimeSec = int64(v)
		}
	}
	// cpu (needs previous counters for a percentage)
	var tot, idle uint64
	if f := strings.Fields(secs["STAT"]); len(f) >= 9 && f[0] == "cpu" {
		for i := 1; i < len(f); i++ {
			v, _ := strconv.ParseUint(f[i], 10, 64)
			tot += v
			if i == 4 || i == 5 { // idle + iowait
				idle += v
			}
		}
	}
	// memory
	var memT, memA uint64
	for _, ln := range strings.Split(secs["MEM"], "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 {
			v, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				memT = v * 1024
			case "MemAvailable:":
				memA = v * 1024
			}
		}
	}
	if memT > 0 {
		s.MemTotal = memT
		s.MemUsed = memT - memA
		s.MemPct = float64(s.MemUsed) * 100 / float64(memT)
	}
	// network (sum all interfaces except loopback)
	var rx, tx uint64
	for _, ln := range strings.Split(secs["NET"], "\n") {
		i := strings.Index(ln, ":")
		if i < 0 || strings.TrimSpace(ln[:i]) == "lo" {
			continue
		}
		f := strings.Fields(ln[i+1:])
		if len(f) >= 9 {
			r, _ := strconv.ParseUint(f[0], 10, 64)
			t, _ := strconv.ParseUint(f[8], 10, 64)
			rx += r
			tx += t
		}
	}
	s.RxTotal, s.TxTotal = rx, tx
	// disk (root filesystem): the probe pipes df through tail -1,
	// but parse the last non-empty line defensively anyway.
	diskLine := ""
	for _, ln := range strings.Split(secs["DISK"], "\n") {
		if strings.TrimSpace(ln) != "" {
			diskLine = ln
		}
	}
	if f := strings.Fields(diskLine); len(f) >= 5 {
		sizeKB, _ := strconv.ParseUint(f[1], 10, 64)
		usedKB, _ := strconv.ParseUint(f[2], 10, 64)
		s.DiskTotal = sizeKB * 1024
		s.DiskUsed = usedKB * 1024
		if s.DiskTotal > 0 {
			s.DiskPct = float64(s.DiskUsed) * 100 / float64(s.DiskTotal)
		}
	}
	// load
	if f := strings.Fields(secs["LOAD"]); len(f) >= 1 {
		s.Load1, _ = strconv.ParseFloat(f[0], 64)
	}
	// services
	for _, ln := range strings.Split(strings.TrimSpace(secs["SVC"]), "\n") {
		if f := strings.Fields(ln); len(f) == 2 {
			s.Services = append(s.Services, monService{Name: f[0], On: f[1] == "on"})
		}
	}
	// listening ports
	seenPort := map[string]bool{}
	for _, ln := range strings.Split(secs["PORTS"], "\n") {
		f := strings.Fields(ln)
		for _, tok := range f {
			if i := strings.LastIndex(tok, ":"); i >= 0 {
				p := tok[i+1:]
				if _, e := strconv.Atoi(p); e == nil && !seenPort[p] {
					seenPort[p] = true
					s.Ports = append(s.Ports, p)
				}
			}
		}
	}
	sort.Slice(s.Ports, func(i, j int) bool {
		a, _ := strconv.Atoi(s.Ports[i])
		b, _ := strconv.Atoi(s.Ports[j])
		return a < b
	})
	// top processes
	lines := strings.Split(strings.TrimSpace(secs["PS"]), "\n")
	for i, ln := range lines {
		if i == 0 || ln == "" { // header
			continue
		}
		if f := strings.Fields(ln); len(f) >= 3 {
			cpu, _ := strconv.ParseFloat(f[1], 64)
			mem, _ := strconv.ParseFloat(f[2], 64)
			s.Top = append(s.Top, monProc{Name: f[0], CPU: cpu, Mem: mem})
		}
	}

	// deltas (cpu %, rates, usage accumulation)
	st.mu.Lock()
	if st.lastCPU != nil && tot > st.lastCPU[0] {
		dTot := float64(tot - st.lastCPU[0])
		dIdle := float64(idle - st.lastCPU[1])
		if dTot > 0 {
			s.CPUPct = (1 - dIdle/dTot) * 100
		}
	}
	if !st.lastTs.IsZero() && (rx >= st.lastNet[0] || tx >= st.lastNet[1]) {
		dt := now.Sub(st.lastTs).Seconds()
		if dt > 0 {
			if rx >= st.lastNet[0] {
				s.RxRate = float64(rx-st.lastNet[0]) / dt
			}
			if tx >= st.lastNet[1] {
				s.TxRate = float64(tx-st.lastNet[1]) / dt
			}
		}
	}
	st.lastCPU = []uint64{tot, idle}
	st.lastNet = [2]uint64{rx, tx}
	st.lastTs = now
	st.mu.Unlock()

	monAccumulateUsage(st, c.ID, rx, tx, now)
	s.OK = true
	return s
}

// monAccumulateUsage folds the NIC counters into the persisted
// day/month/total usage for a server, using the state's previous
// counters for the delta. A counter reset (reboot) counts from
// zero. Usage only accrues while the daemon samples -- totals are
// the terminal's own bookkeeping, not the provider's bill.
func monAccumulateUsage(st *monServerState, id string, rx, tx uint64, now time.Time) {
	st.mu.Lock()
	prev := st.prevCounters
	st.prevCounters = [2]uint64{rx, tx}
	st.mu.Unlock()
	if prev[0] == 0 && prev[1] == 0 {
		return // first sample only establishes the baseline
	}
	var drx, dtx uint64
	if rx >= prev[0] {
		drx = rx - prev[0]
	} else {
		drx = rx
	}
	if tx >= prev[1] {
		dtx = tx - prev[1]
	} else {
		dtx = tx
	}
	if drx == 0 && dtx == 0 {
		return
	}
	u := monGetUsage(id)
	monUsageMu.Lock()
	day := now.Format("2006-01-02")
	month := now.Format("2006-01")
	if u.Day != day {
		u.Day, u.DayRx, u.DayTx = day, 0, 0
	}
	if u.Month != month {
		u.Month, u.MonthRx, u.MonthTx = month, 0, 0
	}
	u.DayRx += drx
	u.DayTx += dtx
	u.MonthRx += drx
	u.MonthTx += dtx
	u.TotalRx += drx
	u.TotalTx += dtx
	monUsageMu.Unlock()
	monSaveUsages()
}

// ---------- history persistence ----------

type monPoint struct {
	Ts   int64   `json:"ts"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Rx   float64 `json:"rx"`
	Tx   float64 `json:"tx"`
	Disk float64 `json:"disk"`
}

func monAppendHistory(id string, s *monSample) {
	p := monPoint{Ts: s.Ts, CPU: s.CPUPct, Mem: s.MemPct, Rx: s.RxRate, Tx: s.TxRate, Disk: s.DiskPct}
	b, _ := json.Marshal(p)
	f, err := os.OpenFile(filepath.Join(monDir(), "hist-"+id+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func monReadHistory(id string, since int64) []monPoint {
	f, err := os.Open(filepath.Join(monDir(), "hist-"+id+".jsonl"))
	if err != nil {
		return []monPoint{}
	}
	defer f.Close()
	out := []monPoint{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var p monPoint
		if json.Unmarshal(sc.Bytes(), &p) == nil && p.Ts >= since {
			out = append(out, p)
		}
	}
	// Downsample to at most ~300 points for the chart.
	if len(out) > 300 {
		step := float64(len(out)) / 300
		ds := make([]monPoint, 0, 300)
		for i := 0; i < 300; i++ {
			ds = append(ds, out[int(float64(i)*step)])
		}
		out = ds
	}
	return out
}

func monPruneHistory() {
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	ents, err := os.ReadDir(monDir())
	if err != nil {
		return
	}
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "hist-") {
			continue
		}
		path := filepath.Join(monDir(), e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var keep []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			var p monPoint
			if json.Unmarshal(sc.Bytes(), &p) == nil && p.Ts >= cutoff {
				keep = append(keep, sc.Text())
			}
		}
		f.Close()
		_ = os.WriteFile(path, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
	}
}

// ---------- alerts & engine ----------

func monAlertOnce(st *monServerState, c sshConn, kind, level, msg string) {
	today := time.Now().Format("2006-01-02")
	st.mu.Lock()
	done := st.alerted[kind] == today
	if !done {
		st.alerted[kind] = today
	}
	st.mu.Unlock()
	if !done {
		logf("servermon", level, "告警[%s]：%s", c.Name, msg)
	}
}

func monEvaluate(st *monServerState, c sshConn, cfg monCfg, s *monSample) {
	if cfg.AlertDisk > 0 && s.DiskPct >= float64(cfg.AlertDisk) {
		monAlertOnce(st, c, "disk", "warn", fmt.Sprintf("磁盘已用 %.0f%%，超过告警线 %d%%", s.DiskPct, cfg.AlertDisk))
	}
	if cfg.QuotaGB > 0 && cfg.AlertTraffic > 0 {
		u := monGetUsage(c.ID)
		usedGB := float64(u.MonthRx+u.MonthTx) / 1e9
		pct := usedGB * 100 / cfg.QuotaGB
		if pct >= float64(cfg.AlertTraffic) {
			monAlertOnce(st, c, "traffic", "warn", fmt.Sprintf("本月流量已用 %.1f GB（%.0f%%），接近配额 %.0f GB", usedGB, pct, cfg.QuotaGB))
		}
	}
	if cfg.AlertSvc {
		for _, svc := range s.Services {
			st.mu.Lock()
			was := st.svcWasOn[svc.Name]
			st.svcWasOn[svc.Name] = svc.On
			st.mu.Unlock()
			if was && !svc.On {
				logf("servermon", "error", "服务停止[%s]：%s 已不在运行", c.Name, svc.Name)
			}
		}
	}
}

func monEngineLoop(st *monServerState, c sshConn) {
	defer func() {
		st.mu.Lock()
		st.engine = false
		st.mu.Unlock()
	}()
	var lastHist time.Time
	for {
		cfg := monGetCfg(c.ID)
		st.mu.Lock()
		live := time.Now().Before(st.liveUntil)
		st.mu.Unlock()
		if !cfg.BG && !live {
			return
		}
		s := monTakeSample(st, c)
		st.mu.Lock()
		wasOnline := st.online
		st.snap = s
		st.online = s.OK
		st.mu.Unlock()
		if s.OK {
			if !wasOnline {
				logf("servermon", "info", "服务器上线/恢复：%s（%s）", c.Name, c.Host)
			}
			if cfg.BG || lastHist.IsZero() || time.Since(lastHist) >= 30*time.Second {
				monAppendHistory(c.ID, s)
				lastHist = time.Now()
			}
			monEvaluate(st, c, cfg, s)
		} else {
			if wasOnline {
				logf("servermon", "warn", "服务器采样失败：%s：%s", c.Name, s.Err)
			}
		}
		interval := 60 * time.Second
		if live {
			interval = 5 * time.Second
		}
		time.Sleep(interval)
	}
}

func monEngineKick(c sshConn) {
	st := monState(c.ID)
	st.mu.Lock()
	running := st.engine
	if !running {
		st.engine = true
	}
	st.mu.Unlock()
	if !running {
		go monEngineLoop(st, c)
	}
}

// monInit starts background sampling for servers that opted in
// and prunes history to 30 days. Called once at daemon start.
func monInit() {
	monLoadPersisted()
	monPruneHistory()
	for _, c := range sshLoadConns() {
		if monGetCfg(c.ID).BG {
			monEngineKick(c)
			logf("servermon", "info", "后台采样已开启：%s", c.Name)
		}
	}
}

// ---------- HTTP API ----------

func monFindConn(id string) (sshConn, bool) {
	for _, c := range sshLoadConns() {
		if c.ID == id {
			return c, true
		}
	}
	return sshConn{}, false
}

func servermonListHandler(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Conn   sshConn    `json:"conn"`
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
		out = append(out, item{Conn: c, Cfg: monGetCfg(c.ID), Usage: &ucopy, Snap: snap, Engine: eng})
	}
	writeJSON(w, map[string]any{"servers": out})
}

func servermonWatchHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
		On bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求格式不对"})
		return
	}
	c, ok := monFindConn(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "连接不存在"})
		return
	}
	st := monState(c.ID)
	st.mu.Lock()
	if body.On {
		st.liveUntil = time.Now().Add(90 * time.Second)
	} else {
		st.liveUntil = time.Time{}
	}
	st.mu.Unlock()
	if body.On {
		monEngineKick(c)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func servermonConfigHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID  string `json:"id"`
		Cfg monCfg `json:"cfg"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求格式不对"})
		return
	}
	c, ok := monFindConn(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "连接不存在"})
		return
	}
	monCfgMu.Lock()
	monCfgs[c.ID] = body.Cfg
	monCfgMu.Unlock()
	monSaveCfgs()
	if body.Cfg.BG {
		monEngineKick(c)
	}
	logf("servermon", "info", "监控设置已更新：%s（后台采样 %v，配额 %.0f GB）", c.Name, body.Cfg.BG, body.Cfg.QuotaGB)
	writeJSON(w, map[string]any{"ok": true})
}

func servermonSampleHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求格式不对"})
		return
	}
	c, ok := monFindConn(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "连接不存在"})
		return
	}
	cl, err := dialSSH(c, body.Password, body.Passphrase)
	if err != nil {
		logf("servermon", "error", "手动采样连接失败：%s：%v", c.Name, err)
		writeJSON(w, map[string]any{"error": "连接失败：" + err.Error()})
		return
	}
	defer cl.Close()
	st := monState(c.ID)
	// One-shot sample on this throwaway client: borrow the state
	// machine by parking the client as the sampler client briefly.
	st.mu.Lock()
	old := st.client
	st.client = cl
	st.mu.Unlock()
	s := monTakeSample(st, c)
	st.mu.Lock()
	st.client = old
	st.snap = s
	st.online = s.OK
	st.mu.Unlock()
	if s.OK {
		monAppendHistory(c.ID, s)
	}
	writeJSON(w, map[string]any{"ok": s.OK, "snap": s})
}

func servermonHistoryHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	rng := r.URL.Query().Get("range")
	dur := 24 * time.Hour
	switch rng {
	case "1h":
		dur = time.Hour
	case "7d":
		dur = 7 * 24 * time.Hour
	case "30d":
		dur = 30 * 24 * time.Hour
	}
	since := time.Now().Add(-dur).Unix()
	writeJSON(w, map[string]any{"points": monReadHistory(id, since)})
}

func servermonExecHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		Cmd        string `json:"cmd"`
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, map[string]any{"error": "请求格式不对"})
		return
	}
	cmd := strings.TrimSpace(body.Cmd)
	if cmd == "" {
		writeJSON(w, map[string]any{"error": "命令为空"})
		return
	}
	c, ok := monFindConn(body.ID)
	if !ok {
		writeJSON(w, map[string]any{"error": "连接不存在"})
		return
	}
	st := monState(c.ID)
	var cl *ssh.Client
	var live bool
	var err error
	if body.Password != "" || body.Passphrase != "" || c.Auth == "password" {
		cl, err = dialSSH(c, body.Password, body.Passphrase)
		if err == nil {
			defer cl.Close()
		}
	} else {
		cl, live, err = monClientFor(st, c)
	}
	if err != nil {
		logf("servermon", "error", "远程命令连接失败：%s：%v", c.Name, err)
		writeJSON(w, map[string]any{"error": "连接失败：" + err.Error()})
		return
	}
	_ = live
	out, err := monExec(cl, cmd, 30*time.Second)
	if len(out) > 64*1024 {
		out = out[:64*1024] + "\n…（输出过长已截断）"
	}
	status := "ok"
	if err != nil {
		status = "error: " + err.Error()
	}
	// Command text itself is never logged (it can embed secrets);
	// target, size and outcome are enough to locate problems.
	logf("servermon", "info", "远程命令：%s@%s:%d（%d 字节， 结果 %s）", c.User, c.Host, c.Port, len(cmd), status)
	writeJSON(w, map[string]any{"ok": err == nil, "output": out, "status": status})
}
