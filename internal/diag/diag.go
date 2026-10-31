package diag

// ---------- diagnostics hub + built-in console backend (v0.22) ----------
//
// One logging hub for the whole daemon. Every module reports its key
// events and errors through Logf(); the entries live in a bounded
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
// API error capture: Middleware wraps the mux and records any
// non-2xx answer on /api/ or /ws/ paths (method, path, status, a
// short body snippet). Handlers that report failures as HTTP 200
// {"error": ...} JSON (the house style) log those failures
// themselves at the point of failure.
//
// v0.60 architecture block 4: this file lives in its own package
// (internal/diag) now. The seams back into the daemon are the
// wiring hooks below (DataDirFunc / CoreLogPathFunc /
// StatusProvider), assigned by package main's bridge; package
// main keeps a logf forwarder so existing call sites are
// untouched.

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
	bufMax      = 2000            // in-memory ring buffer size
	fileMax     = 2 * 1024 * 1024 // rotate mj.log past 2 MB
	backups     = 2               // keep mj.log.1 / mj.log.2
	backfillMax = 1000            // history lines reloaded into the buffer at boot (v0.30)
)

type entry struct {
	Seq    int64  `json:"seq"`
	Time   string `json:"time"` // 2006-01-02 15:04:05 local time
	Level  string `json:"level"`
	Module string `json:"module"`
	Msg    string `json:"msg"`
}

var (
	mu       sync.Mutex
	buf      []entry
	seq      int64
	file     *os.File
	fileLen  int64
	initOnce sync.Once
	bootOnce sync.Once
	// v0.48: while the vault is sealed (or sealing), the on-disk
	// log inside Data must NOT be opened or created -- logging is
	// memory-only until unlock. vaultDetect sets this before the
	// first Logf can run; vault unlock clears it and Logf lazily
	// reopens the (restored) file.
	HoldFile    atomic.Bool
	pendingNote atomic.Int64 // backfilled history count, announced by the next Logf

	startTime = time.Now()
	Port      int // set by runServer once the listener is up
)

// ---------- wiring hooks (v0.60 architecture block 4) ----------
//
// This package was package-main code until v0.60. The few things
// it needs from the rest of the daemon arrive through these
// hooks, assigned once by package main's bridge (bridge.go)
// before the daemon starts serving; the hub owns no domain
// state of its own.

// DataDirFunc resolves the daemon's Data directory (the log
// file lives under it). Assigned by package main.
var DataDirFunc func() string

// CoreLogPathFunc locates the proxy core's own log file for
// the diagnostics bundle. Assigned by package main.
var CoreLogPathFunc func() string

// StatusProvider contributes the domain-owned fields of the
// status map (version, core/node state, accel, OpenClaw,
// external windows, SSH). Assigned by package main.
var StatusProvider func() map[string]any

func dataDir() string {
	if DataDirFunc != nil {
		return DataDirFunc()
	}
	return "Data"
}

func coreLogPath() string {
	if CoreLogPathFunc != nil {
		return CoreLogPathFunc()
	}
	return ""
}

// WriteJSON is the daemon's one JSON response writer. The
// canonical implementation lives here since v0.60; package
// main's writeJSON forwards to it, so every response on every
// route keeps the exact same bytes as before.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func LogsDir() string {
	d := filepath.Join(dataDir(), "logs")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func LogPath() string { return filepath.Join(LogsDir(), "mj.log") }

// initFile opens (once) the on-disk log for appending. Disk
// logging is best effort: if the file cannot be opened the hub
// still keeps the in-memory buffer, so logging can never take the
// daemon down.
func initFile() {
	initOnce.Do(func() {
		if HoldFile.Load() {
			return // vault sealed: do not even create Data/logs
		}
		reopenFileLocked()
	})
}

// reopenFileLocked (re)opens mj.log for appending and adopts
// its current length. Used at boot and lazily by Logf after a
// vault unlock (the file was restored by then). Not internally
// locked: boot calls it before concurrency matters, Logf calls
// it while holding mu.
func reopenFileLocked() {
	p := LogPath()
	if st, err := os.Stat(p); err == nil {
		fileLen = st.Size()
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		file = f
	}
}

