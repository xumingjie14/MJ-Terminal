package main

// Server-side collection (v0.27): an optional tiny collector script the
// user installs on a monitored server from the panel. It appends one
// sample per minute to ~/.mjmon/data-YYYYMMDD.tsv on the SERVER's disk
// (systemd user timer, cron fallback). Collection itself makes no
// network connections at all; the panel pulls only the missing samples
// over the existing SSH connection and merges them into the local
// history + traffic accounting, so monitoring survives the PC being
// off. Everything reports into the console as module "servermon".

import (
	"bufio"
	"encoding/base64"
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

// ---------- collector payload ----------

const monCollectorScript = `#!/bin/sh
# MJ Terminal server collector (mjmon): one sample per minute,
# appended to a per-day file under ~/.mjmon. Local disk only --
# this script makes no network connections at all.
D="$HOME/.mjmon"
mkdir -p "$D" || exit 1
DAY=$(date +%Y%m%d)
F="$D/data-$DAY.tsv"
TS=$(date +%s)
UP=$(awk '{print int($1)}' /proc/uptime 2>/dev/null)
# CPU counters from the aggregate line of /proc/stat
set -- $(head -n 1 /proc/stat)
shift
CTOT=0
for v in "$@"; do CTOT=$((CTOT + v)); done
CIDLE=$(($4 + $5))
MEMT=$(awk '/^MemTotal:/{print $2 * 1024}' /proc/meminfo)
MEMA=$(awk '/^MemAvailable:/{print $2 * 1024}' /proc/meminfo)
NET=$(tail -n +3 /proc/net/dev | grep -v '^ *lo:' | awk '{split($0,a,":"); split(a[2],b," "); rx+=b[1]; tx+=b[9]} END {print rx" "tx}')
set -- $NET
RX=$1
TX=$2
set -- $(df -P / | tail -n 1)
DTOT=$(($2 * 1024))
DUSED=$(($3 * 1024))
LOAD=$(awk '{print $1}' /proc/loadavg)
SVC=""
for s in sing-box xray hysteria-server hysteria nginx docker; do
  if systemctl is-active --quiet "$s" 2>/dev/null || pgrep -x "$s" >/dev/null 2>&1; then
    SVC="${SVC}${SVC:+,}$s=1"
  else
    SVC="${SVC}${SVC:+,}$s=0"
  fi
done
echo "$TS $UP $CTOT $CIDLE $MEMT $MEMA $RX $TX $DUSED $DTOT $LOAD $SVC" >> "$F"
# Rotation: keep 35 days.
find "$D" -name 'data-*.tsv' -mtime +35 -delete 2>/dev/null
exit 0
`

const monCollectorService = `[Unit]
Description=MJ Terminal server collector

[Service]
Type=oneshot
ExecStart=%h/.mjmon/collect.sh
`

const monCollectorTimer = `[Unit]
Description=Run MJ Terminal collector every minute

[Timer]
OnBootSec=1min
OnUnitActiveSec=1min
Unit=mjmon.service

[Install]
WantedBy=timers.target
`

// ---------- agent state ----------

type monAgentMeta struct {
	Installed bool   `json:"installed"`
	LastTs    int64  `json:"lastTs"`   // newest sample timestamp seen on the server
	Lines     int    `json:"lines"`    // samples stored on the server
	LastSync  int64  `json:"lastSync"` // last successful pull (unix)
	BaseTs    int64  `json:"baseTs"`   // usage baseline: last absorbed sample
	BaseRx    uint64 `json:"baseRx"`
	BaseTx    uint64 `json:"baseTx"`
	Method    string `json:"method"` // systemd | cron | manual
}

var (
	monAgentMu    sync.Mutex
	monAgentMetas = map[string]*monAgentMeta{}
	monSyncing    = map[string]bool{}
	monAgentTry   = map[string]int64{}
)

func monAgentPath(id string) string {
	return filepath.Join(monDir(), "agent-"+id+".json")
}

func monAgentGet(id string) *monAgentMeta {
	monAgentMu.Lock()
	defer monAgentMu.Unlock()
	if m, ok := monAgentMetas[id]; ok {
		return m
	}
	m := &monAgentMeta{}
	if b, status, err := dsLoadFile(monAgentPath(id), dsJSONValidatorOf(monAgentMeta{})); err == nil && status != dsMissing {
		_ = json.Unmarshal(b, m)
	}
	monAgentMetas[id] = m
	return m
}

func monAgentSave(id string) {
	m := monAgentGet(id)
	b, err := json.Marshal(m)
	if err == nil {
		_ = dsSaveFile(monAgentPath(id), b, 0o600, dsJSONValidatorOf(monAgentMeta{}), true)
	}
}

func monAgentSetProbe(id string, installed bool, lastTs int64, lines int) {
	m := monAgentGet(id)
	monAgentMu.Lock()
	changed := m.Installed != installed
	m.Installed, m.LastTs, m.Lines = installed, lastTs, lines
	monAgentMu.Unlock()
	if changed {
		monAgentSave(id)
	}
}

func monFmtBytes(v uint64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2f GB", float64(v)/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(v)/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1f KB", float64(v)/1e3)
	}
	return fmt.Sprintf("%d B", v)
}

