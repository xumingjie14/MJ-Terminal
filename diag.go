package main

// ---------- diagnostics hub + built-in console backend (v0.22) ----------
//
// One logging hub for the whole daemon. Every module reports its key
// events and errors through logf(); the entries live in a bounded
// in-memory ring buffer AND in Data/logs/mj.log (rotated), and the
// desktop's built-in 「控制台」 app reads them back over /api/diag/*.
// The point: when something breaks on the user's PC, the answer is
// in one place -- and one click exports a diagnostics bundle
// (mj-diag-*.zip) the user can hand over for debugging.
//
// New code MUST log through here (see README「控制台与日志约定」).
// Red line: never log passwords, private key material, update
// tokens, or full share/subscription links -- log a node's name /
// type / server:port, an SSH target host, a subscription's host,
// never the credential itself.
//
// API error capture: diagMiddleware wraps the mux and records any
// non-2xx answer on /api/ or /ws/ paths (method, path, status, a
// short body snippet). Handlers that report failures as HTTP 200
// {"error": ...} JSON (the house style) log those failures
// themselves at the point of failure.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	diagBufMax      = 2000            // in-memory ring buffer size
	diagFileMax     = 2 * 1024 * 1024 // rotate mj.log past 2 MB
	diagBackups     = 2               // keep mj.log.1 / mj.log.2
	diagBackfillMax = 1000            // history lines reloaded into the buffer at boot (v0.30)
)

type diagEntry struct {
	Seq    int64  `json:"seq"`
	Time   string `json:"time"` // 2006-01-02 15:04:05 local time
	Level  string `json:"level"`
	Module string `json:"module"`
	Msg    string `json:"msg"`
}

var (
	diagMu       sync.Mutex
	diagBuf      []diagEntry
	diagSeq      int64
	diagFile     *os.File
	diagFileLen  int64
	diagOnce     sync.Once
	diagBootOnce sync.Once
	// v0.48: while the vault is sealed (or sealing), the on-disk
	// log inside Data must NOT be opened or created -- logging is
	// memory-only until unlock. vaultDetect sets this before the
	// first logf can run; vault unlock clears it and logf lazily
	// reopens the (restored) file.
	diagHoldFile    atomic.Bool
	diagPendingNote atomic.Int64 // backfilled history count, announced by the next logf

	daemonStart = time.Now()
	daemonPort  int // set by runServer once the listener is up
)

