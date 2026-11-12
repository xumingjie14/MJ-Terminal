package main

// core.go -- the sing-box core lifecycle, extracted from main.go
// in the v0.70 split (file-level move within package main: no
// behaviour change, no call-site change). It owns everything
// that exists for the core's sake: the embedded sing-box
// payload and its extraction, the local proxy port, the core
// process state (guarded by coreMu; the *Locked functions
// require the caller to hold it, exactly as before), starting,
// stopping and status, the config-file selection, and the
// core's log tail. Callers in nodes.go (regen/restart), ssh.go,
// vault.go, taskmgr.go and friends reach the same package-level
// surface they always did.

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// sing-box (official 1.14.4, legacy Windows 7 amd64 build) is embedded
// COMPRESSED (~26MB zip for an 80MB exe) so the whole system ships as
// ONE single MJ-System.exe file. On first run the daemon extracts it
// to core/sing-box.exe next to the EXE (see extractEmbeddedCore).
//
//go:embed singbox.zip
var singboxZip []byte

const proxyPort = 2080

var (
	coreMu  sync.Mutex
	coreCmd *exec.Cmd
	// Atomics: written by the Wait() goroutine, read from handlers that
	// may already hold coreMu (Go mutexes are not re-entrant).
	coreExited  atomic.Bool  // set when sing-box really dies
	coreExitErr atomic.Value // string: why it exited ("" = clean/stopped)
	coreStopReq atomic.Bool  // user pressed stop, so the exit is expected
)

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
		logf("core", "error", "sing-box 配置校验没过：%s", strings.TrimSpace(string(checkOut)))
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
			logf("core", "error", "sing-box 异常退出：%v；日志末尾：%s", waitErr, coreLogTail())
		} else if waitErr == nil && !coreStopReq.Load() {
			logf("core", "warn", "sing-box 已退出（非用户手动停止）")
		}
		if !coreStopReq.Load() {
			// v0.47 hook (notify): the node dropped without the
			// user asking -- an event notification.
			body := "节点连接已断开（核心进程退出）"
			if waitErr != nil {
				body = "节点核心异常退出：" + waitErr.Error()
			}
			postNotify(notifySrcNode, "core-exit", "节点已断开", body, "core")
		}
	}()
	// 3) Wait briefly until the proxy port really accepts connections,
	//    or the process dies -- then report which one happened.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 300*time.Millisecond); err == nil {
			_ = conn.Close()
			logf("core", "info", "核心已启动，本地代理 127.0.0.1:%d 在监听", proxyPort)
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
		logf("core", "info", "核心进程已停止")
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
			writeJSON(w, coreStatusResponse{Running: false, Message: "未找到 core/sing-box.exe（其他功能不受影响）"})
			return
		}
		if coreRunningLocked() {
			writeJSON(w, coreStatusResponse{Running: true, Message: fmt.Sprintf("运行中（已确认进程活着），本地代理 127.0.0.1:%d", proxyPort)})
			return
		}
		if exitErr, _ := coreExitErr.Load().(string); coreCmd != nil && coreExited.Load() && exitErr != "" {
			writeJSON(w, coreStatusResponse{Running: false, Message: "sing-box 已异常退出：" + exitErr + "；日志末尾：" + coreLogTail()})
			return
		}
		writeJSON(w, coreStatusResponse{Running: false, Message: "已停止，sing-box 已就位"})
	case "start":
		if err := startCoreLocked(); err != nil {
			logf("core", "error", "核心启动失败：%v", err)
			// v0.47 hook (notify): node connect failure event.
			postNotify(notifySrcNode, "connect-fail", "节点连接失败", err.Error(), "core")
			writeJSON(w, coreStatusResponse{Running: false, Message: err.Error()})
			return
		}
		busPublish("nodes", "") // v0.59 block 3: core (dis)connect is a nodes change
		writeJSON(w, coreStatusResponse{Running: true, Message: fmt.Sprintf("已启动并确认 %d 端口在监听", proxyPort)})
	case "stop":
		stopCoreLocked()
		busPublish("nodes", "") // v0.59 block 3: live sync
		writeJSON(w, coreStatusResponse{Running: false, Message: "已停止"})
	default:
		apiErrBadRequest(w, errCodeBadRequest, "unknown action")
	}
}

func coreLogHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, coreLogResponse{Log: coreLogTail()})
}