// ---------- client ----------

// monAgentClient mirrors servermonExecHandler: typed credentials (or a
// password connection) dial fresh; otherwise reuse the live terminal
// or the sampler's key client.
func monAgentClient(id, pw, pp string) (*ssh.Client, func(), error) {
	c, ok := monFindConn(id)
	if !ok {
		return nil, nil, fmt.Errorf("连接不存在")
	}
	if pw != "" || pp != "" || c.Auth == "password" {
		cl, err := dialSSH(c, pw, pp)
		if err != nil {
			return nil, nil, err
		}
		return cl, func() { _ = cl.Close() }, nil
	}
	cl, _, err := monClientFor(monState(c.ID), c)
	if err != nil {
		return nil, nil, err
	}
	return cl, func() {}, nil
}

// ---------- install / uninstall ----------

func monAgentInstall(cl *ssh.Client) (string, error) {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	cmd := "mkdir -p ~/.mjmon ~/.config/systemd/user" +
		" && echo " + b64(monCollectorScript) + " | base64 -d > ~/.mjmon/collect.sh && chmod +x ~/.mjmon/collect.sh" +
		" && echo " + b64(monCollectorService) + " | base64 -d > ~/.config/systemd/user/mjmon.service" +
		" && echo " + b64(monCollectorTimer) + " | base64 -d > ~/.config/systemd/user/mjmon.timer" +
		" && echo 1 > ~/.mjmon/version && ~/.mjmon/collect.sh && echo MJ-SCRIPT-OK"
	out, err := monExec(cl, cmd, 30*time.Second)
	if err != nil || !strings.Contains(out, "MJ-SCRIPT-OK") {
		if err == nil {
			err = fmt.Errorf("服务器返回异常：%s", strings.TrimSpace(out))
		}
		return "", err
	}
	// A systemd *user* timer only starts at boot (e.g. after the
	// server's nightly reboot) when lingering is on for the user --
	// so verify it, and if it cannot be confirmed, plant a crontab
	// entry as well: the system cron service always starts at boot.
	out2, _ := monExec(cl, "systemctl --user daemon-reload 2>/dev/null; systemctl --user enable --now mjmon.timer 2>&1; loginctl enable-linger $USER 2>/dev/null; systemctl --user is-active mjmon.timer 2>/dev/null", 30*time.Second)
	sysd := strings.Contains(out2, "active")
	linger := false
	if sysd {
		outL, _ := monExec(cl, "loginctl show-user $USER -p Linger 2>/dev/null", 15*time.Second)
		linger = strings.Contains(outL, "Linger=yes")
	}
	cronOK := false
	if !sysd || !linger {
		out3, _ := monExec(cl, "(crontab -l 2>/dev/null | grep -v mjmon; echo '* * * * * $HOME/.mjmon/collect.sh >/dev/null 2>&1 # mjmon') | crontab - && echo MJ-CRON-OK", 30*time.Second)
		cronOK = strings.Contains(out3, "MJ-CRON-OK")
	}
	switch {
	case sysd && linger:
		return "systemd", nil
	case sysd && cronOK:
		return "systemd+cron", nil
	case cronOK:
		return "cron", nil
	case sysd:
		return "systemd-nolinger", nil
	}
	return "manual", fmt.Errorf("脚本已写入 ~/.mjmon，但 systemd 用户定时器和 crontab 都不可用")
}

func monAgentMethodLabel(m string) string {
	switch m {
	case "systemd":
		return "systemd 定时器（开机自启已确认）"
	case "systemd+cron":
		return "systemd + crontab 双保险（开机自启）"
	case "cron":
		return "crontab（系统 cron 开机自启）"
	case "systemd-nolinger":
		return "systemd 定时器（linger 未开：重启后需登录一次才恢复采集）"
	}
	return m
}