func diagLogsDir() string {
	d := filepath.Join(dataDir(), "logs")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func diagLogPath() string { return filepath.Join(diagLogsDir(), "mj.log") }

// diagInitFile opens (once) the on-disk log for appending. Disk
// logging is best effort: if the file cannot be opened the hub
// still keeps the in-memory buffer, so logging can never take the
// daemon down.
func diagInitFile() {
	diagOnce.Do(func() {
		if diagHoldFile.Load() {
			return // vault sealed: do not even create Data/logs
		}
		diagReopenFileLocked()
	})
}

// diagReopenFileLocked (re)opens mj.log for appending and adopts
// its current length. Used at boot and lazily by logf after a
// vault unlock (the file was restored by then). Not internally
// locked: boot calls it before concurrency matters, logf calls
// it while holding diagMu.
func diagReopenFileLocked() {
	p := diagLogPath()
	if st, err := os.Stat(p); err == nil {
		diagFileLen = st.Size()
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		diagFile = f
	}
}

// diagCloseFileForVault closes the on-disk log before Data is
// sealed (vault quiesce sets diagHoldFile first, so logf will
// not reopen it until the vault unlocks again).
func diagCloseFileForVault() {
	diagMu.Lock()
	if diagFile != nil {
		_ = diagFile.Close()
		diagFile = nil
	}
	diagMu.Unlock()
}

// diagBoot runs once, on the first logf of the process: open the
// on-disk log, then backfill history into the ring buffer. Doing it
// inside the first logf guarantees the history is in place before
// the hub serves anything, with no separate init ordering to break.
func diagBoot() {
	diagBootOnce.Do(func() {
		diagInitFile()
		diagBackfill()
	})
}

// diagLineRe parses one mj.log line back into an entry. The writer
// format is "%s [%-5s] [%s] %s" (time, upper level padded to 5,
// module, message) -- see logf.
var diagLineRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \[(INFO|WARN|ERROR) ?\] \[([^\]]+)\] (.*)$`)

func parseDiagLine(line string) (diagEntry, bool) {
	m := diagLineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if m == nil {
		return diagEntry{}, false
	}
	return diagEntry{
		Time:   m[1],
		Level:  strings.ToLower(m[2]),
		Module: m[3],
		Msg:    m[4],
	}, true
}

// readLogTail returns the last n lines of a log file. The files are
// capped at 2 MB by rotation, so reading one whole is bounded.
func readLogTail(path string, n int) []string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// diagBackfill (v0.30) reloads the tail of the on-disk log into the
// ring buffer at boot, so the console page shows the previous
// session's records instead of starting blank after a restart --
// the records were never lost (they live in Data/logs/mj.log), the
// page just never read them back. Up to diagBackfillMax entries,
// taken from mj.log first and topped up from mj.log.1's tail.
// Backfilled entries take seq 1..N in file order, so the live
// entries of the new session continue right after them and the
// frontend's after-cursor semantics are unchanged. Runs inside
// diagBootOnce, before any new entry exists; it must NOT call logf
// (that would re-enter the once) -- it leaves the count in
// diagPendingNote and the next logf announces it.
func diagBackfill() {
	p := diagLogPath()
	lines := readLogTail(p, diagBackfillMax)
	if len(lines) < diagBackfillMax {
		prev := readLogTail(p+".1", diagBackfillMax-len(lines))
		lines = append(prev, lines...)
	}
	entries := make([]diagEntry, 0, len(lines))
	for _, ln := range lines {
		if e, ok := parseDiagLine(ln); ok {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return
	}
	diagMu.Lock()
	for i := range entries {
		diagSeq++
		entries[i].Seq = diagSeq
	}
	diagBuf = append(diagBuf, entries...)
	if len(diagBuf) > diagBufMax {
		diagBuf = append([]diagEntry{}, diagBuf[len(diagBuf)-diagBufMax:]...)
	}
	diagMu.Unlock()
	diagPendingNote.Store(int64(len(entries)))
}

func diagRotateLocked() {
	if diagFile != nil {
		_ = diagFile.Close()
		diagFile = nil
	}
	p := diagLogPath()
	for i := diagBackups; i >= 1; i-- {
		src := p
		if i > 1 {
			src = fmt.Sprintf("%s.%d", p, i-1)
		}
		_ = os.Rename(src, fmt.Sprintf("%s.%d", p, i))
	}
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		diagFile = f
	}
	diagFileLen = 0
}

// logf is THE logging entry point for the whole program:
// logf("core", "error", "sing-box 启动失败：%v", err). Levels are
// "info", "warn", "error". It never blocks on anything slow and
// never fails the caller.
func logf(module, level, format string, args ...any) {
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	if level != "warn" && level != "error" {
		level = "info"
	}
	now := time.Now()
	e := diagEntry{Time: now.Format("2006-01-02 15:04:05"), Level: level, Module: module, Msg: msg}
	diagBoot()
	diagMu.Lock()
	diagSeq++
	e.Seq = diagSeq
	diagBuf = append(diagBuf, e)
	if len(diagBuf) > diagBufMax {
		diagBuf = append([]diagEntry{}, diagBuf[len(diagBuf)-diagBufMax:]...)
	}
	if diagFile == nil && !diagHoldFile.Load() {
		// After a vault unlock the restored mj.log is picked up
		// again here (while sealed, diagHoldFile keeps us out).
		diagReopenFileLocked()
	}
	if diagFile != nil {
		line := fmt.Sprintf("%s [%-5s] [%s] %s\n", e.Time, strings.ToUpper(level), module, msg)
		if _, err := diagFile.WriteString(line); err == nil {
			diagFileLen += int64(len(line))
			if diagFileLen > diagFileMax {
				diagRotateLocked()
			}
		}
	}
	diagMu.Unlock()
	// Mirror to stdout: invisible in the hidden Windows daemon, but
	// handy in dev / -no-open runs.
	fmt.Printf("[%s] %s\n", module, msg)
	// Announce a boot-time history backfill exactly once, right
	// after the session's first real entry (diagBackfill itself
	// cannot log -- it runs inside the boot once).
	if n := diagPendingNote.Swap(0); n > 0 {
		logf("diag", "info", "已载入历史日志 %d 条（来自 Data/logs/mj.log）", n)
	}
}

func diagCount() int {
	diagMu.Lock()
	defer diagMu.Unlock()
	return len(diagBuf)
}

// diagSnapshot returns buffered entries with Seq > after, optionally
// filtered by level (exact) and module (exact), plus the latest seq.
func diagSnapshot(after int64, level, module string) ([]diagEntry, int64) {
	diagMu.Lock()
	defer diagMu.Unlock()
	out := []diagEntry{}
	for _, e := range diagBuf {
		if e.Seq <= after {
			continue
		}
		if level != "" && e.Level != level {
			continue
		}
		if module != "" && e.Module != module {
			continue
		}
		out = append(out, e)
	}
	return out, diagSeq
}

func fmtUptime(d time.Duration) string {
	s := int64(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%d 秒", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%d 分 %d 秒", s/60, s%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", s/3600, (s%3600)/60)
}

// diagStatusMap is the single source for /api/diag/status and the
// status.json inside the export bundle. Every field is read from
// live program state; nothing is estimated or invented.
func diagStatusMap() map[string]any {
	coreMu.Lock()
	coreUp := coreRunningLocked()
	coreMu.Unlock()
	extMu.Lock()
	extN := 0
	for _, e := range extWins {
		if !e.Shell {
			extN++
		}
	}
	extMu.Unlock()
	sshLiveMu.Lock()
	live := len(sshLive)
	sshLiveMu.Unlock()
	nodeN := len(nodes) // plain global, read the same way the other handlers do
	up := time.Since(daemonStart)
	return map[string]any{
		"version":        AppVersion,
		"os":             runtime.GOOS,
		"arch":           runtime.GOARCH,
		"port":           daemonPort,
		"uptimeSec":      int64(up.Seconds()),
		"uptimeText":     fmtUptime(up),
		"coreRunning":    coreUp,
		"nodeConnected":  coreUp && nodeN > 0,
		"nodeCount":      nodeN,
		"accelEnabled":   accelEnabled(),
		"openclaw":       map[string]any{"installed": ocInstalled(), "running": ocRunning()},
		"extWindows":     extN,
		"sshConnections": len(sshLoadConns()),
		"sshLive":        live,
		"dataDir":        dataDir(),
		"logCount":       diagCount(),
	}
}

func diagStatusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, diagStatusMap())
}

func diagLogsHandler(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	entries, latest := diagSnapshot(after, r.URL.Query().Get("level"), r.URL.Query().Get("module"))
	writeJSON(w, map[string]any{"entries": entries, "latest": latest, "count": diagCount()})
}

// diagExportHandler serves the one-click diagnostics bundle:
// mj.log (the hub's own log), status.json, and the sing-box core's
// own log when it exists. Filename carries a timestamp.
func diagExportHandler(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		fw, err := zw.Create(name)
		if err == nil {
			_, _ = fw.Write(data)
		}
	}
	if b, err := json.MarshalIndent(diagStatusMap(), "", "  "); err == nil {
		add("status.json", b)
	}
	if b, err := os.ReadFile(diagLogPath()); err == nil {
		add("mj.log", b)
	} else {
		add("mj.log", []byte("(暂无日志文件)"))
	}
	if b, err := os.ReadFile(coreLogPath()); err == nil {
		add("sing-box.log", b)
	}
	_ = zw.Close()
	ts := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"mj-diag-%s.zip\"", ts))
	_, _ = w.Write(buf.Bytes())
	logf("system", "info", "诊断包已导出（%d 字节）", buf.Len())
}

// ---------- API error capture middleware ----------

// diagRespWriter records the status code (and a snippet of an error
// body) while staying transparent to the wrapped handler -- in
// particular it forwards http.Hijacker, which the SSH WebSocket
// upgrade depends on.
type diagRespWriter struct {
	http.ResponseWriter
	status  int
	wrote   bool
	snippet bytes.Buffer
}

func (w *diagRespWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *diagRespWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.status >= 400 && w.snippet.Len() < 512 {
		n := 512 - w.snippet.Len()
		if len(b) < n {
			n = len(b)
		}
		w.snippet.Write(b[:n])
	}
	return w.ResponseWriter.Write(b)
}

func (w *diagRespWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking not supported")
	}
	return h.Hijack()
}

func (w *diagRespWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		if !w.wrote {
			w.WriteHeader(http.StatusOK)
		}
		f.Flush()
	}
}

func (w *diagRespWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// diagMiddleware logs every non-2xx answer on /api/ and /ws/ paths
// into the hub as module "api". The diagnostics endpoints themselves
// are excluded so a failing console cannot flood its own log.
func diagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !(strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/")) || strings.HasPrefix(p, "/api/diag/") {
			next.ServeHTTP(w, r)
			return
		}
		rw := &diagRespWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if rw.status >= 400 {
			snip := strings.TrimSpace(strings.Join(strings.Fields(rw.snippet.String()), " "))
			if len(snip) > 200 {
				snip = snip[:200]
			}
			logf("api", "error", "%s %s -> %d %s", r.Method, p, rw.status, snip)
		}
	})
}