// CloseFileForVault closes the on-disk log before Data is
// sealed (vault quiesce sets HoldFile first, so Logf will
// not reopen it until the vault unlocks again).
func CloseFileForVault() {
	mu.Lock()
	if file != nil {
		_ = file.Close()
		file = nil
	}
	mu.Unlock()
}

// boot runs once, on the first Logf of the process: open the
// on-disk log, then backfill history into the ring buffer. Doing it
// inside the first Logf guarantees the history is in place before
// the hub serves anything, with no separate init ordering to break.
func boot() {
	bootOnce.Do(func() {
		initFile()
		backfill()
	})
}

// lineRe parses one mj.log line back into an entry. The writer
// format is "%s [%-5s] [%s] %s" (time, upper level padded to 5,
// module, message) -- see Logf.
var lineRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \[(INFO|WARN|ERROR) ?\] \[([^\]]+)\] (.*)$`)

func parseLine(line string) (entry, bool) {
	m := lineRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if m == nil {
		return entry{}, false
	}
	return entry{
		Time:   m[1],
		Level:  strings.ToLower(m[2]),
		Module: m[3],
		Msg:    m[4],
	}, true
}

// readTail returns the last n lines of a log file. The files are
// capped at 2 MB by rotation, so reading one whole is bounded.
func readTail(path string, n int) []string {
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

// backfill (v0.30) reloads the tail of the on-disk log into the
// ring buffer at boot, so the console page shows the previous
// session's records instead of starting blank after a restart --
// the records were never lost (they live in Data/logs/mj.log), the
// page just never read them back. Up to backfillMax entries,
// taken from mj.log first and topped up from mj.log.1's tail.
// Backfilled entries take seq 1..N in file order, so the live
// entries of the new session continue right after them and the
// frontend's after-cursor semantics are unchanged. Runs inside
// bootOnce, before any new entry exists; it must NOT call Logf
// (that would re-enter the once) -- it leaves the count in
// pendingNote and the next Logf announces it.
func backfill() {
	p := LogPath()
	lines := readTail(p, backfillMax)
	if len(lines) < backfillMax {
		prev := readTail(p+".1", backfillMax-len(lines))
		lines = append(prev, lines...)
	}
	entries := make([]entry, 0, len(lines))
	for _, ln := range lines {
		if e, ok := parseLine(ln); ok {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return
	}
	mu.Lock()
	for i := range entries {
		seq++
		entries[i].Seq = seq
	}
	buf = append(buf, entries...)
	if len(buf) > bufMax {
		buf = append([]entry{}, buf[len(buf)-bufMax:]...)
	}
	mu.Unlock()
	pendingNote.Store(int64(len(entries)))
}

func rotateLocked() {
	if file != nil {
		_ = file.Close()
		file = nil
	}
	p := LogPath()
	for i := backups; i >= 1; i-- {
		src := p
		if i > 1 {
			src = fmt.Sprintf("%s.%d", p, i-1)
		}
		_ = os.Rename(src, fmt.Sprintf("%s.%d", p, i))
	}
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		file = f
	}
	fileLen = 0
}

// Logf is THE logging entry point for the whole program:
// Logf("core", "error", "sing-box 启动失败：%v", err). Levels are
// "info", "warn", "error". It never blocks on anything slow and
// never fails the caller.
func Logf(module, level, format string, args ...any) {
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	if level != "warn" && level != "error" {
		level = "info"
	}
	now := time.Now()
	e := entry{Time: now.Format("2006-01-02 15:04:05"), Level: level, Module: module, Msg: msg}
	boot()
	mu.Lock()
	seq++
	e.Seq = seq
	buf = append(buf, e)
	if len(buf) > bufMax {
		buf = append([]entry{}, buf[len(buf)-bufMax:]...)
	}
	if file == nil && !HoldFile.Load() {
		// After a vault unlock the restored mj.log is picked up
		// again here (while sealed, HoldFile keeps us out).
		reopenFileLocked()
	}
	if file != nil {
		line := fmt.Sprintf("%s [%-5s] [%s] %s\n", e.Time, strings.ToUpper(level), module, msg)
		if _, err := file.WriteString(line); err == nil {
			fileLen += int64(len(line))
			if fileLen > fileMax {
				rotateLocked()
			}
		}
	}
	mu.Unlock()
	// Mirror to stdout: invisible in the hidden Windows daemon, but
	// handy in dev / -no-open runs.
	fmt.Printf("[%s] %s\n", module, msg)
	// Announce a boot-time history backfill exactly once, right
	// after the session's first real entry (backfill itself
	// cannot log -- it runs inside the boot once).
	if n := pendingNote.Swap(0); n > 0 {
		Logf("diag", "info", "已载入历史日志 %d 条（来自 Data/logs/mj.log）", n)
	}
}

func count() int {
	mu.Lock()
	defer mu.Unlock()
	return len(buf)
}

// snapshot returns buffered entries with Seq > after, optionally
// filtered by level (exact) and module (exact), plus the latest seq.
func snapshot(after int64, level, module string) ([]entry, int64) {
	mu.Lock()
	defer mu.Unlock()
	out := []entry{}
	for _, e := range buf {
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
	return out, seq
}

func FmtUptime(d time.Duration) string {
	s := int64(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%d 秒", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%d 分 %d 秒", s/60, s%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", s/3600, (s%3600)/60)
}

// StatusMap is the single source for /api/diag/status and the
// status.json inside the export bundle. Every field is read from
// live program state; nothing is estimated or invented. The
// hub-owned fields are built here; the domain-owned fields
// (version, core/node state, accel, OpenClaw, external windows,
// SSH) come from StatusProvider, assigned by package main's
// bridge -- this package owns no domain state. (JSON object
// keys marshal in sorted order, so the merged map serializes
// byte-identically to the pre-layering single map.)
func StatusMap() map[string]any {
	up := time.Since(startTime)
	m := map[string]any{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"port":       Port,
		"uptimeSec":  int64(up.Seconds()),
		"uptimeText": FmtUptime(up),
		"dataDir":    dataDir(),
		"logCount":   count(),
	}
	if StatusProvider != nil {
		for k, v := range StatusProvider() {
			m[k] = v
		}
	}
	return m
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, StatusMap())
}

func LogsHandler(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	entries, latest := snapshot(after, r.URL.Query().Get("level"), r.URL.Query().Get("module"))
	WriteJSON(w, map[string]any{"entries": entries, "latest": latest, "count": count()})
}

// ExportHandler serves the one-click diagnostics bundle:
// mj.log (the hub's own log), status.json, and the sing-box core's
// own log when it exists. Filename carries a timestamp.
func ExportHandler(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		fw, err := zw.Create(name)
		if err == nil {
			_, _ = fw.Write(data)
		}
	}
	if b, err := json.MarshalIndent(StatusMap(), "", "  "); err == nil {
		add("status.json", b)
	}
	if b, err := os.ReadFile(LogPath()); err == nil {
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
	Logf("system", "info", "诊断包已导出（%d 字节）", buf.Len())
}

// ---------- API error capture middleware ----------

// respWriter records the status code (and a snippet of an error
// body) while staying transparent to the wrapped handler -- in
// particular it forwards http.Hijacker, which the SSH WebSocket
// upgrade depends on.
type respWriter struct {
	http.ResponseWriter
	status  int
	wrote   bool
	snippet bytes.Buffer
}

func (w *respWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *respWriter) Write(b []byte) (int, error) {
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

func (w *respWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking not supported")
	}
	return h.Hijack()
}

func (w *respWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		if !w.wrote {
			w.WriteHeader(http.StatusOK)
		}
		f.Flush()
	}
}

func (w *respWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Middleware logs every non-2xx answer on /api/ and /ws/ paths
// into the hub as module "api". The diagnostics endpoints themselves
// are excluded so a failing console cannot flood its own log.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !(strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/")) || strings.HasPrefix(p, "/api/diag/") {
			next.ServeHTTP(w, r)
			return
		}
		rw := &respWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if rw.status >= 400 {
			snip := strings.TrimSpace(strings.Join(strings.Fields(rw.snippet.String()), " "))
			if len(snip) > 200 {
				snip = snip[:200]
			}
			Logf("api", "error", "%s %s -> %d %s", r.Method, p, rw.status, snip)
		}
	})
}