func monAgentUninstall(cl *ssh.Client) error {
	_, err := monExec(cl, "systemctl --user disable --now mjmon.timer 2>/dev/null; rm -f ~/.config/systemd/user/mjmon.service ~/.config/systemd/user/mjmon.timer; (crontab -l 2>/dev/null | grep -v mjmon) | crontab - 2>/dev/null; rm -rf ~/.mjmon; echo MJ-REMOVED", 30*time.Second)
	return err
}

// ---------- ingest ----------

type monAgentSample struct {
	Ts          int64
	CTot, CIdle uint64
	MemT, MemA  uint64
	Rx, Tx      uint64
	DUsed, DTot uint64
}

func monParseAgentLine(line string) (monAgentSample, bool) {
	var s monAgentSample
	f := strings.Fields(line)
	if len(f) < 12 {
		return s, false
	}
	atou := func(x string) uint64 { v, _ := strconv.ParseUint(x, 10, 64); return v }
	s.Ts, _ = strconv.ParseInt(f[0], 10, 64)
	s.CTot, s.CIdle = atou(f[2]), atou(f[3])
	s.MemT, s.MemA = atou(f[4]), atou(f[5])
	s.Rx, s.Tx = atou(f[6]), atou(f[7])
	s.DUsed, s.DTot = atou(f[8]), atou(f[9])
	return s, s.Ts > 0
}

func monLoadHistAll(id string) []monPoint {
	f, err := os.Open(filepath.Join(monDir(), "hist-"+id+".jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []monPoint
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		var p monPoint
		if json.Unmarshal(sc.Bytes(), &p) == nil {
			out = append(out, p)
		}
	}
	return out
}

// monAgentIngest merges pulled samples into local history and traffic
// accounting. History points from the live engine win on equal ts.
// Usage deltas are attributed by each sample's own date: today's go
// to the day bucket, this month's to the month bucket, all to total.
func monAgentIngest(id string, raw string) (int, uint64, uint64) {
	var ss []monAgentSample
	for _, ln := range strings.Split(raw, "\n") {
		if s, ok := monParseAgentLine(strings.TrimSpace(ln)); ok {
			ss = append(ss, s)
		}
	}
	if len(ss) == 0 {
		return 0, 0, 0
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Ts < ss[j].Ts })

	// --- history merge ---
	existing := monLoadHistAll(id)
	have := map[int64]bool{}
	for _, p := range existing {
		have[p.Ts] = true
	}
	pts := existing
	merged := 0
	for i := 1; i < len(ss); i++ {
		a, b := ss[i-1], ss[i]
		dt := b.Ts - a.Ts
		if dt <= 0 || dt > 3600 || have[b.Ts] {
			continue
		}
		p := monPoint{Ts: b.Ts}
		if dTot := b.CTot - a.CTot; b.CTot > a.CTot && dTot > 0 {
			p.CPU = float64(dTot-(b.CIdle-a.CIdle)) / float64(dTot) * 100
		}
		if b.MemT > 0 {
			p.Mem = float64(b.MemT-b.MemA) / float64(b.MemT) * 100
		}
		if b.DTot > 0 {
			p.Disk = float64(b.DUsed) / float64(b.DTot) * 100
		}
		if b.Rx >= a.Rx {
			p.Rx = float64(b.Rx-a.Rx) / float64(dt)
		}
		if b.Tx >= a.Tx {
			p.Tx = float64(b.Tx-a.Tx) / float64(dt)
		}
		pts = append(pts, p)
		merged++
	}
	if merged > 0 {
		sort.Slice(pts, func(i, j int) bool { return pts[i].Ts < pts[j].Ts })
		cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
		var sb strings.Builder
		for _, p := range pts {
			if p.Ts < cutoff {
				continue
			}
			if b, err := json.Marshal(p); err == nil {
				sb.Write(b)
				sb.WriteByte('\n')
			}
		}
		_ = os.WriteFile(filepath.Join(monDir(), "hist-"+id+".jsonl"), []byte(sb.String()), 0o600)
	}

	// --- usage backfill ---
	meta := monAgentGet(id)
	var addIn, addOut uint64
	now := time.Now()
	nowDay, nowMon := now.Format("2006-01-02"), now.Format("2006-01")
	u := monGetUsage(id)
	monUsageMu.Lock()
	for i := 1; i < len(ss); i++ {
		a, b := ss[i-1], ss[i]
		if b.Ts <= meta.BaseTs {
			continue
		}
		var dIn, dOut uint64
		if b.Rx >= a.Rx {
			dIn = b.Rx - a.Rx
		} else {
			dIn = b.Rx
		}
		if b.Tx >= a.Tx {
			dOut = b.Tx - a.Tx
		} else {
			dOut = b.Tx
		}
		if dIn == 0 && dOut == 0 {
			continue
		}
		t := time.Unix(b.Ts, 0)
		if t.Format("2006-01-02") == nowDay {
			if u.Day != nowDay {
				u.Day, u.DayRx, u.DayTx = nowDay, 0, 0
			}
			u.DayRx += dIn
			u.DayTx += dOut
		}
		if t.Format("2006-01") == nowMon {
			if u.Month != nowMon {
				u.Month, u.MonthRx, u.MonthTx = nowMon, 0, 0
			}
			u.MonthRx += dIn
			u.MonthTx += dOut
		}
		u.TotalRx += dIn
		u.TotalTx += dOut
		addIn += dIn
		addOut += dOut
	}
	monUsageMu.Unlock()
	if addIn > 0 || addOut > 0 {
		monSaveUsages()
	}
	last := ss[len(ss)-1]
	if last.Ts > meta.BaseTs {
		meta.BaseTs, meta.BaseRx, meta.BaseTx = last.Ts, last.Rx, last.Tx
	}
	meta.LastSync = time.Now().Unix()
	if last.Ts > meta.LastTs {
		meta.LastTs = last.Ts
	}
	monAgentSave(id)
	return merged, addIn, addOut
}

func monAgentSync(id, pw, pp string) (int, uint64, uint64, error) {
	monAgentMu.Lock()
	if monSyncing[id] {
		monAgentMu.Unlock()
		return 0, 0, 0, fmt.Errorf("同步进行中")
	}
	monSyncing[id] = true
	monAgentMu.Unlock()
	defer func() {
		monAgentMu.Lock()
		delete(monSyncing, id)
		monAgentMu.Unlock()
	}()

	cl, done, err := monAgentClient(id, pw, pp)
	if err != nil {
		return 0, 0, 0, err
	}
	defer done()
	meta := monAgentGet(id)
	cmd := fmt.Sprintf("awk -v t=%d '$1>t' ~/.mjmon/data-*.tsv 2>/dev/null", meta.BaseTs)
	raw, err := monExec(cl, cmd, 60*time.Second)
	if err != nil {
		logf("servermon", "error", "服务器采集同步失败 %s：%v", id, err)
		return 0, 0, 0, err
	}
	n, ai, ao := monAgentIngest(id, raw)
	if n > 0 || ai > 0 || ao > 0 {
		logf("servermon", "info", "服务器采集已同步 %s：补入历史 %d 点，补记流量 ↓%s ↑%s", id, n, monFmtBytes(ai), monFmtBytes(ao))
	}
	return n, ai, ao, nil
}

// ---------- HTTP ----------

func monAgentHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		Action     string `json:"action"`
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, map[string]any{"error": "请求不完整"})
		return
	}
	c, _ := monFindConn(body.ID)
	switch body.Action {
	case "install", "uninstall":
		cl, done, err := monAgentClient(body.ID, body.Password, body.Passphrase)
		if err != nil {
			writeJSON(w, map[string]any{"error": "连接失败：" + err.Error(), "needCreds": true})
			return
		}
		defer done()
		if body.Action == "install" {
			method, err := monAgentInstall(cl)
			if err != nil {
				logf("servermon", "error", "服务器采集安装未完成 %s：%v", c.Name, err)
				writeJSON(w, map[string]any{"error": err.Error()})
				return
			}
			m := monAgentGet(body.ID)
			monAgentMu.Lock()
			m.Installed, m.Method = true, method
			monAgentMu.Unlock()
			monAgentSave(body.ID)
			logf("servermon", "info", "服务器采集已安装 %s（%s）：每分钟一条，存服务器 ~/.mjmon", c.Name, monAgentMethodLabel(method))
			writeJSON(w, map[string]any{"ok": true, "method": method})
			return
		}
		if err := monAgentUninstall(cl); err != nil {
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		m := monAgentGet(body.ID)
		monAgentMu.Lock()
		m.Installed, m.Method, m.BaseTs, m.BaseRx, m.BaseTx = false, "", 0, 0, 0
		monAgentMu.Unlock()
		monAgentSave(body.ID)
		logf("servermon", "info", "服务器采集已卸载 %s（服务器 ~/.mjmon 已删除）", c.Name)
		writeJSON(w, map[string]any{"ok": true})
	case "sync":
		n, ai, ao, err := monAgentSync(body.ID, body.Password, body.Passphrase)
		if err != nil {
			writeJSON(w, map[string]any{"error": err.Error(), "needCreds": true})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "points": n, "addIn": ai, "addOut": ao})
	default:
		writeJSON(w, map[string]any{"error": "未知操作"})
	}
}
